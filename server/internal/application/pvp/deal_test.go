package pvp_test

import (
	"context"
	"fmt"
	"math"
	"slices"
	"testing"
	"testing/synctest"
	"time"

	"finga/internal/application/pvp"
	model "finga/internal/model/pvp"
	"finga/internal/shared/kernel"
)

// queueAt queues each ticket's leader for mode, each member's mu set first.
func (f fixture) queueAt(t *testing.T, mode string, mu map[kernel.CharacterID]float64, leaders ...model.Player) {
	t.Helper()
	for id, m := range mu {
		f.store.ratings[id] = model.Rating{Mu: m, Sigma: 3}
	}
	for _, p := range leaders {
		if err := f.svc.Queue(context.Background(), p, mode); err != nil {
			t.Fatal(err)
		}
	}
}

// classesOn counts each class on side k of spec, players' as of, bots' their own.
func classesOn(spec model.Spec, k int, of map[kernel.CharacterID]kernel.Class) map[kernel.Class]int {
	out := map[kernel.Class]int{}
	for _, p := range spec.Sides[k] {
		if p.Bot != nil {
			out[p.Bot.Class]++
		} else {
			out[of[p.Character]]++
		}
	}
	return out
}

// pvp.md section 9: thirty players waiting for a battle meet at once, fifteen a side, no bots, dealt high mu to low each to the side with fewer, ties to the lower total, so the sides' mu sums end even.
func TestABattleDealsThirtyAtOnce(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newFixture(t)
		f.run(t)
		var ps []model.Player
		mu := map[kernel.CharacterID]float64{}
		for i := range 30 {
			p := player(fmt.Sprintf("P%d", i+1), "warrior")
			ps, mu[p.ID] = append(ps, p), float64(60-i)
		}
		f.queueAt(t, "battle", mu, ps...)
		wait(time.Second)
		opened := f.open(t, ps...)
		if len(opened) != 1 || opened[0].Mode != "battle" {
			t.Fatalf("opened %+v; want one battle", opened)
		}
		var sum [2]float64
		for k, side := range opened[0].Sides {
			if len(side) != 15 || slices.ContainsFunc(side, func(p model.Place) bool { return p.Bot != nil }) {
				t.Fatalf("side %d %+v; want fifteen players and no bot", k+1, side)
			}
			for _, p := range side {
				sum[k] += mu[p.Character]
			}
		}
		if math.Abs(sum[0]-sum[1]) > 1 {
			t.Fatalf("the sides' mu sums %v; want them within a player's step of each other", sum)
		}
	})
}

// pvp.md section 9: a player alone in the battle's queue waits bots_after, then meets bots in every place left: fourteen beside them and fifteen against, no class past the cap of five on a side.
func TestABattleDealsWithBotsAfterBotsAfter(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newFixture(t)
		f.run(t)
		ann := player("Ann", "warrior")
		f.queueAt(t, "battle", map[kernel.CharacterID]float64{ann.ID: 25}, ann)
		wait(f.data.Pvp().Queue.BotsAfter - time.Second)
		if _, ok := last[pvp.CoreOffered](f.notes, ann.ID); ok {
			t.Fatal("offered a battle before bots_after with one player waiting")
		}
		wait(time.Second)
		if o, ok := last[pvp.CoreOffered](f.notes, ann.ID); !ok || o.Mode != "battle" || o.Bots != 29 {
			t.Fatalf("Ann's offer %+v, %v; want the battle with 29 bots", o, ok)
		}
		opened := f.open(t, ann)
		if len(opened) != 1 {
			t.Fatalf("opened %+v; want one battle", opened)
		}
		spec := opened[0]
		of := map[kernel.CharacterID]kernel.Class{ann.ID: "warrior"}
		for k := range spec.Sides {
			if len(spec.Sides[k]) != 15 {
				t.Fatalf("side %d holds %d; want 15", k+1, len(spec.Sides[k]))
			}
			for class, n := range classesOn(spec, k, of) {
				if n > 5 {
					t.Fatalf("side %d holds %s %d times; want 5 at most", k+1, class, n)
				}
			}
		}
	})
}

