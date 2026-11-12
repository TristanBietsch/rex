package tui

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// chattyEvents is an event source that always has more output.
type chattyEvents struct{ id string }

func (c chattyEvents) NextEvent() (protocol.Envelope, error) {
	raw, _ := json.Marshal(protocol.SessionOutput{SessionID: c.id, Bytes: []byte("x")})
	return protocol.Envelope{Type: protocol.EventSessionOutput, Data: raw}, nil
}

func newTestAttachState(id string) *AttachState {
	return &AttachState{
		SessionID: id,
		outCh:     make(chan AttachOutputMsg, 64),
		errCh:     make(chan AttachClosedMsg, 1),
		done:      make(chan struct{}),
	}
}

// After the popup closes nobody drains outCh: the pump of a chatty agent
// must still return instead of blocking on a full channel forever.
func TestAttachPump_StopsWhenPopupCloses(t *testing.T) {
	st := newTestAttachState("a")
	exited := make(chan struct{})
	go func() {
		pumpAttach(chattyEvents{id: "a"}, "a", nil, st)
		close(exited)
	}()
	require.Eventually(t, func() bool { return len(st.outCh) == cap(st.outCh) }, time.Second, time.Millisecond)
	m := Model{Attach: st, Focus: FocusAttach}
	_ = closeAttach(m)
	select {
	case <-exited:
	case <-time.After(2 * time.Second):
		t.Fatal("pump still blocked after the popup closed")
	}
}

// A listener left waiting on a closed popup returns.
func TestAttachListener_ReturnsWhenPopupCloses(t *testing.T) {
	st := newTestAttachState("a")
	got := make(chan any, 1)
	go func() { got <- listenAttach(st)() }()
	_ = closeAttach(Model{Attach: st, Focus: FocusAttach})
	select {
	case msg := <-got:
		require.Nil(t, msg)
	case <-time.After(2 * time.Second):
		t.Fatal("listener still blocked after the popup closed")
	}
}

// Messages from an earlier popup on the same session neither re-arm a
// listener on the new popup nor end it.
func TestAttach_StaleListenerMessagesIgnored(t *testing.T) {
	old := newTestAttachState("a")
	cur := newTestAttachState("a")
	m := attachedModel()
	m.Attach = cur

	out, cmd := m.Update(AttachOutputMsg{SessionID: "a", Bytes: []byte("x"), from: old})
	require.Nil(t, cmd, "no second listener on the open popup")
	m = out.(Model)

	out, _ = m.Update(AttachClosedMsg{SessionID: "a", Err: errors.New("closed"), from: old})
	m = out.(Model)
	require.False(t, m.Attach.Ended, "an old popup's close does not end the new one")
}
