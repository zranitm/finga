// Package service is the pvp module's application: the queue and its matcher, offers, bots, and the ratings a match's outcome moves.
package service

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"math"
	"math/rand/v2"
	"slices"
	"sync"
	"time"

	"github.com/cenkalti/backoff/v5"
	"golang.org/x/sync/errgroup"

	model "finga/internal/model/pvp"
	"finga/internal/shared/gamedata"
	"finga/internal/shared/kernel"
	"finga/internal/shared/rating"
	"finga/pkg/panics"
	"finga/pkg/transient"
	"finga/pkg/uuidv7"
)

// The matcher's pace, and how long an outcome's store is tried before it goes unrated.
const (
	matchEvery   = time.Second
	openTimeout  = 10 * time.Second
	recordFor    = time.Minute
	storesAtOnce = 16 // outcomes stored at once; more wait, and the world's queue holds the rest
)

// Deps are what the service is built from; all are required.
type Deps struct {
	Data       *gamedata.Data
	World      World
	Social     Social
	Characters Characters
	Store      Store
	Items      Items
	Notifier   Notifier
	Logger     *slog.Logger
	Panics     *panics.Recorder
	Rand       *rand.Rand // picks bots: their class, name and build
}

// Service is the queue: its tickets, the offers made from them, and the matcher that makes them; one mutex orders every change with what it tells.
type Service struct {
	d       Deps
	rules   gamedata.PvpRules
	mu      sync.Mutex
	tickets map[kernel.CharacterID]*ticket // by each member
	offers  map[string]*offer
	opening map[kernel.CharacterID]bool // accepted, while the world opens their match
	// openingBots are the bots of the matches the world is opening; room is how many more bots a step may offer.
	openingBots, room int
	boards            boards
	lifeCtx           context.Context // Run's while it runs: the module's life, which the reads detached from their callers end with
}

// ticket is a player, or a party from its leader, waiting in a mode's queue: each member's rating there, and when they joined, which a requeue keeps.
type ticket struct {
	members []member // the one who queued first
	mode    gamedata.PvpMode
	joined  time.Time
	offer   *offer // while one stands
	waiting bool   // told its match waits for room for its bots
}

// member is a ticket's character as the matcher weighs them.
type member struct {
	model.Player
	rating model.Rating
}

func (t *ticket) ids() []kernel.CharacterID {
	out := make([]kernel.CharacterID, 0, len(t.members))
	for _, m := range t.members {
		out = append(out, m.ID)
	}
	return out
}

// mu is the mean of the members' mu, where the ticket stands in the queue.
func (t *ticket) mu() float64 {
	sum := 0.0
	for _, m := range t.members {
		sum += m.rating.Mu
	}
	return sum / float64(len(t.members))
}

// offer is a match found, waiting for its players to accept: each side's tickets, and bots in the places left.
type offer struct {
	id    string
	mode  gamedata.PvpMode
	sides [2][]*ticket
	yes   map[kernel.CharacterID]bool
	until time.Time
}

func (o *offer) tickets() []*ticket { return slices.Concat(o.sides[0], o.sides[1]) }

func (o *offer) players() int { return players(o.tickets()) }

func (o *offer) bots() int { return 2*o.mode.Team - o.players() }

// accepted reports whether every member of t said yes.
func (o *offer) accepted(t *ticket) bool {
	return len(o.silent(t)) == 0
}

// silent is t's members who haven't accepted o.
func (o *offer) silent(t *ticket) []kernel.CharacterID {
	var out []kernel.CharacterID
	for _, m := range t.members {
		if !o.yes[m.ID] {
			out = append(out, m.ID)
		}
	}
	return out
}

func players(ts []*ticket) int {
	n := 0
	for _, t := range ts {
		n += len(t.members)
	}
	return n
}

// New builds the service; Run runs its matcher.
func New(d Deps) *Service {
	return &Service{d: d, rules: d.Data.Pvp(), tickets: map[kernel.CharacterID]*ticket{}, offers: map[string]*offer{}, opening: map[kernel.CharacterID]bool{}, boards: boards{read: map[string]board{}}, lifeCtx: context.Background()}
}

