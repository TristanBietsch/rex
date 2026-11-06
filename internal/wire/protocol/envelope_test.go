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

// The ack fields are additive: older peers see the shapes they always did.
func TestAckAndVersionFields_WireShape(t *testing.T) {
	b, err := EncodeEvent(EventAck, "r1", Ack{Intent: IntentRename, SessionID: "s1"})
	require.NoError(t, err)
	require.JSONEq(t, `{"v":1,"kind":"Event","type":"Ack","id":"r1","data":{"intent":"Rename","session_id":"s1"}}`, string(b))

	b, err = json.Marshal(Ack{Intent: IntentReload})
	require.NoError(t, err)
	require.JSONEq(t, `{"intent":"Reload"}`, string(b))

	b, err = json.Marshal(Snapshot{Sessions: []SessionSummary{}, Filter: "all"})
	require.NoError(t, err)
	require.JSONEq(t, `{"sessions":[],"filter":"all"}`, string(b), "no daemon_version unless set")

	b, err = json.Marshal(Hello{ClientVersion: "v1"})
	require.NoError(t, err)
	require.JSONEq(t, `{"client_version":"v1"}`, string(b), "no client unless set")
}
