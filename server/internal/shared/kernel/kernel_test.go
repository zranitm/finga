package kernel_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/google/uuid"

	"finga/internal/kernel"
	"finga/pkg/transient"
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
		{"character", func(s string) (string, error) { id, err := kernel.ParseCharacterID(s); return id.String(), err }},
		{"item", func(s string) (string, error) { id, err := kernel.ParseItemID(s); return id.String(), err }},
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

// A module busy or stopping keeps its reason and is transient too, wrapped as a facade wraps it; every other rule is final.
func TestOnlyABusyOrStoppingModuleIsRetryable(t *testing.T) {
	rules := []struct {
		rule  error
		retry bool
	}{
		{kernel.ErrBusy, true},
		{kernel.ErrMapBusy, true},
		{kernel.ErrWorldStopped, true},
		{kernel.ErrLoginRateLimited, false},
		{kernel.ErrSessionRace, false},
		{kernel.ErrChatRateLimited, false},
		{kernel.ErrItemNotFound, false},
	}
	for i, tt := range rules {
		err := fmt.Errorf("%w: %w", tt.rule, errors.New("the model's cause"))
		if _, ok := kernel.ReasonOf(err); !ok || errors.Is(err, transient.ErrUnavailable) != tt.retry || kernel.IsRule(err) == tt.retry {
			t.Errorf("%v: a rule %v, transient %v, final %v; want transient %v", tt.rule, ok, !tt.retry, kernel.IsRule(err), tt.retry)
		}
		for j, other := range rules {
			if errors.Is(err, other.rule) != (i == j) {
				t.Errorf("%v matches %v", tt.rule, other.rule)
			}
		}
	}
}

// Only a rule no retry passes is final: a malformed ID is, and an internal error, a cause without a rule or none isn't.
func TestIsRule(t *testing.T) {
	_, parseErr := kernel.ParseCharacterID("nope")
	for _, tt := range []struct {
		err   error
		final bool
	}{
		{fmt.Errorf("%w: %w", kernel.ErrEquipTabFull, errors.New("the model's cause")), true},
		{parseErr, true},
		{kernel.ErrInternal, false},
		{fmt.Errorf("%w: %w", transient.ErrUnavailable, errors.New("pool closed")), false},
		{errors.New("disk on fire"), false},
		{nil, false},
	} {
		if got := kernel.IsRule(tt.err); got != tt.final {
			t.Errorf("IsRule(%v) = %v, want %v", tt.err, got, tt.final)
		}
	}
}
