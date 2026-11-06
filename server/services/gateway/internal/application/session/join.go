package session

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"time"

	"github.com/cenkalti/backoff/v5"

	"finga/internal/kernel"
	"finga/pkg/panics"
	"finga/services/gateway/internal/model"
)

const (
	leaveTimeout   = 5 * time.Second
	offlineTimeout = 5 * time.Second
	ghostQuiet     = time.Second
	joinRetryFirst = 20 * time.Millisecond
	joinRetryMax   = 500 * time.Millisecond
	// After the world's stream breaks every player rejoins: each after a random pause up to rejoinSpread,
	// and no more than rejoinsAtOnce at a time, so a world that just restarted is not hit by all at once.
	rejoinSpread  = 500 * time.Millisecond
	rejoinsAtOnce = 32
)

// Join is a Hello's join in progress: the session holds the character and its deliveries wait until Finish or Release.
type Join struct {
	Joined model.Joined

	r        *Registry
	s        *Session
	previous *Session // the character's session before this one
	subs     uint64   // the world subscriptions when the join began
	unlock   func()   // lets the character's next join go
	done     bool     // Finish or Release ran
}

// Join joins character to the world through s's connection, retrying while the world cannot take it
// yet, then claims it for s. Deliveries wait until Finish sends Welcome; on an error nothing stays held or claimed.
func (r *Registry) Join(ctx context.Context, s *Session, account kernel.AccountID, sessionID kernel.SessionID, character kernel.CharacterID) (*Join, error) {
	// The character's other joins wait, and deliveries are held so a Snapshot that beats the join follows Welcome.
	unlock, err := r.lockCharacter(ctx, character)
	if err != nil {
		return nil, err
	}
	claimed := false
	defer func() {
		if !claimed {
			unlock() // the join failed or panicked
		}
	}()
	s.out.hold()
	r.mu.Lock()
	s.id.Store(&identity{account: account, sessionID: sessionID, character: model.Player{ID: character}})
	// A join succeeds only once the world has our stream, so the first subscription is never a break.
	subs := max(r.worldSubs, 1)
	r.mu.Unlock()

	joined, err := r.join(ctx, s.connID, account, character)
	if err != nil {
		s.out.abort()
		r.mu.Lock()
		s.id.Store(nil)
		older := r.byCharacter[character]
		r.mu.Unlock()
		if ctx.Err() != nil && older != nil && older != s {
			older.End(model.EndLoggedInElsewhere) // the world may have joined us anyway, taking the character from it
		}
		return nil, err
	}
	// Only once the world has loaded the character as the account's may its social deliveries reach s.
	r.mu.Lock()
	previous := r.byCharacter[character]
	r.byCharacter[character] = s
	r.mu.Unlock()
	claimed = true
	return &Join{Joined: joined, r: r, s: s, previous: previous, subs: subs, unlock: unlock}, nil
}

// Finish makes s the account's, the login session's and the character's active session as player,
// sends welcome ahead of the held deliveries, and returns the older sessions s replaced. If the
// world's stream broke during the join, s rejoins.
func (j *Join) Finish(player model.Player, welcome model.Welcome) (replaced []*Session) {
	r, s := j.r, j.s
	j.done = true
	defer j.unlock()
	r.mu.Lock()
	who := s.who()
	old := r.byAccount[who.account]
	sameSession := r.bySession[who.sessionID]
	r.byAccount[who.account] = s
	r.bySession[who.sessionID] = s
	s.id.Store(&identity{account: who.account, sessionID: who.sessionID, character: player})
	s.setActive()
	missed := r.worldSubs > j.subs
	r.mu.Unlock()
	s.out.release(j.Joined.MapID, func() { s.outbox.Welcome(welcome) })
	if missed {
		r.Rejoin(s)
	}
	for _, prev := range []*Session{old, sameSession, j.previous} {
		if prev != nil && prev != s {
			replaced = append(replaced, prev)
		}
	}
	return replaced
}

