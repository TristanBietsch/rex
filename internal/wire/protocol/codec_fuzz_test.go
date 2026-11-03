package protocol

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/require"
)

// encodeLine renders one envelope the way Writer does, for fuzz seeds.
func encodeLine(f *testing.F, kind Kind, typ, id string, payload any) []byte {
	f.Helper()
	var buf bytes.Buffer
	w := NewWriter(&buf)
	var err error
	if kind == KindIntent {
		err = w.WriteIntent(typ, id, payload)
	} else {
		err = w.WriteEvent(typ, id, payload)
	}
	if err != nil {
		f.Fatalf("seed encode: %v", err)
	}
	return buf.Bytes()
}

// FuzzReaderRead feeds arbitrary bytes to Reader.Read. The daemon runs Read
// on every client connection, so it must never panic and must always make
// progress: each call consumes one line and the stream ends in io.EOF.
func FuzzReaderRead(f *testing.F) {
	hello := encodeLine(f, KindIntent, IntentHello, "1", Hello{ClientVersion: "test"})
	snap := encodeLine(f, KindEvent, EventSnapshot, "", Snapshot{Filter: "all"})
	wrongVersion, _ := json.Marshal(Envelope{V: 99, Kind: KindIntent, Type: "Hello", Data: json.RawMessage(`{}`)})

	f.Add(hello)
	f.Add(append(append([]byte{}, hello...), snap...))
	f.Add(append(wrongVersion, '\n'))
	f.Add([]byte(""))
	f.Add([]byte("\n"))
	f.Add([]byte(`{"v":1,"kind":"Intent","type":"Hello","data":{}}`)) // no trailing newline
	f.Add([]byte("not json\n{\"v\":1}\n"))

	f.Fuzz(func(t *testing.T, data []byte) {
		r := NewReader(bytes.NewReader(data))
		// Every Read consumes through the next '\n' (or to EOF), so the
		// stream must end within lines+1 non-EOF results.
		maxReads := bytes.Count(data, []byte{'\n'}) + 1
		for i := 0; ; i++ {
			env, err := r.Read()
			if errors.Is(err, io.EOF) {
				return
			}
			require.Less(t, i, maxReads, "Read did not reach io.EOF")
			if err == nil {
				require.Equal(t, ProtocolVersion, env.V, "Read returned an envelope with an unsupported version")
			}
		}
	})
}

// FuzzWriterReaderRoundTrip checks that anything Writer emits, Reader decodes
// back unchanged, and that one envelope never bleeds into the next (JSONL
// framing survives newlines and arbitrary bytes in the payload).
func FuzzWriterReaderRoundTrip(f *testing.F) {
	f.Add(true, IntentHello, "1", "", []byte(nil))
	f.Add(false, EventSnapshot, "", "", []byte("all"))
	f.Add(true, IntentSendInput, "req-7", "7d4f3c8a-1234-4abc-89ab-cdefabcdef01", []byte("hello\n"))
	f.Add(false, "Error", "x\ny", "a\"b\\c", []byte{0x1b, '[', '3', '1', 'm', 0x00, 0xff})

	f.Fuzz(func(t *testing.T, intent bool, typ, id, sessionID string, payload []byte) {
		// encoding/json replaces invalid UTF-8 in strings with U+FFFD, so
		// only valid strings can round-trip byte-for-byte. Raw bytes travel
		// as base64 and need no such guard.
		if !utf8.ValidString(typ) || !utf8.ValidString(id) || !utf8.ValidString(sessionID) {
			t.Skip("invalid UTF-8 is not representable in a JSON string")
		}
		in := SendInput{SessionID: sessionID, Bytes: payload}

		var buf bytes.Buffer
		w := NewWriter(&buf)
		write := w.WriteEvent
		wantKind := KindEvent
		if intent {
			write = w.WriteIntent
			wantKind = KindIntent
		}
		// Two copies back to back prove the first line ends exactly at its
		// newline.
		require.NoError(t, write(typ, id, in))
		require.NoError(t, write(typ, id, in))
		require.Equal(t, 2, bytes.Count(buf.Bytes(), []byte{'\n'}), "each envelope must be exactly one line")

		r := NewReader(&buf)
		for range 2 {
			env, err := r.Read()
			require.NoError(t, err)
			require.Equal(t, ProtocolVersion, env.V)
			require.Equal(t, wantKind, env.Kind)
			require.Equal(t, typ, env.Type)
			require.Equal(t, id, env.ID)

			var out SendInput
			require.NoError(t, json.Unmarshal(env.Data, &out))
			require.Equal(t, in.SessionID, out.SessionID)
			require.True(t, bytes.Equal(in.Bytes, out.Bytes), "payload bytes changed: %q -> %q", in.Bytes, out.Bytes)
		}
		_, err := r.Read()
		require.ErrorIs(t, err, io.EOF)
	})
}
