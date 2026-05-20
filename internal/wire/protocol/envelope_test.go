package protocol

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEnvelope_MarshalRoundTrip(t *testing.T) {
	e := Envelope{V: 1, Kind: KindIntent, Type: "Hello", ID: "abc", Data: json.RawMessage(`{"client_version":"manual"}`)}
	b, err := json.Marshal(e)
	require.NoError(t, err)

	var got Envelope
	require.NoError(t, json.Unmarshal(b, &got))
	require.Equal(t, e.V, got.V)
	require.Equal(t, e.Kind, got.Kind)
	require.Equal(t, e.Type, got.Type)
	require.Equal(t, e.ID, got.ID)
	require.JSONEq(t, `{"client_version":"manual"}`, string(got.Data))
}

func TestEnvelope_OmitsEmptyID(t *testing.T) {
	e := Envelope{V: 1, Kind: KindEvent, Type: "Snapshot", Data: json.RawMessage(`{}`)}
	b, err := json.Marshal(e)
	require.NoError(t, err)
	require.NotContains(t, string(b), `"id"`)
}
