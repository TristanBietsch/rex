package protocol

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
)

// Reader reads newline-delimited Envelopes from an io.Reader.
type Reader struct {
	br *bufio.Reader
}

// NewReader wraps r.
func NewReader(r io.Reader) *Reader {
	return &Reader{br: bufio.NewReaderSize(r, 64*1024)}
}

// Read returns the next envelope or io.EOF.
func (r *Reader) Read() (Envelope, error) {
	line, err := r.br.ReadBytes('\n')
	if err == io.EOF && len(line) == 0 {
		return Envelope{}, io.EOF
	}
	if err != nil && err != io.EOF {
		return Envelope{}, fmt.Errorf("read line: %w", err)
	}
	var e Envelope
	if err := json.Unmarshal(line, &e); err != nil {
		return Envelope{}, fmt.Errorf("decode envelope: %w", err)
	}
	if e.V != ProtocolVersion {
		return Envelope{}, fmt.Errorf("unsupported protocol version %d (want %d)", e.V, ProtocolVersion)
	}
	return e, nil
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
		return fmt.Errorf("marshal envelope: %w", err)
	}
	b = append(b, '\n')
	if _, err := w.w.Write(b); err != nil {
		return fmt.Errorf("write envelope: %w", err)
	}
	return nil
}
