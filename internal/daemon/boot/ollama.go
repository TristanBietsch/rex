package boot

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/tristanbietsch/rex/internal/features/summarizer"
)

// ProbeOllamaHealth runs the initial Ollama reachability + model-presence check,
// then re-checks every 30s so the worker actively detects healthy→unhealthy
// transitions instead of waiting for its failure threshold. Each successful
// check that finds the configured model present marks the worker available;
// failures (unreachable / model missing) mark it unavailable with a reason.
func ProbeOllamaHealth(ctx context.Context, w *summarizer.Worker, model string) {
	cfg := summarizer.Defaults()
	cfg.Model = model
	if u := OllamaBaseURL(); u != "" {
		cfg.BaseURL = u
	}
	client := summarizer.NewClient(cfg)
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
