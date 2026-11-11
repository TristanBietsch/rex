//go:build ollama_eval

package summarizer

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// Live prompt evaluation against a real Ollama. Never part of make check: it
// needs a running Ollama and pulled models, and its numbers are for a human
// to read before and after a prompt change.
//
//	REX_EVAL_MODELS=llama3.2:3b,gemma2:2b \
//	  go test -tags ollama_eval -run TestLiveEval -v ./internal/features/summarizer
//
// REX_EVAL_BASE_URL overrides http://127.0.0.1:11434. The fixtures under
// testdata/screens are synthetic. The test logs raw and cleaned model output
// per fixture and detail level, so run it on a machine where that is fine.
var openerRaw = regexp.MustCompile(`(?i)^(?:(?:the|this)\s+)?(?:(?:ai|coding|cli)\s+)?(?:agent|assistant|model|bot|session|claude(?:\s+code)?|codex|agy|antigravity|gemini|it|i|we|this|currently)\b`)

func TestLiveEval(t *testing.T) {
	models := strings.Split(os.Getenv("REX_EVAL_MODELS"), ",")
	if len(models) == 1 && strings.TrimSpace(models[0]) == "" {
		t.Skip("set REX_EVAL_MODELS (comma-separated Ollama model names) to run the live eval")
	}
	base := os.Getenv("REX_EVAL_BASE_URL")
	screens, err := filepath.Glob("testdata/screens/*.txt")
	if err != nil || len(screens) == 0 {
		t.Fatalf("no fixtures: %v", err)
	}
	states := map[string]protocol.State{
		"claude_permission": protocol.StateNeedsInput,
		"claude_question":   protocol.StateNeedsInput,
	}

	for _, model := range models {
		model = strings.TrimSpace(model)
		cfg := Defaults()
		cfg.Model = model
		if base != "" {
			cfg.BaseURL = base
		}
		cfg.RequestTimeout = 60 * time.Second
		w := New(cfg, nil, nil)

		var calls, openers, tooLong int
		for _, path := range screens {
			name := strings.TrimSuffix(filepath.Base(path), ".txt")
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			st, ok := states[name]
			if !ok {
				st = protocol.StateWorking
			}
			for _, d := range promptDetails {
				w.SetDetail(d)
				in := Input{Tool: "claude", Task: "fix invoice rounding", State: st, Text: strings.TrimRight(string(raw), "\n")}
				p, err := BuildPrompt(KindActivity, in, d)
				if err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
				resp, err := w.complete(ctx, p)
				cancel()
				if err != nil {
					t.Errorf("%s %s %s: %v", model, name, d, err)
					continue
				}
				calls++
				first, _, _ := strings.Cut(strings.TrimSpace(resp), "\n")
				if openerRaw.MatchString(first) {
					openers++
				}
				if utf8.RuneCountInString(first) > d.MaxRunes() {
					tooLong++
				}
				t.Logf("%-14s %-18s %-6s raw=%q cleaned=%q", model, name, d, first, Clean(resp, d.MaxRunes(), in.Tool))
			}
		}
		if calls > 0 {
			t.Logf("%s: %d calls, opener rate %.0f%% (%d), over-length rate %.0f%% (%d)",
				model, calls, 100*float64(openers)/float64(calls), openers, 100*float64(tooLong)/float64(calls), tooLong)
		}
	}
}
