package application_test

import (
	"testing"

	"go.uber.org/goleak"
)

// Every goroutine a test starts must be gone once the tests end.
func TestMain(m *testing.M) { goleak.VerifyTestMain(m) }
