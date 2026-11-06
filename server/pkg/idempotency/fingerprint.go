package idempotency

import (
	"crypto/sha256"
	"strconv"
)

// Fingerprint identifies a request: SHA-256 of version and values as netstrings, so ("ab", "c") and ("a", "bc") differ.
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
