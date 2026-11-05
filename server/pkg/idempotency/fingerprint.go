package idempotency

import (
	"crypto/sha256"
	"strconv"
)

// Fingerprint identifies a request for its key: SHA-256 over version and values, in order, each as a
// netstring (its length, a colon, the value, a comma), so ("ab", "c") and ("a", "bc") differ. It changes
// when a value does, or when version is bumped on purpose, never with the Go types the values came from.
func Fingerprint(version string, values ...string) []byte {
	h := sha256.New()
	var buf []byte
	for _, v := range append([]string{version}, values...) {
		buf = strconv.AppendInt(buf[:0], int64(len(v)), 10)
		buf = append(buf, ':')
		buf = append(buf, v...)
		buf = append(buf, ',')
		_, _ = h.Write(buf)
	}
	return h.Sum(nil)
}
