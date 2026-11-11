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

	"github.com/stretchr/testify/require"
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

func TestClient_SetModelChangesGenerateRequestModel(t *testing.T) {
	var captured string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Model string `json:"model"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		captured = body.Model
		_, _ = w.Write([]byte(`{"response":"ok"}`))
	}))
	defer srv.Close()

	c := NewClient(Config{BaseURL: srv.URL, Model: "gemma2:2b"})
	c.SetModel("llama3.1")
	_, err := c.Generate(context.Background(), "hi")
	require.NoError(t, err)
	require.Equal(t, "llama3.1", captured)
}

func captureBody(t *testing.T, reply string) (*Client, func() map[string]any) {
	t.Helper()
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = nil
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = io.WriteString(w, reply)
	}))
	t.Cleanup(srv.Close)
	c := NewClient(Config{BaseURL: srv.URL, Model: "m", RequestTimeout: 2 * time.Second})
	return c, func() map[string]any { return got }
}

func TestClientComplete_SendsSystemStopNumPredict(t *testing.T) {
	c, body := captureBody(t, `{"response":"Editing x.go"}`)
	got, err := c.Complete(context.Background(), CompleteRequest{
		System: "be brief", Prompt: "screen", NumPredict: 64, Stop: []string{"\n"}, Temperature: 0.2,
	})
	require.NoError(t, err)
	require.Equal(t, "Editing x.go", got)
	b := body()
	require.Equal(t, "be brief", b["system"])
	require.Equal(t, "screen", b["prompt"])
	require.Equal(t, false, b["stream"])
	opts := b["options"].(map[string]any)
	require.Equal(t, float64(64), opts["num_predict"])
	require.Equal(t, []any{"\n"}, opts["stop"])
	require.Equal(t, 0.2, opts["temperature"])
}

func TestClientGenerate_Unchanged(t *testing.T) {
	c, body := captureBody(t, `{"response":"ok"}`)
	_, err := c.Generate(context.Background(), "ping")
	require.NoError(t, err)
	b := body()
	require.NotContains(t, b, "system", "the probe sends no system prompt")
	opts := b["options"].(map[string]any)
	require.Equal(t, float64(30), opts["num_predict"])
	require.NotContains(t, opts, "stop")
}

func TestClientComplete_OmitsZeroOptions(t *testing.T) {
	c, body := captureBody(t, `{"response":"ok"}`)
	_, err := c.Complete(context.Background(), CompleteRequest{Prompt: "p"})
	require.NoError(t, err)
	opts := body()["options"].(map[string]any)
	require.NotContains(t, opts, "num_predict")
	require.NotContains(t, opts, "stop")
}