// Queue puts the player in a mode's queue, with their party when the mode takes one, which only its leader queues; the world must say each of them can fight.
func (s *Service) Queue(ctx context.Context, p model.Player, modeID string) error {
	mode, ok := s.d.Data.PvpMode(modeID)
	if !ok {
		return model.ErrUnknownMode
	}
	members, err := s.membersOf(ctx, p, mode)
	if err != nil {
		return err
	}
	for k := range members {
		m := &members[k]
		if err := s.free(m.ID); err != nil {
			return asMember(k, err)
		}
		if err := s.d.World.CanFight(ctx, m.ID); err != nil {
			return asMember(k, err)
		}
		if m.rating, err = s.ratingOf(ctx, m.ID, mode); err != nil {
			return err
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, m := range members {
		if err := s.taken(m.ID); err != nil {
			return asMember(k, err)
		}
	}
	// A change in the checks above found no ticket to take out: read the party again where the next change will find one.
	again, err := s.membersOf(ctx, p, mode)
	if err != nil {
		return err
	}
	if !sameSet(ids(again), ids(members)) {
		return model.ErrPartyChanged
	}
	t := &ticket{members: members, mode: mode, joined: time.Now()}
	for _, m := range members {
		s.tickets[m.ID] = t
	}
	s.tell(t, Queued{Mode: mode.ID, Members: t.ids()})
	return nil
}

// membersOf are who p's ticket holds: p alone in a mode of one a side, else p's party with p first, which only its leader queues.
func (s *Service) membersOf(ctx context.Context, p model.Player, mode gamedata.PvpMode) ([]member, error) {
	out := []member{{Player: p}}
	if mode.Party == 1 {
		return out, nil
	}
	party, ok, err := s.d.Social.PartyOf(ctx, p.ID)
	switch {
	case err != nil:
		return nil, err
	case !ok || len(party.Members) < 2:
		return out, nil
	case party.Leader != p.ID:
		return nil, model.ErrNotLeader
	case len(party.Members) > mode.Party:
		return nil, model.ErrPartyTooBig
	}
	for _, m := range party.Members {
		if m.ID != p.ID {
			out = append(out, member{Player: m})
		}
	}
	return out, nil
}

// asMember is a refusal met by a ticket's k-th member: the queuer's own, and for any other, that the party isn't ready.
func asMember(k int, err error) error {
	if k == 0 {
		return err
	}
	return fmt.Errorf("%w: %v", model.ErrPartyNotReady, err) //nolint:errorlint // the member's refusal is the party's, not theirs
}

// tell pushes d to each of t's members. The caller holds mu.
func (s *Service) tell(t *ticket, d Delivery) {
	for _, m := range t.members {
		s.d.Notifier.Push(m.ID, d)
	}
}

func (s *Service) free(who kernel.CharacterID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.taken(who)
}

// taken refuses a player queued already, or whose match is opening: until it opens, the world doesn't know they're in it. The caller holds mu.
func (s *Service) taken(who kernel.CharacterID) error {
	switch {
	case s.tickets[who] != nil:
		return model.ErrAlreadyQueued
	case s.opening[who]:
		return model.ErrInMatch
	}
	return nil
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

// Leave takes the player's ticket, their party's with them, out of the queue; with an offer standing, they decline it.
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

// Offline takes the ticket of a player who went offline out of the queue, as their leaving, declining any offer, and tells nobody but those it affects.
func (s *Service) Offline(_ context.Context, who kernel.CharacterID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if t, ok := s.tickets[who]; ok {
		s.out(t, UnqueueLeft)
	}
}

// PartyChanged takes out of the queue each ticket in a mode that takes parties whose members are no longer a character's party, now members, none for out of one: a member left, or one joined, or a player queued alone joined one.
func (s *Service) PartyChanged(characters, members []kernel.CharacterID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, id := range characters {
		t, ok := s.tickets[id]
		if !ok || t.mode.Party == 1 {
			continue
		}
		now := members
		if len(now) == 0 {
			now = []kernel.CharacterID{id}
		}
		if !sameSet(now, t.ids()) {
			s.out(t, UnqueuePartyChanged)
		}
	}
}

// Resync looks again at each ticket in a mode that takes parties, as a break in the party stream may have hidden a change to one.
func (s *Service) Resync(ctx context.Context) {
	s.mu.Lock()
	var check []kernel.CharacterID
	for _, t := range s.unique() {
		if t.mode.Party > 1 {
			check = append(check, t.members[0].ID)
		}
	}
	s.mu.Unlock()
	for _, id := range check {
		party, ok, err := s.d.Social.PartyOf(ctx, id)
		if err != nil {
			s.d.Logger.WarnContext(ctx, "a queued party couldn't be looked at again: its ticket goes out", "character_id", id, "err", err)
			s.mu.Lock()
			if t := s.tickets[id]; t != nil {
				s.out(t, UnqueuePartyChanged)
			}
			s.mu.Unlock()
			continue
		}
		var members []kernel.CharacterID
		if ok {
			for _, m := range party.Members {
				members = append(members, m.ID)
			}
		}
		s.PartyChanged([]kernel.CharacterID{id}, members)
	}
}

func ids(ms []member) []kernel.CharacterID {
	out := make([]kernel.CharacterID, 0, len(ms))
	for _, m := range ms {
		out = append(out, m.ID)
	}
	return out
}

func sameSet(a, b []kernel.CharacterID) bool {
	return len(a) == len(b) && !slices.ContainsFunc(a, func(id kernel.CharacterID) bool { return !slices.Contains(b, id) })
}

// unique are the tickets standing, each once, oldest first. The caller holds mu.
func (s *Service) unique() []*ticket {
	var out []*ticket
	for id, t := range s.tickets {
		if t.members[0].ID == id {
			out = append(out, t)
		}
	}
	slices.SortFunc(out, func(a, b *ticket) int {
		return cmp.Or(a.joined.Compare(b.joined), cmp.Compare(a.members[0].ID.String(), b.members[0].ID.String()))
	})
	return out
}

// out takes t out of the queue for reason; an offer it's in falls through, the others going back to the queue with their wait kept. The caller holds mu.
func (s *Service) out(t *ticket, reason UnqueueReason) {
	if o := t.offer; o != nil {
		s.fail(o, GoneDeclined, func(o *ticket) bool { return o == t })
	}
	s.drop(t)
	s.tell(t, Unqueued{Reason: reason})
}

// drop forgets t under each of its members. The caller holds mu.
func (s *Service) drop(t *ticket) {
	for _, m := range t.members {
		delete(s.tickets, m.ID)
	}
}

// fail ends offer o for reason, telling each player: those leaving go out of the queue, and the rest back to it with their wait kept; it returns those leaving, for the caller to tell why. The caller holds mu.
func (s *Service) fail(o *offer, reason GoneReason, leaving func(*ticket) bool) []*ticket {
	delete(s.offers, o.id)
	var gone []*ticket
	for _, t := range o.tickets() {
		t.offer = nil
		out := leaving(t)
		s.tell(t, OfferGone{Offer: o.id, Reason: reason, Requeued: !out})
		if out {
			s.drop(t)
			gone = append(gone, t)
		}
	}
	return gone
}

// Answer accepts or declines an offer: a decline sends its player's ticket out of the queue, their party's with them, and the others back; the last accept opens the match.
func (s *Service) Answer(ctx context.Context, who kernel.CharacterID, offerID string, accept bool) error {
	s.mu.Lock()
	t, o, err := s.offerOf(who, offerID)
	if err != nil || !accept {
		if err == nil {
			s.fail(o, GoneDeclined, func(o *ticket) bool { return o == t })
			s.tell(t, Unqueued{Reason: UnqueueDeclined, By: []kernel.CharacterID{who}})
		}
		s.mu.Unlock()
		return err
	}
	s.mu.Unlock()
	if err := s.d.World.CanFight(ctx, who); err != nil {
		return err // a dead player's accept waits for them to rise
	}
	s.mu.Lock()
	if _, o, err = s.offerOf(who, offerID); err != nil {
		s.mu.Unlock()
		return err
	}
	o.yes[who] = true
	if len(o.yes) < o.players() {
		s.mu.Unlock()
		return nil
	}
	delete(s.offers, o.id)
	for _, t := range o.tickets() {
		s.drop(t)
		for _, m := range t.members {
			s.opening[m.ID] = true
		}
	}
	spec := s.spec(o)
	s.openingBots += o.bots()
	s.mu.Unlock()
	s.open(context.WithoutCancel(ctx), spec, o)
	s.mu.Lock()
	s.openingBots -= o.bots() // the world counts them once it has opened the match
	for _, t := range o.tickets() {
		for _, m := range t.members {
			delete(s.opening, m.ID)
		}
	}
	s.mu.Unlock()
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
		s.sendBuilds(ctx, spec)
		return
	}
	s.d.Logger.ErrorContext(ctx, "the world didn't open a match", "match_id", spec.ID, "err", err)
	for _, t := range o.tickets() {
		s.tell(t, OfferGone{Offer: o.id, Reason: GoneLapsed})
		s.tell(t, Unqueued{Reason: UnqueueLapsed})
	}
}

// Run makes matches each second until ctx ends, the module's life.
func (s *Service) Run(ctx context.Context) error {
	s.mu.Lock()
	s.lifeCtx = ctx
	s.mu.Unlock()
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

// step lapses the offers past their time, then matches each mode's waiting tickets, oldest first: one a side in pairs, round modes' teams in sides, and a battle's dealt; every offer's bots within the room bots.most leaves beside the bots running, offered and opening.
func (s *Service) step(now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, o := range s.offersByAge() {
		if now.Before(o.until) {
			continue
		}
		for _, t := range s.fail(o, GoneLapsed, func(t *ticket) bool { return !o.accepted(t) }) {
			s.tell(t, Unqueued{Reason: UnqueueLapsed, By: o.silent(t)})
		}
	}
	s.room = s.rules.Bots.Most - s.d.World.Bots() - s.openingBots
	for _, o := range s.offers {
		s.room -= o.bots()
	}
	byMode := map[string][]*ticket{}
	for _, t := range s.unique() {
		if t.offer == nil {
			byMode[t.mode.ID] = append(byMode[t.mode.ID], t)
		}
	}
	for _, id := range slices.Sorted(maps.Keys(byMode)) {
		waiting := byMode[id]
		switch mode := waiting[0].mode; {
		case mode.Team == 1:
			s.pair(waiting, now)
		case mode.Battle():
			s.battle(waiting, now)
		default:
			s.teams(waiting, now)
		}
	}
}

// pair pairs each waiting ticket, oldest first, with the nearest in mu within both their windows, and one waiting past bots_after with a bot. The caller holds mu.
func (s *Service) pair(waiting []*ticket, now time.Time) {
	for _, a := range waiting {
		if a.offer != nil {
			continue
		}
		var best *ticket
		for _, b := range waiting {
			gap := math.Abs(a.mu() - b.mu())
			if b == a || b.offer != nil || gap > min(s.window(a, now), s.window(b, now)) {
				continue
			}
			if best == nil || gap < math.Abs(a.mu()-best.mu()) {
				best = b
			}
		}
		switch {
		case best != nil:
			s.offer(a.mode, [2][]*ticket{{a}, {best}}, now)
		case now.Sub(a.joined) >= s.rules.Queue.BotsAfter:
			s.offer(a.mode, [2][]*ticket{{a}, nil}, now)
		}
	}
}

// teams builds, for each waiting ticket oldest first, a match of the tickets within each other's windows, oldest first and parties whole: two full sides at the split that best balances their mean mu, or, once it has waited bots_after, the players found with bots in the places left. The caller holds mu.
func (s *Service) teams(waiting []*ticket, now time.Time) {
	team := waiting[0].mode.Team
	for _, a := range waiting {
		if a.offer != nil {
			continue
		}
		group := []*ticket{a}
		for _, b := range waiting {
			n := players(group) + len(b.members)
			if b == a || b.offer != nil || n > 2*team || !s.near(b, group, now) {
				continue
			}
			if _, ok := split(append(slices.Clone(group), b), team, false); n == 2*team && !ok {
				continue // no two full sides of these parties
			}
			group = append(group, b)
		}
		full := players(group) == 2*team
		if !full && now.Sub(a.joined) < s.rules.Queue.BotsAfter {
			continue
		}
		if sides, ok := split(group, team, !full); ok {
			s.offer(a.mode, sides, now)
		}
	}
}

// battle deals a battle once its waiting tickets hold both sides, or the oldest has waited bots_after: the oldest that fit both sides, parties whole, as deal sides them, with bots in the places left. It's an open queue, with no windows. The caller holds mu.
func (s *Service) battle(waiting []*ticket, now time.Time) {
	mode := waiting[0].mode
	if players(waiting) < 2*mode.Team && now.Sub(waiting[0].joined) < s.rules.Queue.BotsAfter {
		return
	}
	var in []*ticket
	n := 0
	for _, t := range waiting {
		if n+len(t.members) <= 2*mode.Team {
			in, n = append(in, t), n+len(t.members)
		}
	}
	s.offer(mode, deal(in, mode.Team), now)
}

// deal sides the tickets, parties whole, team players a side at most: high mu to low, each to the side with fewer players, ties to the lower total mu. One that doesn't fit there fits neither side, and is left out for the next deal.
func deal(ts []*ticket, team int) [2][]*ticket {
	byMu := slices.Clone(ts)
	slices.SortStableFunc(byMu, func(a, b *ticket) int { return cmp.Compare(b.mu(), a.mu()) })
	var sides [2][]*ticket
	var n [2]int
	var sum [2]float64
	for _, t := range byMu {
		k := 0
		if n[1] < n[0] || n[1] == n[0] && sum[1] < sum[0] {
			k = 1
		}
		if n[k]+len(t.members) > team {
			continue // nor does it fit the other side, which holds as many or more
		}
		sides[k] = append(sides[k], t)
		n[k] += len(t.members)
		for _, m := range t.members {
			sum[k] += m.rating.Mu
		}
	}
	return sides
}

// near reports whether b is within the window of every ticket in group, and each within b's.
func (s *Service) near(b *ticket, group []*ticket, now time.Time) bool {
	return !slices.ContainsFunc(group, func(t *ticket) bool {
		return math.Abs(b.mu()-t.mu()) > min(s.window(b, now), s.window(t, now))
	})
}

// split is the tickets in two sides of team players each, parties whole, at the least gap between the sides' mean mu; with bots, a side may hold fewer, a side with no players counting as even, so players who fit on one side fight together. The first ticket stays on the first side, and of equal splits the first found is kept.
func split(ts []*ticket, team int, bots bool) ([2][]*ticket, bool) {
	var best [2][]*ticket
	gap, found := math.Inf(1), false
	for mask := 1; mask < 1<<len(ts); mask += 2 {
		var sides [2][]*ticket
		for k, t := range ts {
			sides[1-mask>>k&1] = append(sides[1-mask>>k&1], t)
		}
		n0, n1 := players(sides[0]), players(sides[1])
		if n0 > team || n1 > team || !bots && (n0 != team || n1 != team) {
			continue
		}
		g := 0.0
		if n1 > 0 {
			g = math.Abs(meanMu(sides[0]) - meanMu(sides[1]))
		}
		if g < gap {
			best, gap, found = sides, g, true
		}
	}
	return best, found
}

// meanMu is the mean mu of the tickets' members.
func meanMu(ts []*ticket) float64 {
	sum, n := 0.0, 0
	for _, t := range ts {
		for _, m := range t.members {
			sum += m.rating.Mu
			n++
		}
	}
	return sum / float64(n)
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

// offer makes an offer of sides, with bots in the places left, and tells each player; with no room for its bots, its tickets wait, each told once that it waits and again before its offer. A match of players alone never waits. The caller holds mu.
func (s *Service) offer(mode gamedata.PvpMode, sides [2][]*ticket, now time.Time) {
	o := &offer{id: uuidv7.NewString(), mode: mode, sides: sides, yes: map[kernel.CharacterID]bool{}, until: now.Add(s.rules.Queue.Offer)}
	room := o.bots() == 0 || o.bots() <= s.room
	for _, t := range o.tickets() {
		if t.waiting != !room {
			t.waiting = !room
			s.tell(t, Queued{Mode: mode.ID, Since: now.Sub(t.joined), Members: t.ids(), WaitingRoom: t.waiting})
		}
	}
	if !room {
		return
	}
	s.room -= o.bots()
	s.offers[o.id] = o
	for _, t := range o.tickets() {
		t.offer = o
		s.tell(t, Offered{Offer: o.id, Mode: mode.ID, Bots: o.bots(), Until: s.rules.Queue.Offer})
	}
}

// spec is the match an accepted offer opens: each side's players, then bots in its places left, of the tier nearest its players' mean mu − sigma, else the other side's, with no class past the mode's cap on a side and every name unlike anyone's in it. A team mode's leaver's stand-in takes the tier nearest all the players'. The caller holds mu.
func (s *Service) spec(o *offer) model.Spec {
	spec := model.Spec{ID: kernel.NewMatchID(), Mode: o.mode.ID}
	var names []string
	var classes [2][]kernel.Class
	var strength [2][]float64
	for k, side := range o.sides {
		for _, t := range side {
			for _, m := range t.members {
				spec.Sides[k] = append(spec.Sides[k], model.Place{Character: m.ID})
				names = append(names, m.Name)
				classes[k] = append(classes[k], m.Class)
				strength[k] = append(strength[k], m.rating.Mu-m.rating.Sigma)
			}
		}
	}
	spec.StandIn = s.tierNear(mean(slices.Concat(strength[0], strength[1]))).ID
	for k := range spec.Sides {
		of := strength[k]
		if len(of) == 0 {
			of = strength[1-k]
		}
		tier := s.tierNear(mean(of))
		for range o.mode.Team - len(spec.Sides[k]) {
			b := s.bot(tier, names, classes[k], o.mode.ClassCap())
			names, classes[k] = append(names, b.Name), append(classes[k], b.Class)
			spec.Sides[k] = append(spec.Sides[k], model.Place{Bot: b})
		}
	}
	return spec
}

func mean(xs []float64) float64 {
	sum := 0.0
	for _, x := range xs {
		sum += x
	}
	return sum / float64(len(xs))
}

// tierNear is the bot tier whose mu is nearest mu.
func (s *Service) tierNear(mu float64) gamedata.BotTier {
	tiers := s.rules.Bots.Tiers
	return slices.MinFunc(tiers, func(a, b gamedata.BotTier) int { return cmp.Compare(math.Abs(a.Mu-mu), math.Abs(b.Mu-mu)) })
}

// bot is a bot of tier: a random promoted class its side holds fewer than most of, one of its builds, and a name not in taken.
func (s *Service) bot(tier gamedata.BotTier, taken []string, side []kernel.Class, most int) *model.Bot {
	classes := make([]kernel.Class, 0, len(s.rules.Bots.Builds))
	for c := range s.rules.Bots.Builds {
		if count(side, c) < most {
			classes = append(classes, c)
		}
	}
	slices.Sort(classes)
	class := classes[s.d.Rand.IntN(len(classes))]
	builds := s.rules.Bots.Builds[class]
	free := slices.DeleteFunc(slices.Clone(s.rules.Bots.Names), func(n string) bool { return slices.Contains(taken, n) })
	return &model.Bot{ID: kernel.NewCharacterID(), Name: free[s.d.Rand.IntN(len(free))], Class: class, Tier: tier.ID, Bar: builds[s.d.Rand.IntN(len(builds))].Bar}
}

func count(of []kernel.Class, c kernel.Class) int {
	n := 0
	for _, x := range of {
		if x == c {
			n++
		}
	}
	return n
}

// Outcomes stores each outcome follow hands it on its own goroutine, storesAtOnce at most, so one slow store holds up no other match's; it waits for those under way before it returns.
func (s *Service) Outcomes(ctx context.Context, follow func(ctx context.Context, record func(model.Result)) error) error {
	var stores errgroup.Group
	stores.SetLimit(storesAtOnce)
	err := follow(ctx, func(r model.Result) {
		stores.Go(func() error {
			s.d.Panics.Do(ctx, panics.FlowJob, func() { s.Record(ctx, r) }, "job", "record_match", "match_id", r.Match)
			return nil
		})
	})
	_ = stores.Wait()
	return err
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
	rate := func(had map[kernel.CharacterID]model.Rating) model.Rated {
		before := map[kernel.CharacterID]model.Rating{}
		for _, c := range characters {
			if rt, ok := had[c]; ok {
				before[c] = rt
			} else {
				before[c] = s.fresh()
			}
		}
		return s.rate(r, mode.Rated, before)
	}
	match := model.Match{ID: r.Match, Mode: mode.ID, Started: r.Started, Ended: r.Ended, Winner: r.Winner}
	fighters, err := backoff.Retry(ctx, func() ([]model.Fighter, error) {
		fighters, err := s.d.Store.Record(ctx, match, characters, rate)
		if err != nil && !errors.Is(err, transient.ErrUnavailable) {
			return nil, backoff.Permanent(err)
		}
		return fighters, err
	}, backoff.WithMaxElapsedTime(recordFor))
	if err != nil {
		s.d.Logger.ErrorContext(ctx, "a match's outcome wasn't stored; it goes unrated", "match_id", r.Match, "err", err)
	}
	changes := map[kernel.CharacterID]*RatingChange{}
	for _, f := range fighters {
		if !f.Character.IsZero() && mode.Rated {
			changes[f.Character] = s.change(f.Before, f.After)
		}
	}
	for side, fs := range r.Sides {
		for _, f := range fs {
			if !f.Character.IsZero() {
				s.d.Notifier.Push(f.Character, Ended{Mode: r.Mode, Outcome: outcomeFor(f, r.Winner, side+1), Battle: mode.Battle(), Wins: r.Wins, Score: r.Score, Rating: changes[f.Character], Left: f.Left, Match: r.Match, Away: f.Away})
			}
		}
	}
	grantsCtx, cancel := context.WithTimeout(ctx, recordFor) // one budget for the match's grants, however many wait on a wardrobe that's down
	defer cancel()
	for _, f := range fighters {
		if f.Character.IsZero() {
			continue
		}
		if mode.Rated {
			s.grantRanks(grantsCtx, f)
		}
		if f.Side == r.Winner && !f.Left {
			s.grantWins(grantsCtx, f)
		}
	}
}

// grantWins gives a winner's account the pieces of each feat their wins over every mode reach, and tells them of those new to it (outfits.md section 4), retrying till ctx ends; one lost to a failure comes with their next win's, which asks again.
func (s *Service) grantWins(ctx context.Context, f model.Fighter) {
	var feats []gamedata.Feat
	for _, ft := range s.d.Data.Feats() {
		if ft.When.PvpWins > 0 {
			feats = append(feats, ft)
		}
	}
	if len(feats) == 0 {
		return
	}
	added, err := backoff.Retry(ctx, func() ([]model.FeatPiece, error) {
		pieces, err := s.winPieces(ctx, f.Character, feats)
		if err == nil && len(pieces) == 0 {
			return nil, nil
		}
		if err == nil {
			var c model.Character
			if c, err = s.d.Characters.Character(ctx, f.Character); err == nil {
				var added []model.FeatPiece
				if added, err = s.d.Items.GrantFeatPieces(ctx, c.Account, pieces); err == nil {
					return added, nil
				}
			}
		}
		if !errors.Is(err, transient.ErrUnavailable) {
			return nil, backoff.Permanent(err)
		}
		return nil, err
	}, backoff.WithMaxElapsedTime(0))
	if err != nil {
		s.d.Logger.ErrorContext(ctx, "a win feat's piece wasn't granted; the next win grants it", "character_id", f.Character, "err", err)
		return
	}
	if len(added) > 0 {
		s.d.Notifier.Push(f.Character, PiecesAdded{Feats: added})
	}
}

// winPieces is the pieces of each of feats the character's wins over every mode, this match's counted, reach.
func (s *Service) winPieces(ctx context.Context, id kernel.CharacterID, feats []gamedata.Feat) ([]model.FeatPiece, error) {
	ratings, err := s.d.Store.Ratings(ctx, id)
	if err != nil {
		return nil, err
	}
	wins := 0
	for _, rt := range ratings {
		wins += rt.Wins
	}
	var pieces []model.FeatPiece
	for _, ft := range feats {
		if ft.When.PvpWins <= wins {
			for _, p := range s.d.Data.FeatPieces(ft.ID) {
				pieces = append(pieces, model.FeatPiece{Piece: p, Feat: ft.ID})
			}
		}
	}
	return pieces, nil
}

// grantRanks gives the fighter's account the piece of each rank their shown rating reaches, once they're placed, and tells them of those new to it (outfits.md section 4), retrying till ctx ends; one lost to a failure comes with their next rated match's.
func (s *Service) grantRanks(ctx context.Context, f model.Fighter) {
	if f.After.Matches < s.rules.Rating.Placement {
		return // placing: no rank yet
	}
	shown := s.shown(f.After)
	var pieces []model.RankPiece
	for _, r := range s.rules.Ranks {
		if p, ok := s.d.Data.RankPiece(r.ID); ok && shown >= r.From {
			pieces = append(pieces, model.RankPiece{Piece: p.ID, Rank: r.ID})
		}
	}
	if len(pieces) == 0 {
		return
	}
	added, err := backoff.Retry(ctx, func() ([]model.RankPiece, error) {
		c, err := s.d.Characters.Character(ctx, f.Character)
		if err == nil {
			var added []model.RankPiece
			if added, err = s.d.Items.GrantRankPieces(ctx, c.Account, pieces); err == nil {
				return added, nil
			}
		}
		if !errors.Is(err, transient.ErrUnavailable) {
			return nil, backoff.Permanent(err)
		}
		return nil, err
	}, backoff.WithMaxElapsedTime(0))
	if err != nil {
		s.d.Logger.ErrorContext(ctx, "a rank's piece wasn't granted; the next rated match grants it", "character_id", f.Character, "err", err)
		return
	}
	if len(added) > 0 {
		s.d.Notifier.Push(f.Character, PiecesAdded{Pieces: added})
	}
}

// outcomeFor is how the match went for f, on side: a loss for one who left, whatever their side's.
func outcomeFor(f model.Outcome, winner, side int) Outcome {
	if f.Left {
		return Loss
	}
	return outcomeOf(winner, side)
}

// rate is what r does to the characters' ratings, from before: bots take part at their tier's mu and a sigma of 1, and keep it. A leaver is rated as their side losing; their side by the outcome, the stand-in that took their place counted at their rating. An unrated mode's rows count the record alone: mu and sigma stay, and they're never placed.
func (s *Service) rate(r model.Result, rated bool, before map[kernel.CharacterID]model.Rating) model.Rated {
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
	rules := rating.Rules{Beta: rr.Beta, Tau: rr.Tau}
	moved, lost := sides, [2][2][]rating.Rating{sides, sides}
	if rated {
		moved = rating.Rate(rules, sides, score)
		lost = [2][2][]rating.Rating{rating.Rate(rules, sides, 0), rating.Rate(rules, sides, 1)} // as side 1 loses, and as side 2 does
	}
	out := model.Rated{After: map[kernel.CharacterID]model.Rating{}}
	for k, side := range r.Sides {
		for slot, f := range side {
			fr := model.Fighter{Side: k + 1, Slot: slot, Character: f.Character, Tier: f.Tier, Left: f.Left, Downs: f.Downs, Untold: f.Away && !f.Character.IsZero()}
			fr.Before = model.Rating{Mu: sides[k][slot].Mu, Sigma: sides[k][slot].Sigma}
			fr.After = fr.Before
			if !f.Character.IsZero() {
				b, m := before[f.Character], moved[k][slot]
				if f.Left {
					m = lost[k][k][slot]
				}
				a := model.Rating{Mu: m.Mu, Sigma: m.Sigma, Matches: b.Matches + 1, Wins: b.Wins, Losses: b.Losses, Draws: b.Draws}
				switch outcomeFor(f, r.Winner, k+1) {
				case Win:
					a.Wins++
				case Loss:
					a.Losses++
				case Draw:
					a.Draws++
				}
				a.Shown, a.Placed = s.shown(a), rated && a.Matches >= s.rules.Rating.Placement
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
	b, a := s.shown(before), s.shown(after)
	return &RatingChange{Before: b, After: a, RankBefore: s.rankOf(b), RankAfter: s.rankOf(a), PlacementLeft: max(0, s.rules.Rating.Placement-after.Matches)}
}

// shown is a rating as one number, by pvp.yaml's rule.
func (s *Service) shown(rt model.Rating) int {
	sh := s.rules.Rating.Shown
	return rating.Shown(rating.Rating{Mu: rt.Mu, Sigma: rt.Sigma}, sh.Base, sh.PerPoint)
}

// rankOf is the rank a shown rating is in: the highest it reaches.
func (s *Service) rankOf(shown int) string {
	rank := s.rules.Ranks[0].ID
	for _, r := range s.rules.Ranks {
		if shown >= r.From {
			rank = r.ID
		}
	}
	return rank
}