// pvp.md section 9: a full house of parties the deal can't side whole, as it goes high mu to low: three 4s and six 3s side 13 and 14, the last 3 left out though 4+4+4+3 against five 3s would fit. It stays queued, bots take the places left, and it gets the next deal.
func TestAPartyTheDealCantFitGetsTheNextDeal(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newFixture(t)
		f.run(t)
		mu := map[kernel.CharacterID]float64{}
		var parties [][]model.Player
		for k, size := range []int{4, 4, 4, 3, 3, 3, 3, 3, 3} {
			var ps []model.Player
			for i := range size {
				p := player(fmt.Sprintf("P%d-%d", k+1, i+1), "warrior")
				ps, mu[p.ID] = append(ps, p), float64(30-k)
			}
			parties = append(parties, f.party(ps...))
		}
		var leaders []model.Player
		for _, p := range parties {
			leaders = append(leaders, p[0])
		}
		f.queueAt(t, "battle", mu, leaders...)
		wait(time.Second)
		left := parties[8]
		if o, ok := last[pvp.CoreOffered](f.notes, parties[0][0].ID); !ok || o.Bots != 3 {
			t.Fatalf("the first party's offer %+v, %v; want 27 players and 3 bots", o, ok)
		}
		if _, ok := last[pvp.CoreOffered](f.notes, left[0].ID); ok {
			t.Fatal("the last 3 was offered; want it waiting for the next deal")
		}
		opened := f.open(t, slices.Concat(parties[:8]...)...)
		if len(opened) != 1 || len(opened[0].Sides[0])+len(opened[0].Sides[1]) != 30 {
			t.Fatalf("opened %+v; want one battle of 30 places", opened)
		}
		wait(f.data.Pvp().Queue.BotsAfter)
		if o, ok := last[pvp.CoreOffered](f.notes, left[0].ID); !ok || o.Mode != "battle" || o.Bots != 27 {
			t.Fatalf("the last 3's offer %+v, %v; want the next battle, with 27 bots", o, ok)
		}
	})
}

// pvp.md section 8: a side's bots keep each class to ⌈team/4⌉ + 1, its players counted, so a side of eleven Warriors gets four bots of other classes, none past five.
func TestElevenWarriorsOnASideGetOtherClassesBeside(t *testing.T) {
	for seed := range uint64(10) {
		synctest.Test(t, func(t *testing.T) {
			f := seeded(t, seed)
			f.run(t)
			mu := map[kernel.CharacterID]float64{}
			of := map[kernel.CharacterID]kernel.Class{}
			var ps []model.Player
			warriors := []int{1, 4, 5, 8, 9, 12, 13, 16, 17, 20, 21} // where the deal's turns put them on one side
			for i := 1; i <= 22; i++ {
				class := kernel.Class("magician")
				if slices.Contains(warriors, i) {
					class = "warrior"
				}
				p := player(fmt.Sprintf("P%d", i), class)
				ps, mu[p.ID], of[p.ID] = append(ps, p), float64(100-i), class
			}
			f.queueAt(t, "battle", mu, ps...)
			wait(f.data.Pvp().Queue.BotsAfter)
			opened := f.open(t, ps...)
			if len(opened) != 1 {
				t.Fatalf("seed %d: opened %+v; want one battle", seed, opened)
			}
			spec := opened[0]
			for k := range spec.Sides {
				own := of[spec.Sides[k][0].Character]
				counts := classesOn(spec, k, of)
				if counts[own] != 11 {
					t.Fatalf("seed %d: side %d %v; want eleven %ss dealt together", seed, k+1, counts, own)
				}
				for class, n := range counts {
					if class != own && n > 5 {
						t.Fatalf("seed %d: side %d holds %s %d times beside its eleven %ss; want 5 at most", seed, k+1, class, n, own)
					}
				}
			}
		})
	}
}

// waits are the WaitingRoom flags id was told, in order.
func (f fixture) waits(id kernel.CharacterID) []bool {
	var out []bool
	for _, d := range f.notes.of(id) {
		if q, ok := d.(pvp.CoreQueued); ok && q.Since > 0 {
			out = append(out, q.WaitingRoom)
		}
	}
	return out
}

// pvp.md section 8: bots.most caps the bots running, offered and opening: a 1v1 that would pass it waits, told once, and once a match's end makes room it's told the wait is over, then offered.
func TestA1v1PastTheBotCapWaitsForRoom(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newFixture(t)
		f.world.bots = f.data.Pvp().Bots.Most // the node runs as many as it may
		f.run(t)
		ann := player("Ann", "warrior")
		f.queueAt(t, "duel", map[kernel.CharacterID]float64{ann.ID: 25}, ann)
		wait(f.data.Pvp().Queue.BotsAfter + 3*time.Second)
		if _, ok := last[pvp.CoreOffered](f.notes, ann.ID); ok || !slices.Equal(f.waits(ann.ID), []bool{true}) {
			t.Fatalf("Ann told %v; want no offer, and told once that her match waits for room", f.notes.of(ann.ID))
		}
		f.world.end(1)
		wait(time.Second)
		got := f.notes.of(ann.ID)
		q, waited := got[len(got)-2].(pvp.CoreQueued)
		o, offered := got[len(got)-1].(pvp.CoreOffered)
		if !waited || q.WaitingRoom || q.Mode != "duel" || q.Since < f.data.Pvp().Queue.BotsAfter || !offered || o.Bots != 1 {
			t.Fatalf("Ann told %+v; want the wait over, with her time in the queue, then her offer against a bot", got)
		}
	})
}

