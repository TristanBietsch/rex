package summarizer

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/daemon/state"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// recOllama is an httptest Ollama that records every request's system prompt
// and time, and answers with a fixed description.
type recOllama struct {
	srv *httptest.Server
	mu  sync.Mutex
	sys []string
	at  []time.Time
}

func newRecOllama(t *testing.T) *recOllama {
	t.Helper()
	r := &recOllama{}
	r.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		body, _ := io.ReadAll(req.Body)
		var cr struct {
			System string `json:"system"`
		}
		_ = json.Unmarshal(body, &cr)
		r.mu.Lock()
		r.sys = append(r.sys, cr.System)
		r.at = append(r.at, time.Now())
		r.mu.Unlock()
		_, _ = io.WriteString(w, `{"response":"Running tests"}`)
	}))
	t.Cleanup(r.srv.Close)
	return r
}

func (r *recOllama) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.sys)
}

func (r *recOllama) system(i int) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.sys[i]
}

func (r *recOllama) time(i int) time.Time {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.at[i]
}

// progressSys reports whether a recorded system prompt is the late-phase one.
func progressSys(s string) bool { return strings.Contains(s, "finished so far") }

func addLive(t *testing.T, st *state.Store, id string, startedAt time.Time) {
	t.Helper()
	require.NoError(t, st.Add(&state.Session{
		ID: id, ShortID: id[:4], ToolID: "codex", Slug: "task-" + id,
		State: protocol.StateWorking, StartedAt: startedAt,
	}))
}

func schedCfg(url string, c Cadence, on bool) Config {
	cfg := Defaults()
	cfg.BaseURL = url
	cfg.MinInterval = time.Millisecond // New maps 0 to its 800 ms default
	cfg.Cadence = c
	cfg.Schedule = on
	return cfg
}

func (w *Worker) pendingLen() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.pending)
}

func (w *Worker) schedLen() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.sched)
}

func (w *Worker) firedOf(id string) int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.sched[id].fired
}
