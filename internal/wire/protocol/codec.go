package protocol

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// MaxIntentLineBytes caps one line the daemon reads from a client. Intents
// are small; the largest are pastes and initial prompts.
const MaxIntentLineBytes = 1 << 20

// Errors from Reader.Read for a line that arrived but can't be used. The
// sender is still listening, so a daemon can say why before hanging up.
var (
	ErrBadEnvelope        = errors.New("decode envelope")
	ErrUnsupportedVersion = errors.New("unsupported protocol version")
	ErrLineTooLong        = errors.New("line too long")
)

// Reader reads newline-delimited Envelopes from an io.Reader.
type Reader struct {
	br  *bufio.Reader
	max int // max line length in bytes; 0 = unlimited
}

// NewReader wraps r. Lines may be any length (daemon events such as a
// Snapshot grow with the board).
func NewReader(r io.Reader) *Reader {
	return &Reader{br: bufio.NewReaderSize(r, 64*1024)}
}

// NewLimitedReader is NewReader with lines capped at max bytes: a longer
// line fails with ErrLineTooLong after reading at most max bytes of it, and
// the stream is no longer in sync.
func NewLimitedReader(r io.Reader, max int) *Reader {
	return &Reader{br: bufio.NewReaderSize(r, 64*1024), max: max}
}

// Read returns the next envelope or io.EOF.
func (r *Reader) Read() (Envelope, error) {
	line, err := r.readLine()
	if errors.Is(err, ErrLineTooLong) {
		return Envelope{}, err
	}
	if err == io.EOF && len(line) == 0 {
		return Envelope{}, io.EOF
	}
	if err != nil && err != io.EOF {
		return Envelope{}, fmt.Errorf("read line: %w", err)
	}
	var e Envelope
	if err := json.Unmarshal(line, &e); err != nil {
		return Envelope{}, fmt.Errorf("%w: %w", ErrBadEnvelope, err)
	}
	if e.V != ProtocolVersion {
		return Envelope{}, fmt.Errorf("%w %d (want %d)", ErrUnsupportedVersion, e.V, ProtocolVersion)
	}
	return e, nil
}

func (r *Reader) readLine() ([]byte, error) {
	if r.max <= 0 {
		return r.br.ReadBytes('\n')
	}
	var line []byte
	for {
		frag, err := r.br.ReadSlice('\n')
		if len(line)+len(frag) > r.max {
			return nil, fmt.Errorf("%w (max %d bytes)", ErrLineTooLong, r.max)
		}
		line = append(line, frag...)
		if err != bufio.ErrBufferFull {
			return line, err
		}
	}
}

// Writer writes Envelopes as JSONL.
type Writer struct {
	w io.Writer
}

// NewWriter wraps w.
func NewWriter(w io.Writer) *Writer {
	return &Writer{w: w}
}

// WriteIntent writes an Intent envelope.
func (w *Writer) WriteIntent(typ, id string, payload any) error {
	return w.write(KindIntent, typ, id, payload)
}

// WriteEvent writes an Event envelope.
func (w *Writer) WriteEvent(typ, id string, payload any) error {
	return w.write(KindEvent, typ, id, payload)
}

func (w *Writer) write(kind Kind, typ, id string, payload any) error {
	b, err := encode(kind, typ, id, payload)
	if err != nil {
		return err
	}
	if _, err := w.w.Write(b); err != nil {
		return fmt.Errorf("write envelope: %w", err)
	}
	return nil
}

// EncodeEvent returns an Event envelope as one wire line (newline included),
// for senders that queue lines instead of writing them directly.
func EncodeEvent(typ, id string, payload any) ([]byte, error) {
	return encode(KindEvent, typ, id, payload)
}

func encode(kind Kind, typ, id string, payload any) ([]byte, error) {
	// Single JSON encode: embedding payload as a typed field avoids a second
	// full marshal pass over the payload bytes.
	type wireLine struct {
		V    int    `json:"v"`
		Kind Kind   `json:"kind"`
		Type string `json:"type"`
		ID   string `json:"id,omitempty"`
		Data any    `json:"data"`
	}
	b, err := json.Marshal(wireLine{
		V: ProtocolVersion, Kind: kind, Type: typ, ID: id, Data: payload,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal envelope: %w", err)
	}
	return append(b, '\n'), nil
}