// Release gives up the join unless Finish ran, and ends the older connection, whose presence it took; calling it again does nothing.
func (j *Join) Release() {
	if j.done {
		return
	}
	j.done = true
	j.unlock()
	if j.previous != nil && j.previous != j.s {
		j.previous.End(model.EndLoggedInElsewhere)
	}
}

// Rejoin joins s again on its own goroutine, so a stream that noticed the break keeps flowing.
func (r *Registry) Rejoin(s *Session) {
	r.deps.Panics.Go(context.Background(), panics.FlowJob, func() {
		select {
		case <-time.After(rand.N(rejoinSpread)):
		case <-s.ctx.Done():
			return
		}
		select {
		case r.rejoins <- struct{}{}:
			defer func() { <-r.rejoins }()
			r.rejoin(s)
		case <-s.ctx.Done():
		}
	}, "job", "rejoin", "character_id", s.Character())
}

// rejoin joins a player again after the world's stream broke, unless a newer connection joined the
// character since; other failures end the session.
func (r *Registry) rejoin(s *Session) {
	character := s.Character()
	unlock, err := r.lockCharacter(s.ctx, character)
	if err != nil {
		return // closed while another join ran
	}
	defer unlock()
	r.mu.Lock()
	current := r.byCharacter[character] == s
	r.mu.Unlock()
	if !current {
		return
	}
	ctx, cancel := context.WithTimeout(s.ctx, r.deps.JoinTimeout)
	defer cancel()
	before := s.mapID()
	s.out.hold()
	who := s.who()
	joined, err := r.join(ctx, s.connID, who.account, who.character.ID)
	if err != nil {
		s.out.abort()
		if s.ctx.Err() != nil {
			r.Leave(s) // closed mid-join; the world may have joined it anyway
			return
		}
		if errors.Is(err, model.ErrNotSubscribed) {
			// The stream broke again: the next subscription rejoins.
			r.deps.Logger.WarnContext(ctx, "rejoin waits for the world's stream", "character_id", s.Character())
			return
		}
		r.deps.Logger.ErrorContext(ctx, "rejoin failed", "character_id", s.Character(), "err", err)
		s.outbox.End(model.EndRejoinFailed)
		return
	}
	var first func()
	if joined.MapID != before {
		moved := model.Delivery{Push: model.MapChange{MapID: joined.MapID, X: joined.X, Y: joined.Y}}
		first = func() { s.outbox.Deliver(moved) }
	}
	s.out.release(joined.MapID, first)
	r.mu.Lock()
	closed := s.closed
	r.mu.Unlock()
	if closed {
		r.Leave(s)
		return
	}
	r.deps.Logger.InfoContext(ctx, "ws rejoined", "conn", s.connID, "character_id", s.Character(), "map_id", joined.MapID)
}

// join retries while the world cannot take the join yet, until ctx ends; a join is safe to repeat.
func (r *Registry) join(ctx context.Context, conn string, account kernel.AccountID, character kernel.CharacterID) (model.Joined, error) {
	var last error
	joined, err := backoff.Retry(ctx, func() (model.Joined, error) {
		joined, err := r.deps.World.Join(ctx, r.deps.Gateway, conn, account, character)
		last = err
		if err != nil && !waitable(err) {
			return joined, backoff.Permanent(err)
		}
		return joined, err
	},
		backoff.WithBackOff(&backoff.ExponentialBackOff{InitialInterval: joinRetryFirst, RandomizationFactor: backoff.DefaultRandomizationFactor, Multiplier: 2, MaxInterval: joinRetryMax}),
		backoff.WithMaxElapsedTime(0),
	)
	// The caller acts on why the world refused (ErrNotSubscribed), not on the deadline.
	if err != nil && last != nil && !errors.Is(err, last) {
		return joined, fmt.Errorf("%w (last error: %w)", err, last)
	}
	return joined, err
}

