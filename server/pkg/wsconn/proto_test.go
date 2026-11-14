package wsconn_test

import (
	"encoding/json"
	"errors"
	"testing"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"

	clientv1 "finga/api/gen/client/v1"
	"finga/pkg/wsconn"
)

// The client protocol's envelopes stand in for any pair with a oneof named body.
func serverCodec(f wsconn.Format) *wsconn.ProtoCodec {
	return wsconn.NewProtoCodec(&clientv1.ClientMessage{}, &clientv1.ServerMessage{}, f)
}

func clientCodec(f wsconn.Format) *wsconn.ProtoCodec {
	return wsconn.NewProtoCodec(&clientv1.ServerMessage{}, &clientv1.ClientMessage{}, f)
}

func TestProtoCodecRoundTrips(t *testing.T) {
	for _, f := range []wsconn.Format{wsconn.Binary, wsconn.JSON} {
		server, client := serverCodec(f), clientCodec(f)
		t.Run(f.String(), func(t *testing.T) {
			if server.Binary() != (f == wsconn.Binary) {
				t.Fatalf("Binary() = %v", server.Binary())
			}
			in := wsconn.Message{Seq: 7, ReqID: "r-1", Data: &clientv1.Input{Right: true, Jump: true}}
			b, err := client.Encode(in)
			if err != nil {
				t.Fatal(err)
			}
			got, err := server.Decode(b)
			if err != nil {
				t.Fatal(err)
			}
			if got.Type != "Input" || got.Seq != 7 || got.ReqID != "r-1" || got.Tick != 0 || !proto.Equal(got.Data.(proto.Message), in.Data.(proto.Message)) {
				t.Fatalf("decoded %+v", got)
			}

			out := wsconn.Message{ReqID: "r-1", Tick: 81235, Data: &clientv1.Delta{AckSeq: 7, RemovedMobs: []uint32{6}}}
			b, err = server.Encode(out)
			if err != nil {
				t.Fatal(err)
			}
			back, err := client.Decode(b)
			if err != nil {
				t.Fatal(err)
			}
			if back.Type != "Delta" || back.Tick != 81235 || back.ReqID != "r-1" || !proto.Equal(back.Data.(proto.Message), out.Data.(proto.Message)) {
				t.Fatalf("decoded %+v", back)
			}
		})
	}
}

