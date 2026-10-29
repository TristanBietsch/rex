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
	require.ErrorIs(t, err, ErrUnsupportedVersion)
}

func TestReader_BadJSONIsBadEnvelope(t *testing.T) {
	_, err := NewReader(bytes.NewBufferString("not json\n")).Read()
	require.ErrorIs(t, err, ErrBadEnvelope)
	require.ErrorContains(t, err, "decode envelope")
}

// A limited reader must refuse an oversize line without buffering all of
// it; an unlimited one (clients reading big Snapshots) reads it whole.
func TestReader_LineLimit(t *testing.T) {
	big, err := json.Marshal(Envelope{V: ProtocolVersion, Kind: KindIntent, Type: "SendInput",
		Data: json.RawMessage(`"` + strings.Repeat("a", 3<<20) + `"`)})
	require.NoError(t, err)
	big = append(big, '\n')

	_, err = NewLimitedReader(bytes.NewReader(big), 1<<20).Read()
	require.ErrorIs(t, err, ErrLineTooLong)

	env, err := NewReader(bytes.NewReader(big)).Read()
	require.NoError(t, err)
	require.Equal(t, "SendInput", env.Type)

	// Lines under the limit pass, across several buffer fills.
	var buf bytes.Buffer
	w := NewWriter(&buf)
	require.NoError(t, w.WriteIntent(IntentReply, "r", Reply{SessionID: "s", Text: strings.Repeat("b", 200<<10)}))
	require.NoError(t, w.WriteIntent(IntentHello, "h", Hello{}))
	r := NewLimitedReader(&buf, 1<<20)
	env, err = r.Read()
	require.NoError(t, err)
	require.Equal(t, IntentReply, env.Type)
	env, err = r.Read()
	require.NoError(t, err)
	require.Equal(t, IntentHello, env.Type)
}
