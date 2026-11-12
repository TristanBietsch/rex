//go:build windows

package settings

import (
	"errors"
	"os"
	"time"

	"golang.org/x/sys/windows"
)

// replaceRetryFor bounds how long replaceFile retries a refused rename.
const replaceRetryFor = 2 * time.Second

// replaceFile renames tmp over path. Windows refuses to replace a file while
// another handle has it open without delete sharing (another rex reading or
// saving config.yaml at that moment): ERROR_ACCESS_DENIED or
// ERROR_SHARING_VIOLATION. Those handles are short-lived, so retry briefly.
func replaceFile(tmp, path string) error {
	deadline := time.Now().Add(replaceRetryFor)
	for {
		err := os.Rename(tmp, path)
		if err == nil || !replaceBusy(err) || time.Now().After(deadline) {
			return err
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// readConfig reads path, retrying briefly while another save is renaming
// over it (the open fails with a sharing violation for that moment).
func readConfig(path string) ([]byte, error) {
	deadline := time.Now().Add(replaceRetryFor)
	for {
		b, err := os.ReadFile(path)
		if err == nil || !replaceBusy(err) || time.Now().After(deadline) {
			return b, err
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func replaceBusy(err error) bool {
	return errors.Is(err, windows.ERROR_ACCESS_DENIED) || errors.Is(err, windows.ERROR_SHARING_VIOLATION)
}
