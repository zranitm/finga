// Package wsconn is a WebSocket session layer with origin checks, pings and a bounded send queue.
package wsconn

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
)

// Message is one decoded frame.
type Message struct {
	Type  string
	Seq   uint32
	ReqID string
	Tick  uint64
	Data  any // concrete message struct, resolved through the registry
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

type envelope struct {
	Type  string          `json:"t"`
	ReqID string          `json:"req,omitempty"`
	Seq   uint32          `json:"seq,omitempty"`
	Tick  uint64          `json:"tick,omitempty"`
	Data  json.RawMessage `json:"d,omitempty"`
}

// JSONCodec is a JSON Codec over a registry of named message types.
type JSONCodec struct {
	types map[string]reflect.Type
	names map[reflect.Type]string
}

// NewJSONCodec is an empty registry.
func NewJSONCodec() *JSONCodec {
	return &JSONCodec{types: map[string]reflect.Type{}, names: map[reflect.Type]string{}}
}

// Register binds name to zero's struct type; a duplicate name or type panics.
func (c *JSONCodec) Register(name string, zero any) {
	t := reflect.TypeOf(zero)
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if _, dup := c.types[name]; dup {
		panic(fmt.Sprintf("wsconn: message type %q registered twice", name))
	}
	if other, dup := c.names[t]; dup {
		panic(fmt.Sprintf("wsconn: type %s registered as both %q and %q", t, other, name))
	}
	c.types[name] = t
	c.names[t] = name
}

// Binary is false: JSON goes in text frames.
func (c *JSONCodec) Binary() bool { return false }

// Types lists the registered names, sorted.
func (c *JSONCodec) Types() []string {
	out := make([]string, 0, len(c.types))
	for name := range c.types {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// Encode writes the envelope, inferring Type from Data when unset; nil Data encodes as {}.
func (c *JSONCodec) Encode(m Message) ([]byte, error) {
	name := m.Type
	if m.Data != nil {
		t := reflect.TypeOf(m.Data)
		for t.Kind() == reflect.Pointer {
			t = t.Elem()
		}
		registered, ok := c.names[t]
		if !ok || (name != "" && name != registered) {
			return nil, fmt.Errorf("%w: %s as %q", ErrUnknownType, t, name)
		}
		name = registered
	} else if _, ok := c.types[name]; !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnknownType, name)
	}
	data := []byte("{}")
	if m.Data != nil {
		var err error
		if data, err = json.Marshal(m.Data); err != nil {
			return nil, fmt.Errorf("wsconn: encode %s: %w", name, err)
		}
	}
	return json.Marshal(envelope{Type: name, ReqID: m.ReqID, Seq: m.Seq, Tick: m.Tick, Data: data})
}

// Decode yields Data as a value of the registered type; unknown fields are ignored.
func (c *JSONCodec) Decode(b []byte) (Message, error) {
	var env envelope
	if err := json.Unmarshal(b, &env); err != nil {
		return Message{}, fmt.Errorf("%w: %w", ErrMalformed, err)
	}
	t, ok := c.types[env.Type]
	if !ok {
		return Message{}, fmt.Errorf("%w: %q", ErrUnknownType, env.Type)
	}
	v := reflect.New(t)
	if len(env.Data) > 0 {
		if err := json.Unmarshal(env.Data, v.Interface()); err != nil {
			return Message{}, fmt.Errorf("%w: %s: %w", ErrMalformed, env.Type, err)
		}
	}
	return Message{Type: env.Type, Seq: env.Seq, ReqID: env.ReqID, Tick: env.Tick, Data: v.Elem().Interface()}, nil
}
