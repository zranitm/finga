package pvp_test

import (
	"context"
	"testing"

	adapter "finga/internal/adapter/pvp"
	"finga/internal/application/world"
	model "finga/internal/model/pvp"
	"finga/pkg/copytest"
)

// worldModule hands FollowResults one result.
type worldModule struct {
	adapter.WorldModule
	result world.MatchResult
}

func (w worldModule) FollowResults(_ context.Context, deliver func(world.MatchResult)) error {
	deliver(w.result)
	return nil
}

// A match's result reaches pvp with every field the world told.
func TestAResultKeepsItsFields(t *testing.T) {
	for _, problem := range copytest.Diff(copytest.Match{}, func(r world.MatchResult) model.Result {
		var got model.Result
		_ = adapter.NewWorld(worldModule{result: r}).FollowResults(context.Background(), func(out model.Result) { got = out })
		return got
	}) {
		t.Error(problem)
	}
}
