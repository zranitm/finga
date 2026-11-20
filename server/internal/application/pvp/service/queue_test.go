package service_test

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
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
	"finga/pkg/panics"
	"finga/pkg/transient"
)

// world is the world as the matcher sees it: who can't fight, the matches it opened and the builds it was sent, which fail with buildErr; while hold is open, an open waits for it.
type world struct {
	mu       sync.Mutex
	cant     map[kernel.CharacterID]error
	opened   []model.Spec
	hold     chan struct{}
	builds   map[kernel.CharacterID][]model.Build
	buildErr error
}

func (w *world) SetBuild(_ context.Context, id kernel.CharacterID, b model.Build) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.builds[id] = append(w.builds[id], b)
	return w.buildErr
}

func (w *world) sent(id kernel.CharacterID) []model.Build {
	w.mu.Lock()
	defer w.mu.Unlock()
	return slices.Clone(w.builds[id])
}

func (w *world) CanFight(_ context.Context, id kernel.CharacterID) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.cant[id]
}

func (w *world) OpenMatch(_ context.Context, spec model.Spec) error {
	w.mu.Lock()
	hold := w.hold
	w.mu.Unlock()
	if hold != nil {
		<-hold
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.opened = append(w.opened, spec)
	return nil
}

// store keeps ratings, matches and builds in memory, as the Postgres store does: the next lost Records store, then fail as a commit whose answer was lost; a match in hold waits for it to close. The board and a profile read modes, ratings by mode; buildErr fails every build read and save.
type store struct {
	mu       sync.Mutex
	ratings  map[kernel.CharacterID]model.Rating
	matches  map[kernel.MatchID]model.Rated
	lost     int
	hold     map[kernel.MatchID]chan struct{}
	modes    map[string]map[kernel.CharacterID]model.Rating
	builds   map[kernel.CharacterID]model.Build
	buildErr error
	boards   int                                  // the boards read
	stall    map[kernel.CharacterID]bool          // whose build reads hang till their ctx ends
	stalled  map[kernel.CharacterID]time.Duration // how long each hung read lasted
	hang     bool                                 // board reads hang till their ctx ends
}

func newStore() *store {
	return &store{ratings: map[kernel.CharacterID]model.Rating{}, matches: map[kernel.MatchID]model.Rated{}, hold: map[kernel.MatchID]chan struct{}{}, modes: map[string]map[kernel.CharacterID]model.Rating{}, builds: map[kernel.CharacterID]model.Build{}, stall: map[kernel.CharacterID]bool{}, stalled: map[kernel.CharacterID]time.Duration{}}
}

func (s *store) Ratings(_ context.Context, id kernel.CharacterID) (map[string]model.Rating, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string]model.Rating{}
	for mode, rs := range s.modes {
		if r, ok := rs[id]; ok {
			out[mode] = r
		}
	}
	return out, nil
}

func (s *store) Board(ctx context.Context, mode string, most int) ([]model.Ranked, error) {
	s.mu.Lock()
	s.boards++
	hang := s.hang
	s.mu.Unlock()
	if hang {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []model.Ranked
	for id, r := range s.modes[mode] {
		if r.Placed {
			out = append(out, model.Ranked{Character: id, Rating: r})
		}
	}
	slices.SortFunc(out, func(a, b model.Ranked) int {
		return cmp.Or(cmp.Compare(b.Rating.Shown, a.Rating.Shown), slices.Compare(a.Character[:], b.Character[:]))
	})
	return out[:min(most, len(out))], nil
}

func (s *store) Above(_ context.Context, mode string, shown int) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, r := range s.modes[mode] {
		if r.Placed && r.Shown > shown {
			n++
		}
	}
	return n, nil
}

func (s *store) Build(ctx context.Context, id kernel.CharacterID) (model.Build, bool, error) {
	s.mu.Lock()
	stall := s.stall[id]
	s.mu.Unlock()
	if stall {
		start := time.Now()
		<-ctx.Done()
		s.mu.Lock()
		s.stalled[id] = time.Since(start)
		s.mu.Unlock()
		return model.Build{}, false, ctx.Err()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.builds[id]
	return b, ok, s.buildErr
}

func (s *store) PutBuild(_ context.Context, id kernel.CharacterID, b model.Build) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.buildErr != nil {
		return 0, s.buildErr
	}
	b.Revision = s.builds[id].Revision + 1
	s.builds[id] = b
	return b.Revision, nil
}

// characters are the characters pvp reads: whose, their class, world bar and traits, and names.
type characters struct {
	mu sync.Mutex
	of map[kernel.CharacterID]model.Character
	as map[kernel.CharacterID]model.Named
}

