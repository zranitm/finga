package service_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"testing/synctest"
	"time"

	"finga/internal/application/pvp/service"
	model "finga/internal/model/pvp"
	"finga/internal/shared/kernel"
)

// party puts players in one party led by the first, and gives them back.
func (f fixture) party(ps ...model.Player) []model.Player {
	f.social.mu.Lock()
	defer f.social.mu.Unlock()
	f.social.parties = append(f.social.parties, model.Party{Leader: ps[0].ID, Members: ps})
	return ps
}

func player(name string, class kernel.Class) model.Player {
	return model.Player{ID: kernel.NewCharacterID(), Name: name, Class: class}
}

func idsOf(ps []model.Player) []kernel.CharacterID {
	out := make([]kernel.CharacterID, 0, len(ps))
	for _, p := range ps {
		out = append(out, p.ID)
	}
	return out
}

// queued reports whether id stands in the queue: leaving answers anything but ErrNotQueued, which leaves.
func (f fixture) queued(id kernel.CharacterID) bool {
	return !errors.Is(f.svc.Leave(context.Background(), id), model.ErrNotQueued)
}

// A party's leader queues the whole party for a team mode, and each member hears the ticket; a member queues a mode of one a side alone.
func TestALeaderQueuesTheirParty(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	ps := f.party(player("Ann", "warrior"), player("Bo", "thief"), player("Cy", "magician"))
	if err := f.svc.Queue(ctx, ps[0], "team"); err != nil {
		t.Fatal(err)
	}
	for _, p := range ps {
		if q, ok := last[service.Queued](f.notes, p.ID); !ok || q.Mode != "team" || !slices.Equal(q.Members, idsOf(ps)) {
			t.Fatalf("%s heard %+v, want queued for team with the party", p.Name, f.notes.of(p.ID))
		}
	}
	if err := f.svc.Queue(ctx, ps[1], "duel"); !errors.Is(err, model.ErrAlreadyQueued) {
		t.Fatalf("a queued member queueing again: %v, want ErrAlreadyQueued", err)
	}
	solo := newFixture(t)
	mates := solo.party(player("Dee", "warrior"), player("Eli", "warrior"))
	if err := solo.svc.Queue(ctx, mates[1], "duel"); err != nil || !solo.queued(mates[1].ID) || solo.queued(mates[0].ID) {
		t.Fatalf("a member queueing a duel: %v; want them queued alone", err)
	}
}

// A team queue is refused, leaving nobody queued, for a member who isn't the leader, a party too big, or a member who can't fight or is queued; the leader's own refusal stays theirs.
func TestAPartyTicketIsAllOrNothing(t *testing.T) {
	ctx := context.Background()
	for _, tt := range []struct {
		name  string
		setup func(f fixture, ps []model.Player)
		by    int // who queues
		size  int
		want  error
		alone int // a member queued on their own, 0 for none
	}{
		{"a member who isn't the leader", nil, 1, 3, model.ErrNotLeader, 0},
		{"a party over the mode's", nil, 0, 4, model.ErrPartyTooBig, 0},
		{"a member who can't fight", func(f fixture, ps []model.Player) { f.world.cant[ps[2].ID] = model.ErrInMatch }, 0, 3, model.ErrPartyNotReady, 0},
		{"a member queued alone", func(f fixture, ps []model.Player) {
			if err := f.svc.Queue(ctx, ps[1], "duel"); err != nil {
				t.Fatal(err)
			}
		}, 0, 2, model.ErrPartyNotReady, 1},
		{"the leader who can't fight", func(f fixture, ps []model.Player) { f.world.cant[ps[0].ID] = model.ErrInMatch }, 0, 3, model.ErrInMatch, 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			var ps []model.Player
			for k := range tt.size {
				ps = append(ps, player(string(rune('A'+k)), "warrior"))
			}
			f.party(ps...)
			if tt.setup != nil {
				tt.setup(f, ps)
			}
			if err := f.svc.Queue(ctx, ps[tt.by], "team"); !errors.Is(err, tt.want) {
				t.Fatalf("queueing: %v, want %v", err, tt.want)
			}
			for k, p := range ps {
				if k != tt.alone && f.queued(p.ID) {
					t.Fatalf("%s is queued after a refused party ticket", p.Name)
				}
			}
		})
	}
}

