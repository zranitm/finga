package account_test

import (
	"testing"

	"go.uber.org/goleak"

	"finga/internal/repository/dbtest"
)

// withDatabase runs the tests and then releases the database their Postgres tests started, so the leak check below sees what is left.
type withDatabase struct{ m *testing.M }

func (w withDatabase) Run() int { return dbtest.Run(w.m) }

// TestMain gives the tests that run the use cases on Postgres their database, started when the first one asks, and checks that every goroutine a test started, the connections' and streams' included, is gone once the tests end.
func TestMain(m *testing.M) { goleak.VerifyTestMain(withDatabase{m}) }
