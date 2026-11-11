package pty

import (
	"context"
	"regexp"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/daemon/state"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

const (
	resumeA = "7d4f3c8a-1234-4abc-89ab-cdefabcdef01"
	resumeB = "11111111-2222-4333-8444-555555555555"
)

// idAdapter reports a resume id that the test changes.
type idAdapter struct {
	stubAdapter
	id atomic.Value // string
}

func (a *idAdapter) ResumeID() string {
	v, _ := a.id.Load().(string)
	return v
}

func newResumeSession(t *testing.T, id string) (*state.Store, *state.Session) {
	t.Helper()
	store := state.NewStore()
	sess := &state.Session{ID: id, ShortID: id, ToolID: "claude", Slug: id,
		State: protocol.StateQueued, StartedAt: time.Now().UTC()}
	require.NoError(t, store.Add(sess))
	return store, sess
}

// An adapter's id is stored, persisted to meta.json, and only then broadcast:
// a client that reads meta.json on the patch sees it. Empty is no report, an
// unchanged id sends nothing, and a new one (/clear) replaces the old.
func TestSupervisor_ReportsResumeID(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the child reads its exit signal from a Unix tty")
	}
	stateDir := t.TempDir()
	store, sess := newResumeSession(t, "rid1")
	ad := &idAdapter{}

	var (
		mu       sync.Mutex
		seen     []string
		metaSeen []string
	)
	release := make(chan struct{})
	var once sync.Once
	store.Subscribe(func(e state.Event) {
		v, ok := e.Patch["resume_id"].(string)
		if !ok {
			return
		}
		m, err := state.LoadMeta(stateDir, "rid1")
		mu.Lock()
		seen = append(seen, v)
		if err == nil {
			metaSeen = append(metaSeen, m.ResumeID)
		}
		mu.Unlock()
		switch v {
		case resumeA:
			ad.id.Store(resumeB)
		case resumeB:
			once.Do(func() { close(release) })
		}
	})
	input := make(chan []byte, 1)
	go func() {
		<-release
		input <- []byte("\n")
	}()
	sup := New(SupervisorConfig{
		StateDir: stateDir, Store: store,
		Command:  []string{"sh", "-c", "read x"},
		Adapter:  ad,
		InputCh:  input,
		IdleTick: 20 * time.Millisecond,
	})
	// Empty reports nothing; then the first id arrives.
	go func() {
		time.Sleep(100 * time.Millisecond)
		ad.id.Store(resumeA)
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	require.NoError(t, sup.Run(ctx, sess))

	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, []string{resumeA, resumeB}, seen, "one patch per change")
	require.Equal(t, seen, metaSeen, "meta.json already held the id when the patch went out")
}

// recordPatches returns a func giving the patch keys seen so far, in order,
// and the resume_id meta.json held when its patch went out.
func recordPatches(store *state.Store, stateDir, id string) (order func() []string, metaAtPatch func() string) {
	var (
		mu   sync.Mutex
		keys []string
		meta string
	)
	store.Subscribe(func(e state.Event) {
		mu.Lock()
		defer mu.Unlock()
		if _, ok := e.Patch["resume_id"]; ok {
			keys = append(keys, "resume_id")
			if m, err := state.LoadMeta(stateDir, id); err == nil {
				meta = m.ResumeID + "/" + string(m.State)
			}
		} else if _, ok := e.Patch["state"]; ok {
			keys = append(keys, "state")
		}
	})
	return func() []string { mu.Lock(); defer mu.Unlock(); return append([]string(nil), keys...) },
		func() string { mu.Lock(); defer mu.Unlock(); return meta }
}