// A change to a queued party takes its whole ticket out, telling each member: while queued, and while offered, when the others in the offer go back; once its match opens, nothing is queued to take. One member going offline takes the ticket out too, and a change that leaves the members as they were changes nothing.
func TestAPartyChangeTakesItsTicketOut(t *testing.T) {
	ctx := context.Background()
	left := func(t *testing.T, f fixture, ps []model.Player, reason service.UnqueueReason) {
		t.Helper()
		for _, p := range ps {
			if u, ok := last[service.Unqueued](f.notes, p.ID); !ok || u.Reason != reason || f.queued(p.ID) {
				t.Fatalf("%s heard %+v; want out of the queue, %v", p.Name, f.notes.of(p.ID), reason)
			}
		}
	}
	t.Run("queued", func(t *testing.T) {
		f := newFixture(t)
		ps := f.party(player("Ann", "warrior"), player("Bo", "warrior"))
		if err := f.svc.Queue(ctx, ps[0], "team"); err != nil {
			t.Fatal(err)
		}
		f.svc.PartyChanged(idsOf(ps), idsOf(ps)) // an HP or map change: the same members
		if _, ok := last[service.Unqueued](f.notes, ps[0].ID); ok {
			t.Fatal("a change keeping the members took the ticket out")
		}
		f.svc.PartyChanged([]kernel.CharacterID{ps[1].ID}, nil) // Bo left
		left(t, f, ps, service.UnqueuePartyChanged)
	})
	t.Run("offered", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			f := newFixture(t)
			f.run(t)
			ps := f.party(player("Ann", "warrior"), player("Bo", "warrior"))
			cy := player("Cy", "magician")
			for _, p := range []model.Player{ps[0], cy} {
				if err := f.svc.Queue(ctx, p, "team"); err != nil {
					t.Fatal(err)
				}
			}
			wait(f.data.Pvp().Queue.BotsAfter)
			offer, ok := last[service.Offered](f.notes, cy.ID)
			if bo, _ := last[service.Offered](f.notes, ps[1].ID); !ok || bo.Offer != offer.Offer || offer.Bots != 3 {
				t.Fatalf("Cy heard %+v, Bo %+v; want one offer for the three with three bots", offer, bo)
			}
			f.svc.PartyChanged([]kernel.CharacterID{ps[0].ID, ps[1].ID, kernel.NewCharacterID()}, []kernel.CharacterID{ps[0].ID, ps[1].ID, kernel.NewCharacterID()})
			left(t, f, ps, service.UnqueuePartyChanged)
			if g, ok := last[service.OfferGone](f.notes, cy.ID); !ok || !g.Requeued {
				t.Fatalf("Cy heard %+v; want back in the queue", f.notes.of(cy.ID))
			}
		})
	})
	t.Run("in prep", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			f := newFixture(t)
			f.run(t)
			ps := f.party(player("Ann", "warrior"), player("Bo", "warrior"))
			if err := f.svc.Queue(ctx, ps[0], "team"); err != nil {
				t.Fatal(err)
			}
			wait(f.data.Pvp().Queue.BotsAfter)
			offer, _ := last[service.Offered](f.notes, ps[0].ID)
			for _, p := range ps {
				if err := f.svc.Answer(ctx, p.ID, offer.Offer, true); err != nil {
					t.Fatal(err)
				}
			}
			if len(f.world.opened) != 1 {
				t.Fatalf("opened %+v, want the match", f.world.opened)
			}
			heard := len(f.notes.of(ps[0].ID))
			f.svc.PartyChanged([]kernel.CharacterID{ps[1].ID}, nil)
			if got := f.notes.of(ps[0].ID); len(got) != heard || f.queued(ps[0].ID) || f.queued(ps[1].ID) {
				t.Fatalf("after the match opened, a party change told %+v; want nothing, nobody queued", got[heard:])
			}
		})
	})
	t.Run("a member going offline", func(t *testing.T) {
		f := newFixture(t)
		ps := f.party(player("Ann", "warrior"), player("Bo", "warrior"))
		if err := f.svc.Queue(ctx, ps[0], "team"); err != nil {
			t.Fatal(err)
		}
		f.svc.Offline(ctx, ps[1].ID)
		left(t, f, ps, service.UnqueueLeft)
	})
	t.Run("a player queued alone joining a party", func(t *testing.T) {
		f := newFixture(t)
		cy := player("Cy", "magician")
		if err := f.svc.Queue(ctx, cy, "team"); err != nil {
			t.Fatal(err)
		}
		f.svc.PartyChanged([]kernel.CharacterID{cy.ID}, []kernel.CharacterID{kernel.NewCharacterID(), cy.ID})
		left(t, f, []model.Player{cy}, service.UnqueuePartyChanged)
	})
	t.Run("a change hidden by a break", func(t *testing.T) {
		f := newFixture(t)
		ps := f.party(player("Ann", "warrior"), player("Bo", "warrior"))
		if err := f.svc.Queue(ctx, ps[0], "team"); err != nil {
			t.Fatal(err)
		}
		f.svc.Resync(ctx)
		if !f.queued(ps[0].ID) {
			t.Fatal("a resync with the party as it was took the ticket out")
		}
		if err := f.svc.Queue(ctx, ps[0], "team"); err != nil {
			t.Fatal(err)
		}
		f.social.parties[0].Members = ps[:1] // Bo left unseen
		f.svc.Resync(ctx)
		left(t, f, ps, service.UnqueuePartyChanged)
	})
}

