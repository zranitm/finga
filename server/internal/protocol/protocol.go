// Package protocol wraps the generated clientv1 messages with the version, the subprotocols and their codecs.
package protocol

import (
	clientv1 "finga/api/gen/client/v1"
	"finga/pkg/wsconn"
)

// Version is the protocol version Hello must carry; bump it when a message changes meaning.
const Version = 1

// The subprotocols a client asks for when it connects.
const (
	SubprotocolProto = "finga.v1.proto" // binary frames
	SubprotocolJSON  = "finga.v1.json"  // protojson text frames, for reading them in devtools
)

// Subprotocols are the server's codecs, binary preferred.
func Subprotocols() []wsconn.Subprotocol {
	return []wsconn.Subprotocol{
		{Name: SubprotocolProto, Codec: ServerCodec(wsconn.Binary)},
		{Name: SubprotocolJSON, Codec: ServerCodec(wsconn.JSON)},
	}
}

// ServerCodec reads ClientMessages and writes ServerMessages.
func ServerCodec(f wsconn.Format) *wsconn.ProtoCodec {
	return wsconn.NewProtoCodec(&clientv1.ClientMessage{}, &clientv1.ServerMessage{}, f)
}

// ClientCodec reads ServerMessages and writes ClientMessages, for the bot and the end-to-end tests.
func ClientCodec(f wsconn.Format) *wsconn.ProtoCodec {
	return wsconn.NewProtoCodec(&clientv1.ServerMessage{}, &clientv1.ClientMessage{}, f)
}
