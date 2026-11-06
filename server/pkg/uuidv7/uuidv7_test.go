package uuidv7_test

import (
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"finga/pkg/uuidv7"
)

func TestNewIsAVersion7UUIDCarryingItsTime(t *testing.T) {
	before := time.Now().Truncate(time.Millisecond)
	u := uuidv7.New()
	after := time.Now()
	if u.Version() != 7 || u.Variant() != uuid.RFC4122 {
		t.Fatalf("version %d variant %v", u.Version(), u.Variant())
	}
	at := time.Unix(u.Time().UnixTime())
	if at.Before(before) || at.After(after) {
		t.Fatalf("time %v outside [%v, %v]", at, before, after)
	}
}

func TestIDsSortInTheOrderTheyWereMade(t *testing.T) {
	ids := make([]string, 1000)
	for i := range ids {
		ids[i] = uuidv7.NewString()
	}
	if !slices.IsSorted(ids) {
		t.Fatal("IDs made one after another are not in order")
	}
	if len(slices.Compact(slices.Clone(ids))) != len(ids) {
		t.Fatal("an ID repeated")
	}
}

func TestNewStringIsTheCanonicalForm(t *testing.T) {
	s := uuidv7.NewString()
	u, err := uuid.Parse(s)
	if err != nil || u.String() != s || u.Version() != 7 {
		t.Fatalf("%q: %v", s, err)
	}
}

func TestParseAcceptsOnlyVersion7(t *testing.T) {
	v7 := uuidv7.New()
	if got, err := uuidv7.Parse(v7.String()); err != nil || got != v7 {
		t.Fatalf("Parse(v7) = %v, %v", got, err)
	}
	for _, s := range []string{"6f1c2a3b-4d5e-4f60-8a71-92b3c4d5e6f7", uuid.Nil.String(), "nope", ""} {
		if _, err := uuidv7.Parse(s); err == nil {
			t.Fatalf("Parse(%q) accepted it", s)
		}
	}
}
