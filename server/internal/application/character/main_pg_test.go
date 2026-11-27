package character_test

import (
	"testing"

	"finga/internal/repository/dbtest"
)

// TestMain gives the tests that run the use cases on Postgres their database, started when the first one asks.
func TestMain(m *testing.M) { dbtest.Main(m) }