func (c *characters) Character(_ context.Context, id kernel.CharacterID) (model.Character, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	ch, ok := c.of[id]
	if !ok {
		return model.Character{}, model.ErrNoCharacter
	}
	return ch, nil
}

func (c *characters) Names(_ context.Context, ids []kernel.CharacterID) (map[kernel.CharacterID]model.Named, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := map[kernel.CharacterID]model.Named{}
	for _, id := range ids {
		if n, ok := c.as[id]; ok {
			out[id] = n
		}
	}
	return out, nil
}

func (s *store) Rating(_ context.Context, id kernel.CharacterID, _ string) (model.Rating, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.ratings[id]
	return r, ok, nil
}

func (s *store) Record(_ context.Context, m model.Match, characters []kernel.CharacterID, rate func(map[kernel.CharacterID]model.Rating) model.Rated) ([]model.Fighter, error) {
	s.mu.Lock()
	hold := s.hold[m.ID]
	s.mu.Unlock()
	if hold != nil {
		<-hold
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if done, ok := s.matches[m.ID]; ok {
		var out []model.Fighter
		for _, f := range done.Fighters {
			if !f.Character.IsZero() {
				r := s.ratings[f.Character]
				f.After = model.Rating{Mu: f.After.Mu, Sigma: f.After.Sigma, Matches: r.Matches, Wins: r.Wins, Losses: r.Losses, Draws: r.Draws}
				f.Before = model.Rating{Mu: f.Before.Mu, Sigma: f.Before.Sigma}
				out = append(out, f)
			}
		}
		return out, nil
	}
	had := map[kernel.CharacterID]model.Rating{}
	for _, c := range characters {
		if r, ok := s.ratings[c]; ok {
			had[c] = r
		}
	}
	rated := rate(had)
	s.matches[m.ID] = rated
	maps.Copy(s.ratings, rated.After)
	if s.lost > 0 {
		s.lost--
		return nil, fmt.Errorf("%w: the commit's answer was lost", transient.ErrUnavailable)
	}
	return rated.Fighters, nil
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
	for _, d := range slices.Backward(n.of(id)) {
		if d, ok := d.(T); ok {
			return d, true
		}
	}
	return zero, false
}

// social is the parties as the queue sees them; after runs once, after the next read, and fail fails every read.
type social struct {
	mu      sync.Mutex
	parties []model.Party
	after   func()
	fail    error
}

func (s *social) PartyOf(_ context.Context, id kernel.CharacterID) (model.Party, bool, error) {
	s.mu.Lock()
	var party model.Party
	ok := false
	for _, p := range s.parties {
		if slices.ContainsFunc(p.Members, func(m model.Player) bool { return m.ID == id }) {
			party, ok = p, true
			break
		}
	}
	after, fail := s.after, s.fail
	s.after = nil
	s.mu.Unlock()
	if after != nil {
		after()
	}
	return party, ok, fail
}

// FollowParties is the module's to run; a test calls PartyChanged and Resync itself.
func (*social) FollowParties(context.Context, func(_, _ []kernel.CharacterID), func(context.Context)) {
}

type fixture struct {
	svc    *service.Service
	world  *world
	store  *store
	chars  *characters
	notes  *notes
	social *social
	data   *gamedata.Data
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	return seeded(t, 1)
}

// seeded is a fixture whose bots are drawn from seed.
func seeded(t *testing.T, seed uint64) fixture {
	t.Helper()
	data := gamedatatest.Shipped(t)
	f := fixture{world: &world{cant: map[kernel.CharacterID]error{}, builds: map[kernel.CharacterID][]model.Build{}}, store: newStore(), chars: &characters{of: map[kernel.CharacterID]model.Character{}, as: map[kernel.CharacterID]model.Named{}}, notes: &notes{got: map[kernel.CharacterID][]service.Delivery{}}, social: &social{}, data: data}
	log := slog.New(slog.DiscardHandler)
	f.svc = service.New(service.Deps{Data: data, World: f.world, Social: f.social, Characters: f.chars, Store: f.store, Notifier: f.notes, Logger: log, Panics: panics.NewRecorder(log, nil), Rand: rand.New(rand.NewPCG(seed, 2))}) //nolint:gosec // a test's bots
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
			if err := f.svc.Queue(ctx, model.Player{ID: id}, "duel"); err != nil {
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
			if err := f.svc.Queue(context.Background(), model.Player{ID: id}, "duel"); err != nil {
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
		if err := f.svc.Queue(context.Background(), model.Player{ID: ayla}, "duel"); err != nil {
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
			if err := f.svc.Queue(ctx, model.Player{ID: id}, "duel"); err != nil {
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
			if err := f.svc.Queue(ctx, model.Player{ID: id}, "duel"); err != nil {
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
		if err := f.svc.Queue(ctx, model.Player{ID: ayla}, "battle"); !errors.Is(err, model.ErrUnknownMode) {
			t.Fatalf("an unknown mode: %v", err)
		}
		if err := f.svc.Queue(ctx, model.Player{ID: ayla}, "duel"); err != nil {
			t.Fatal(err)
		}
		if err := f.svc.Queue(ctx, model.Player{ID: ayla}, "duel"); !errors.Is(err, model.ErrAlreadyQueued) {
			t.Fatalf("queueing twice: %v", err)
		}
		inMatch := errors.New("in a match")
		f.world.cant[bruno] = inMatch
		if err := f.svc.Queue(ctx, model.Player{ID: bruno}, "duel"); !errors.Is(err, inMatch) {
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
		f.svc.Record(context.Background(), r)
		again, _ := last[service.Ended](f.notes, ayla)
		if f.store.ratings[ayla] != rt || again.Rating == nil || *again.Rating != *e.Rating {
			t.Fatalf("a repeat moved the rating to %+v, or told %+v; want it kept and told as before, %+v", f.store.ratings[ayla], again.Rating, *e.Rating)
		}
	})
}

// A store whose commit landed but whose answer was lost is tried again, finds the match stored, and tells each player the rating as stored: one MatchEnded, rated.
func TestALostCommitStillTellsTheRating(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newFixture(t)
		f.store.lost = 1
		ayla := kernel.NewCharacterID()
		f.svc.Record(context.Background(), model.Result{Match: kernel.NewMatchID(), Mode: "duel", Winner: 2, Sides: [2][]model.Outcome{{{Character: ayla}}, {{Tier: "novice"}}}})
		var ended []service.Ended
		for _, d := range f.notes.of(ayla) {
			if e, ok := d.(service.Ended); ok {
				ended = append(ended, e)
			}
		}
		if len(ended) != 1 || ended[0].Outcome != service.Loss || ended[0].Rating == nil || ended[0].Rating.After >= ended[0].Rating.Before || ended[0].Rating.PlacementLeft != f.data.Pvp().Rating.Placement-1 {
			t.Fatalf("Ayla heard %+v, want one loss with its rating", ended)
		}
	})
}

// Each outcome is stored on its own: one whose store hangs holds up no other match's MatchEnded, and Outcomes waits for it before returning.
func TestASlowStoreHoldsUpNoOtherOutcome(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newFixture(t)
		ayla, robin := kernel.NewCharacterID(), kernel.NewCharacterID()
		slow, quick := kernel.NewMatchID(), kernel.NewMatchID()
		f.store.hold[slow] = make(chan struct{})
		ctx, cancel := context.WithCancel(context.Background())
		results := make(chan model.Result, 2)
		results <- model.Result{Match: slow, Mode: "duel", Winner: 1, Sides: [2][]model.Outcome{{{Character: ayla}}, {{Tier: "novice"}}}}
		results <- model.Result{Match: quick, Mode: "duel", Winner: 1, Sides: [2][]model.Outcome{{{Character: robin}}, {{Tier: "novice"}}}}
		done := make(chan error, 1)
		go func() {
			done <- f.svc.Outcomes(ctx, func(ctx context.Context, record func(model.Result)) error {
				for {
					select {
					case r := <-results:
						record(r)
					case <-ctx.Done():
						return nil
					}
				}
			})
		}()
		synctest.Wait()
		if _, ok := last[service.Ended](f.notes, robin); !ok {
			t.Fatal("Robin's outcome waited for Ayla's store")
		}
		if _, ok := last[service.Ended](f.notes, ayla); ok {
			t.Fatal("Ayla heard before her match was stored")
		}
		cancel()
		synctest.Wait()
		select {
		case err := <-done:
			t.Fatalf("Outcomes returned with a store under way: %v", err)
		default:
		}
		close(f.store.hold[slow])
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		if e, ok := last[service.Ended](f.notes, ayla); !ok || e.Rating == nil {
			t.Fatalf("Ayla heard %+v, want her rated win", f.notes.of(ayla))
		}
	})
}

// The bot takes no name of a player in the match: each name bots use, given to the player, is never the bot's.
func TestABotIsNamedUnlikeItsOpponent(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newFixture(t)
		f.run(t)
		ctx := context.Background()
		for k, name := range f.data.Pvp().Bots.Names {
			id := kernel.NewCharacterID()
			if err := f.svc.Queue(ctx, model.Player{ID: id, Name: name}, "duel"); err != nil {
				t.Fatal(err)
			}
			wait(f.data.Pvp().Queue.BotsAfter)
			offer, ok := last[service.Offered](f.notes, id)
			if !ok {
				t.Fatalf("%s got no offer", name)
			}
			if err := f.svc.Answer(ctx, id, offer.Offer, true); err != nil {
				t.Fatal(err)
			}
			if bot := f.world.opened[k].Sides[1][0].Bot; bot.Name == name {
				t.Fatalf("the bot against %s took their name", name)
			}
		}
	})
}

