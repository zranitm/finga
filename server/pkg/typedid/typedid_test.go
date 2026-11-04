package typedid_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"

	"finga/pkg/typedid"
	"finga/pkg/uuidv7"
)

type (
	orderKind    struct{}
	customerKind struct{}
)

type (
	orderID    = typedid.ID[orderKind]
	customerID = typedid.ID[customerKind]
)

func TestPrintAndParse(t *testing.T) {
	id := typedid.New[orderKind]()
	back, err := typedid.Parse[orderKind](id.String())
	if err != nil {
		t.Fatal(err)
	}
	if back != id {
		t.Fatalf("parsed %s, want %s", back, id)
	}
	if len(id.String()) != 36 {
		t.Fatalf("text %q is not the canonical 36 characters", id.String())
	}
}

func TestParseRejects(t *testing.T) {
	tests := []struct {
		name string
		text string
	}{
		{"empty", ""},
		{"not hex", "zzzzzzzz-zzzz-zzzz-zzzz-zzzzzzzzzzzz"},
		{"too short", "0192f0a0-0000-7000-8000-00000000000"},
		{"too long", "0192f0a0-0000-7000-8000-0000000000000"},
		{"words", "an order"},
		{"the nil UUID", "00000000-0000-0000-0000-000000000000"},
		{"version 4", "9b2d3c4e-5f60-4a71-8b92-a3b4c5d6e7f8"},
		{"version 1", "6ba7b810-9dad-11d1-80b4-00c04fd430c8"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id, err := typedid.Parse[orderKind](tt.text)
			if !errors.Is(err, typedid.ErrMalformed) {
				t.Fatalf("err = %v, want ErrMalformed", err)
			}
			if !id.IsZero() {
				t.Fatalf("a failed parse returned %s", id)
			}
		})
	}
}

func TestZero(t *testing.T) {
	var zero orderID
	if !zero.IsZero() {
		t.Fatal("the zero ID is not zero")
	}
	if typedid.New[orderKind]().IsZero() {
		t.Fatal("a new ID is zero")
	}
}

func TestNewIsVersion7InCreationOrder(t *testing.T) {
	a := typedid.New[orderKind]()
	b := typedid.New[orderKind]()
	for _, id := range []orderID{a, b} {
		if v := uuid.UUID(id).Version(); v != 7 {
			t.Fatalf("%s is version %d, want 7", id, v)
		}
	}
	if bytes.Compare(a[:], b[:]) >= 0 {
		t.Fatalf("%s was minted before %s but does not sort first", a, b)
	}
}

func TestTextRoundTripsThroughJSON(t *testing.T) {
	type body struct {
		Order orderID `json:"order"`
	}
	in := body{Order: typedid.New[orderKind]()}
	raw, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"order":"` + in.Order.String() + `"}`; string(raw) != want {
		t.Fatalf("JSON %s, want %s", raw, want)
	}
	var out body
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if out != in {
		t.Fatalf("round trip %+v, want %+v", out, in)
	}
	if err := json.Unmarshal([]byte(`{"order":"nope"}`), &out); !errors.Is(err, typedid.ErrMalformed) {
		t.Fatalf("bad text: err = %v, want ErrMalformed", err)
	}
}

// `var c customerID = orderID{}` does not compile; a type switch tells kinds apart at run time too.
func TestKindsAreDistinctTypes(t *testing.T) {
	var v any = typedid.New[orderKind]()
	switch v.(type) {
	case customerID:
		t.Fatal("an order ID matched the customer ID type")
	case orderID:
	default:
		t.Fatalf("unexpected type %T", v)
	}
}

func TestConvertsToAndFromUUID(t *testing.T) {
	u := uuidv7.New()
	id := orderID(u)
	if uuid.UUID(id) != u || id.String() != u.String() {
		t.Fatalf("%s does not match %s", id, u)
	}
}
