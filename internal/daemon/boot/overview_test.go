package boot

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/registry"
	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/daemon/server"
	"github.com/tristanbietsch/rex/internal/daemon/state"
	"github.com/tristanbietsch/rex/internal/features/overview"
	"github.com/tristanbietsch/rex/internal/features/summarizer"
	"github.com/tristanbietsch/rex/internal/wire/client"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

func TestOverviewRelevance(t *testing.T) {
	working := protocol.StateWorking
	for name, tc := range map[string]struct {
		e    state.Event
		want overview.Relevance
	}{
		"added":         {state.Event{Kind: state.EventAdded}, overview.StructuralChange},
		"removed":       {state.Event{Kind: state.EventRemoved}, overview.StructuralChange},
		"state change":  {state.Event{Kind: state.EventUpdated, NewState: &working}, overview.StructuralChange},
		"wait reason":   {state.Event{Kind: state.EventUpdated, Patch: map[string]any{"wait_reason": "review"}}, overview.StructuralChange},
		"description":   {state.Event{Kind: state.EventUpdated, Patch: map[string]any{"description": "x"}}, overview.ContentChange},
		"tokens":        {state.Event{Kind: state.EventUpdated, Patch: map[string]any{"tokens": 3}}, overview.Ignored},
		"unknown kind":  {state.Event{Kind: state.EventKind(99)}, overview.Ignored},
		"empty updated": {state.Event{Kind: state.EventUpdated}, overview.Ignored},
	} {
		require.Equal(t, tc.want, OverviewRelevance(tc.e), name)
	}
}

func TestOverviewEnabled(t *testing.T) {
	require.False(t, OverviewEnabled(nil))
	st := settings.NewStore()
	require.False(t, OverviewEnabled(st), "off by default")
	require.NoError(t, st.Set("overview_enabled", true))
	require.True(t, OverviewEnabled(st))
}

// fakeOllama answers every generate request with response and counts them.
type fakeOllama struct {
	srv    *httptest.Server
	calls  atomic.Int32
	mu     sync.Mutex
	prompt string
}

func newFakeOllama(t *testing.T, response string) *fakeOllama {
	t.Helper()
	f := &fakeOllama{}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var req struct {
			Prompt string `json:"prompt"`
		}
		_ = json.Unmarshal(b, &req)
		f.mu.Lock()
		f.prompt = req.Prompt
		f.mu.Unlock()
		f.calls.Add(1)
		out, _ := json.Marshal(map[string]string{"response": response})
		_, _ = w.Write(out)
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeOllama) lastPrompt() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.prompt
}

type ovRig struct {
	srv   *server.Server
	store *state.Store
	ov    *Overview
	w     *summarizer.Worker
	ollm  *fakeOllama
	sock  string
}

var fastOverview = overview.Config{Debounce: 10 * time.Millisecond, MinInterval: 30 * time.Millisecond, Refresh: 100 * time.Millisecond, Timeout: 2 * time.Second}

func newOvRig(t *testing.T) *ovRig {
	t.Helper()
	g := &ovRig{ollm: newFakeOllama(t, "Review billing-tests now")}
	dir, err := os.MkdirTemp("", "rex-ov")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	g.sock = filepath.Join(dir, "rex.sock")
	reg, err := registry.Load("")
	require.NoError(t, err)
	g.store = state.NewStore()
	require.NoError(t, g.store.Add(&state.Session{ID: "s1", ShortID: "s1", Slug: "billing-tests", ToolID: "echo",
		State: protocol.StateDone, StartedAt: time.Now().UTC(), LastEventAt: time.Now().UTC()}))
	g.w = summarizer.New(summarizer.Config{BaseURL: g.ollm.srv.URL, Model: "m"}, g.store, func(string, int) []byte { return nil })
	g.srv, err = server.New(server.Config{
		Socket: g.sock, StateDir: dir, Registry: reg, Store: g.store,
		OnBoardClient: func() { g.ov.Notify(overview.StructuralChange) },
	})
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	served := make(chan struct{})
	go func() { defer close(served); _ = g.srv.Serve(ctx) }()
	g.ov = NewOverview(ctx, fastOverview, g.store, g.srv, g.w)
	t.Cleanup(func() { cancel(); g.ov.Close(); <-served })
	require.Eventually(t, func() bool {
		c, err := client.Dial(g.sock)
		if err != nil {
			return false
		}
		_ = c.Close()
		return true
	}, 3*time.Second, 10*time.Millisecond)
	return g
}

