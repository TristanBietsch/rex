package setup

import (
	"errors"
	"os"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/daemon/boot"
)

// checkSummarizerSetting runs checkSummarizerEndpoint on this shell's
// OLLAMA_HOST and the summary_allow_remote setting.
func checkSummarizerSetting() checkResult {
	allow := false
	if store, _, err := settings.LoadDefault(); err == nil {
		allow, _ = store.Get("summary_allow_remote").(bool)
	}
	return checkSummarizerEndpoint(os.Getenv, allow)
}

// checkSummarizerEndpoint reports where the summarizer would send session
// text, from getenv("OLLAMA_HOST") and the summary_allow_remote setting. The
// daemon reads its own environment, which can differ from this shell's.
func checkSummarizerEndpoint(getenv func(string) string, allowRemote bool) checkResult {
	const name = "summarizer endpoint"
	raw := getenv("OLLAMA_HOST")
	src := "from this shell's OLLAMA_HOST"
	if raw == "" {
		src = "OLLAMA_HOST unset in this shell"
	}
	ep, err := boot.OllamaEndpoint(raw, false)
	if err == nil {
		return checkResult{name, checkPass, "local " + ep + " (" + src + ")"}
	}
	if !errors.Is(err, boot.ErrRemoteSummarizer) {
		return checkResult{name, checkFail, err.Error() + " (" + src + "): the summarizer stays off"}
	}
	if !allowRemote {
		return checkResult{name, checkFail, "OLLAMA_HOST is not local (" + src + "): the summarizer stays off until summary_allow_remote is true and the URL is https"}
	}
	ep, err = boot.OllamaEndpoint(raw, true)
	if err != nil {
		return checkResult{name, checkFail, err.Error() + " (" + src + "): the summarizer stays off"}
	}
	return checkResult{name, checkWarn, "remote " + ep + " (" + src + "): session text leaves this machine"}
}
