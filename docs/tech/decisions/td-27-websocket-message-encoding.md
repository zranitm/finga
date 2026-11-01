# TD-27. WebSocket message encoding

- **Area:** Architecture
- **Decision:** TD-27. WebSocket message encoding
- **Default:** JSON in Stage 1, Protobuf in Stage 2 (the Stage 1 page's earlier plan)
- **Outcome:** Protobuf in Stage 1, decided by the user on 2026-11-01: one schema, `api/proto/client/v1/client.proto`, generates the Go and the TypeScript types and their encoding. Binary frames in play, and the same messages as protojson text frames as a debug mode, chosen when the socket connects. This was chosen over keeping JSON with its four hand-synced copies of every message (Go structs, TypeScript schemas, the golden files, an AsyncAPI spec), and over Protobuf schemas with JSON frames only.
- **Status:** Decided

## Why

- **One definition:** every message is written once and generated for both sides, so a new message is one change, not five in two languages.
- **The services' toolchain:** buf already lints and generates the gRPC contracts, and `buf breaking` now fails CI on an incompatible change to the client's contract too.
- **Size:** about 3× smaller frames than JSON, 7× for Input: a Delta with two players, a mob and a drop is 215 bytes against 712.

## How it works

- **Subprotocols:** the client asks for `finga.v1.proto` (binary) or `finga.v1.json` (protojson with the `.proto` field names) in the WebSocket handshake; the server prefers binary and closes a client that asks for neither with 1008 (page 4).
- **Envelopes:** ClientMessage (seq, req) and ServerMessage (req, tick) each hold one message in a `oneof body`, so each side can only send its own messages. `pkg/wsconn`'s ProtoCodec reads the body's message name as the message type, with no type table.
- **Generated code:** Go in `api/gen/client/v1`, committed with the gRPC code; TypeScript by protobuf-es into `web/src/protocol/gen`, generated before every build and git-ignored, like the gamedata.
- **Field types:** ticks and mob and drop IDs are `uint32`, since protobuf-es turns 64-bit integers into `bigint` and the game loop keys on them; 32 bits of ticks last 6.8 years at 20 Hz. EXP and Unix-millisecond times stay `int64`, `bigint` in the client.
- **Golden files:** `testdata/contracts/` keeps one full example of each message, now as protojson, which the server's and the client's tests both decode.
