// Package service is the pvp module's application: the queue and its matcher, offers, bots, and the ratings a match's outcome moves.
package service

import (
	"cmp"
	"context"
	"errors"
	"log/slog"
	"math"
	"math/rand/v2"
	"slices"
	"sync"
	"time"

	"github.com/cenkalti/backoff/v5"

	model "finga/internal/model/pvp"
	"finga/internal/shared/gamedata"
	"finga/internal/shared/kernel"
	"finga/internal/shared/rating"
	"finga/pkg/transient"
	"finga/pkg/uuidv7"
)

// The matcher's pace, and how long an outcome's store is tried before it goes unrated.
const (
	matchEvery  = time.Second
	openTimeout = 10 * time.Second
	recordFor   = time.Minute
)

// Deps are what the service is built from; all are required.
type Deps struct {
	Data     *gamedata.Data
	World    World
	Store    Store
	Notifier Notifier
	Logger   *slog.Logger
	Rand     *rand.Rand // picks bots: their class, name and build
}

// Service is the queue: its tickets, the offers made from them, and the matcher that makes them; one mutex orders every change with what it tells.
type Service struct {
	d       Deps
	rules   gamedata.PvpRules
	mu      sync.Mutex
	tickets map[kernel.CharacterID]*ticket
	offers  map[string]*offer
}

// ticket is a player waiting in a mode's queue, with their rating there and when they joined, which a requeue keeps.
type ticket struct {
	who    kernel.CharacterID
	mode   gamedata.PvpMode
	rating model.Rating
	joined time.Time
	offer  *offer // while one stands
}

// offer is a match found, waiting for its players to accept.
type offer struct {
	id      string
	mode    gamedata.PvpMode
	tickets []*ticket
	bots    int
	yes     map[kernel.CharacterID]bool
	until   time.Time
}

// New builds the service; Run runs its matcher.
func New(d Deps) *Service {
	return &Service{d: d, rules: d.Data.Pvp(), tickets: map[kernel.CharacterID]*ticket{}, offers: map[string]*offer{}}
}

// Queue puts the player in a mode's queue, if the world says they can fight.
func (s *Service) Queue(ctx context.Context, who kernel.CharacterID, modeID string) error {
	mode, ok := s.d.Data.PvpMode(modeID)
	if !ok {
		return model.ErrUnknownMode
	}
	if s.queued(who) {
		return model.ErrAlreadyQueued
	}
	if err := s.d.World.CanFight(ctx, who); err != nil {
		return err
	}
	rt, err := s.ratingOf(ctx, who, mode)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, in := s.tickets[who]; in {
		return model.ErrAlreadyQueued
	}
	s.tickets[who] = &ticket{who: who, mode: mode, rating: rt, joined: time.Now()}
	s.d.Notifier.Push(who, Queued{Mode: mode.ID, Members: []kernel.CharacterID{who}})
	return nil
}

func (s *Service) queued(who kernel.CharacterID) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, in := s.tickets[who]
	return in
}

// ratedMode is the mode whose rating a ticket of mode reads.
func ratedMode(mode gamedata.PvpMode) string { return cmp.Or(mode.RatingFrom, mode.ID) }

// ratingOf is the player's rating in mode, a new one's when they have none.
func (s *Service) ratingOf(ctx context.Context, who kernel.CharacterID, mode gamedata.PvpMode) (model.Rating, error) {
	rt, ok, err := s.d.Store.Rating(ctx, who, ratedMode(mode))
	if err != nil || ok {
		return rt, err
	}
	return s.fresh(), nil
}

func (s *Service) fresh() model.Rating {
	return model.Rating{Mu: s.rules.Rating.Mu, Sigma: s.rules.Rating.Sigma}
}

// Leave takes the player out of the queue; with an offer standing, they decline it.
func (s *Service) Leave(_ context.Context, who kernel.CharacterID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tickets[who]
	if !ok {
		return model.ErrNotQueued
	}
	s.out(t, UnqueueLeft)
	return nil
}

// Offline takes a player who went offline out of the queue, declining any offer, and tells nobody but those it affects.
func (s *Service) Offline(_ context.Context, who kernel.CharacterID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if t, ok := s.tickets[who]; ok {
		s.out(t, UnqueueLeft)
	}
}

// out takes t out of the queue for reason; an offer it's in falls through, the others going back to the queue with their wait kept. The caller holds mu.
func (s *Service) out(t *ticket, reason UnqueueReason) {
	if o := t.offer; o != nil {
		s.fail(o, GoneDeclined, func(o *ticket) bool { return o == t })
	}
	delete(s.tickets, t.who)
	s.d.Notifier.Push(t.who, Unqueued{Reason: reason})
}