// Every member of a party accepts before its match opens.
func TestEveryMemberAccepts(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newFixture(t)
		f.run(t)
		ctx := context.Background()
		ps := f.party(player("Ann", "warrior"), player("Bo", "warrior"))
		if err := f.svc.Queue(ctx, ps[0], "team"); err != nil {
			t.Fatal(err)
		}
		wait(f.data.Pvp().Queue.BotsAfter)
		offer, _ := last[service.Offered](f.notes, ps[0].ID)
		if err := f.svc.Answer(ctx, ps[0].ID, offer.Offer, true); err != nil || len(f.world.opened) != 0 {
			t.Fatalf("one of two accepting: %v, opened %+v; want it waiting for Bo", err, f.world.opened)
		}
		if err := f.svc.Answer(ctx, ps[1].ID, offer.Offer, true); err != nil || len(f.world.opened) != 1 {
			t.Fatalf("both accepting: %v, opened %+v; want the match", err, f.world.opened)
		}
	})
}

// sideOf is which side of spec holds id, or -1.
func sideOf(spec model.Spec, id kernel.CharacterID) int {
	for k, side := range spec.Sides {
		if slices.ContainsFunc(side, func(p model.Place) bool { return p.Character == id }) {
			return k
		}
	}
	return -1
}

// open accepts every offer each player in ps heard, and gives back the matches opened.
func (f fixture) open(t *testing.T, ps ...model.Player) []model.Spec {
	t.Helper()
	for _, p := range ps {
		if o, ok := last[service.Offered](f.notes, p.ID); ok {
			if err := f.svc.Answer(context.Background(), p.ID, o.Offer, true); err != nil && !errors.Is(err, model.ErrOfferGone) {
				t.Fatal(err)
			}
		}
	}
	return f.world.opened
}

