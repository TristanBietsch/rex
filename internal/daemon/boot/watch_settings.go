package boot

import (
	"context"
	"log/slog"
	"os"
	"time"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/features/summarizer"
)

// WatchSettings polls the settings file at path every `every` and, when it
// changes (modified, replaced, created or removed), loads a fresh settings
// store from it and hands that store to each apply func in order. It is the
// daemon's single settings watcher: features add an applier rather than a
// poller of their own.
//
// The file's state when WatchSettings starts counts as already applied (the
// daemon read it at startup), so appliers run only for later changes. A file
// that fails to load is logged and skipped until it changes again; a removed
// file applies the registry defaults. Appliers run on this goroutine, one
// after another, and must not block. Blocks until ctx is canceled.
func WatchSettings(ctx context.Context, path string, every time.Duration, apply ...func(*settings.Store)) {
	watchSettings(ctx, path, every, nil, apply...)
}

// watchSettings is WatchSettings with a test seam: onBaseline, when non-nil,
// is called once right after the baseline stat, before the first poll.
func watchSettings(ctx context.Context, path string, every time.Duration, onBaseline func(), apply ...func(*settings.Store)) {
	if path == "" || every <= 0 || len(apply) == 0 {
		return
	}
	last := statSig(path)
	if onBaseline != nil {
		onBaseline()
	}
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		cur := statSig(path)
		if cur == last {
			continue
		}
		last = cur
		st := settings.NewStore()
		if err := st.Load(path); err != nil {
			slog.Warn("daemon: settings reload failed (keeping current values)", "path", path, "err", err)
			continue
		}
		slog.Info("daemon: settings changed, applying")
		for _, fn := range apply {
			fn(st)
		}
	}
}

// fileSig identifies one state of the settings file. exists is false when it
// is missing (or unreadable), which is its own state.
type fileSig struct {
	exists  bool
	modTime time.Time
	size    int64
}

func statSig(path string) fileSig {
	fi, err := os.Stat(path)
	if err != nil {
		return fileSig{}
	}
	return fileSig{exists: true, modTime: fi.ModTime(), size: fi.Size()}
}

// SummaryDetailApplier returns a WatchSettings applier that keeps w's
// description detail in step with the summary_detail setting.
func SummaryDetailApplier(w *summarizer.Worker) func(*settings.Store) {
	return func(s *settings.Store) {
		d := summarizer.ParseDetail(s.String("summary_detail"))
		if d != w.Detail() {
			slog.Info("daemon: summary detail changed", "detail", d.String())
		}
		w.SetDetail(d)
	}
}