// fail ends offer o for reason, telling each player: those leaving go out of the queue, and the rest back to it with their wait kept; it returns those leaving, for the caller to tell why. The caller holds mu.
func (s *Service) fail(o *offer, reason GoneReason, leaving func(*ticket) bool) []*ticket {
	delete(s.offers, o.id)
	var gone []*ticket
	for _, t := range o.tickets {
		t.offer = nil
		out := leaving(t)
		s.d.Notifier.Push(t.who, OfferGone{Offer: o.id, Reason: reason, Requeued: !out})
		if out {
			delete(s.tickets, t.who)
			gone = append(gone, t)
		}
	}
	return gone
}

// Answer accepts or declines an offer: a decline sends its player out of the queue and the others back; the last accept opens the match.
func (s *Service) Answer(ctx context.Context, who kernel.CharacterID, offerID string, accept bool) error {
	s.mu.Lock()
	t, o, err := s.offerOf(who, offerID)
	if err != nil || !accept {
		if err == nil {
			s.fail(o, GoneDeclined, func(o *ticket) bool { return o == t })
			s.d.Notifier.Push(who, Unqueued{Reason: UnqueueDeclined})
		}
		s.mu.Unlock()
		return err
	}
	s.mu.Unlock()
	if err := s.d.World.CanFight(ctx, who); err != nil {
		return err // a dead player's accept waits for them to rise
	}
	s.mu.Lock()
	if t, o, err = s.offerOf(who, offerID); err != nil {
		s.mu.Unlock()
		return err
	}
	o.yes[who] = true
	if len(o.yes) < len(o.tickets) {
		s.mu.Unlock()
		return nil
	}
	delete(s.offers, o.id)
	for _, t := range o.tickets {
		delete(s.tickets, t.who)
	}
	spec := s.spec(o)
	s.mu.Unlock()
	s.open(context.WithoutCancel(ctx), spec, o)
	return nil
}

// offerOf is the player's ticket and the offer named id, if it stands for them. The caller holds mu.
func (s *Service) offerOf(who kernel.CharacterID, id string) (*ticket, *offer, error) {
	t, ok := s.tickets[who]
	o, has := s.offers[id]
	if !ok || !has || t.offer != o {
		return nil, nil, model.ErrOfferGone
	}
	return t, o, nil
}

// open asks the world to open the match; if it can't, its players are told the offer is gone and they're out of the queue.
func (s *Service) open(ctx context.Context, spec model.Spec, o *offer) {
	ctx, cancel := context.WithTimeout(ctx, openTimeout)
	defer cancel()
	err := s.d.World.OpenMatch(ctx, spec)
	if err == nil {
		return
	}
	s.d.Logger.ErrorContext(ctx, "the world didn't open a match", "match_id", spec.ID, "err", err)
	for _, t := range o.tickets {
		s.d.Notifier.Push(t.who, OfferGone{Offer: o.id, Reason: GoneLapsed})
		s.d.Notifier.Push(t.who, Unqueued{Reason: UnqueueLapsed})
	}
}

// Run makes matches each second until ctx ends.
func (s *Service) Run(ctx context.Context) error {
	tick := time.NewTicker(matchEvery)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case now := <-tick.C:
			s.step(now)
		}
	}
}

// step lapses the offers past their time, then pairs each mode's waiting tickets, oldest first, with the nearest in mu within both their windows, and one waiting past bots_after with bots.
func (s *Service) step(now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, o := range s.offersByAge() {
		if now.Before(o.until) {
			continue
		}
		for _, t := range s.fail(o, GoneLapsed, func(t *ticket) bool { return !o.yes[t.who] }) {
			s.d.Notifier.Push(t.who, Unqueued{Reason: UnqueueLapsed})
		}
	}
	waiting := make([]*ticket, 0, len(s.tickets))
	for _, t := range s.tickets {
		if t.offer == nil && t.mode.Team == 1 {
			waiting = append(waiting, t)
		}
	}
	slices.SortFunc(waiting, func(a, b *ticket) int {
		return cmp.Or(a.joined.Compare(b.joined), cmp.Compare(a.who.String(), b.who.String()))
	})
	for _, a := range waiting {
		if a.offer != nil {
			continue
		}
		var best *ticket
		for _, b := range waiting {
			gap := math.Abs(a.rating.Mu - b.rating.Mu)
			if b == a || b.offer != nil || b.mode.ID != a.mode.ID || gap > min(s.window(a, now), s.window(b, now)) {
				continue
			}
			if best == nil || gap < math.Abs(a.rating.Mu-best.rating.Mu) {
				best = b
			}
		}
		switch {
		case best != nil:
			s.offer(a.mode, []*ticket{a, best}, 0, now)
		case now.Sub(a.joined) >= s.rules.Queue.BotsAfter:
			s.offer(a.mode, []*ticket{a}, a.mode.Team, now)
		}
	}
}