// Six players within each other's windows meet at once, parties whole, at the split that best balances the sides' mean mu.
func TestTeamsSplitPartiesWholeAtTheBestBalance(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newFixture(t)
		f.run(t)
		ctx := context.Background()
		a := f.party(player("A1", "warrior"), player("A2", "warrior"))
		b := f.party(player("B1", "warrior"), player("B2", "warrior"))
		c, d := player("C", "magician"), player("D", "magician")
		mu := map[kernel.CharacterID]float64{a[0].ID: 25.5, a[1].ID: 25.5, b[0].ID: 24.5, b[1].ID: 24.5, c.ID: 26, d.ID: 24}
		for id, m := range mu {
			f.store.ratings[id] = model.Rating{Mu: m, Sigma: 3}
		}
		for _, p := range []model.Player{a[0], b[0], c, d} { // A with D balances at 25 a side; A with C, the first split tried, would be 25.7 to 24.3
			if err := f.svc.Queue(ctx, p, "team"); err != nil {
				t.Fatal(err)
			}
		}
		wait(time.Second)
		all := slices.Concat(a, b, []model.Player{c, d})
		opened := f.open(t, all...)
		if len(opened) != 1 {
			t.Fatalf("opened %+v, want one match", opened)
		}
		spec := opened[0]
		s := sideOf(spec, a[0].ID)
		if sideOf(spec, a[1].ID) != s || sideOf(spec, d.ID) != s || sideOf(spec, b[0].ID) != 1-s || sideOf(spec, b[1].ID) != 1-s || sideOf(spec, c.ID) != 1-s {
			t.Fatalf("spec %+v; want A's party with D, B's with C", spec)
		}
		if len(spec.Sides[0]) != 3 || len(spec.Sides[1]) != 3 || slices.ContainsFunc(slices.Concat(spec.Sides[0], spec.Sides[1]), func(p model.Place) bool { return p.Bot != nil }) {
			t.Fatalf("spec %+v; want three players a side, no bots", spec)
		}
	})
}

// Three parties of two can't make two sides of three whole: they wait, and past bots_after the two oldest meet, a party a side with a bot each, and the third meets bots.
func TestPartiesAreNeverSplit(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newFixture(t)
		f.run(t)
		ctx := context.Background()
		var parties [][]model.Player
		for _, n := range []string{"A", "B", "C"} {
			parties = append(parties, f.party(player(n+"1", "warrior"), player(n+"2", "magician")))
		}
		for _, p := range parties {
			if err := f.svc.Queue(ctx, p[0], "team"); err != nil {
				t.Fatal(err)
			}
		}
		wait(f.data.Pvp().Queue.BotsAfter - time.Second)
		if _, ok := last[service.Offered](f.notes, parties[0][0].ID); ok {
			t.Fatal("offered a match before bots_after: the parties were split")
		}
		wait(time.Second)
		opened := f.open(t, slices.Concat(parties...)...)
		if len(opened) != 2 {
			t.Fatalf("opened %+v, want two matches", opened)
		}
		first := slices.IndexFunc(opened, func(s model.Spec) bool { return sideOf(s, parties[0][0].ID) >= 0 })
		for k, p := range parties {
			spec := opened[first]
			if k == 2 {
				spec = opened[1-first]
			}
			if s := sideOf(spec, p[0].ID); s < 0 || sideOf(spec, p[1].ID) != s || len(spec.Sides[s]) != 3 {
				t.Fatalf("spec %+v; want %s's party whole on one side, a bot beside it", spec, p[0].Name)
			}
		}
		if spec := opened[first]; sideOf(spec, parties[0][0].ID) == sideOf(spec, parties[1][0].ID) {
			t.Fatalf("spec %+v; want the two oldest parties a side each", spec)
		}
	})
}

