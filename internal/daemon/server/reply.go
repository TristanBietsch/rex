package server

import (
	"errors"
	"log/slog"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// Terminal replies (see the protocol package doc for the full table).
//
// Handlers never send an Ack or an Error for their own intent. They return
// an outcome and an error, and dispatch turns that pair into the intent's
// single terminal reply:
//
//   - a non-nil error becomes an Error event with the intent's ID (sent even
//     when the ID is empty, as before acks existed), its code taken from an
//     *intentError and internal_error for any other error;
//   - otherwise, when the handler already sent the terminal event itself
//     (outcome.replied, Hello's Snapshot), nothing more is sent;
//   - otherwise, when the intent carried an ID, an Ack is sent.
//
// Because every path through dispatch ends in exactly one of these, an
// intent with an ID gets exactly one terminal reply, and one without an ID
// gets an Error only on failure.

// outcome is what a handler that succeeded reports to dispatch.
type outcome struct {
	// sessionID is echoed in the Ack: the session the intent acted on or,
	// for NewSession, created.
	sessionID string
	// replied is set when the handler already sent the intent's terminal
	// event (Hello's Snapshot, Shutdown's early Ack); dispatch then sends
	// nothing more.
	replied bool
}

// intentError is a handler failure with the protocol error code it is
// reported under. Its message is the wrapped error's, unchanged.
type intentError struct {
	code string
	err  error
}

func (e *intentError) Error() string { return e.err.Error() }
func (e *intentError) Unwrap() error { return e.err }

// replyErr tags err with an Error code for dispatch.
func replyErr(code string, err error) error {
	return &intentError{code: code, err: err}
}

// replyMsg is replyErr for a fixed message.
func replyMsg(code, msg string) error {
	return replyErr(code, errors.New(msg))
}

// errorCode is the Error code dispatch reports err under. An error without
// one is a handler bug, reported as internal_error.
func errorCode(err error) string {
	var ie *intentError
	if errors.As(err, &ie) {
		return ie.code
	}
	return protocol.ErrCodeInternal
}

// reply sends the terminal reply for env given its handler's result.
func (c *client) reply(env protocol.Envelope, res outcome, err error) {
	switch {
	case err != nil:
		code := errorCode(err)
		if code == protocol.ErrCodeInternal {
			slog.Error("server: intent failed without an error code", "conn", c.out.id,
				"type", env.Type, "req_id", env.ID, "err", err)
		}
		c.sendError(env.ID, code, err.Error())
	case res.replied:
	default:
		c.ack(env, res.sessionID)
	}
}

// ack sends the Ack for env when it carried an ID.
func (c *client) ack(env protocol.Envelope, sessionID string) {
	if env.ID == "" {
		return
	}
	c.sendEvent(protocol.EventAck, env.ID, protocol.Ack{Intent: env.Type, SessionID: sessionID})
}
