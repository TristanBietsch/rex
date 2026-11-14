package workspace

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// registryVersion is the "v" of workspaces.json.
const registryVersion = 1

// maxRegistryBytes caps workspaces.json; a larger file is refused unread.
const maxRegistryBytes = 4 << 20

// Entry is one workspace as the registry last saw it.
type Entry struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	Color    string    `json:"color"`
	Root     string    `json:"root"`
	LastSeen time.Time `json:"last_seen"`
}

type registryFile struct {
	V          int              `json:"v"`
	Workspaces map[string]Entry `json:"workspaces"`
}

// Registry is ~/.local/share/rex/workspaces.json: which folder each
// workspace id was last seen at. It lets a moved folder keep its id and lets
// Init avoid reusing an id or a color. Safe for concurrent use in a process;
// rex writes it rarely and last write wins across processes.
type Registry struct {
	path string
	mu   sync.Mutex
}

// OpenRegistry returns the registry at path. It touches nothing: the file
// and its directory are created by the first write.
func OpenRegistry(path string) (*Registry, error) {
	if path == "" || !filepath.IsAbs(path) {
		return nil, fmt.Errorf("workspace: registry path %q is not absolute", path)
	}
	return &Registry{path: path}, nil
}

// Path is the registry file.
func (r *Registry) Path() string { return r.path }

func (r *Registry) read() (registryFile, error) {
	rf := registryFile{V: registryVersion, Workspaces: map[string]Entry{}}
	fi, err := os.Lstat(r.path)
	if errors.Is(err, os.ErrNotExist) {
		return rf, nil
	}
	if err != nil {
		return rf, err
	}
	if !fi.Mode().IsRegular() {
		return rf, fmt.Errorf("%w: %s is not a regular file", ErrUnsafe, r.path)
	}
	if fi.Size() > maxRegistryBytes {
		return rf, fmt.Errorf("%w: %s is larger than %d bytes", ErrInvalid, r.path, maxRegistryBytes)
	}
	f, err := os.Open(r.path)
	if err != nil {
		return rf, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, maxRegistryBytes+1))
	if err != nil {
		return rf, err
	}
	if len(b) > maxRegistryBytes {
		return rf, fmt.Errorf("%w: %s is larger than %d bytes", ErrInvalid, r.path, maxRegistryBytes)
	}
	var got registryFile
	if err := json.Unmarshal(b, &got); err != nil {
		return rf, fmt.Errorf("%w: %s: %w", ErrInvalid, r.path, err)
	}
	for id, e := range got.Workspaces {
		// Keep only well-formed rows whose key is their id.
		if !ValidID(id) || e.ID != id || e.Root == "" {
			continue
		}
		e.Name = CleanName(e.Name)
		if !ValidColor(e.Color) {
			e.Color = ""
		}
		rf.Workspaces[id] = e
	}
	return rf, nil
}

func (r *Registry) write(rf registryFile) error {
	rf.V = registryVersion
	b, err := json.MarshalIndent(rf, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(r.path), 0o700); err != nil {
		return err
	}
	return writeFileAtomic(r.path, append(b, '\n'))
}

// Seen records w under its id: the root it is at now, its name and color,
// and the time. A workspace seen at a new path moves its root. An id already
// registered for another live folder is overwritten (the newest sighting
// wins).
func (r *Registry) Seen(w *Workspace) error {
	if !ValidID(w.ID) || w.Root == "" {
		return fmt.Errorf("workspace: cannot register id %q root %q", w.ID, w.Root)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	rf, err := r.read()
	if err != nil {
		return err
	}
	rf.Workspaces[w.ID] = Entry{ID: w.ID, Name: w.Name, Color: w.Color, Root: w.Root, LastSeen: time.Now().UTC()}
	return r.write(rf)
}

// Taken reports whether id is registered. An unreadable registry reports
// false: the id space is 32 bits and Init also checks the folder.
func (r *Registry) Taken(id string) bool {
	_, ok := r.Get(id)
	return ok
}

// Get returns the entry for id.
func (r *Registry) Get(id string) (Entry, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	rf, err := r.read()
	if err != nil {
		return Entry{}, false
	}
	e, ok := rf.Workspaces[id]
	return e, ok
}

// List returns every entry, by name then id.
func (r *Registry) List() ([]Entry, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	rf, err := r.read()
	if err != nil {
		return nil, err
	}
	out := make([]Entry, 0, len(rf.Workspaces))
	for _, e := range rf.Workspaces {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

// usedColors lists the colors registered workspaces hold.
func (r *Registry) usedColors() []string {
	es, err := r.List()
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(es))
	for _, e := range es {
		out = append(out, e.Color)
	}
	return out
}

// maxIDTries is how often Init draws a new id while the draw is taken.
const maxIDTries = 5

// genID draws 8 lowercase hex digits from rnd, retrying while taken.
func genID(rnd io.Reader, taken func(string) bool) (string, error) {
	for range maxIDTries {
		var b [4]byte
		if _, err := io.ReadFull(rnd, b[:]); err != nil {
			return "", err
		}
		id := hex.EncodeToString(b[:])
		if taken == nil || !taken(id) {
			return id, nil
		}
	}
	return "", fmt.Errorf("workspace: no free id after %d tries", maxIDTries)
}

// newID is genID on crypto/rand.
func newID(taken func(string) bool) (string, error) { return genID(rand.Reader, taken) }