// Bots never make a class three times on one side, whatever the players there are.
func TestNoSideHoldsAClassThreeTimes(t *testing.T) {
	for seed := range uint64(30) {
		synctest.Test(t, func(t *testing.T) {
			f := seeded(t, seed)
			f.run(t)
			ps := f.party(player("Ann", "warrior"), player("Bo", "warrior"))
			if err := f.svc.Queue(context.Background(), ps[0], "team"); err != nil {
				t.Fatal(err)
			}
			wait(f.data.Pvp().Queue.BotsAfter)
			opened := f.open(t, ps...)
			if len(opened) != 1 {
				t.Fatalf("opened %+v, want one match", opened)
			}
			for k, side := range opened[0].Sides {
				classes := map[kernel.Class]int{}
				for _, p := range side {
					class := kernel.Class("warrior") // Ann and Bo
					if p.Bot != nil {
						class = p.Bot.Class
					}
					if classes[class]++; classes[class] > 2 {
						t.Fatalf("seed %d: side %d holds %v three times: %+v", seed, k+1, class, side)
					}
				}
			}
		})
	}
}

// Each side's bots take the tier nearest its own players' mean mu − sigma, else the other side's; a leaver's stand-in, the tier nearest all the players'.
func TestEachSidesBotsMatchItsPlayers(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newFixture(t)
		f.run(t)
		ctx := context.Background()
		strong := f.party(player("Ann", "warrior"), player("Bo", "warrior"), player("Cy", "warrior"))
		weak := f.party(player("Dee", "warrior"), player("Eli", "warrior"))
		for _, p := range strong {
			f.store.ratings[p.ID] = model.Rating{Mu: 26.5, Sigma: 2} // 4.5 apart: the windows reach it at 12.5 s
		}
		for _, p := range weak {
			f.store.ratings[p.ID] = model.Rating{Mu: 22, Sigma: 2} // 20: adept
		}
		f.svc.Queue(ctx, strong[0], "team") //nolint:errcheck // checked by the offer
		f.svc.Queue(ctx, weak[0], "team")   //nolint:errcheck // checked by the offer
		wait(f.data.Pvp().Queue.BotsAfter)
		opened := f.open(t, slices.Concat(strong, weak)...)
		if len(opened) != 1 {
			t.Fatalf("opened %+v, want one match", opened)
		}
		spec := opened[0]
		w := sideOf(spec, weak[0].ID)
		bot := spec.Sides[w][2].Bot
		if bot == nil || bot.Tier != "adept" || spec.StandIn != "veteran" {
			t.Fatalf("the weak side's bot %+v, stand-in %q; want an adept, and a veteran for the 22.7 of all five", bot, spec.StandIn)
		}
	})
}

// A leaver is rated as their side losing, and told so; their side, winning, is rated by the outcome.
func TestALeaverLosesWhateverTheirSideDoes(t *testing.T) {
	f := newFixture(t)
	ann, bo, cy := kernel.NewCharacterID(), kernel.NewCharacterID(), kernel.NewCharacterID()
	r := model.Result{Match: kernel.NewMatchID(), Mode: "team", Winner: 1}
	r.Sides[0] = []model.Outcome{{Character: ann}, {Character: bo, Left: true}, {Tier: "novice"}}
	r.Sides[1] = []model.Outcome{{Character: cy}, {Tier: "novice"}, {Tier: "novice"}}
	f.svc.Record(context.Background(), r)
	fresh := f.data.Pvp().Rating.Mu
	a, b, c := f.store.ratings[ann], f.store.ratings[bo], f.store.ratings[cy]
	if a.Mu <= fresh || a.Wins != 1 || b.Mu >= fresh || b.Losses != 1 || c.Mu >= fresh || c.Losses != 1 {
		t.Fatalf("Ann %+v, Bo %+v, Cy %+v; want Ann up with a win, Bo who left down with a loss, Cy down", a, b, c)
	}
	if e, ok := last[service.Ended](f.notes, bo); !ok || e.Outcome != service.Loss {
		t.Fatalf("Bo heard %+v; want a loss", f.notes.of(bo))
	}
	if e, _ := last[service.Ended](f.notes, ann); e.Outcome != service.Win {
		t.Fatalf("Ann heard %+v; want a win", f.notes.of(ann))
	}
}
