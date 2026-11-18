package service_test

import (
	"context"
	"errors"
	"log/slog"
	"math/rand/v2"
	"slices"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"finga/internal/application/pvp/service"
	model "finga/internal/model/pvp"
	"finga/internal/shared/gamedata"
	"finga/internal/shared/gamedata/gamedatatest"
	"finga/internal/shared/kernel"
)

// world is the world as the matcher sees it: who can't fight, and the matches it opened.
type world struct {
	mu     sync.Mutex
	cant   map[kernel.CharacterID]error
	opened []model.Spec
}

func (w *world) CanFight(_ context.Context, id kernel.CharacterID) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.cant[id]
}

func (w *world) OpenMatch(_ context.Context, spec model.Spec) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.opened = append(w.opened, spec)
	return nil
}

// store keeps ratings and matches in memory, as the Postgres store does.
type store struct {
	mu       sync.Mutex
	ratings  map[kernel.CharacterID]model.Rating
	matches  map[kernel.MatchID]model.Rated
	failures int // Record fails as unavailable this many times first
}

func newStore() *store {
	return &store{ratings: map[kernel.CharacterID]model.Rating{}, matches: map[kernel.MatchID]model.Rated{}}
}

func (s *store) Rating(_ context.Context, id kernel.CharacterID, _ string) (model.Rating, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.ratings[id]
	return r, ok, nil
}

func (s *store) Record(_ context.Context, m model.Match, characters []kernel.CharacterID, rate func(map[kernel.CharacterID]model.Rating) model.Rated) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.matches[m.ID]; ok {
		return false, nil
	}
	had := map[kernel.CharacterID]model.Rating{}
	for _, c := range characters {
		if r, ok := s.ratings[c]; ok {
			had[c] = r
		}
	}
	rated := rate(had)
	s.matches[m.ID] = rated
	for c, r := range rated.After {
		s.ratings[c] = r
	}
	return true, nil
}

// notes records what each player is told.
type notes struct {
	mu  sync.Mutex
	got map[kernel.CharacterID][]service.Delivery
}

func (n *notes) Push(to kernel.CharacterID, d service.Delivery) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.got[to] = append(n.got[to], d)
}

func (n *notes) of(id kernel.CharacterID) []service.Delivery {
	n.mu.Lock()
	defer n.mu.Unlock()
	return slices.Clone(n.got[id])
}

// last is the latest delivery of type T to id.
func last[T service.Delivery](n *notes, id kernel.CharacterID) (T, bool) {
	var zero T
	got := n.of(id)
	for k := len(got) - 1; k >= 0; k-- {
		if d, ok := got[k].(T); ok {
			return d, true
		}
	}
	return zero, false
}

type fixture struct {
	svc   *service.Service
	world *world
	store *store
	notes *notes
	data  *gamedata.Data
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	data := gamedatatest.Shipped(t)
	f := fixture{world: &world{cant: map[kernel.CharacterID]error{}}, store: newStore(), notes: &notes{got: map[kernel.CharacterID][]service.Delivery{}}, data: data}
	f.svc = service.New(service.Deps{Data: data, World: f.world, Store: f.store, Notifier: f.notes, Logger: slog.New(slog.DiscardHandler), Rand: rand.New(rand.NewPCG(1, 2))}) //nolint:gosec // a test's bots
	return f
}

// run runs the matcher until the test ends.
func (f fixture) run(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); _ = f.svc.Run(ctx) }()
	t.Cleanup(func() { cancel(); <-done })
}

func wait(d time.Duration) {
	time.Sleep(d)
	synctest.Wait()
}

// Two players near in rating are paired as soon as the matcher runs, and both accepting opens their match a side each.
func TestTwoPlayersNearInRatingMeet(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newFixture(t)
		f.run(t)
		ctx := context.Background()
		ayla, robin := kernel.NewCharacterID(), kernel.NewCharacterID()
		for _, id := range []kernel.CharacterID{ayla, robin} {
			if err := f.svc.Queue(ctx, id, "duel"); err != nil {
				t.Fatal(err)
			}
		}
		if q, ok := last[service.Queued](f.notes, ayla); !ok || q.Mode != "duel" {
			t.Fatalf("Ayla heard %+v, want queued for duel", f.notes.of(ayla))
		}
		wait(time.Second)
		offer, ok := last[service.Offered](f.notes, ayla)
		if other, _ := last[service.Offered](f.notes, robin); !ok || offer.Bots != 0 || offer.Offer != other.Offer || offer.Until != f.data.Pvp().Queue.Offer {
			t.Fatalf("offers %+v and %+v, want one for both, no bots", offer, other)
		}
		for _, id := range []kernel.CharacterID{ayla, robin} {
			if err := f.svc.Answer(ctx, id, offer.Offer, true); err != nil {
				t.Fatal(err)
			}
		}
		if len(f.world.opened) != 1 {
			t.Fatalf("opened %+v, want one match", f.world.opened)
		}
		spec := f.world.opened[0]
		if spec.Mode != "duel" || len(spec.Sides[0]) != 1 || len(spec.Sides[1]) != 1 || spec.Sides[0][0].Character == spec.Sides[1][0].Character {
			t.Fatalf("spec %+v, want Ayla and Robin a side each", spec)
		}
		if err := f.svc.Leave(ctx, ayla); !errors.Is(err, model.ErrNotQueued) {
			t.Fatalf("leaving after the match opened: %v, want ErrNotQueued", err)
		}
	})
}

