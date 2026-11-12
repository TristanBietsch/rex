package registry

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// ModelsCacheMaxAge is how long a detected model list is trusted even when
// the binary did not change: a version-manager shim (asdf, nvm) keeps its
// mtime while the tool behind it updates.
const ModelsCacheMaxAge = 24 * time.Hour

const modelsCacheVersion = 1

type modelsCacheFile struct {
	Version int                         `json:"version"`
	Tools   map[string]modelsCacheEntry `json:"tools"`
}

// modelsCacheEntry is one tool's detected ids, valid while the binary (path
// and mtime), the command and the age all still match.
type modelsCacheEntry struct {
	Binary    string    `json:"binary"`
	MTimeNs   int64     `json:"mtime_ns"`
	Cmd       string    `json:"cmd"`
	FetchedAt time.Time `json:"fetched_at"`
	IDs       []string  `json:"ids"`
}

// cacheKey describes the tool's binary and model command now; ok is false
// when the tool cannot be asked (no models_cmd, binary missing).
func cacheKey(t Tool) (binary string, mtimeNs int64, cmd string, ok bool) {
	if t.ModelsCmd == nil || len(t.Command) == 0 {
		return "", 0, "", false
	}
	path, err := exec.LookPath(t.Command[0])
	if err != nil {
		return "", 0, "", false
	}
	fi, err := os.Stat(path)
	if err != nil {
		return "", 0, "", false
	}
	cmd = strings.Join(append(append([]string{}, t.Command...), t.ModelsCmd.Args...), "\x00")
	return path, fi.ModTime().UnixNano(), cmd, true
}

func readModelsCache(path string) modelsCacheFile {
	empty := modelsCacheFile{Version: modelsCacheVersion, Tools: map[string]modelsCacheEntry{}}
	if path == "" {
		return empty
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return empty
	}
	var f modelsCacheFile
	if err := json.Unmarshal(raw, &f); err != nil || f.Version != modelsCacheVersion || f.Tools == nil {
		slog.Debug("models: cache unreadable, ignoring", "path", path)
		return empty
	}
	return f
}

// writeModelsCache replaces the cache file atomically, owner-only.
func writeModelsCache(path string, f modelsCacheFile) error {
	raw, err := json.Marshal(f)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".models-cache-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) //nolint:errcheck // gone after the rename; best effort otherwise
	if err := tmp.Chmod(0o600); err != nil && !isWindowsChmod(err) {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// isWindowsChmod ignores a chmod that the platform does not support.
func isWindowsChmod(error) bool { return filepath.Separator == '\\' }

// entryValid reports whether e still describes tool t at time now.
func entryValid(e modelsCacheEntry, t Tool, now time.Time, allowStale bool) bool {
	bin, mtime, cmd, ok := cacheKey(t)
	if !ok || e.Binary != bin || e.MTimeNs != mtime || e.Cmd != cmd || len(e.IDs) == 0 {
		return false
	}
	if allowStale {
		return true
	}
	age := now.Sub(e.FetchedAt)
	return age >= 0 && age < ModelsCacheMaxAge
}

// entryModels turns cached ids back into models, re-validating each id (the
// file is user-writable).
func entryModels(t Tool, e modelsCacheEntry) []Model {
	var ids []string
	for _, id := range e.IDs {
		if ValidModelID(id) {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	return modelsFromIDs(t, ids)
}

// CachedModels returns the detected models held in the cache file at path
// for tools whose binary, command and age (unless allowStale) still match.
// It starts no subprocess and writes nothing.
func CachedModels(tools []Tool, path string, now time.Time, allowStale bool) map[string][]Model {
	f := readModelsCache(path)
	out := map[string][]Model{}
	for _, t := range tools {
		e, ok := f.Tools[t.ID]
		if !ok || !entryValid(e, t, now, allowStale) {
			continue
		}
		if m := entryModels(t, e); len(m) > 0 {
			out[t.ID] = m
		}
	}
	return out
}

// RefreshModels detects the models of every tool in tools that has a
// models_cmd and a binary on PATH, and keeps the result in the cache file at
// path ("" = no cache). A cache entry that is still valid is used without
// running the command, unless force is set. A tool whose detection fails
// (timeout, auth error, garbage) logs one line and is left out, so the caller
// shows its static models. Tools run concurrently, each bounded by its own
// timeout.
func RefreshModels(ctx context.Context, tools []Tool, path string, force bool, now time.Time) map[string][]Model {
	cache := readModelsCache(path)
	var mu sync.Mutex
	var wg sync.WaitGroup
	out := map[string][]Model{}
	changed := false
	for _, t := range tools {
		if t.ModelsCmd == nil || !Installed(t) {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			mu.Lock()
			e, have := cache.Tools[t.ID]
			mu.Unlock()
			if !force && have && entryValid(e, t, now, false) {
				if m := entryModels(t, e); len(m) > 0 {
					mu.Lock()
					out[t.ID] = m
					mu.Unlock()
					return
				}
			}
			models, err := DetectModels(ctx, t)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				if ctx.Err() != nil {
					return // canceled or shutting down: not a failure, change nothing
				}
				// Keep the last good list while the binary and command are the
				// ones it was fetched for (served stale); drop it only when
				// they changed, since its ids then describe another tool.
				if have && entryValid(e, t, now, true) {
					slog.Warn("models: detection failed, keeping the last list", "tool", t.ID, "err", err)
					if m := entryModels(t, e); len(m) > 0 {
						out[t.ID] = m
					}
					return
				}
				slog.Warn("models: detection failed, using the static list", "tool", t.ID, "err", err)
				if have {
					delete(cache.Tools, t.ID)
					changed = true
				}
				return
			}
			bin, mtime, cmd, ok := cacheKey(t)
			if ok {
				ids := make([]string, len(models))
				for i, m := range models {
					ids[i] = m.ID
				}
				cache.Tools[t.ID] = modelsCacheEntry{Binary: bin, MTimeNs: mtime, Cmd: cmd, FetchedAt: now.UTC(), IDs: ids}
				changed = true
			}
			out[t.ID] = models
		}()
	}
	wg.Wait()
	if changed && path != "" {
		if err := writeModelsCache(path, cache); err != nil {
			slog.Warn("models: cache write failed", "path", path, "err", err)
		}
	}
	return out
}
