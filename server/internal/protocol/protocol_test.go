package protocol_test

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"finga/internal/protocol"
	"finga/pkg/wsconn"
)

// The golden files in testdata/contracts are the message catalogue's
// contract with the client: one file per message type, read by the
// client's tests too. Run with -update to rewrite them from the samples
// below after a deliberate change to a message.
var update = flag.Bool("update", false, "rewrite testdata/contracts from the samples")

const contractsDir = "../../testdata/contracts"

func TestGoldenFiles(t *testing.T) {
	codec := protocol.NewCodec()
	samples := protocol.Samples()
	seen := map[string]bool{}
	for _, m := range samples {
		if m.Type == "" {
			t.Fatalf("sample without a type: %+v", m)
		}
		if seen[m.Type] {
			t.Fatalf("two samples for %s", m.Type)
		}
		seen[m.Type] = true
	}
	for _, name := range codec.Types() {
		if !seen[name] {
			t.Errorf("registered type %s has no sample, so no golden file", name)
		}
	}
	for _, m := range samples {
		t.Run(m.Type, func(t *testing.T) {
			encoded, err := codec.Encode(m)
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
			// The golden decodes into its type...
			decoded, err := codec.Decode(golden)
			if err != nil {
				t.Fatalf("golden does not decode: %v", err)
			}
			if !reflect.DeepEqual(decoded, m) {
				t.Fatalf("golden decodes to\n%+v\nwant the sample\n%+v", decoded, m)
			}
			// ...and re-encodes to the same JSON.
			if canon(t, encoded) != canon(t, golden) {
				t.Fatalf("re-encoded\n%s\ngolden\n%s", encoded, golden)
			}
		})
	}
	t.Run("every golden file is a registered type", func(t *testing.T) {
		files, err := filepath.Glob(filepath.Join(contractsDir, "*.json"))
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range files {
			name := strings.TrimSuffix(filepath.Base(f), ".json")
			if !seen[name] {
				t.Errorf("%s has no registered type", name)
			}
		}
		if len(files) != len(samples) {
			t.Errorf("%d golden files for %d samples", len(files), len(samples))
		}
	})
}

func TestCatalogueCoversPage4(t *testing.T) {
	codec := protocol.NewCodec()
	have := map[string]bool{}
	for _, name := range codec.Types() {
		have[name] = true
	}
	want := []string{
		// client to server
		"Hello", "Input", "Attack", "PickUp", "EnterPortal", "ChatSend",
		"PartyInvite", "PartyRespond", "PartyLeave", "PartyKick", "PartyPromote",
		"InventoryMove", "InventoryDiscard", "Ping",
		// server to client
		"Welcome", "Snapshot", "Delta", "Correction", "Damage", "Died", "Respawned",
		"ExpGained", "LevelUp", "InventoryChanged", "MapChange", "ChatMessage",
		"PartyState", "PartyInvited", "Response", "Kicked", "Pong",
	}
	for _, name := range want {
		if !have[name] {
			t.Errorf("page 4 message %s is not registered", name)
		}
	}
	for _, name := range codec.Types() {
		if strings.HasPrefix(name, "Market") || name == "WalletChanged" {
			t.Errorf("%s is Stage 2", name)
		}
	}
}

func TestKickReasons(t *testing.T) {
	for _, r := range []protocol.KickReason{protocol.KickLoggedInElsewhere, protocol.KickOutdatedProtocol, protocol.KickServerRestart, protocol.KickAbuse, protocol.KickUnauthenticated} {
		if r == "" {
			t.Fatal("empty reason")
		}
	}
	if protocol.Version < 1 {
		t.Fatal("protocol version must start at 1")
	}
}

func TestEnvelopeOnTheWire(t *testing.T) {
	// The exact envelope keys from page 4, so the client can rely on them.
	codec := protocol.NewCodec()
	b, err := codec.Encode(wsconn.Message{Type: "Delta", Tick: 81234, Data: protocol.Delta{AckSeq: 117}})
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{`"t":"Delta"`, `"tick":81234`, `"ack_seq":117`} {
		if !strings.Contains(string(b), key) {
			t.Fatalf("%s lacks %s", b, key)
		}
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
