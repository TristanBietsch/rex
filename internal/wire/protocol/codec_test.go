package protocol

import (
	"bytes"
	"encoding/json"
	"io"
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
