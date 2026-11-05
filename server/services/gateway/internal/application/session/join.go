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
	joinRetryFirst = 20 * time.Millisecond
	joinRetryMax   = 500 * time.Millisecond
	// After the world's stream breaks every player rejoins: each after a random pause up to rejoinSpread,
	// and no more than rejoinsAtOnce at a time, so a world that just restarted is not hit by all at once.
	rejoinSpread  = 500 * time.Millisecond
	rejoinsAtOnce = 32
)

// Join is a Hello's join in progress: the session holds the character and its deliveries wait,
// until Finish or Cancel.
type Join struct {
	Joined model.Joined

	r        *Registry
	s        *Session
	previous *Session // the character's session before this one
	subs     uint64   // the world subscriptions when the join began
	unlock   func()   // lets the character's next join go
}

// Join joins character to the world through s's connection, retrying while the world cannot take it
// yet, then claims it for s. Deliveries wait until Finish sends Welcome; on an error nothing stays held or claimed.
func (r *Registry) Join(ctx context.Context, s *Session, account kernel.AccountID, sessionID kernel.SessionID, character kernel.CharacterID) (*Join, error) {
	// The character's other joins wait, and deliveries are held so a Snapshot that beats the join follows Welcome.
	unlock, err := r.lockCharacter(ctx, character)
	if err != nil {
		return nil, err
	}
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
		r.mu.Unlock()
		unlock()
		return nil, err
	}
	// Only once the world has loaded the character as the account's may its social deliveries reach s.
	r.mu.Lock()
	previous := r.byCharacter[character]
	r.byCharacter[character] = s
	r.mu.Unlock()
	return &Join{Joined: joined, r: r, s: s, previous: previous, subs: subs, unlock: unlock}, nil
}

// Finish makes s the account's, the login session's and the character's active session as player,
// sends welcome ahead of the held deliveries, and returns the older sessions s replaced. If the
// world's stream broke during the join, s rejoins.
func (j *Join) Finish(player model.Player, welcome model.Welcome) (replaced []*Session) {
	r, s := j.r, j.s
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

// Cancel gives up a join whose Hello failed after the world joined; the handler then closes the connection.
func (j *Join) Cancel() { j.unlock() }

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

// Leave takes the player off its map; a failure is only logged.
func (r *Registry) Leave(s *Session) {
	ctx, cancel := context.WithTimeout(context.Background(), leaveTimeout)
	defer cancel()
	if err := r.deps.World.Leave(ctx, s.Presence()); err != nil {
		r.deps.Logger.WarnContext(ctx, "world leave failed", "character_id", s.Character(), "err", err)
	}
}

// Online registers the player with the social service; failures are only logged.
func (r *Registry) Online(ctx context.Context, s *Session) {
	if r.deps.Social == nil {
		return
	}
	if err := r.deps.Social.Online(ctx, r.deps.Gateway, s.connID, s.Player(), s.mapID()); err != nil {
		r.deps.Logger.WarnContext(ctx, "social online failed", "character_id", s.Character(), "err", err)
	}
}