func (s *Service) offersByAge() []*offer {
	out := make([]*offer, 0, len(s.offers))
	for _, o := range s.offers {
		out = append(out, o)
	}
	slices.SortFunc(out, func(a, b *offer) int { return cmp.Or(a.until.Compare(b.until), cmp.Compare(a.id, b.id)) })
	return out
}

// window is how far in mu t reaches after waiting since it joined.
func (s *Service) window(t *ticket, now time.Time) float64 {
	w := s.rules.Queue.Window
	return min(w.Start+w.PerSecond*now.Sub(t.joined).Seconds(), w.Most)
}

// offer makes an offer of tickets with bots in the places left and tells each player. The caller holds mu.
func (s *Service) offer(mode gamedata.PvpMode, tickets []*ticket, bots int, now time.Time) {
	o := &offer{id: uuidv7.NewString(), mode: mode, tickets: tickets, bots: bots, yes: map[kernel.CharacterID]bool{}, until: now.Add(s.rules.Queue.Offer)}
	s.offers[o.id] = o
	for _, t := range tickets {
		t.offer = o
		s.d.Notifier.Push(t.who, Offered{Offer: o.id, Mode: mode.ID, Bots: bots, Until: s.rules.Queue.Offer})
	}
}

// spec is the match an accepted offer opens: its players a side each, or all on side 1 against bots of the tier nearest their mu − sigma. The caller holds mu.
func (s *Service) spec(o *offer) model.Spec {
	spec := model.Spec{ID: kernel.NewMatchID(), Mode: o.mode.ID}
	if o.bots == 0 {
		for k, t := range o.tickets {
			spec.Sides[k] = append(spec.Sides[k], model.Place{Character: t.who})
		}
		return spec
	}
	strength := 0.0
	for _, t := range o.tickets {
		spec.Sides[0] = append(spec.Sides[0], model.Place{Character: t.who})
		strength += t.rating.Mu - t.rating.Sigma
	}
	tier := s.tierNear(strength / float64(len(o.tickets)))
	var names []string
	for range o.bots {
		b := s.bot(tier, names)
		names = append(names, b.Name)
		spec.Sides[1] = append(spec.Sides[1], model.Place{Bot: b})
	}
	return spec
}

// tierNear is the bot tier whose mu is nearest mu.
func (s *Service) tierNear(mu float64) gamedata.BotTier {
	tiers := s.rules.Bots.Tiers
	return slices.MinFunc(tiers, func(a, b gamedata.BotTier) int { return cmp.Compare(math.Abs(a.Mu-mu), math.Abs(b.Mu-mu)) })
}

// bot is a bot of tier: a random promoted class, one of its builds, and a name not in taken.
func (s *Service) bot(tier gamedata.BotTier, taken []string) *model.Bot {
	classes := make([]kernel.Class, 0, len(s.rules.Bots.Builds))
	for c := range s.rules.Bots.Builds {
		classes = append(classes, c)
	}
	slices.Sort(classes)
	class := classes[s.d.Rand.IntN(len(classes))]
	builds := s.rules.Bots.Builds[class]
	free := slices.DeleteFunc(slices.Clone(s.rules.Bots.Names), func(n string) bool { return slices.Contains(taken, n) })
	return &model.Bot{ID: kernel.NewCharacterID(), Name: free[s.d.Rand.IntN(len(free))], Class: class, Tier: tier.ID, Bar: builds[s.d.Rand.IntN(len(builds))].Bar}
}