func waitable(err error) bool {
	return errors.Is(err, model.ErrNotSubscribed) || errors.Is(err, model.ErrUnavailable)
}

// Leave takes the player off its map.
func (r *Registry) Leave(s *Session) { r.leave(s.Presence()) }

// LeaveGhost takes off its map a presence whose connection this gateway does not have: its deliveries
// still come, so its Leave went missing. One at a time per connection, then none for ghostQuiet while
// the deliveries already on their way drain.
func (r *Registry) LeaveGhost(p model.Presence) {
	if p.Character.IsZero() {
		return
	}
	now := time.Now()
	r.mu.Lock()
	_, known := r.byConn[p.Connection]
	waiting := now.Before(r.ghosts[p.Connection])
	if !known && !waiting {
		for conn, until := range r.ghosts {
			if !now.Before(until) {
				delete(r.ghosts, conn)
			}
		}
		r.ghosts[p.Connection] = now.Add(leaveTimeout + ghostQuiet)
	}
	r.mu.Unlock()
	if known || waiting {
		return
	}
	r.deps.Panics.Go(context.Background(), panics.FlowJob, func() {
		r.leave(p)
		r.mu.Lock()
		r.ghosts[p.Connection] = time.Now().Add(ghostQuiet)
		r.mu.Unlock()
	}, "job", "leave_ghost", "character_id", p.Character)
}

// leave takes the presence off its map, trying again while the world is too busy, for up to
// leaveTimeout; a failure is only logged.
func (r *Registry) leave(p model.Presence) {
	ctx, cancel := context.WithTimeout(context.Background(), leaveTimeout)
	defer cancel()
	_, err := backoff.Retry(ctx, func() (struct{}, error) {
		err := r.deps.World.Leave(ctx, p)
		if err != nil && !errors.Is(err, model.ErrUnavailable) {
			return struct{}{}, backoff.Permanent(err)
		}
		return struct{}{}, err
	},
		backoff.WithBackOff(&backoff.ExponentialBackOff{InitialInterval: joinRetryFirst, RandomizationFactor: backoff.DefaultRandomizationFactor, Multiplier: 2, MaxInterval: joinRetryMax}),
		backoff.WithMaxElapsedTime(0),
	)
	if err != nil {
		r.deps.Logger.WarnContext(ctx, "world leave failed", "character_id", p.Character, "conn", p.Connection, "err", err)
	}
}

// Online registers the player with the social service unless its connection closed; failures are only logged.
func (r *Registry) Online(ctx context.Context, s *Session) {
	if r.deps.Social == nil {
		return
	}
	if err := r.online(ctx, s); err != nil {
		r.deps.Logger.WarnContext(ctx, "social online failed", "character_id", s.Character(), "err", err)
	}
}

func (r *Registry) online(ctx context.Context, s *Session) error {
	if err := s.social.Acquire(ctx, 1); err != nil {
		return err
	}
	defer s.social.Release(1)
	r.mu.Lock()
	closed := s.closed
	r.mu.Unlock()
	if closed {
		return nil // its close's Offline has the last word
	}
	return r.deps.Social.Online(ctx, r.deps.Gateway, s.connID, s.Player(), s.mapID())
}

// Offline tells the social service the player's connection closed, after any registration of it still on its way.
func (r *Registry) Offline(s *Session) {
	if r.deps.Social == nil {
		return
	}
	_ = s.social.Acquire(context.Background(), 1) // a registration holds it no longer than its own deadline
	defer s.social.Release(1)
	ctx, cancel := context.WithTimeout(context.Background(), offlineTimeout)
	defer cancel()
	if err := r.deps.Social.Offline(ctx, s.Character(), s.connID); err != nil {
		r.deps.Logger.WarnContext(ctx, "social offline failed", "character_id", s.Character(), "err", err)
	}
}
