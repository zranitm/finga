package kernel_test

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"

	"finga/internal/kernel"
)

func TestParseClass(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    kernel.Class
		wantErr bool
	}{
		{"warrior", "warrior", kernel.Warrior, false},
		{"bowman", "bowman", kernel.Bowman, false},
		{"thief", "thief", kernel.Thief, false},
		{"magician", "magician", kernel.Magician, false},
		{"case matters", "Warrior", "", true},
		{"unknown", "paladin", "", true},
		{"empty", "", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := kernel.ParseClass(tt.in)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil {
				if !errors.Is(err, kernel.ErrUnknownClass) {
					t.Fatalf("err = %v, want ErrUnknownClass", err)
				}
				return
			}
			if got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
			if got.String() != tt.in {
				t.Fatalf("String() = %q, want %q", got.String(), tt.in)
			}
		})
	}
}

func TestClassesRoundTrip(t *testing.T) {
	seen := map[kernel.Class]bool{}
	for _, c := range kernel.Classes() {
		t.Run(c.String(), func(t *testing.T) {
			if seen[c] {
				t.Fatal("listed twice")
			}
			seen[c] = true
			got, err := kernel.ParseClass(c.String())
			if err != nil || got != c {
				t.Fatalf("ParseClass(%q) = %q, %v", c, got, err)
			}
		})
	}
	if len(seen) != 4 {
		t.Fatalf("Classes() lists %d classes, want 4", len(seen))
	}
}

func TestNewIDs(t *testing.T) {
	a, b := kernel.NewAccountID(), kernel.NewAccountID()
	if a == b {
		t.Fatal("two new IDs are equal")
	}
	if v := uuid.UUID(a).Version(); v != 7 {
		t.Fatalf("version = %d, want 7", v)
	}
	if a.IsZero() {
		t.Fatal("a new ID is zero")
	}
	var zero kernel.AccountID
	if !zero.IsZero() {
		t.Fatal("the zero value is not zero")
	}
	// Each type has its own constructor; mixing them is a compile error.
	if kernel.NewCharacterID().IsZero() || kernel.NewItemID().IsZero() || kernel.NewSessionID().IsZero() {
		t.Fatal("a new ID is zero")
	}
}

func TestParseIDs(t *testing.T) {
	const good = "0192f6a4-6f2b-7c3e-8a1b-2c3d4e5f6a7b"
	const bad = "not-a-uuid-at-all"
	parsers := []struct {
		name  string
		parse func(string) (string, error)
	}{
		{"account", func(s string) (string, error) { id, err := kernel.ParseAccountID(s); return id.String(), err }},
		{"character", func(s string) (string, error) { id, err := kernel.ParseCharacterID(s); return id.String(), err }},
		{"item", func(s string) (string, error) { id, err := kernel.ParseItemID(s); return id.String(), err }},
		{"session", func(s string) (string, error) { id, err := kernel.ParseSessionID(s); return id.String(), err }},
	}
	for _, p := range parsers {
		t.Run(p.name+" parses and prints back", func(t *testing.T) {
			got, err := p.parse(good)
			if err != nil || got != good {
				t.Fatalf("got %q, %v", got, err)
			}
		})
		t.Run(p.name+" rejects bad text as ErrBadID", func(t *testing.T) {
			if _, err := p.parse(bad); !errors.Is(err, kernel.ErrBadID) {
				t.Fatalf("err = %v, want ErrBadID", err)
			}
		})
	}
}

func TestIDJSON(t *testing.T) {
	type doc struct {
		ID kernel.CharacterID `json:"id"`
	}
	in := kernel.NewCharacterID()
	data, err := json.Marshal(doc{in})
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"id":"`+in.String()+`"}` {
		t.Fatalf("marshalled %s", data)
	}
	var out doc
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	if out.ID != in {
		t.Fatalf("round trip: %s != %s", out.ID, in)
	}
	var bad doc
	if err := json.Unmarshal([]byte(`{"id":"nope"}`), &bad); err == nil {
		t.Fatal("bad text unmarshalled")
	}
}