// capture_regex at child exit: the id is read from the normalized screen
// text (ANSI stripped), stored before the final meta.json write, and its
// patch goes out after the terminal transition.
func TestSupervisor_CapturesResumeIDAtExit(t *testing.T) {
	stateDir := t.TempDir()
	store, sess := newResumeSession(t, "cap1")
	order, metaAtPatch := recordPatches(store, stateDir, "cap1")
	sup := New(SupervisorConfig{
		StateDir: stateDir, Store: store,
		Command:       []string{"bash", "-c", `printf 'noise\n\033[1mid=` + resumeB + `\033[0m\nid=` + resumeA + `\n'`},
		ResumeCapture: regexp.MustCompile(`(?m)id=([0-9a-f-]{36})`),
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, sup.Run(ctx, sess))

	got, _ := store.Get("cap1")
	require.Equal(t, protocol.StateDone, got.State)
	require.Equal(t, resumeA, got.Summary().ResumeID, "the last match wins")
	require.Equal(t, resumeA+"/"+string(protocol.StateDone), metaAtPatch(), "meta.json final before the patch")
	require.Equal(t, "resume_id", order()[len(order())-1], "resume_id patch follows the transition")
	require.Contains(t, order(), "state")
}

// Exit text that does not hold a valid id changes nothing and sends no patch:
// an id that is not a UUID, no match, and a regex with no group hit.
func TestSupervisor_CaptureIgnoresBadIDs(t *testing.T) {
	for name, tc := range map[string]struct{ script, re string }{
		"not a uuid": {`echo id=--resume`, `id=(\S+)`},
		"no match":   {`echo nothing`, `id=(\S+)`},
	} {
		t.Run(name, func(t *testing.T) {
			stateDir := t.TempDir()
			store, sess := newResumeSession(t, "bad1")
			sess.ResumeID = resumeA // the spawn id stays
			order, _ := recordPatches(store, stateDir, "bad1")
			sup := New(SupervisorConfig{
				StateDir: stateDir, Store: store,
				Command:       []string{"bash", "-c", tc.script},
				ResumeCapture: regexp.MustCompile(tc.re),
			})
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			require.NoError(t, sup.Run(ctx, sess))
			require.NotContains(t, order(), "resume_id")
			require.Equal(t, resumeA, sess.Summary().ResumeID)
		})
	}
}

// The CompleteCh exit also refreshes the screen text first (last_line used
// to stay stale there) and captures the id, ordered like the child-exit path.
func TestSupervisor_CompleteCapturesResumeIDAndRefreshesScreen(t *testing.T) {
	stateDir := t.TempDir()
	store, sess := newResumeSession(t, "comp1")
	order, metaAtPatch := recordPatches(store, stateDir, "comp1")
	completeCh := make(chan struct{}, 1)
	var (
		mu  sync.Mutex
		out []byte
	)
	sup := New(SupervisorConfig{
		StateDir: stateDir, Store: store,
		Command:       []string{"bash", "-c", "echo last-visible-line id=" + resumeA + "; sleep 30"},
		CompleteCh:    completeCh,
		ResumeCapture: regexp.MustCompile(`id=([0-9a-f-]{36})`),
		IdleTick:      time.Hour, // the tick never refreshes the screen
		OutputSink:    func(b []byte) { mu.Lock(); out = append(out, b...); mu.Unlock() },
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- sup.Run(ctx, sess) }()
	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return regexp.MustCompile(`id=` + resumeA).Match(out)
	}, 5*time.Second, 10*time.Millisecond)
	completeCh <- struct{}{}
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("supervisor did not return after complete signal")
	}

	got, _ := store.Get("comp1")
	require.Equal(t, protocol.StateDone, got.State)
	require.Equal(t, resumeA, got.Summary().ResumeID)
	require.Contains(t, got.Summary().LastLine, "last-visible-line", "refreshScreenText ran before the exit")
	require.Equal(t, resumeA+"/"+string(protocol.StateDone), metaAtPatch())
	require.Equal(t, "resume_id", order()[len(order())-1])
}

// With nothing to capture, the exit paths send no resume_id patch.
func TestSupervisor_NoCaptureConfiguredNoPatch(t *testing.T) {
	stateDir := t.TempDir()
	store, sess := newResumeSession(t, "none1")
	order, _ := recordPatches(store, stateDir, "none1")
	sup := New(SupervisorConfig{
		StateDir: stateDir, Store: store,
		Command: []string{"bash", "-c", "echo id=" + resumeA},
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, sup.Run(ctx, sess))
	require.NotContains(t, order(), "resume_id")
	require.Empty(t, sess.Summary().ResumeID)
}
