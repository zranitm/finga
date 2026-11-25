package service_test

import (
	"context"
	"slices"
	"testing"
	"testing/synctest"
	"time"

	"finga/internal/application/pvp/service"
	model "finga/internal/model/pvp"
	"finga/internal/shared/kernel"
)

// An outcome a player was away for is pushed saying so, and every Welcome till they're told lists it: the newest five, newest first, and the older ones to count, each as it went for them, a leaver's a loss. Told, they're gone, as is one handed on live; a player in the match at its end is owed none.
func TestMatchesEndedWhileAwayAreToldOnce(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newFixture(t)
		ctx := context.Background()
		ayla, kai, nia := kernel.NewCharacterID(), kernel.NewCharacterID(), kernel.NewCharacterID()
		start := time.Now()
		var matches []kernel.MatchID
		for k := range 7 { // side 1 wins the even ones; Ayla, on it, leaves the last
			id := kernel.NewMatchID()
			matches = append(matches, id)
			f.svc.Record(ctx, model.Result{Match: id, Mode: "duel", Started: start, Ended: start.Add(time.Duration(k) * time.Minute), Winner: 1 + k%2,
				Sides: [2][]model.Outcome{{{Character: ayla, Away: true, Left: k == 6}}, {{Character: kai}}}})
		}
		if e, ok := last[service.Ended](f.notes, ayla); !ok || !e.Away || e.Match != matches[6] {
			t.Fatalf("Ayla was pushed %+v; want the last match, saying she was away", e)
		}
		if e, ok := last[service.Ended](f.notes, kai); !ok || e.Away || e.Match != matches[6] {
			t.Fatalf("Kai was pushed %+v; want the last match, not away", e)
		}
		got, err := f.svc.Missed(ctx, ayla)
		if err != nil {
			t.Fatal(err)
		}
		want := []service.MissedMatch{
			{Mode: "duel", Outcome: service.Loss, Left: true, Ended: start.Add(6 * time.Minute)},
			{Mode: "duel", Outcome: service.Loss, Ended: start.Add(5 * time.Minute)},
			{Mode: "duel", Outcome: service.Win, Ended: start.Add(4 * time.Minute)},
			{Mode: "duel", Outcome: service.Loss, Ended: start.Add(3 * time.Minute)},
			{Mode: "duel", Outcome: service.Win, Ended: start.Add(2 * time.Minute)},
		}
		if len(got.Matches) != len(want) || !slices.Equal(got.Older, []kernel.MatchID{matches[1], matches[0]}) {
			t.Fatalf("Ayla missed %+v; want %+v and the first two older", got, want)
		}
		for k := range want {
			if g := got.Matches[k]; g.Match != matches[6-k] || g.Mode != want[k].Mode || g.Outcome != want[k].Outcome || g.Left != want[k].Left || !g.Ended.Equal(want[k].Ended) {
				t.Fatalf("Ayla's missed match %d: %+v; want %+v of match %d", k, g, want[k], 6-k)
			}
		}
		if again, _ := f.svc.Missed(ctx, ayla); len(again.Matches) != len(want) || len(again.Older) != 2 {
			t.Fatalf("Ayla missed %+v at a Welcome before she was told; want them all again", again)
		}
		if err := f.svc.Told(ctx, ayla, matches); err != nil {
			t.Fatal(err)
		}
		for _, id := range []kernel.CharacterID{ayla, kai} {
			if again, _ := f.svc.Missed(ctx, id); len(again.Matches) != 0 || len(again.Older) != 0 {
				t.Fatalf("%v missed %+v once told; want none", id, again)
			}
		}
		seen, owed := kernel.NewMatchID(), kernel.NewMatchID()
		for _, id := range []kernel.MatchID{seen, owed} {
			f.svc.Record(ctx, model.Result{Match: id, Mode: "duel", Started: start, Ended: start, Winner: 0, Sides: [2][]model.Outcome{{{Character: nia, Away: true}}, {{Tier: "novice"}}}})
		}
		if err := f.svc.Told(ctx, nia, []kernel.MatchID{seen}); err != nil {
			t.Fatal(err)
		}
		if got, _ := f.svc.Missed(ctx, nia); len(got.Matches) != 1 || got.Matches[0].Match != owed || got.Matches[0].Outcome != service.Draw || len(got.Older) != 0 {
			t.Fatalf("Nia missed %+v; want the one draw she wasn't handed live", got)
		}
	})
}