// Players far apart wait, their windows widening each second, until they reach each other — or bots_after, whichever comes first.
func TestAWindowWidensUntilTheyMeet(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newFixture(t)
		ayla, robin := kernel.NewCharacterID(), kernel.NewCharacterID()
		f.store.ratings[ayla] = model.Rating{Mu: 25, Sigma: 3}
		f.store.ratings[robin] = model.Rating{Mu: 27.5, Sigma: 3} // 2.5 apart: the window reaches it after 2.5 s
		f.run(t)
		for _, id := range []kernel.CharacterID{ayla, robin} {
			if err := f.svc.Queue(context.Background(), id, "duel"); err != nil {
				t.Fatal(err)
			}
		}
		wait(2 * time.Second)
		if _, ok := last[service.Offered](f.notes, ayla); ok {
			t.Fatal("paired before their windows reached each other")
		}
		wait(time.Second)
		if o, ok := last[service.Offered](f.notes, ayla); !ok || o.Bots != 0 {
			t.Fatalf("after 3 s: %+v, want an offer against Robin", f.notes.of(ayla))
		}
	})
}

// A player alone past bots_after is offered a match against a bot of the tier nearest their mu − sigma, with a class, a name and a build from the data.
func TestAPlayerAloneMeetsABot(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newFixture(t)
		f.run(t)
		ayla := kernel.NewCharacterID()
		if err := f.svc.Queue(context.Background(), ayla, "duel"); err != nil {
			t.Fatal(err)
		}
		bots := f.data.Pvp().Queue.BotsAfter
		wait(bots - time.Second)
		if _, ok := last[service.Offered](f.notes, ayla); ok {
			t.Fatal("offered bots before bots_after")
		}
		wait(time.Second)
		offer, ok := last[service.Offered](f.notes, ayla)
		if !ok || offer.Bots != 1 {
			t.Fatalf("Ayla heard %+v, want an offer with a bot", f.notes.of(ayla))
		}
		if err := f.svc.Answer(context.Background(), ayla, offer.Offer, true); err != nil {
			t.Fatal(err)
		}
		bot := f.world.opened[0].Sides[1][0].Bot
		// A new player's mu − sigma is 25 − 8.333: the novice tier's 15 is nearest.
		if bot == nil || bot.Tier != "novice" || bot.Name == "" || len(bot.Bar) != gamedata.BarKeys || bot.ID.IsZero() {
			t.Fatalf("bot %+v, want a novice with a name and a build", bot)
		}
	})
}

// A decline takes its player out of the queue, and the other goes back with their wait kept: bots come at bots_after from when they first joined.
func TestADeclineSendsOneOutAndTheOtherBack(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newFixture(t)
		f.run(t)
		ctx := context.Background()
		ayla, robin := kernel.NewCharacterID(), kernel.NewCharacterID()
		for _, id := range []kernel.CharacterID{ayla, robin} {
			if err := f.svc.Queue(ctx, id, "duel"); err != nil {
				t.Fatal(err)
			}
		}
		wait(time.Second)
		offer, _ := last[service.Offered](f.notes, ayla)
		if err := f.svc.Answer(ctx, robin, offer.Offer, false); err != nil {
			t.Fatal(err)
		}
		if g, _ := last[service.OfferGone](f.notes, robin); g.Requeued {
			t.Fatal("the decliner is back in the queue")
		}
		if u, ok := last[service.Unqueued](f.notes, robin); !ok || u.Reason != service.UnqueueDeclined {
			t.Fatalf("Robin heard %+v, want unqueued for declining", f.notes.of(robin))
		}
		if g, ok := last[service.OfferGone](f.notes, ayla); !ok || !g.Requeued || g.Reason != service.GoneDeclined {
			t.Fatalf("Ayla heard %+v, want back in the queue", f.notes.of(ayla))
		}
		if err := f.svc.Answer(ctx, ayla, offer.Offer, true); !errors.Is(err, model.ErrOfferGone) {
			t.Fatalf("accepting a declined offer: %v, want ErrOfferGone", err)
		}
		wait(f.data.Pvp().Queue.BotsAfter - time.Second) // bots_after since she joined, a second less since she went back
		if o, _ := last[service.Offered](f.notes, ayla); o.Bots != 1 {
			t.Fatal("the wait didn't count from when Ayla first joined")
		}
	})
}

