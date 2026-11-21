//go:build darwin

package server

import (
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseProcArgsExe(t *testing.T) {
	argc := func(n uint32) []byte {
		b := make([]byte, 4)
		binary.NativeEndian.PutUint32(b, n)
		return b
	}
	tests := []struct {
		name    string
		buf     []byte
		want    string
		wantErr bool
	}{
		{"ok", append(argc(1), []byte("/bin/sh\x00sh\x00")...), "/bin/sh", false},
		{"path to end of buffer without NUL", append(argc(1), []byte("/bin/sh")...), "", true},
		{"empty buffer", nil, "", true},
		{"short buffer", []byte{1, 0}, "", true},
		{"argc only", argc(1), "", true},
		{"empty path", append(argc(1), 0, 'x'), "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseProcArgsExe(tt.buf)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}
