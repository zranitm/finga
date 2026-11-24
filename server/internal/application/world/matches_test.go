package world

import (
	"testing"

	"finga/pkg/copytest"
)

// A match's result leaves the world with every field the instance told.
func TestAResultKeepsItsFields(t *testing.T) {
	for _, problem := range copytest.Diff(copytest.Match{}, resultOf) {
		t.Error(problem)
	}
}
