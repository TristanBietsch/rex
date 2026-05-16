// Package client is a Go SDK for rex-daemon's UDS protocol.
package client

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"time"

	"github.com/tristanbietsch/rex/internal/protocol"
)

// Client wraps a UDS connection with reader/writer helpers.
type Client struct {
	conn net.Conn
	r    *protocol.Reader
	w    *protocol.Writer
}

// Dial opens a UDS connection.
func Dial(socket string) (*Client, error) {
	conn, err := net.Dial("unix", socket)
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", socket, err)
	}
	return &Client{
		conn: conn,
		r:    protocol.NewReader(conn),
		w:    protocol.NewWriter(conn),
	}, nil
}

// Close drops the connection.
func (c *Client) Close() error { return c.conn.Close() }

// SetReadDeadline forwards to the underlying conn.
func (c *Client) SetReadDeadline(t time.Time) error { return c.conn.SetReadDeadline(t) }

// Hello sends a Hello intent and decodes the Snapshot response.
func (c *Client) Hello(clientVersion string) (*protocol.Snapshot, error) {
	if err := c.w.WriteIntent(protocol.IntentHello, "h", protocol.Hello{ClientVersion: clientVersion}); err != nil {
		return nil, err
	}
	env, err := c.r.Read()
	if err != nil {
		return nil, err
	}
	if env.Kind != protocol.KindEvent || env.Type != protocol.EventSnapshot {
		return nil, fmt.Errorf("expected Snapshot, got %s/%s", env.Kind, env.Type)
	}
	var snap protocol.Snapshot
	if err := json.Unmarshal(env.Data, &snap); err != nil {
		return nil, fmt.Errorf("decode snapshot: %w", err)
	}
	return &snap, nil
}

// Subscribe pins this connection to a session's output stream. Use sessionID="" for board-wide only.
func (c *Client) Subscribe(sessionID string) error {
	return c.w.WriteIntent(protocol.IntentSubscribe, "", protocol.Subscribe{SessionID: sessionID})
}

// SubscribeReplay subscribes and also requests the recent transcript tail as a SessionOutput event.
func (c *Client) SubscribeReplay(sessionID string) error {
	return c.w.WriteIntent(protocol.IntentSubscribe, "", protocol.Subscribe{SessionID: sessionID, Replay: true})
}

// Resize tells the daemon to resize the session's PTY.
func (c *Client) Resize(sessionID string, cols, rows uint16) error {
	return c.w.WriteIntent(protocol.IntentResize, "", protocol.Resize{SessionID: sessionID, Cols: cols, Rows: rows})
}

// NewSession submits a NewSession intent.
func (c *Client) NewSession(req protocol.NewSession) error {
	return c.w.WriteIntent(protocol.IntentNewSession, "", req)
}

// SendInput forwards raw bytes to the session's PTY.
func (c *Client) SendInput(sessionID string, b []byte) error {
	return c.w.WriteIntent(protocol.IntentSendInput, "", protocol.SendInput{SessionID: sessionID, Bytes: b})
}

// Reply sends an inline text reply.
func (c *Client) Reply(sessionID, text string) error {
	return c.w.WriteIntent(protocol.IntentReply, "", protocol.Reply{SessionID: sessionID, Text: text})
}

// Rename changes a session's slug or title.
func (c *Client) Rename(sessionID, slug, title string) error {
	return c.w.WriteIntent(protocol.IntentRename, "", protocol.Rename{SessionID: sessionID, Slug: slug, Title: title})
}

// Delete removes a session.
func (c *Client) Delete(sessionID string) error {
	return c.w.WriteIntent(protocol.IntentDelete, "", protocol.Delete{SessionID: sessionID})
}

// Complete asks the daemon to cleanly terminate a session and mark it done.
func (c *Client) Complete(sessionID string) error {
	return c.w.WriteIntent(protocol.IntentComplete, "", protocol.Complete{SessionID: sessionID})
}

// FocusFilter sets the cosmetic tool filter.
func (c *Client) FocusFilter(toolID string) error {
	return c.w.WriteIntent(protocol.IntentFocusFilter, "", protocol.FocusFilter{ToolID: toolID})
}

// SetMaxConcurrent updates the daemon's concurrent-session cap. n <= 0 means uncapped.
func (c *Client) SetMaxConcurrent(n int) error {
	return c.w.WriteIntent(protocol.IntentSetMaxConcurrent, "", protocol.SetMaxConcurrent{N: n})
}

// SetSessionFleet updates the fleet label of an existing session.
// An empty fleet string clears the fleet.
func (c *Client) SetSessionFleet(sessionID, fleet string) error {
	return c.w.WriteIntent(protocol.IntentSetSessionFleet, "", protocol.SetSessionFleet{SessionID: sessionID, Fleet: fleet})
}

// NextEvent reads one envelope from the stream.
func (c *Client) NextEvent() (protocol.Envelope, error) {
	return c.r.Read()
}

// Drain reads events until EOF or the handler returns false.
func (c *Client) Drain(handler func(protocol.Envelope) bool) error {
	for {
		env, err := c.r.Read()
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		if !handler(env) {
			return nil
		}
	}
}
