package loop

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// File modes: loop state holds the folder, the prompt and check output.
const (
	dirPerm  os.FileMode = 0o700
	filePerm os.FileMode = 0o600
)

// stateVersion is the persisted format's version.
const stateVersion = 1

// retainFinished is how long a finished loop's state and log are kept.
const retainFinished = 7 * 24 * time.Hour

// State is a loop as persisted in <state-dir>/loops/<id>.json. It holds the
// whole definition, so a loop goes on with the file as it was at start even
// if the file is edited or deleted meanwhile.
type State struct {
	Version     int                `json:"version"`
	ID          string             `json:"id"`
	Def         Def                `json:"def"`
	SessionID   string             `json:"session_id,omitempty"`
	State       protocol.LoopState `json:"state"`
	PauseReason string             `json:"pause_reason,omitempty"`
	StopReason  string             `json:"stop_reason,omitempty"`
	Notice      string             `json:"notice,omitempty"`
	Iteration   int                `json:"iteration"`
	// ElapsedNS is the running time so far (pauses and downtime excluded),
	// which max_minutes counts.
	ElapsedNS     int64     `json:"elapsed_ns,omitempty"`
	LastCheckExit *int      `json:"last_check_exit,omitempty"`
	StartedAt     time.Time `json:"started_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// Summary is the wire form of s.
func (s State) Summary() protocol.LoopSummary {
	return protocol.LoopSummary{
		ID: s.ID, Name: s.Def.Name, Kind: s.Def.Kind, State: s.State,
		PauseReason: s.PauseReason, StopReason: s.StopReason,
		SessionID: s.SessionID, CWD: s.Def.CWD,
		Iteration: s.Iteration, MaxIterations: s.Def.Until.MaxIterations,
		LastCheckExit: s.LastCheckExit,
		StartedAt:     s.StartedAt, UpdatedAt: s.UpdatedAt, Notice: s.Notice,
	}
}

func statePath(dir, id string) string { return filepath.Join(dir, id+".json") }

// LogPath is where loop id's log lives under the loop state dir.
func LogPath(dir, id string) string { return filepath.Join(dir, id+".log") }

// writeState persists s atomically.
func writeState(dir string, s State) error {
	if !protocol.ValidLoopID(s.ID) {
		return fmt.Errorf("invalid loop id %q", s.ID)
	}
	if err := os.MkdirAll(dir, dirPerm); err != nil {
		return fmt.Errorf("loop state dir: %w", err)
	}
	s.Version = stateVersion
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal loop state: %w", err)
	}
	tmp, err := os.CreateTemp(dir, "loop-*.tmp")
	if err != nil {
		return fmt.Errorf("create loop state: %w", err)
	}
	name := tmp.Name()
	_, werr := tmp.Write(b)
	cerr := tmp.Close()
	if err := errors.Join(werr, cerr, os.Chmod(name, filePerm)); err != nil {
		_ = os.Remove(name)
		return fmt.Errorf("write loop state: %w", err)
	}
	if err := os.Rename(name, statePath(dir, s.ID)); err != nil {
		_ = os.Remove(name)
		return fmt.Errorf("rename loop state: %w", err)
	}
	return nil
}

// removeState deletes a loop's state and log.
func removeState(dir, id string) {
	_ = os.Remove(statePath(dir, id))
	_ = os.Remove(LogPath(dir, id))
}

// loadStates reads every loop state in dir. Unreadable or invalid files are
// skipped with a warning. Finished loops older than retainFinished are
// deleted.
func loadStates(dir string, now time.Time) ([]State, error) {
	des, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read loop state dir: %w", err)
	}
	var out []State
	for _, de := range des {
		id, ok := strings.CutSuffix(de.Name(), ".json")
		if !ok || !protocol.ValidLoopID(id) || !de.Type().IsRegular() {
			continue
		}
		b, err := os.ReadFile(statePath(dir, id))
		if err != nil {
			slog.Warn("loop: skipping unreadable state", "id", id, "err", err)
			continue
		}
		var s State
		if err := json.Unmarshal(b, &s); err != nil || s.ID != id || s.Version != stateVersion {
			slog.Warn("loop: skipping invalid state", "id", id, "err", err)
			continue
		}
		if err := s.Def.Compile(); err != nil {
			slog.Warn("loop: skipping state with a bad definition", "id", id, "err", err)
			continue
		}
		if s.State.Terminal() && now.Sub(s.UpdatedAt) > retainFinished {
			removeState(dir, id)
			continue
		}
		out = append(out, s)
	}
	return out, nil
}