// From the last accept until the world has opened the match, its players can't queue again: the world doesn't know they're in it yet.
func TestNoQueueingWhileAMatchOpens(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newFixture(t)
		f.run(t)
		ctx := context.Background()
		ayla := kernel.NewCharacterID()
		if err := f.svc.Queue(ctx, model.Player{ID: ayla}, "duel"); err != nil {
			t.Fatal(err)
		}
		wait(f.data.Pvp().Queue.BotsAfter)
		offer, _ := last[service.Offered](f.notes, ayla)
		f.world.mu.Lock()
		f.world.hold = make(chan struct{})
		f.world.mu.Unlock()
		accepted := make(chan error, 1)
		go func() { accepted <- f.svc.Answer(ctx, ayla, offer.Offer, true) }()
		synctest.Wait()
		if err := f.svc.Queue(ctx, model.Player{ID: ayla}, "duel"); !errors.Is(err, model.ErrInMatch) {
			t.Fatalf("queueing while the match opens: %v, want ErrInMatch", err)
		}
		close(f.world.hold)
		if err := <-accepted; err != nil {
			t.Fatal(err)
		}
		if err := f.svc.Queue(ctx, model.Player{ID: ayla}, "duel"); err != nil {
			t.Fatalf("queueing once it's open, as the world allows: %v", err)
		}
	})
}