// Record stores a match's outcome and its ratings once, then tells each of its players how it went; a store that keeps failing leaves it unrated.
func (s *Service) Record(ctx context.Context, r model.Result) {
	mode, ok := s.d.Data.PvpMode(r.Mode)
	if !ok {
		s.d.Logger.ErrorContext(ctx, "an outcome of no mode", "match_id", r.Match, "mode", r.Mode)
		return
	}
	var characters []kernel.CharacterID
	for _, side := range r.Sides {
		for _, f := range side {
			if !f.Character.IsZero() {
				characters = append(characters, f.Character)
			}
		}
	}
	var before, after map[kernel.CharacterID]model.Rating
	rate := func(had map[kernel.CharacterID]model.Rating) model.Rated {
		before = map[kernel.CharacterID]model.Rating{}
		for _, c := range characters {
			if rt, ok := had[c]; ok {
				before[c] = rt
			} else {
				before[c] = s.fresh()
			}
		}
		rated := s.rate(r, before)
		after = rated.After
		return rated
	}
	match := model.Match{ID: r.Match, Mode: ratedMode(mode), Started: r.Started, Ended: r.Ended, Winner: r.Winner}
	stored, err := backoff.Retry(ctx, func() (bool, error) {
		stored, err := s.d.Store.Record(ctx, match, characters, rate)
		if err != nil && !errors.Is(err, transient.ErrUnavailable) {
			return false, backoff.Permanent(err)
		}
		return stored, err
	}, backoff.WithMaxElapsedTime(recordFor))
	switch {
	case err != nil:
		s.d.Logger.ErrorContext(ctx, "a match's outcome wasn't stored; it goes unrated", "match_id", r.Match, "err", err)
		before, after = nil, nil
	case !stored:
		return // told already
	}
	for side, fs := range r.Sides {
		for _, f := range fs {
			if f.Character.IsZero() {
				continue
			}
			e := Ended{Outcome: outcomeOf(r.Winner, side+1), Wins: r.Wins}
			if b, ok := before[f.Character]; ok {
				e.Rating = s.change(b, after[f.Character])
			}
			s.d.Notifier.Push(f.Character, e)
		}
	}
}

// rate is what r does to the characters' ratings, from before: bots take part at their tier's mu and a sigma of 1, and keep it.
func (s *Service) rate(r model.Result, before map[kernel.CharacterID]model.Rating) model.Rated {
	var sides [2][]rating.Rating
	for k, side := range r.Sides {
		for _, f := range side {
			sides[k] = append(sides[k], s.ratingIn(f, before))
		}
	}
	score := 0.5
	switch r.Winner {
	case 1:
		score = 1
	case 2:
		score = 0
	}
	rr := s.rules.Rating
	moved := rating.Rate(rating.Rules{Beta: rr.Beta, Tau: rr.Tau}, sides, score)
	out := model.Rated{After: map[kernel.CharacterID]model.Rating{}}
	for k, side := range r.Sides {
		for slot, f := range side {
			fr := model.Fighter{Side: k + 1, Slot: slot, Character: f.Character, Tier: f.Tier, Left: f.Left, Downs: f.Downs}
			fr.Before = model.Rating{Mu: sides[k][slot].Mu, Sigma: sides[k][slot].Sigma}
			fr.After = fr.Before
			if !f.Character.IsZero() {
				b := before[f.Character]
				a := model.Rating{Mu: moved[k][slot].Mu, Sigma: moved[k][slot].Sigma, Matches: b.Matches + 1, Wins: b.Wins, Losses: b.Losses, Draws: b.Draws}
				switch outcomeOf(r.Winner, k+1) {
				case Win:
					a.Wins++
				case Loss:
					a.Losses++
				case Draw:
					a.Draws++
				}
				a.Shown = s.Shown(a)
				fr.Before, fr.After = b, a
				out.After[f.Character] = a
			}
			out.Fighters = append(out.Fighters, fr)
		}
	}
	return out
}

// ratingIn is a fighter's rating going into the match.
func (s *Service) ratingIn(f model.Outcome, before map[kernel.CharacterID]model.Rating) rating.Rating {
	if f.Character.IsZero() {
		for _, t := range s.rules.Bots.Tiers {
			if t.ID == f.Tier {
				return rating.Rating{Mu: t.Mu, Sigma: 1}
			}
		}
		return rating.Rating{Mu: s.rules.Rating.Mu, Sigma: 1}
	}
	b := before[f.Character]
	return rating.Rating{Mu: b.Mu, Sigma: b.Sigma}
}

func outcomeOf(winner, side int) Outcome {
	switch winner {
	case 0:
		return Draw
	case side:
		return Win
	}
	return Loss
}

// change is the shown rating and rank before and after, and the placement matches left.
func (s *Service) change(before, after model.Rating) *RatingChange {
	b, a := s.Shown(before), s.Shown(after)
	return &RatingChange{Before: b, After: a, RankBefore: s.RankOf(b), RankAfter: s.RankOf(a), PlacementLeft: max(0, s.rules.Rating.Placement-after.Matches)}
}

// Shown is a rating as one number, by pvp.yaml's rule.
func (s *Service) Shown(rt model.Rating) int {
	sh := s.rules.Rating.Shown
	return rating.Shown(rating.Rating{Mu: rt.Mu, Sigma: rt.Sigma}, sh.Base, sh.PerPoint)
}

// RankOf is the rank a shown rating is in: the highest it reaches.
func (s *Service) RankOf(shown int) string {
	rank := s.rules.Ranks[0].ID
	for _, r := range s.rules.Ranks {
		if shown >= r.From {
			rank = r.ID
		}
	}
	return rank
}
