package service_test

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"finga/internal/application/pvp/service"
	model "finga/internal/model/pvp"
	"finga/internal/shared/kernel"
)

// An outcome a player was away for is pushed saying so, and their next Welcome takes it once: the newest five, newest first, and a count of the rest, each as it went for them, a leaver's a loss. One handed on live is told, and a player in the match at its end is owed none.
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
		if len(got.Matches) != len(want) || got.More != 2 {
			t.Fatalf("Ayla missed %+v; want %+v and 2 more", got, want)
		}
		for k := range want {
			if g := got.Matches[k]; g.Mode != want[k].Mode || g.Outcome != want[k].Outcome || g.Left != want[k].Left || !g.Ended.Equal(want[k].Ended) {
				t.Fatalf("Ayla's missed match %d: %+v; want %+v", k, g, want[k])
			}
		}
		for _, id := range []kernel.CharacterID{ayla, kai} {
			if again, _ := f.svc.Missed(ctx, id); len(again.Matches) != 0 || again.More != 0 {
				t.Fatalf("%v missed %+v at a later Welcome; want none", id, again)
			}
		}
		seen, owed := kernel.NewMatchID(), kernel.NewMatchID()
		for _, id := range []kernel.MatchID{seen, owed} {
			f.svc.Record(ctx, model.Result{Match: id, Mode: "duel", Started: start, Ended: start, Winner: 0, Sides: [2][]model.Outcome{{{Character: nia, Away: true}}, {{Tier: "novice"}}}})
		}
		if err := f.svc.Told(ctx, nia, seen); err != nil {
			t.Fatal(err)
		}
		if got, _ := f.svc.Missed(ctx, nia); len(got.Matches) != 1 || got.Matches[0].Outcome != service.Draw || got.More != 0 {
			t.Fatalf("Nia missed %+v; want the one draw she wasn't handed live", got)
		}
	})
}
