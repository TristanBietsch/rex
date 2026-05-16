package summarizer

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestClientGenerateOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/generate" {
			t.Fatalf("path: %s", r.URL.Path)
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["model"] != "gemma2:2b" {
			t.Fatalf("model: %v", body["model"])
		}
		if !strings.Contains(body["prompt"].(string), "TRANSCRIPT") {
			t.Fatalf("prompt missing transcript marker: %v", body["prompt"])
		}
		if body["stream"] != false {
			t.Fatalf("stream: %v want false", body["stream"])
		}
		opts, _ := body["options"].(map[string]any)
		if opts["num_predict"] != float64(30) {
			t.Fatalf("num_predict: %v want 30", opts["num_predict"])
		}
		if opts["temperature"] != 0.2 {
			t.Fatalf("temperature: %v want 0.2", opts["temperature"])
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"response":"running pnpm test"}`)
	}))
	defer srv.Close()

	c := NewClient(Config{BaseURL: srv.URL, Model: "gemma2:2b", RequestTimeout: 2 * time.Second})
	got, err := c.Generate(context.Background(), "TRANSCRIPT body")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if got != "running pnpm test" {
		t.Fatalf("got %q", got)
	}
}

func TestClientGenerate500(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()
	c := NewClient(Config{BaseURL: srv.URL, Model: "gemma2:2b", RequestTimeout: 2 * time.Second})
	_, err := c.Generate(context.Background(), "x")
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestClientTagsContains(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tags" {
			t.Fatalf("path: %s", r.URL.Path)
		}
		_, _ = io.WriteString(w, `{"models":[{"name":"gemma2:2b"},{"name":"phi3:mini"}]}`)
	}))
	defer srv.Close()
	c := NewClient(Config{BaseURL: srv.URL, RequestTimeout: 2 * time.Second})
	tags, err := c.Tags(context.Background())
	if err != nil {
		t.Fatalf("Tags: %v", err)
	}
	got := map[string]bool{}
	for _, t := range tags {
		got[t] = true
	}
	if !got["gemma2:2b"] {
		t.Fatalf("missing gemma2:2b in tags: %v", tags)
	}
}
