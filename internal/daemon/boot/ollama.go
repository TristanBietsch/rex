package boot

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/url"
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

// ollamaDefaultPort is the port Ollama listens on when none is given.
const ollamaDefaultPort = "11434"

// ErrRemoteSummarizer is returned (wrapped) by OllamaEndpoint when the host is
// not loopback and a remote summarizer has not been allowed.
var ErrRemoteSummarizer = errors.New("summarizer endpoint is not local")

// OllamaEndpoint parses an OLLAMA_HOST value the way Ollama accepts it and
// returns a normalized base URL (scheme://host:port, no path). A bare IP is
// checked first, then a full URL, then host:port or :port. No scheme means
// http, no port means 11434, and an empty or unspecified host means
// 127.0.0.1. Credentials are refused. A non-loopback host is refused with
// ErrRemoteSummarizer unless allowRemote is true and the scheme is https.
func OllamaEndpoint(raw string, allowRemote bool) (string, error) {
	raw = strings.TrimSpace(raw)
	scheme, host, port := "http", "", ollamaDefaultPort

	switch {
	case net.ParseIP(raw) != nil:
		host = raw
	case strings.Contains(raw, "://"):
		u, err := url.Parse(raw)
		if err != nil {
			return "", fmt.Errorf("invalid OLLAMA_HOST: %w", err)
		}
		if u.Scheme != "http" && u.Scheme != "https" {
			return "", fmt.Errorf("unsupported scheme %q in OLLAMA_HOST", u.Scheme)
		}
		if u.User != nil {
			return "", errors.New("credentials in OLLAMA_HOST are not allowed")
		}
		if (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
			return "", errors.New("OLLAMA_HOST must not include a path, query or fragment")
		}
		scheme, host = u.Scheme, u.Hostname()
		if p := u.Port(); p != "" {
			port = p
		}
	default:
		h, p, err := net.SplitHostPort(raw)
		if err != nil {
			var addrErr *net.AddrError
			if !errors.As(err, &addrErr) || addrErr.Err != "missing port in address" {
				return "", fmt.Errorf("invalid OLLAMA_HOST: %w", err)
			}
			h = strings.TrimSuffix(strings.TrimPrefix(raw, "["), "]")
		} else if p != "" {
			port = p
		}
		host = h
	}

	if ip := net.ParseIP(host); host == "" || (ip != nil && ip.IsUnspecified()) {
		host = "127.0.0.1"
	}
	if ip := net.ParseIP(host); (ip == nil || !ip.IsLoopback()) && !strings.EqualFold(host, "localhost") {
		if !allowRemote {
			return "", fmt.Errorf("%w: %s", ErrRemoteSummarizer, host)
		}
		if scheme != "https" {
			return "", fmt.Errorf("remote summarizer endpoint %s requires https", host)
		}
	}
	return scheme + "://" + net.JoinHostPort(host, port), nil
}
