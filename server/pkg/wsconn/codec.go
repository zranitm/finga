// Package wsconn is a WebSocket session layer with origin checks, pings and a bounded send queue.
package wsconn

import "errors"

// Message is one decoded frame.
type Message struct {
	Type  string
	Seq   uint32
	ReqID string
	Tick  uint64
	Data  any // the message itself, as the codec's types
}

// Codec encodes and decodes frames.
type Codec interface {
	Encode(Message) ([]byte, error)
	Decode([]byte) (Message, error)
	// Binary reports whether frames are binary, else text.
	Binary() bool
}

// The codec's errors.
var (
	ErrUnknownType = errors.New("wsconn: unknown message type")
	ErrMalformed   = errors.New("wsconn: malformed message")
)