func (g *ovRig) hello(t *testing.T, name string) *client.Client {
	t.Helper()
	c, err := client.Dial(g.sock)
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })
	_, err = c.Hello(name)
	require.NoError(t, err)
	return c
}

// With overview_enabled off the overview is inert: store changes and viewers
// cause no model call and nothing is published.
func TestOverview_OffIsInert(t *testing.T) {
	g := newOvRig(t)
	g.ov.SetEnabled(false)
	g.hello(t, protocol.ClientTUI)
	require.NoError(t, g.store.Add(&state.Session{ID: "s2", ShortID: "s2", Slug: "two", ToolID: "echo",
		State: protocol.StateWorking, StartedAt: time.Now().UTC()}))
	time.Sleep(10 * fastOverview.Debounce)
	require.Zero(t, g.ollm.calls.Load())
	require.Nil(t, g.srv.Overview())
}

func TestOverview_EnabledPublishesHeadlineForViewer(t *testing.T) {
	g := newOvRig(t)
	g.ov.SetEnabled(true)
	require.NotNil(t, g.srv.Overview(), "switching on publishes {enabled:true}")
	require.Empty(t, g.srv.Overview().Headline)

	time.Sleep(5 * fastOverview.Debounce)
	require.Zero(t, g.ollm.calls.Load(), "nobody is looking: no model call")

	c := g.hello(t, protocol.ClientTUI)
	require.Eventually(t, func() bool {
		o := g.srv.Overview()
		return o != nil && o.Headline == "Review billing-tests now"
	}, 5*time.Second, 5*time.Millisecond)
	require.Contains(t, g.ollm.lastPrompt(), "<sessions>")
	require.Contains(t, g.ollm.lastPrompt(), "- [review] billing-tests")

	// The viewer receives it as an event.
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		env, err := c.NextEventContext(ctx)
		cancel()
		require.NoError(t, err)
		if env.Type == protocol.EventOverview {
			var o protocol.Overview
			require.NoError(t, json.Unmarshal(env.Data, &o))
			if o.Headline != "" {
				require.Equal(t, "Review billing-tests now", o.Headline)
				break
			}
		}
	}
}

func TestOverview_BackendDownKeepsEnabledWithoutHeadline(t *testing.T) {
	g := newOvRig(t)
	g.w.MarkUnavailable("test")
	g.ov.SetEnabled(true)
	g.hello(t, protocol.ClientTUI)
	time.Sleep(10 * fastOverview.Debounce)
	require.Zero(t, g.ollm.calls.Load())
	o := g.srv.Overview()
	require.NotNil(t, o)
	require.True(t, o.Enabled)
	require.Empty(t, o.Headline)
}

func TestOverview_ApplierFollowsSetting(t *testing.T) {
	g := newOvRig(t)
	apply := g.ov.Applier()

	on := settings.NewStore()
	require.NoError(t, on.Set("overview_enabled", true))
	apply(on)
	require.True(t, g.srv.Overview().Enabled)

	apply(settings.NewStore()) // default: off
	require.False(t, g.srv.Overview().Enabled)
	require.Empty(t, g.srv.Overview().Headline)
}

func TestOverview_NilNotifyIsNoop(t *testing.T) {
	var o *Overview
	require.NotPanics(t, func() { o.Notify(overview.StructuralChange) })
}
