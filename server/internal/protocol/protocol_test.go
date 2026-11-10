package protocol_test

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"

	clientv1 "finga/api/gen/client/v1"
	"finga/internal/protocol"
	"finga/pkg/wsconn"
)

// The golden files show every message as protojson; -update rewrites them after a deliberate change.
var update = flag.Bool("update", false, "rewrite testdata/contracts from the samples")

const contractsDir = "../../testdata/contracts"

// bodies are the message names an envelope's oneof body carries, sorted.
func bodies(envelope proto.Message) []string {
	fields := envelope.ProtoReflect().Descriptor().Oneofs().ByName("body").Fields()
	out := make([]string, 0, fields.Len())
	for i := range fields.Len() {
		out = append(out, string(fields.Get(i).Message().Name()))
	}
	slices.Sort(out)
	return out
}

var (
	clientMessages = bodies(&clientv1.ClientMessage{})
	serverMessages = bodies(&clientv1.ServerMessage{})
)

// codecFor is the codec that writes m: the client's for its messages, the server's for the rest.
func codecFor(m wsconn.Message, f wsconn.Format) *wsconn.ProtoCodec {
	if slices.Contains(clientMessages, m.Type) {
		return protocol.ClientCodec(f)
	}
	return protocol.ServerCodec(f)
}

// readerFor decodes what codecFor wrote.
func readerFor(m wsconn.Message, f wsconn.Format) *wsconn.ProtoCodec {
	if slices.Contains(clientMessages, m.Type) {
		return protocol.ServerCodec(f)
	}
	return protocol.ClientCodec(f)
}

func TestGoldenFiles(t *testing.T) {
	samples := samples()
	seen := map[string]bool{}
	for _, m := range samples {
		if seen[m.Type] {
			t.Fatalf("two samples for %s", m.Type)
		}
		seen[m.Type] = true
	}
	for _, name := range slices.Concat(clientMessages, serverMessages) {
		if !seen[name] {
			t.Errorf("message %s has no sample, so no golden file", name)
		}
	}
	for _, m := range samples {
		t.Run(m.Type, func(t *testing.T) {
			encoded, err := codecFor(m, wsconn.JSON).Encode(m)
			if err != nil {
				t.Fatal(err)
			}
			file := filepath.Join(contractsDir, m.Type+".json")
			if *update {
				var pretty bytes.Buffer
				if err := json.Indent(&pretty, encoded, "", "  "); err != nil {
					t.Fatal(err)
				}
				pretty.WriteByte('\n')
				if err := os.WriteFile(file, pretty.Bytes(), 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			golden, err := os.ReadFile(file)
			if err != nil {
				t.Fatalf("%v; run with -update after a deliberate change", err)
			}
			decoded, err := readerFor(m, wsconn.JSON).Decode(golden)
			if err != nil {
				t.Fatalf("golden does not decode: %v", err)
			}
			same(t, decoded, m)
			if canon(t, encoded) != canon(t, golden) {
				t.Fatalf("re-encoded\n%s\ngolden\n%s", encoded, golden)
			}
		})
	}
	t.Run("every golden file is a message", func(t *testing.T) {
		files, err := filepath.Glob(filepath.Join(contractsDir, "*.json"))
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range files {
			if name := strings.TrimSuffix(filepath.Base(f), ".json"); !seen[name] {
				t.Errorf("%s is no message", name)
			}
		}
		if len(files) != len(samples) {
			t.Errorf("%d golden files for %d samples", len(files), len(samples))
		}
	})
}

func TestEverySampleSurvivesTheBinaryFormat(t *testing.T) {
	for _, m := range samples() {
		b, err := codecFor(m, wsconn.Binary).Encode(m)
		if err != nil {
			t.Fatalf("%s: %v", m.Type, err)
		}
		decoded, err := readerFor(m, wsconn.Binary).Decode(b)
		if err != nil {
			t.Fatalf("%s: %v", m.Type, err)
		}
		same(t, decoded, m)
	}
}

func TestCatalogueCoversPage4(t *testing.T) {
	want := map[string][]string{
		"client": {"Attack", "ChatSend", "EnterPortal", "Equip", "Hello", "Input", "InventoryDiscard", "InventoryMove",
			"PartyInvite", "PartyKick", "PartyLeave", "PartyPromote", "PartyRespond", "PickUp", "Ping", "Unequip"},
		"server": {"ChatMessage", "Correction", "Damage", "Delta", "Died", "ExpGained", "InventoryChanged", "Kicked",
			"LevelUp", "Loot", "MapChange", "PartyInvited", "PartyState", "Pong", "Respawned", "Response", "Snapshot", "StatsChanged", "Welcome"},
	}
	if got := clientMessages; !slices.Equal(got, want["client"]) {
		t.Errorf("client messages %v", got)
	}
	if got := serverMessages; !slices.Equal(got, want["server"]) {
		t.Errorf("server messages %v", got)
	}
}

func TestTheDebugFormatReadsLikeTheSchema(t *testing.T) {
	b, err := protocol.ServerCodec(wsconn.JSON).Encode(wsconn.Message{Tick: 81234, Data: &clientv1.Delta{AckSeq: 117}})
	if err != nil {
		t.Fatal(err)
	}
	if canon(t, b) != `{"delta":{"ack_seq":117},"tick":81234}` {
		t.Fatalf("encoded %s", b)
	}
}

func TestTheServerPrefersBinary(t *testing.T) {
	sps := protocol.Subprotocols()
	if len(sps) != 2 || sps[0].Name != protocol.SubprotocolProto || !sps[0].Codec.Binary() || sps[1].Name != protocol.SubprotocolJSON || sps[1].Codec.Binary() {
		t.Fatalf("subprotocols %+v", sps)
	}
	if protocol.Version < 1 {
		t.Fatal("the protocol version starts at 1")
	}
}

func same(t *testing.T, got, want wsconn.Message) {
	t.Helper()
	if got.Type != want.Type || got.Seq != want.Seq || got.ReqID != want.ReqID || got.Tick != want.Tick ||
		!proto.Equal(got.Data.(proto.Message), want.Data.(proto.Message)) {
		t.Fatalf("decoded %+v\nwant %+v", got, want)
	}
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