// A spliced body, shared bytes and a message's own fields after them, reads as the whole message.
func TestASplicedBodyReadsAsTheWholeMessage(t *testing.T) {
	shared := wsconn.NewShared(&clientv1.Delta{Players: []*clientv1.PlayerView{{Id: "p-1", X: 3}}, RemovedMobs: []uint32{6}})
	want := &clientv1.Delta{AckSeq: 7, Players: []*clientv1.PlayerView{{Id: "p-1", X: 3}}, RemovedMobs: []uint32{6}}
	for _, f := range []wsconn.Format{wsconn.Binary, wsconn.JSON} {
		t.Run(f.String(), func(t *testing.T) {
			for _, ack := range []uint32{7, 7} { // the second reuses the shared bytes
				b, err := serverCodec(f).Encode(wsconn.Message{ReqID: "r-2", Tick: 81235, Data: wsconn.Spliced{Shared: shared, Own: &clientv1.Delta{AckSeq: ack}}})
				if err != nil {
					t.Fatal(err)
				}
				back, err := clientCodec(f).Decode(b)
				if err != nil {
					t.Fatal(err)
				}
				if back.Type != "Delta" || back.ReqID != "r-2" || back.Tick != 81235 || !proto.Equal(back.Data.(proto.Message), want) {
					t.Fatalf("decoded %+v", back)
				}
			}
		})
	}
	t.Run("as one message", func(t *testing.T) {
		if got := (wsconn.Spliced{Shared: shared, Own: &clientv1.Delta{AckSeq: 7}}).Message(); !proto.Equal(got, want) {
			t.Fatalf("got %v", got)
		}
	})
	t.Run("a body the envelope does not carry", func(t *testing.T) {
		_, err := serverCodec(wsconn.Binary).Encode(wsconn.Message{Data: wsconn.Spliced{Shared: wsconn.NewShared(&clientv1.Input{}), Own: &clientv1.Input{}}})
		if !errors.Is(err, wsconn.ErrUnknownType) {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestProtoCodecWritesTheProtoFieldNames(t *testing.T) {
	b, err := serverCodec(wsconn.JSON).Encode(wsconn.Message{Tick: 3, Data: &clientv1.Delta{AckSeq: 117}})
	if err != nil {
		t.Fatal(err)
	}
	if canon(t, b) != `{"delta":{"ack_seq":117},"tick":3}` {
		t.Fatalf("encoded %s", b)
	}
}

func TestProtoCodecRefuses(t *testing.T) {
	server := serverCodec(wsconn.Binary)
	encode := []struct {
		name string
		msg  wsconn.Message
	}{
		{"a message the envelope does not carry", wsconn.Message{Data: &clientv1.Input{}}},
		{"a type that is not the data's", wsconn.Message{Type: "Pong", Data: &clientv1.Delta{}}},
		{"data that is not a proto message", wsconn.Message{Data: struct{}{}}},
		{"no data", wsconn.Message{Type: "Delta"}},
		{"a field the envelope lacks", wsconn.Message{Seq: 1, Data: &clientv1.Delta{}}},
		{"a message of another package with the same name", wsconn.Message{Data: otherDelta(t)}},
	}
	for _, tt := range encode {
		t.Run("encode: "+tt.name, func(t *testing.T) {
			if _, err := server.Encode(tt.msg); err == nil {
				t.Fatal("encoded")
			}
		})
	}
	t.Run("encode: unknown types are ErrUnknownType", func(t *testing.T) {
		if _, err := server.Encode(wsconn.Message{Data: &clientv1.Input{}}); !errors.Is(err, wsconn.ErrUnknownType) {
			t.Fatalf("err = %v", err)
		}
	})

	noBody, _ := proto.Marshal(&clientv1.ClientMessage{Seq: 3})
	decode := []struct {
		name  string
		codec *wsconn.ProtoCodec
		frame []byte
		want  error
	}{
		{"not the wire format", server, []byte{0xff, 0xff, 0xff}, wsconn.ErrMalformed},
		{"no body: empty, or one this side does not know", server, noBody, wsconn.ErrUnknownType},
		{"not JSON", serverCodec(wsconn.JSON), []byte(`{"input":`), wsconn.ErrMalformed},
		{"a JSON field of the wrong type", serverCodec(wsconn.JSON), []byte(`{"seq":"soon","input":{}}`), wsconn.ErrMalformed},
	}
	for _, tt := range decode {
		t.Run("decode: "+tt.name, func(t *testing.T) {
			if _, err := tt.codec.Decode(tt.frame); !errors.Is(err, tt.want) {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}
		})
	}
	t.Run("decode: unknown JSON fields are ignored, for forward compatibility", func(t *testing.T) {
		m, err := serverCodec(wsconn.JSON).Decode([]byte(`{"seq":2,"input":{"left":true,"sprint":true},"later":1}`))
		if err != nil || m.Type != "Input" || !m.Data.(*clientv1.Input).GetLeft() {
			t.Fatalf("%+v, %v", m, err)
		}
	})
}

func TestProtoCodecPanicsOnAnEnvelopeWithoutABody(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("no panic")
		}
	}()
	wsconn.NewProtoCodec(&clientv1.Input{}, &clientv1.ServerMessage{}, wsconn.Binary)
}

func canon(t *testing.T, b []byte) string {
	t.Helper()
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatalf("not JSON: %s", b)
	}
	out, _ := json.Marshal(v)
	return string(out)
}

// otherDelta is an empty message named Delta in a package other than the client's.
func otherDelta(t *testing.T) proto.Message {
	t.Helper()
	fd, err := protodesc.NewFile(&descriptorpb.FileDescriptorProto{
		Name:        new("other/v1/other.proto"),
		Package:     new("other.v1"),
		Syntax:      new("proto3"),
		MessageType: []*descriptorpb.DescriptorProto{{Name: new("Delta")}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	md := fd.Messages().ByName(protoreflect.Name("Delta"))
	if md.FullName() == (&clientv1.Delta{}).ProtoReflect().Descriptor().FullName() || md.Name() != "Delta" {
		t.Fatalf("the stand-in is %s", md.FullName())
	}
	return dynamicpb.NewMessage(md)
}
