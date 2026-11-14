// Package summarizer drives a local Ollama daemon to produce a one-line activity
// description per active session. It consumes session IDs from a channel and
// writes results into state.Store.
package summarizer

import "time"

// Config is the per-process configuration for the summarizer worker.
type Config struct {
	BaseURL        string        // Ollama base URL; default "http://127.0.0.1:11434"
	Model          string        // Ollama model name; default "gemma2:2b"
	RequestTimeout time.Duration // per HTTP call; default 15s
	MinInterval    time.Duration // per-session floor between Ollama calls; default 800ms
	MaxBytes       int           // transcript window size sent to the model; default 2048
	Detail         Detail        // description detail level; zero value is DetailMedium
	// Schedule turns on the scheduled refresh (R15): ticks per Cadence, and
	// output-driven requests are ignored. Off (the zero value) is the
	// pre-R15 behavior. The daemon sets it from summary_schedule; it can be
	// flipped live with Worker.SetSchedule.
	Schedule bool
	// Cadence is the tick schedule; the zero value is DefaultCadence. Tests
	// scale it down.
	Cadence Cadence
}

// Defaults returns a Config populated with library-wide defaults.
func Defaults() Config {
	return Config{
		BaseURL:        "http://127.0.0.1:11434",
		Model:          "gemma2:2b",
		RequestTimeout: 15 * time.Second,
		MinInterval:    800 * time.Millisecond,
		MaxBytes:       2048,
		Detail:         DetailMedium,
	}
}
