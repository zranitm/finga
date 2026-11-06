package session

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sync/semaphore"

	"finga/internal/kernel"
	"finga/pkg/panics"
	"finga/services/gateway/internal/model"
)

// Ports fail with a model.ServiceError.

// Outbox is how the gateway reaches one player; the handler picks the wire format. No method blocks.
type Outbox interface {
	Welcome(w model.Welcome)
	Deliver(d model.Delivery)
	// End ends the player's session; the first End wins.
	End(reason model.EndReason)
}

// World is the world service as joins use it.
type World interface {
	// Join fails with model.ErrUnavailable or model.ErrNotSubscribed while the world cannot take it yet.
	Join(ctx context.Context, gateway, conn string, account kernel.AccountID, character kernel.CharacterID) (model.Joined, error)
	// Leave removes the player, if it joined through the presence's connection.
	Leave(ctx context.Context, p model.Presence) error
}

// Social is the social service as joins use it: who is online, and on which map.
type Social interface {
	Online(ctx context.Context, gateway, conn string, p model.Player, mapID string) error
	// Offline is ignored when the character is online through a newer connection than conn.
	Offline(ctx context.Context, character kernel.CharacterID, conn string) error
}

// Deps are what the registry and its joins are built from.
type Deps struct {
	World  World
	Social Social
	Logger *slog.Logger
	Panics *panics.Recorder // a rejoin's panic
}

// Options tune the registry.
type Options struct {
	Gateway     string        // this gateway's ID, on every join, registration and stream
	JoinTimeout time.Duration // a rejoin's whole join, retries included
}

// Registry is the gateway's sessions, found by connection, account, character and login session.
type Registry struct {
	deps Deps
	opts Options

	mu          sync.Mutex
	byConn      map[string]*Session
	byAccount   map[kernel.AccountID]*Session
	byCharacter map[kernel.CharacterID]*Session
	bySession   map[kernel.SessionID]*Session
	worldSubs   uint64 // more than one means the world's stream broke
	draining    atomic.Bool
	unclosed    int // under mu: sessions from Open until their Close returns
	jobs        int // under mu: background jobs not yet returned

	joining map[kernel.CharacterID]*characterLock // under mu: each character's joins, one at a time
	rejoins chan struct{}                         // a slot per rejoin running
	ghosts  map[string]time.Time                  // under mu: until when a ghost connection gets no other Leave
}

type characterLock struct {
	held  chan struct{} // full while a join runs
	users int           // under the registry's mu
}

// lockCharacter waits for the character's join in progress, if any, until ctx ends; unlock lets the next go.
func (r *Registry) lockCharacter(ctx context.Context, id kernel.CharacterID) (unlock func(), err error) {
	r.mu.Lock()
	l := r.joining[id]
	if l == nil {
		l = &characterLock{held: make(chan struct{}, 1)}
		r.joining[id] = l
	}
	l.users++
	r.mu.Unlock()
	done := func() {
		r.mu.Lock()
		if l.users--; l.users == 0 {
			delete(r.joining, id)
		}
		r.mu.Unlock()
	}
	select {
	case l.held <- struct{}{}:
		return func() { <-l.held; done() }, nil
	case <-ctx.Done():
		done()
		return nil, ctx.Err()
	}
}

// NewRegistry builds an empty registry.
func NewRegistry(deps Deps, opts Options) *Registry {
	return &Registry{
		deps:        deps,
		opts:        opts,
		byConn:      map[string]*Session{},
		byAccount:   map[kernel.AccountID]*Session{},
		byCharacter: map[kernel.CharacterID]*Session{},
		bySession:   map[kernel.SessionID]*Session{},
		joining:     map[kernel.CharacterID]*characterLock{},
		rejoins:     make(chan struct{}, rejoinsAtOnce),
		ghosts:      map[string]time.Time{},
	}
}

// Gateway is this gateway's ID.
func (r *Registry) Gateway() string { return r.opts.Gateway }