// An offer not accepted in time lapses: those who accepted go back to the queue, the rest out.
func TestAnOfferLapses(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newFixture(t)
		f.run(t)
		ctx := context.Background()
		ayla, robin := kernel.NewCharacterID(), kernel.NewCharacterID()
		for _, id := range []kernel.CharacterID{ayla, robin} {
			if err := f.svc.Queue(ctx, id, "duel"); err != nil {
				t.Fatal(err)
			}
		}
		wait(time.Second)
		offer, _ := last[service.Offered](f.notes, ayla)
		if err := f.svc.Answer(ctx, ayla, offer.Offer, true); err != nil {
			t.Fatal(err)
		}
		wait(f.data.Pvp().Queue.Offer)
		if u, ok := last[service.Unqueued](f.notes, robin); !ok || u.Reason != service.UnqueueLapsed {
			t.Fatalf("Robin heard %+v, want out for letting it lapse", f.notes.of(robin))
		}
		if g, ok := last[service.OfferGone](f.notes, ayla); !ok || !g.Requeued || g.Reason != service.GoneLapsed {
			t.Fatalf("Ayla heard %+v, want back in the queue", f.notes.of(ayla))
		}
		if len(f.world.opened) != 0 {
			t.Fatal("a lapsed offer opened a match")
		}
	})
}

// Queueing is refused for an unknown mode, twice, or when the world says no; a dead player's accept is refused and the offer stands.
func TestQueueRefusals(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newFixture(t)
		ctx := context.Background()
		ayla, bruno := kernel.NewCharacterID(), kernel.NewCharacterID()
		if err := f.svc.Queue(ctx, ayla, "battle"); !errors.Is(err, model.ErrUnknownMode) {
			t.Fatalf("an unknown mode: %v", err)
		}
		if err := f.svc.Queue(ctx, ayla, "duel"); err != nil {
			t.Fatal(err)
		}
		if err := f.svc.Queue(ctx, ayla, "duel"); !errors.Is(err, model.ErrAlreadyQueued) {
			t.Fatalf("queueing twice: %v", err)
		}
		inMatch := errors.New("in a match")
		f.world.cant[bruno] = inMatch
		if err := f.svc.Queue(ctx, bruno, "duel"); !errors.Is(err, inMatch) {
			t.Fatalf("the world's no: %v", err)
		}
		if err := f.svc.Leave(ctx, bruno); !errors.Is(err, model.ErrNotQueued) {
			t.Fatalf("leaving no queue: %v", err)
		}
		if err := f.svc.Leave(ctx, ayla); err != nil {
			t.Fatal(err)
		}
		if u, ok := last[service.Unqueued](f.notes, ayla); !ok || u.Reason != service.UnqueueLeft {
			t.Fatalf("Ayla heard %+v, want unqueued for leaving", f.notes.of(ayla))
		}
	})
}

// An outcome rates its players once, bots keeping theirs, and tells each their result and rating's change; told again, it does nothing.
func TestAnOutcomeRatesItsPlayersOnce(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newFixture(t)
		ayla := kernel.NewCharacterID()
		r := model.Result{
			Match: kernel.NewMatchID(), Mode: "duel", Winner: 1, Wins: [2]int{2, 1},
			Sides: [2][]model.Outcome{{{Character: ayla}}, {{Tier: "novice"}}},
		}
		f.svc.Record(context.Background(), r)
		e, ok := last[service.Ended](f.notes, ayla)
		if !ok || e.Outcome != service.Win || e.Wins != [2]int{2, 1} || e.Rating == nil {
			t.Fatalf("Ayla heard %+v, want her win with its rating", f.notes.of(ayla))
		}
		if e.Rating.Before != 1000 || e.Rating.After <= e.Rating.Before || e.Rating.PlacementLeft != f.data.Pvp().Rating.Placement-1 || e.Rating.RankBefore != "bronze" {
			t.Fatalf("rating change %+v", *e.Rating)
		}
		rt := f.store.ratings[ayla]
		if rt.Matches != 1 || rt.Wins != 1 || rt.Mu <= 25 || rt.Sigma >= 8.333 {
			t.Fatalf("Ayla's row %+v", rt)
		}
		rows := f.store.matches[r.Match].Fighters
		if len(rows) != 2 || rows[1].Tier != "novice" || rows[1].Before != rows[1].After || rows[1].Before.Sigma != 1 {
			t.Fatalf("fighters %+v, want the bot at its tier, unchanged", rows)
		}
		n := len(f.notes.of(ayla))
		f.svc.Record(context.Background(), r)
		if len(f.notes.of(ayla)) != n || f.store.ratings[ayla] != rt {
			t.Fatal("a repeated outcome moved the rating again")
		}
	})
}
