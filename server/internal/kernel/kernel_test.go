package kernel_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/google/uuid"

	"finga/internal/kernel"
)

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

// A rule keeps its reason through any wrapping, a malformed ID is the rule bad_id, and nothing else is a rule.
func TestReasonOf(t *testing.T) {
	_, parseErr := kernel.ParseItemID("nope")
	for _, tt := range []struct {
		name   string
		err    error
		reason string
	}{
		{"a rule", kernel.ErrAccountNotFound, "account_not_found"},
		{"a wrapped rule", fmt.Errorf("login: %w", fmt.Errorf("%w: %w", kernel.ErrUnauthenticated, errors.New("account: wrong password"))), "unauthenticated"},
		{"an unknown class", kernel.ErrUnknownClass, "unknown_class"},
		{"a malformed ID", parseErr, "bad_id"},
		{"an internal error", kernel.ErrInternal, ""},
		{"a plain error", errors.New("disk on fire"), ""},
		{"no error", nil, ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			reason, ok := kernel.ReasonOf(tt.err)
			if reason != tt.reason || ok != (tt.reason != "") {
				t.Fatalf("got %q, %v; want %q", reason, ok, tt.reason)
			}
		})
	}
}
