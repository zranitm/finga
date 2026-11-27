package social_test

import (
	"os"
	"testing"

	"finga/internal/application/social"
	repository "finga/internal/repository/social"
)

// TestMain gives the internal tests the repository's in-memory party store; the repository imports this package, so they can't import it.
func TestMain(m *testing.M) {
	social.SetMemory(func() social.PartyStore { return repository.NewMemory() })
	os.Exit(m.Run())
}
