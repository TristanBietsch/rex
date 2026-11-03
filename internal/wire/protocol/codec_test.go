package protocol

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestReaderWriter_RoundTrip(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(&buf)

	require.NoError(t, w.WriteIntent(IntentHello, "1", Hello{ClientVersion: "test"}))
	require.NoError(t, w.WriteEvent(EventSnapshot, "", Snapshot{Filter: "all"}))

	r := NewReader(&buf)
	first, err := r.Read()
	require.NoError(t, err)
	require.Equal(t, KindIntent, first.Kind)
	require.Equal(t, IntentHello, first.Type)
	require.Equal(t, "1", first.ID)

	second, err := r.Read()
	require.NoError(t, err)
	require.Equal(t, KindEvent, second.Kind)
	require.Equal(t, EventSnapshot, second.Type)

	_, err = r.Read()
	require.ErrorIs(t, err, io.EOF)
}

func TestReader_RejectsWrongVersion(t *testing.T) {
	var buf bytes.Buffer
	bad, _ := json.Marshal(Envelope{V: 99, Kind: KindIntent, Type: "Hello", Data: json.RawMessage(`{}`)})
	buf.Write(append(bad, '\n'))

	_, err := NewReader(&buf).Read()
	require.ErrorContains(t, err, "version")
}

func TestReader_RejectsWrongVersionTyped(t *testing.T) {
	bad, _ := json.Marshal(Envelope{V: 99, Kind: KindIntent, Type: "Hello", Data: json.RawMessage(`{}`)})
	good := []byte(`{"v":1,"kind":"Intent","type":"Hello","data":{}}` + "\n")
	r := NewReader(bytes.NewReader(append(append(bad, '\n'), good...)))

	_, err := r.Read()
	require.ErrorIs(t, err, ErrUnsupportedVersion)
	// The bad line was consumed whole, so the stream stays framed.
	env, err := r.Read()
	require.NoError(t, err)
	require.Equal(t, IntentHello, env.Type)
}

func TestReader_MalformedLineIsTypedAndRecoverable(t *testing.T) {
	r := NewReader(bytes.NewReader([]byte("not json\n" + `{"v":1,"kind":"Intent","type":"Hello","data":{}}` + "\n")))

	_, err := r.Read()
	require.ErrorIs(t, err, ErrMalformed)
	env, err := r.Read()
	require.NoError(t, err)
	require.Equal(t, IntentHello, env.Type)
}

func TestReader_PartialLineAtEOF(t *testing.T) {
	r := NewReader(bytes.NewReader([]byte(`{"v":1,"kind":"Intent","type":"Hello","data":{}}`)))

	_, err := r.Read()
	require.ErrorIs(t, err, io.ErrUnexpectedEOF)
	_, err = r.Read()
	require.ErrorIs(t, err, io.EOF)
}

func TestReader_CapsLineLength(t *testing.T) {
	line := []byte(`{"v":1,"kind":"Intent","type":"Hello","data":{"client_version":"` +
		strings.Repeat("x", 300) + `"}}` + "\n")
	r := NewReaderSize(bytes.NewReader(bytes.Repeat(line, 2)), 128)

	_, err := r.Read()
	require.ErrorIs(t, err, ErrLineTooLong)
	// The stream is now mid-line: the error is sticky rather than a resync.
	_, err = r.Read()
	require.ErrorIs(t, err, ErrLineTooLong)
}

func TestReader_LineAtCapIsAccepted(t *testing.T) {
	line := []byte(`{"v":1,"kind":"Intent","type":"Hello","data":{}}` + "\n")
	r := NewReaderSize(bytes.NewReader(line), len(line))

	env, err := r.Read()
	require.NoError(t, err)
	require.Equal(t, IntentHello, env.Type)
}

func TestNewReader_DefaultCapIsMaxLineBytes(t *testing.T) {
	require.Equal(t, MaxLineBytes, NewReader(bytes.NewReader(nil)).max)
	require.Equal(t, MaxLineBytes, NewReaderSize(bytes.NewReader(nil), 0).max)
}

// timeoutReader returns its first part, then a timeout, then the rest, the
// way a net.Conn with a read deadline does when a line arrives in two parts.
type timeoutReader struct {
	parts [][]byte
	n     int
}

type timeoutErr struct{}

func (timeoutErr) Error() string   { return "i/o timeout" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return true }

func (t *timeoutReader) Read(p []byte) (int, error) {
	t.n++
	switch {
	case t.n == 2:
		return 0, timeoutErr{}
	case len(t.parts) == 0:
		return 0, io.EOF
	}
	n := copy(p, t.parts[0])
	t.parts = t.parts[1:]
	return n, nil
}

func TestReader_KeepsPartialLineAcrossTimeout(t *testing.T) {
	line := `{"v":1,"kind":"Intent","type":"Hello","data":{}}` + "\n"
	r := NewReader(&timeoutReader{parts: [][]byte{[]byte(line[:10]), []byte(line[10:])}})

	_, err := r.Read()
	var te timeoutErr
	require.ErrorAs(t, err, &te)
	env, err := r.Read()
	require.NoError(t, err, "bytes read before the timeout must not be lost")
	require.Equal(t, IntentHello, env.Type)
}

func TestEncodeEvent_MatchesWriter(t *testing.T) {
	ev := ErrorEvent{ID: "7", Code: ErrCodeBadIntent, Message: "m"}
	var buf bytes.Buffer
	require.NoError(t, NewWriter(&buf).WriteEvent(EventError, "7", ev))
	line, err := EncodeEvent(EventError, "7", ev)
	require.NoError(t, err)
	require.Equal(t, buf.Bytes(), line)
}

func TestEncodeEvent_UnencodablePayload(t *testing.T) {
	_, err := EncodeEvent(EventError, "", make(chan int))
	require.Error(t, err)
}
