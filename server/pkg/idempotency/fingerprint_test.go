package idempotency_test

import (
	"bytes"
	"encoding/hex"
	"testing"

	"finga/pkg/idempotency"
)

// The bytes are stored with each key: a change refuses every retry made before the deploy.
func TestFingerprintIsStable(t *testing.T) {
	got := hex.EncodeToString(idempotency.Fingerprint("scope/1", "ab", "", "c"))
	const want = "5ca0c5c7b8f91cc1521e416ee4efab5e079fa5222f258587ddae3ec34118cc74"
	if got != want {
		t.Fatalf("fingerprint changed\ngot  %s\nwant %s", got, want)
	}
}

func TestFingerprintKeepsValuesApart(t *testing.T) {
	differ := func(a, b []byte) {
		t.Helper()
		if bytes.Equal(a, b) {
			t.Fatal("two requests share a fingerprint")
		}
	}
	differ(idempotency.Fingerprint("v", "ab", "c"), idempotency.Fingerprint("v", "a", "bc"))
	differ(idempotency.Fingerprint("v", "a"), idempotency.Fingerprint("v", "a", ""))
	differ(idempotency.Fingerprint("v1", "a"), idempotency.Fingerprint("v2", "a"))
	differ(idempotency.Fingerprint("v", "a"), idempotency.Fingerprint("va"))
}
