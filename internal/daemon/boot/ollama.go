package boot

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/tristanbietsch/rex/internal/features/summarizer"
)

// warmupTimeout bounds the probe's load-the-model generate call. Cold loads
// of a small model take a few seconds; anything slower is unusable anyway.
const warmupTimeout = 60 * time.Second

// ProbeOllamaHealth runs the initial Ollama reachability + model-presence check,
// then re-checks every 30s so the worker actively detects healthy→unhealthy
// transitions instead of waiting for its failure threshold. A worker is only
// marked available after a real generate succeeds (which also loads the model
// into memory), so /api/tags alone can't flip it back on after call failures.
func ProbeOllamaHealth(ctx context.Context, w *summarizer.Worker, model string) {
	cfg := summarizer.Defaults()
	cfg.Model = model
	cfg.RequestTimeout = warmupTimeout
	if u := OllamaBaseURL(); u != "" {
		cfg.BaseURL = u
	}
	client := summarizer.NewClient(cfg)
	warmed := ""
	check := func() {
		tCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		tags, err := client.Tags(tCtx)
		if err != nil {
			slog.Debug("daemon: ollama unreachable", "base_url", cfg.BaseURL, "err", err)
			w.MarkUnavailable("ollama unreachable")
			return
		}
		resolved, ok := summarizer.ResolveModel(model, tags)
		if !ok {
			slog.Debug("daemon: ollama reachable but no compatible model", "configured", model, "tags", tags)
			w.MarkUnavailable("no compatible summary model pulled; try `ollama pull " + model + "`")
			return
		}
		if resolved != model {
			slog.Info("summarizer: model_substituted", "from", model, "to", resolved, "reason", "configured model not pulled")
			w.SetModel(resolved)
			model = resolved
		}
		client.SetModel(model)
		if warmed != model || !w.BackendAvailable() {
			gCtx, gCancel := context.WithTimeout(ctx, warmupTimeout)
			defer gCancel()
			start := time.Now()
			if _, err := client.Generate(gCtx, "Reply with the single word: ok"); err != nil {
				slog.Warn("summarizer: warmup failed", "model", model, "err", err)
				w.MarkUnavailable("warm-up generate failed: " + err.Error())
				return
			}
			slog.Info("summarizer: warmup ok", "model", model, "duration_ms", time.Since(start).Milliseconds())
			warmed = model
		}
		w.MarkAvailable()
	}
	check()
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			check()
		}
	}
}

// OllamaBaseURL returns the OLLAMA_HOST env var as a fully-qualified URL
// (adding the http:// prefix if missing) or empty string when unset.
func OllamaBaseURL() string {
	env := os.Getenv("OLLAMA_HOST")
	if env == "" {
		return ""
	}
	if !strings.HasPrefix(env, "http") {
		env = "http://" + env
	}
	return env
}
