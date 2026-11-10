package session

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"time"

	"github.com/cenkalti/backoff/v5"

	"finga/internal/kernel"
	"finga/services/gateway/internal/model"
)

const (
	leaveTimeout     = 5 * time.Second
	offlineTimeout   = 5 * time.Second
	registerTimeout  = 5 * time.Second        // each try of a login's registration with social
	registerRetry    = 500 * time.Millisecond // the pause after a failed one, doubled up to registerRetryMax
	registerRetryMax = 30 * time.Second
	ghostQuiet       = time.Second
	joinRetryFirst   = 20 * time.Millisecond
	joinRetryMax     = 500 * time.Millisecond
	// Rejoins after a stream break are spread and capped, so a world that just restarted is not hit by all at once.
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

// Join joins character to the world and claims it for s, holding deliveries until Finish; an error undoes both.
func (r *Registry) Join(ctx context.Context, s *Session, account kernel.AccountID, sessionID kernel.SessionID, character kernel.CharacterID) (*Join, error) {
	// The character's other joins wait, and deliveries are held so a Snapshot that beats the join follows Welcome.
	unlock, err := r.joining.Lock(ctx, character)
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
			r.Rejoin(older) // the world may have joined us anyway, taking the character from it
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

// Finish makes s the active session, sends welcome ahead of the held deliveries, and returns the sessions s replaced.
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
	s.tickRate.Store(int32(j.Joined.TickRateHz)) //nolint:gosec // a tick rate is small
	s.joinSeq.Store(j.Joined.JoinSeq)
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

// Release gives up the join unless Finish ran, giving the character back to the older connection, whose presence it took, to join again if it is still open; calling it again does nothing.
func (j *Join) Release() {
	if j.done {
		return
	}
	j.done = true
	r, older := j.r, j.previous
	if older == nil || older == j.s {
		j.unlock()
		return
	}
	r.mu.Lock()
	open := !older.closed
	if character := older.Character(); r.byCharacter[character] == j.s {
		if open {
			r.byCharacter[character] = older
		} else {
			delete(r.byCharacter, character)
		}
	}
	r.mu.Unlock()
	j.unlock()
	if open {
		r.Rejoin(older)
	}
}

// Rejoin joins s again on its own goroutine, so a stream that noticed the break keeps flowing; one asked for while another waits to start is that one.
func (r *Registry) Rejoin(s *Session) {
	if !s.rejoin.CompareAndSwap(false, true) {
		return
	}
	r.job(s.ctx, "gateway.rejoin", func(ctx context.Context) {
		select {
		case <-time.After(rand.N(rejoinSpread)):
		case <-ctx.Done():
			return
		}
		select {
		case r.rejoins <- struct{}{}:
			defer func() { <-r.rejoins }()
			s.rejoin.Store(false) // from here a new break needs a rejoin of its own
			r.rejoin(ctx, s)
		case <-ctx.Done():
		}
	}, "job", "rejoin", "character_id", s.Character())
}

// rejoin joins a player again after the world's stream broke, unless a newer connection joined the character since.
func (r *Registry) rejoin(ctx context.Context, s *Session) {
	character := s.Character()
	unlock, err := r.joining.Lock(ctx, character)
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
	ctx, cancel := context.WithTimeout(ctx, r.opts.JoinTimeout)
	defer cancel()
	before := s.mapID()
	s.out.hold()
	who := s.who()
	joined, err := r.join(ctx, s.connID, who.account, who.character.ID)
	if err != nil {
		s.out.abort()
		if s.ctx.Err() != nil {
			r.leave(ctx, s.Presence()) // closed mid-join; the world may have joined it anyway
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
	if was := s.tickRate.Swap(int32(joined.TickRateHz)); was != int32(joined.TickRateHz) { //nolint:gosec // a tick rate is small
		s.out.abort() // the client predicts at the rate its Welcome gave, so it reconnects for the new one
		r.deps.Logger.InfoContext(ctx, "rejoined at another tick rate; the client reconnects", "character_id", s.Character(), "from_hz", was, "to_hz", joined.TickRateHz)
		s.outbox.End(model.EndTickRateChanged)
		return
	}
	s.joinSeq.Store(joined.JoinSeq)
	// The log as this join found it, ahead of the stream: changes sent while it was broken are lost.
	log := model.Delivery{Push: model.QuestLog{Quests: joined.Quests}}
	first := func() { s.outbox.Deliver(log) }
	if joined.MapID != before {
		moved := model.Delivery{Push: model.MapChange{MapID: joined.MapID, X: joined.X, Y: joined.Y}}
		first = func() {
			s.outbox.Deliver(moved)
			s.outbox.Deliver(log)
		}
	}
	s.out.release(joined.MapID, first)
	r.mu.Lock()
	closed := s.closed
	r.mu.Unlock()
	if closed {
		r.leave(ctx, s.Presence())
		return
	}
	r.deps.Logger.InfoContext(ctx, "ws rejoined", "conn", s.connID, "character_id", s.Character(), "map_id", joined.MapID)
}

// join retries while the world cannot take the join yet, until ctx ends; a join is safe to repeat.
func (r *Registry) join(ctx context.Context, conn string, account kernel.AccountID, character kernel.CharacterID) (model.Joined, error) {
	var last error
	joined, err := backoff.Retry(ctx, func() (model.Joined, error) {
		joined, err := r.deps.World.Join(ctx, r.opts.Gateway, conn, account, character)
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

// LeaveGhost takes off its map a presence whose Leave went missing: its connection is gone, its deliveries still come.
func (r *Registry) LeaveGhost(p model.Presence) {
	if p.Character.IsZero() {
		return
	}
	now := time.Now()
	r.mu.Lock()
	_, known := r.byConn[p.Connection]
	waiting := now.Before(r.ghosts[p.Connection])
	if !known && !waiting {
		r.quiet(p.Connection, now)
	}
	r.mu.Unlock()
	if known || waiting {
		return
	}
	r.job(context.Background(), "gateway.leave_ghost", func(ctx context.Context) {
		r.leave(ctx, p)
		r.mu.Lock()
		r.ghosts[p.Connection] = time.Now().Add(ghostQuiet)
		r.mu.Unlock()
	}, "job", "leave_ghost", "character_id", p.Character)
}

// quiet takes no delivery for conn as a ghost's while its Leave, from now on, runs and then drains. Called with mu held.
func (r *Registry) quiet(conn string, now time.Time) {
	for c, until := range r.ghosts {
		if !now.Before(until) {
			delete(r.ghosts, c)
		}
	}
	r.ghosts[conn] = now.Add(leaveTimeout + ghostQuiet)
}

// leave takes the presence off its map, retrying while the world is busy, for up to leaveTimeout even past ctx's end; failures are logged.
func (r *Registry) leave(ctx context.Context, p model.Presence) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), leaveTimeout)
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

// Register registers a player who just logged in with the social service in the background, so a slow one holds up none of their commands; it retries while the session lives, as their party comes only from it.
func (r *Registry) Register(s *Session) {
	r.deps.Observer.RegistrationsWaiting(1)
	r.job(s.ctx, "gateway.register", func(ctx context.Context) {
		defer r.deps.Observer.RegistrationsWaiting(-1)
		failed := false
		_, err := backoff.Retry(ctx, func() (struct{}, error) {
			tryCtx, cancel := context.WithTimeout(ctx, registerTimeout)
			defer cancel()
			err := r.online(tryCtx, s)
			if err != nil && !failed {
				failed = true
				r.deps.Logger.WarnContext(ctx, "social online failed; retrying", "character_id", s.Character(), "err", err)
			}
			return struct{}{}, err
		},
			backoff.WithBackOff(&backoff.ExponentialBackOff{InitialInterval: registerRetry, RandomizationFactor: backoff.DefaultRandomizationFactor, Multiplier: 2, MaxInterval: registerRetryMax}),
			backoff.WithMaxElapsedTime(0),
		)
		if err == nil && failed {
			r.deps.Logger.InfoContext(ctx, "social online after retrying", "character_id", s.Character())
		}
	}, "job", "register", "character_id", s.Character())
}

// Online registers the player with the social service unless its connection closed; failures are only logged.
func (r *Registry) Online(ctx context.Context, s *Session) {
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
	current := r.byCharacter[s.Character()] == s
	r.mu.Unlock()
	if closed || !current {
		return nil // its close's Offline has the last word, or a newer login took the character
	}
	return r.deps.Social.Online(ctx, r.opts.Gateway, s.connID, s.joinSeq.Load(), s.Player(), s.mapID())
}

// offline tells the social service the player's connection closed, after any registration of it still on its way.
func (r *Registry) offline(ctx context.Context, s *Session) {
	_ = s.social.Acquire(ctx, 1) // a registration holds it no longer than its own deadline
	defer s.social.Release(1)
	ctx, cancel := context.WithTimeout(ctx, offlineTimeout)
	defer cancel()
	if err := r.deps.Social.Offline(ctx, s.Character(), s.connID); err != nil {
		r.deps.Logger.WarnContext(ctx, "social offline failed", "character_id", s.Character(), "err", err)
	}
}
