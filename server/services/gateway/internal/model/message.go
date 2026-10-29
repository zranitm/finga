package model

// Message is one message to or from a client: a client protocol message
// (Data, a value from internal/protocol) with its envelope.
type Message struct {
	Type  string
	Seq   uint32 // the client's input sequence, optional
	ReqID string // request and response correlation, optional
	Tick  uint64 // the server tick, optional
	Data  any
}

// CloseCode says why the gateway ends a connection.
type CloseCode int

// The ways the gateway ends a connection.
const (
	// CloseKicked ends a session on purpose: a Kicked message went first, or
	// the client broke the handshake.
	CloseKicked CloseCode = iota + 1
	// CloseInternal is a failure the client cannot fix but may retry: it
	// reconnects.
	CloseInternal
)