// A match of players alone needs no room: it never waits, however many bots run.
func TestPlayersAloneNeverWaitForRoom(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newFixture(t)
		f.world.bots = f.data.Pvp().Bots.Most + 100
		f.run(t)
		ann, bo := player("Ann", "warrior"), player("Bo", "warrior")
		f.queueAt(t, "duel", map[kernel.CharacterID]float64{ann.ID: 25, bo.ID: 25}, ann, bo)
		wait(time.Second)
		if o, ok := last[pvp.CoreOffered](f.notes, ann.ID); !ok || o.Bots != 0 || len(f.waits(ann.ID)) != 0 {
			t.Fatalf("Ann told %+v; want her offer against Bo at once, never a wait", f.notes.of(ann.ID))
		}
	})
}

// The room counts the bots of standing offers and of matches the world is opening, not only those it runs.
func TestOffersAndOpeningsTakeRoom(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newFixture(t)
		f.world.bots = f.data.Pvp().Bots.Most - 1 // room for one bot
		f.run(t)
		ctx := context.Background()
		ann, bo := player("Ann", "warrior"), player("Bo", "warrior")
		f.queueAt(t, "duel", map[kernel.CharacterID]float64{ann.ID: 10, bo.ID: 40}, ann) // too far apart ever to meet
		wait(time.Second)
		f.queueAt(t, "duel", nil, bo)
		wait(f.data.Pvp().Queue.BotsAfter)
		offer, ok := last[pvp.CoreOffered](f.notes, ann.ID)
		if !ok {
			t.Fatal("Ann wasn't offered her bot")
		}
		if _, ok := last[pvp.CoreOffered](f.notes, bo.ID); ok || !slices.Equal(f.waits(bo.ID), []bool{true}) {
			t.Fatal("Bo was offered a bot while Ann's offer held the last room")
		}
		f.world.mu.Lock()
		f.world.hold = make(chan struct{})
		f.world.mu.Unlock()
		accepted := make(chan error, 1)
		go func() { accepted <- f.svc.Answer(ctx, ann.ID, offer.Offer, true) }()
		wait(3 * time.Second)
		if _, ok := last[pvp.CoreOffered](f.notes, bo.ID); ok {
			t.Fatal("Bo was offered a bot while Ann's match opened with the last room")
		}
		close(f.world.hold)
		if err := <-accepted; err != nil {
			t.Fatal(err)
		}
		wait(time.Second)
		if _, ok := last[pvp.CoreOffered](f.notes, bo.ID); ok {
			t.Fatal("Bo was offered a bot while Ann's match ran with the last room")
		}
		f.world.end(1)
		wait(time.Second)
		if o, ok := last[pvp.CoreOffered](f.notes, bo.ID); !ok || o.Bots != 1 || !slices.Equal(f.waits(bo.ID), []bool{true, false}) {
			t.Fatalf("Bo told %+v; want his wait over once Ann's match ended, then his offer", f.notes.of(bo.ID))
		}
	})
}

// The deal takes the oldest who fit both sides: with 31 waiting, the newest waits for the next, however high their mu.
func TestABattleDealsTheOldestThirty(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newFixture(t)
		f.run(t)
		var ps []model.Player
		mu := map[kernel.CharacterID]float64{}
		for i := range 31 {
			p := player(fmt.Sprintf("P%d", i+1), "warrior")
			ps, mu[p.ID] = append(ps, p), float64(20+i) // the newest highest
		}
		f.queueAt(t, "battle", mu, ps[:30]...)
		wait(500 * time.Millisecond)
		f.queueAt(t, "battle", nil, ps[30])
		wait(time.Second)
		if _, ok := last[pvp.CoreOffered](f.notes, ps[30].ID); ok {
			t.Fatal("the newest, 31st, was dealt in; want the oldest thirty")
		}
		for _, p := range ps[:30] {
			if o, ok := last[pvp.CoreOffered](f.notes, p.ID); !ok || o.Bots != 0 {
				t.Fatalf("%s's offer %+v, %v; want each of the oldest thirty in a battle of players alone", p.Name, o, ok)
			}
		}
	})
}

// The offers one step makes share its room: two players past bots_after with room for one bot, one gets it and the other waits.
func TestOneStepsOffersShareTheRoom(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newFixture(t)
		f.world.bots = f.data.Pvp().Bots.Most - 1
		f.run(t)
		ann, bo := player("Ann", "warrior"), player("Bo", "warrior")
		f.queueAt(t, "duel", map[kernel.CharacterID]float64{ann.ID: 10, bo.ID: 40}, ann, bo) // too far apart ever to meet
		wait(f.data.Pvp().Queue.BotsAfter)
		_, annOffered := last[pvp.CoreOffered](f.notes, ann.ID)
		_, boOffered := last[pvp.CoreOffered](f.notes, bo.ID)
		waiting := bo
		if boOffered {
			waiting = ann
		}
		if annOffered == boOffered || !slices.Equal(f.waits(waiting.ID), []bool{true}) {
			t.Fatalf("Ann offered %v, Bo %v, %s told %v; want one bot match, the other waiting for room", annOffered, boOffered, waiting.Name, f.waits(waiting.ID))
		}
	})
}
