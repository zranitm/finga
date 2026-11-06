package wsconn

import (
	"fmt"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// Format is how a ProtoCodec writes frames.
type Format int

// The formats.
const (
	Binary Format = iota // the protobuf wire format, in binary frames
	JSON                 // protojson with the .proto field names, in text frames
)

func (f Format) String() string {
	if f == JSON {
		return "json"
	}
	return "binary"
}

// ProtoCodec reads one envelope and writes another: a oneof body named by Message type, and optional seq, req, tick.
type ProtoCodec struct {
	in, out protoEnvelope
	format  Format
}

type protoEnvelope struct {
	typ            protoreflect.MessageType
	body           protoreflect.OneofDescriptor
	seq, req, tick protoreflect.FieldDescriptor // nil when the envelope has none
	byName         map[string]protoreflect.FieldDescriptor
}

// NewProtoCodec decodes in and encodes out; an envelope without a oneof body panics.
func NewProtoCodec(in, out proto.Message, f Format) *ProtoCodec {
	return &ProtoCodec{in: newEnvelope(in), out: newEnvelope(out), format: f}
}

func newEnvelope(m proto.Message) protoEnvelope {
	d := m.ProtoReflect().Descriptor()
	body := d.Oneofs().ByName("body")
	if body == nil {
		panic(fmt.Sprintf("wsconn: %s has no oneof body", d.FullName()))
	}
	e := protoEnvelope{typ: m.ProtoReflect().Type(), body: body, byName: map[string]protoreflect.FieldDescriptor{}}
	e.seq, e.req, e.tick = d.Fields().ByName("seq"), d.Fields().ByName("req"), d.Fields().ByName("tick")
	for i := range body.Fields().Len() {
		f := body.Fields().Get(i)
		name := string(f.Message().Name())
		if _, dup := e.byName[name]; dup {
			panic(fmt.Sprintf("wsconn: %s carries %s twice", d.FullName(), name))
		}
		e.byName[name] = f
	}
	return e
}

// Binary reports whether frames are binary.
func (c *ProtoCodec) Binary() bool { return c.format == Binary }

// Encode wraps m.Data, a message of the out envelope's body or a Spliced one, with m's envelope fields.
func (c *ProtoCodec) Encode(m Message) ([]byte, error) {
	if s, ok := m.Data.(Spliced); ok {
		return c.encodeSpliced(m, s)
	}
	data, ok := m.Data.(proto.Message)
	if !ok || data == nil {
		return nil, fmt.Errorf("%w: %T is not a proto message", ErrUnknownType, m.Data)
	}
	env, field, err := c.envelope(m, data)
	if err != nil {
		return nil, err
	}
	env.Set(field, protoreflect.ValueOfMessage(data.ProtoReflect()))
	if c.format == JSON {
		return protojson.MarshalOptions{UseProtoNames: true}.Marshal(env.Interface())
	}
	return proto.Marshal(env.Interface())
}

// encodeSpliced writes the envelope, then the body field: the shared bytes and the message's own after them.
func (c *ProtoCodec) encodeSpliced(m Message, s Spliced) ([]byte, error) {
	if s.Shared == nil || s.Own == nil || s.Shared.msg.ProtoReflect().Descriptor().FullName() != s.Own.ProtoReflect().Descriptor().FullName() {
		return nil, fmt.Errorf("%w: a spliced body needs a shared and an own part of one type", ErrUnknownType)
	}
	if c.format == JSON {
		m.Data = s.Message()
		return c.Encode(m)
	}
	env, field, err := c.envelope(m, s.Own)
	if err != nil {
		return nil, err
	}
	shared, err := s.Shared.bytes()
	if err != nil {
		return nil, err
	}
	own, err := proto.Marshal(s.Own)
	if err != nil {
		return nil, err
	}
	b, err := proto.Marshal(env.Interface())
	if err != nil {
		return nil, err
	}
	b = protowire.AppendTag(b, field.Number(), protowire.BytesType)
	b = protowire.AppendVarint(b, uint64(len(shared)+len(own)))
	return append(append(b, shared...), own...), nil
}

// envelope is a new out envelope holding m's seq, req and tick, and the body field data's type goes in.
func (c *ProtoCodec) envelope(m Message, data proto.Message) (protoreflect.Message, protoreflect.FieldDescriptor, error) {
	d := data.ProtoReflect().Descriptor()
	name := string(d.Name())
	field, ok := c.out.byName[name]
	if !ok || field.Message().FullName() != d.FullName() || (m.Type != "" && m.Type != name) {
		return nil, nil, fmt.Errorf("%w: %s as %q", ErrUnknownType, d.FullName(), m.Type)
	}
	env := c.out.typ.New()
	for _, set := range []struct {
		field protoreflect.FieldDescriptor
		name  string
		value protoreflect.Value
		zero  bool
	}{
		{c.out.seq, "seq", unsigned(c.out.seq, uint64(m.Seq)), m.Seq == 0},
		{c.out.req, "req", protoreflect.ValueOfString(m.ReqID), m.ReqID == ""},
		{c.out.tick, "tick", unsigned(c.out.tick, m.Tick), m.Tick == 0},
	} {
		if set.zero {
			continue
		}
		if set.field == nil {
			return nil, nil, fmt.Errorf("wsconn: %s has no %s field", env.Descriptor().FullName(), set.name)
		}
		env.Set(set.field, set.value)
	}
	return env, field, nil
}

// unsigned is v as the field's kind; a 32-bit field keeps the low 32 bits.
func unsigned(f protoreflect.FieldDescriptor, v uint64) protoreflect.Value {
	if f != nil && f.Kind() == protoreflect.Uint32Kind {
		return protoreflect.ValueOfUint32(uint32(v))
	}
	return protoreflect.ValueOfUint64(v)
}

// Decode unwraps a frame of the in envelope; unknown fields are ignored.
func (c *ProtoCodec) Decode(b []byte) (Message, error) {
	env := c.in.typ.New()
	var err error
	if c.format == JSON {
		err = protojson.UnmarshalOptions{DiscardUnknown: true}.Unmarshal(b, env.Interface())
	} else {
		err = proto.Unmarshal(b, env.Interface())
	}
	if err != nil {
		return Message{}, fmt.Errorf("%w: %w", ErrMalformed, err)
	}
	field := env.WhichOneof(c.in.body)
	if field == nil {
		return Message{}, fmt.Errorf("%w: no body", ErrUnknownType)
	}
	m := Message{Type: string(field.Message().Name()), Data: env.Get(field).Message().Interface()}
	if c.in.seq != nil {
		m.Seq = uint32(env.Get(c.in.seq).Uint())
	}
	if c.in.req != nil {
		m.ReqID = env.Get(c.in.req).String()
	}
	if c.in.tick != nil {
		m.Tick = env.Get(c.in.tick).Uint()
	}
	return m, nil
}
