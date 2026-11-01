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
