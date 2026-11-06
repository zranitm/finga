package wsconn

import (
	"sync"

	"google.golang.org/protobuf/proto"
)

// Shared is a body many messages carry; it is marshaled once, on first use, and must not change.
type Shared struct {
	msg  proto.Message
	once sync.Once
	wire []byte
	err  error
}

// NewShared shares m.
func NewShared(m proto.Message) *Shared { return &Shared{msg: m} }

func (s *Shared) bytes() ([]byte, error) {
	s.once.Do(func() { s.wire, s.err = proto.Marshal(s.msg) })
	return s.wire, s.err
}

// Spliced is a Shared body plus Own, of the same type with only this message's fields; protobuf reads both as one.
type Spliced struct {
	Shared *Shared
	Own    proto.Message
}

// Message is the body as one message.
func (s Spliced) Message() proto.Message {
	m := proto.Clone(s.Shared.msg)
	proto.Merge(m, s.Own)
	return m
}
