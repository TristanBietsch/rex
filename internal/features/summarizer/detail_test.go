package summarizer

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/daemon/state"
)

func TestParseDetail(t *testing.T) {
	require.Equal(t, DetailShort, ParseDetail("short"))
	require.Equal(t, DetailMedium, ParseDetail("medium"))
	require.Equal(t, DetailLong, ParseDetail("long"))
	for _, bad := range []string{"", "Long", "huge", "0"} {
		require.Equal(t, DetailMedium, ParseDetail(bad), bad)
	}
	require.Equal(t, DetailMedium, Detail(0), "the zero value is the default")
	require.Equal(t, DetailMedium, Config{}.Detail)
}

func TestWorker_SetDetailChangesPromptAndCap(t *testing.T) {
	type seen struct {
		system     string
		numPredict float64
	}
	var (
		mu   sync.Mutex
		reqs []seen
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var b struct {
			System  string         `json:"system"`
			Options map[string]any `json:"options"`
		}
		_ = json.NewDecoder(r.Body).Decode(&b)
		np, _ := b.Options["num_predict"].(float64)
		mu.Lock()
		reqs = append(reqs, seen{b.System, np})
		mu.Unlock()
		_, _ = io.WriteString(w, `{"response":"Editing x.go to verify signatures on every handler in the billing service and then run the whole suite again"}`)
	}))
	defer srv.Close()

	st, _ := newTestStoreWithSession("sess-detail")
	cfg := Defaults()
	cfg.BaseURL = srv.URL
	cfg.MinInterval = 20 * time.Millisecond
	w := startWorker(t, cfg, st, uniqueTranscript(nil, nil))

	last := func() seen {
		mu.Lock()
		defer mu.Unlock()
		return reqs[len(reqs)-1]
	}
	count := func() int { mu.Lock(); defer mu.Unlock(); return len(reqs) }

	w.Channel() <- "sess-detail"
	require.Eventually(t, func() bool { return count() == 1 }, 2*time.Second, 5*time.Millisecond)
	require.Equal(t, float64(64), last().numPredict)
	require.Contains(t, last().system, "about 15 words")

	w.SetDetail(DetailShort)
	require.Equal(t, DetailShort, w.Detail())
	w.Channel() <- "sess-detail"
	require.Eventually(t, func() bool { return count() == 2 }, 2*time.Second, 5*time.Millisecond)
	require.Equal(t, float64(40), last().numPredict)
	require.Contains(t, last().system, "about 8 words")

	w.SetDetail(DetailLong)
	w.Channel() <- "sess-detail"
	require.Eventually(t, func() bool { return count() == 3 }, 2*time.Second, 5*time.Millisecond)
	require.Equal(t, float64(100), last().numPredict)
	require.Contains(t, last().system, "about 28 words")

	// The stored description is clamped to the level's cap.
	w.SetDetail(DetailShort)
	w.Channel() <- "sess-detail"
	require.Eventually(t, func() bool { return count() == 4 }, 2*time.Second, 5*time.Millisecond)
	require.Eventually(t, func() bool {
		return len([]rune(descriptionOf(st, "sess-detail"))) <= 60 && strings.HasSuffix(descriptionOf(st, "sess-detail"), "…")
	}, 2*time.Second, 5*time.Millisecond)
}

func TestWorker_ConfigDetailAppliesAtStart(t *testing.T) {
	cfg := Defaults()
	cfg.Detail = DetailLong
	w := New(cfg, state.NewStore(), transcriptStub("x"))
	require.Equal(t, DetailLong, w.Detail())
}
