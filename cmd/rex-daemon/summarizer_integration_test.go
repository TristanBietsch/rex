package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/tristanbietsch/rex/internal/protocol"
	"github.com/tristanbietsch/rex/internal/state"
	"github.com/tristanbietsch/rex/internal/summarizer"
)

func TestEndToEndSummaryDelivery(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/tags" {
			_, _ = io.WriteString(w, `{"models":[{"name":"gemma2:2b"}]}`)
			return
		}
		_, _ = io.WriteString(w, `{"response":"running pnpm test:billing"}`)
	}))
	defer srv.Close()

	st := state.NewStore()
	sess := &state.Session{
		ID:        "sess-eeee",
		ShortID:   "eeee",
		ToolID:    "codex",
		Slug:      "test-task",
		State:     protocol.StateWorking,
		StartedAt: time.Now().UTC(),
	}
	if err := st.Add(sess); err != nil {
		t.Fatalf("Add: %v", err)
	}

	cfg := summarizer.Defaults()
	cfg.BaseURL = srv.URL
	cfg.MinInterval = 0
	w := summarizer.New(cfg, st, func(_ string, _ int) []byte {
		return []byte("recent terminal output here")
	})

	got := make(chan string, 1)
	st.Subscribe(func(e state.Event) {
		if v, ok := e.Patch["description"].(string); ok {
			select {
			case got <- v:
			default:
			}
		}
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = w.Start(ctx) }()

	w.Channel() <- "sess-eeee"
	select {
	case desc := <-got:
		if desc != "running pnpm test:billing" {
			t.Fatalf("desc: %q", desc)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("no description delivered within 2s")
	}
}
