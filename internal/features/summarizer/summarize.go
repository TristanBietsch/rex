package summarizer

import (
	"context"
	"errors"
	"fmt"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// Kind selects what a summary is about, and with it the prompt template.
// A new kind adds a constant, one case in BuildPrompt and its template in its
// own prompt_<kind>.go; it must not edit prompt_activity.go.
type Kind string

// KindActivity is one session's screen, written to its description column.
const KindActivity Kind = "activity"

// Input is what a summary is built from.
type Input struct {
	Tool  string         // tool id; only the opener guard sees it, never the prompt
	Task  string         // session title ("" falls back to the caller's slug)
	State protocol.State // working | needs_input | done | failed | crashed
	Text  string         // UNTRUSTED text: a screen, or a digest of rows
}

// Prompt is a built request: the two prompt halves and the caps that govern
// the reply.
type Prompt struct {
	System, User string
	MaxRunes     int // cleaned reply is clamped to this
	NumPredict   int // model token budget
}

// ErrUnavailable is returned by Summarize while the backend is believed down.
var ErrUnavailable = errors.New("summarizer: backend unavailable")

// BuildPrompt renders the prompt for kind. An unknown kind is an error.
func BuildPrompt(kind Kind, in Input, d Detail) (Prompt, error) {
	switch kind {
	case KindActivity:
		return activityPrompt(in, d), nil
	default:
		return Prompt{}, fmt.Errorf("summarizer: unknown kind %q", kind)
	}
}

// Summarize runs one summary and returns the cleaned single line ("" with a
// nil error when the reply cleans to nothing). It shares the Ollama slot with
// the worker loop, so concurrent callers and the loop never overlap, and it
// is safe from any goroutine. Nothing is stored: the caller owns the result.
func (w *Worker) Summarize(ctx context.Context, kind Kind, in Input) (string, error) {
	p, err := BuildPrompt(kind, in, w.Detail())
	if err != nil {
		return "", err
	}
	if !w.BackendAvailable() {
		return "", ErrUnavailable
	}
	resp, err := w.complete(ctx, p)
	if err != nil {
		return "", err
	}
	return Clean(resp, p.MaxRunes, in.Tool), nil
}

// complete makes the call for p with the retry policy and the shared slot.
func (w *Worker) complete(ctx context.Context, p Prompt) (string, error) {
	return w.callWithRetry(ctx, CompleteRequest{
		System:      p.System,
		Prompt:      p.User,
		NumPredict:  p.NumPredict,
		Stop:        []string{"\n"},
		Temperature: 0.2,
	})
}