// The matcher never opens a match of bots alone, which a match instance would run to its end: bots fill only the places of players still waiting, so a player who leaves before their bot, declines it, or lets it lapse takes that match with them, and one sent back by another's decline still meets a bot of their own.
func TestTheMatcherNeverOpensAMatchOfBotsAlone(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newFixture(t)
		f.run(t)
		ctx := context.Background()
		q := f.data.Pvp().Queue
		queue := func(name string) kernel.CharacterID {
			id := kernel.NewCharacterID()
			if err := f.svc.Queue(ctx, model.Player{ID: id, Name: name}, "duel"); err != nil {
				t.Fatal(err)
			}
			return id
		}
		answer := func(id kernel.CharacterID, accept bool) {
			offer, ok := last[service.Offered](f.notes, id)
			if !ok {
				t.Fatalf("no offer for %v: %+v", id, f.notes.of(id))
			}
			if err := f.svc.Answer(ctx, id, offer.Offer, accept); err != nil {
				t.Fatal(err)
			}
		}
		ayla := queue("Ayla")
		wait(q.BotsAfter - time.Second)
		if err := f.svc.Leave(ctx, ayla); err != nil {
			t.Fatal(err)
		}
		robin := queue("Robin")
		wait(q.BotsAfter)
		answer(robin, false)
		cleo := queue("Cleo")
		wait(q.BotsAfter + q.Offer + time.Second)
		if _, ok := last[service.Offered](f.notes, cleo); !ok {
			t.Fatal("Cleo had no bot to let lapse")
		}
		dan := queue("Dan")
		wait(q.BotsAfter)
		answer(dan, true)
		eve, finn := queue("Eve"), queue("Finn")
		wait(time.Second)
		answer(eve, true)
		answer(finn, false)
		wait(q.BotsAfter)
		answer(eve, true)
		wait(q.BotsAfter + q.Offer)

		accepted := map[kernel.CharacterID]bool{dan: true, eve: true}
		if len(f.world.opened) != 2 {
			t.Fatalf("opened %+v, want Dan's and Eve's against bots", f.world.opened)
		}
		for _, spec := range f.world.opened {
			players := 0
			for _, side := range spec.Sides {
				for _, fr := range side {
					if fr.Bot == nil {
						players++
						if !accepted[fr.Character] {
							t.Fatalf("%v is in %+v without accepting", fr.Character, spec)
						}
					}
				}
			}
			if players == 0 {
				t.Fatalf("opened a match of bots alone: %+v", spec)
			}
		}
	})
}