// Open starts the session of the connection named connID; once draining, it ends at once with server_restart.
func (r *Registry) Open(connID string, o Outbox) *Session {
	s := &Session{connID: connID, outbox: o, out: out{outbox: o}, social: semaphore.NewWeighted(1)}
	s.ctx, s.cancel = context.WithCancel(context.Background())
	r.mu.Lock()
	r.byConn[connID] = s
	r.unclosed++
	draining := r.draining.Load() // under the lock, so Drain cannot miss this connection
	r.mu.Unlock()
	if draining {
		o.End(model.EndServerRestart)
	}
	return s
}

// Close ends the session of a connection that closed: it stops its rejoin, takes its player off the map, then offline; online reports whether it was.
func (r *Registry) Close(s *Session) (online bool) {
	defer func() {
		r.mu.Lock()
		r.unclosed--
		r.mu.Unlock()
	}()
	if r.remove(s) {
		r.leave(s.Presence())
	}
	if !s.Active() {
		return false
	}
	r.offline(s)
	return true
}

// remove forgets a session whose connection closed and stops its rejoin; joined reports whether it claimed a character, which its caller then takes off its map.
func (r *Registry) remove(s *Session) (joined bool) {
	s.cancel()
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.byConn, s.connID)
	s.closed = true
	who := s.who()
	if !who.character.ID.IsZero() {
		r.quiet(s.connID, time.Now()) // the deliveries on their way are not a ghost's: the caller leaves
	}
	if r.byAccount[who.account] == s {
		delete(r.byAccount, who.account)
	}
	if r.byCharacter[who.character.ID] == s {
		delete(r.byCharacter, who.character.ID)
	}
	if r.bySession[who.sessionID] == s {
		delete(r.bySession, who.sessionID)
	}
	return !who.character.ID.IsZero()
}

// ByConn is the session of the connection named connID; nil when none.
func (r *Registry) ByConn(connID string) *Session {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.byConn[connID]
}

// ByCharacter is the session playing the character; nil when none.
func (r *Registry) ByCharacter(id kernel.CharacterID) *Session {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.byCharacter[id]
}

// ByAccount is the account's active session; nil when none.
func (r *Registry) ByAccount(id kernel.AccountID) *Session {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.byAccount[id]
}

// BySession is the login session's active session; nil when none.
func (r *Registry) BySession(id kernel.SessionID) *Session {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.bySession[id]
}

// Playing is every session that claimed a character: its world join succeeded.
func (r *Registry) Playing() []*Session {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]*Session, 0, len(r.byCharacter))
	for _, s := range r.byCharacter {
		out = append(out, s)
	}
	return out
}

// LoggedIn is every session whose join finished, one per login session.
func (r *Registry) LoggedIn() []*Session {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]*Session, 0, len(r.bySession))
	for _, s := range r.bySession {
		out = append(out, s)
	}
	return out
}

// WorldSubscribed counts a subscription to the world's stream. Past the first, the stream broke: it
// returns the active sessions, which must rejoin, since their Snapshots arrive on the new stream.
func (r *Registry) WorldSubscribed() (broke bool, active []*Session) {
	r.mu.Lock()
	defer r.mu.Unlock()
	broke = r.worldSubs > 0
	r.worldSubs++
	if !broke {
		return false, nil
	}
	for _, s := range r.byCharacter {
		if s.Active() {
			active = append(active, s)
		}
	}
	return true, active
}

// Draining reports whether Drain has begun; new connections are refused from then on.
func (r *Registry) Draining() bool { return r.draining.Load() }

// Drain refuses new sessions from now on and returns the open ones.
func (r *Registry) Drain() []*Session {
	r.draining.Store(true)
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]*Session, 0, len(r.byConn))
	for _, s := range r.byConn {
		out = append(out, s)
	}
	return out
}

// Pending is what the drain waits for: sessions not yet closed (open, or still taking their player off
// the map and offline) and background jobs still running.
func (r *Registry) Pending() (sessions, jobs int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.unclosed, r.jobs
}

// job runs fn on its own goroutine, counted in Pending until it returns.
func (r *Registry) job(fn func(), attrs ...any) {
	r.mu.Lock()
	r.jobs++
	r.mu.Unlock()
	r.deps.Panics.Go(context.Background(), panics.FlowJob, func() {
		defer func() {
			r.mu.Lock()
			r.jobs--
			r.mu.Unlock()
		}()
		fn()
	}, attrs...)
}
