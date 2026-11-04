package protocol

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// MaxLineBytes caps one JSONL line, newline included. The largest legitimate
// line is a transcript replay (256 KiB of output, base64-encoded in JSON);
// 16 MiB leaves ample headroom while bounding how much memory one peer can
// make the other buffer.
const MaxLineBytes = 16 << 20

// readBufSize is the Reader's read buffer; longer lines are assembled from
// several buffer fills.
const readBufSize = 64 * 1024

// Errors returned by Reader.Read. Each is wrapped with detail; match with
// errors.Is.
var (
	// ErrLineTooLong reports a line longer than the Reader's cap. The stream
	// is then mid-line, so the error is sticky: every later Read returns it
	// too, and the caller should close the connection.
	ErrLineTooLong = errors.New("protocol: line too long")
	// ErrMalformed reports a complete line that is not a JSON envelope. The
	// line was consumed whole, so the next Read starts on the next line.
	ErrMalformed = errors.New("protocol: malformed envelope")
	// ErrUnsupportedVersion reports an envelope whose "v" is not
	// ProtocolVersion. The line was consumed whole.
	ErrUnsupportedVersion = errors.New("protocol: unsupported protocol version")
)

// Reader reads newline-delimited Envelopes from an io.Reader. It is not safe
// for concurrent use.
type Reader struct {
	br  *bufio.Reader
	max int
	// pending holds the start of a line whose read was cut short by a
	// transport error (typically a read-deadline timeout), so the next Read
	// resumes it instead of losing those bytes and desyncing the stream.
	pending []byte
	// err is the sticky ErrLineTooLong once the cap was exceeded.
	err error
}

// NewReader wraps r with the default MaxLineBytes cap.
func NewReader(r io.Reader) *Reader {
	return NewReaderSize(r, MaxLineBytes)
}

// NewReaderSize wraps r, capping each line (newline included) at maxLine
// bytes. maxLine <= 0 means MaxLineBytes.
func NewReaderSize(r io.Reader, maxLine int) *Reader {
	if maxLine <= 0 {
		maxLine = MaxLineBytes
	}
	// The buffer never exceeds the cap, so an over-cap prefix fills it and
	// trips the check without waiting for a newline or more input.
	return &Reader{br: bufio.NewReaderSize(r, min(readBufSize, maxLine)), max: maxLine}
}

// Read returns the next envelope. At a clean end of stream it returns io.EOF;
// a stream that ends inside a line returns io.ErrUnexpectedEOF. Bad lines
// return ErrLineTooLong (sticky), ErrMalformed or ErrUnsupportedVersion.
// Transport errors are wrapped as-is; after a timeout, Read may be called
// again and resumes the interrupted line.
func (r *Reader) Read() (Envelope, error) {
	if r.err != nil {
		return Envelope{}, r.err
	}
	line, err := r.readLine()
	if err != nil {
		if errors.Is(err, ErrLineTooLong) {
			r.err = err
		}
		return Envelope{}, err
	}
	var e Envelope
	if err := json.Unmarshal(line, &e); err != nil {
		return Envelope{}, fmt.Errorf("%w: %w", ErrMalformed, err)
	}
	if e.V != ProtocolVersion {
		return Envelope{}, fmt.Errorf("%w %d (want %d)", ErrUnsupportedVersion, e.V, ProtocolVersion)
	}
	return e, nil
}

// readLine returns one line including its '\n'. The result may alias the
// bufio buffer, so it is only valid until the next read.
func (r *Reader) readLine() ([]byte, error) {
	for {
		frag, err := r.br.ReadSlice('\n')
		if len(r.pending)+len(frag) > r.max {
			r.pending = nil
			return nil, fmt.Errorf("%w (limit %d bytes)", ErrLineTooLong, r.max)
		}
		if err == nil && len(r.pending) == 0 {
			return frag, nil // common case: the whole line was buffered
		}
		r.pending = append(r.pending, frag...)
		switch {
		case err == nil:
			line := r.pending
			r.pending = nil
			return line, nil
		case errors.Is(err, bufio.ErrBufferFull):
			continue
		case errors.Is(err, io.EOF):
			if len(r.pending) == 0 {
				return nil, io.EOF
			}
			r.pending = nil
			return nil, fmt.Errorf("read line: %w", io.ErrUnexpectedEOF)
		default:
			return nil, fmt.Errorf("read line: %w", err)
		}
	}
}

// Writer writes Envelopes as JSONL. It is not safe for concurrent use.
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

// EncodeEvent renders an Event envelope as one newline-terminated JSONL
// line: the exact bytes Writer.WriteEvent sends. The daemon encodes up front
// so a connection's writer goroutine only copies bytes to the socket.
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
