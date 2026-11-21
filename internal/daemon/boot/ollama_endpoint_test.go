package boot

import (
	"errors"
	"strings"
	"testing"
)

func TestOllamaEndpoint(t *testing.T) {
	tests := []struct {
		name        string
		raw         string
		allowRemote bool
		want        string
		wantErr     error
		errContains string
	}{
		{name: "empty", raw: "", want: "http://127.0.0.1:11434"},
		{name: "unspecified v4", raw: "0.0.0.0", want: "http://127.0.0.1:11434"},
		{name: "unspecified v4 port", raw: "0.0.0.0:11434", want: "http://127.0.0.1:11434"},
		{name: "port only", raw: ":11434", want: "http://127.0.0.1:11434"},
		{name: "unspecified v6", raw: "::", want: "http://127.0.0.1:11434"},
		{name: "loopback v6", raw: "::1", want: "http://[::1]:11434"},
		{name: "loopback v6 port", raw: "[::1]:9999", want: "http://[::1]:9999"},
		{name: "loopback v4", raw: "127.0.0.1", want: "http://127.0.0.1:11434"},
		{name: "localhost port", raw: "localhost:9999", want: "http://localhost:9999"},
		{name: "url trailing slash", raw: "http://127.0.0.1:11434/", want: "http://127.0.0.1:11434"},
		{name: "remote refused", raw: "https://gpu.example:11434", wantErr: ErrRemoteSummarizer},
		{name: "remote allowed", raw: "https://gpu.example:11434", allowRemote: true, want: "https://gpu.example:11434"},
		{name: "remote http refused", raw: "http://gpu.example", allowRemote: true, errContains: "https"},
		{name: "bad scheme", raw: "httpx://127.0.0.1", errContains: "scheme"},
		{name: "credentials", raw: "http://u:p@127.0.0.1", //nolint:gosec // test input, not a credential
			errContains: "credentials"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := OllamaEndpoint(tc.raw, tc.allowRemote)
			switch {
			case tc.wantErr != nil:
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v, want %v", err, tc.wantErr)
				}
			case tc.errContains != "":
				if err == nil || !strings.Contains(err.Error(), tc.errContains) {
					t.Fatalf("err = %v, want containing %q", err, tc.errContains)
				}
			default:
				if err != nil {
					t.Fatalf("unexpected err: %v", err)
				}
				if got != tc.want {
					t.Fatalf("got %q, want %q", got, tc.want)
				}
			}
		})
	}
}
