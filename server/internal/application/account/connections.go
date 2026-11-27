package account

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel/trace"
	"golang.org/x/sync/semaphore"

	model "finga/internal/model/account"
	"finga/internal/shared/kernel"
	"finga/pkg/keylock"
	"finga/pkg/panics"
)

// Ports fail with a failure.ServiceError.

// Outbox is how the server reaches one player; the handler picks the wire format. No method blocks.
type Outbox interface {
	Welcome(w model.Welcome)
	Deliver(d model.Delivery)
	// End ends the player's session; the first End wins.
	End(reason model.EndReason)
}

// ConnectionsWorld is the world module as joins use it.
type ConnectionsWorld interface {
	// Join fails with failure.ErrUnavailable or failure.ErrNotSubscribed while the world cannot take it yet.
	Join(ctx context.Context, node, conn string, account kernel.AccountID, character kernel.CharacterID) (model.Joined, error)
	// Leave removes the player, if it joined through the presence's connection.
	Leave(ctx context.Context, p model.Presence) error
	// Drop is the presence's connection lost, not logged out: a match holds the player's place a while; elsewhere it's a Leave.
	Drop(ctx context.Context, p model.Presence) error
}

// ConnectionsSocial is the social module as joins use it: who is online, and on which map.
type ConnectionsSocial interface {
	Online(ctx context.Context, node, conn string, joinSeq uint64, p model.Player, mapID string) error
	// Offline is ignored when the character is online through another connection than conn.
	Offline(ctx context.Context, character kernel.CharacterID, conn string) error
}

// ConnectionsPvp is the pvp module as logouts use it: a player who leaves leaves its queue.
type ConnectionsPvp interface {
	Offline(ctx context.Context, character kernel.CharacterID) error
	// Missed is the matches that ended while the player was away and they haven't been told of.
	Missed(ctx context.Context, character kernel.CharacterID) (model.MissedMatches, error)
	// Told marks the player told of matches, their outcomes handed to them.
	Told(ctx context.Context, character kernel.CharacterID, matches []kernel.MatchID) error
}

// ConnectionsObserver counts the logins whose registration with the social module has not landed yet.
type ConnectionsObserver interface {
	RegistrationsWaiting(delta int)
}

// ConnectionsDeps are what the registry and its joins are built from.
type ConnectionsDeps struct {
	World    ConnectionsWorld
	Social   ConnectionsSocial
	Pvp      ConnectionsPvp
	Items    ConnectionsItems
	Logger   *slog.Logger
	Panics   *panics.Recorder     // a rejoin's panic
	Traces   trace.TracerProvider // each background job's trace, never kept: it joins the job's log lines and calls
	Observer ConnectionsObserver
}

// ConnectionsOptions tune the registry.
type ConnectionsOptions struct {
	Node        string        // this node's ID, on every join, registration and stream
	JoinTimeout time.Duration // a rejoin's whole join, retries included
}

// Connections is the server's sessions, found by connection, account, character and login session.
type Connections struct {
	deps   ConnectionsDeps
	opts   ConnectionsOptions
	tracer trace.Tracer

	mu          sync.Mutex
	byConn      map[string]*Connection
	byAccount   map[kernel.AccountID]*Connection
	byCharacter map[kernel.CharacterID]*Connection
	bySession   map[kernel.SessionID]*Connection
	worldSubs   uint64 // more than one means the world's stream broke
	draining    atomic.Bool
	unclosed    int // under mu: sessions from Open until their Close returns
	jobs        int // under mu: background jobs not yet returned

	joining *keylock.Locks[kernel.CharacterID] // each character's joins, one at a time
	rejoins chan struct{}                      // a slot per rejoin running
	ghosts  map[string]time.Time               // under mu: until when a ghost connection gets no other Leave
}

// NewConnections builds an empty registry.
func NewConnections(deps ConnectionsDeps, opts ConnectionsOptions) *Connections {
	return &Connections{
		deps:        deps,
		opts:        opts,
		tracer:      deps.Traces.Tracer(connectionsTracer),
		byConn:      map[string]*Connection{},
		byAccount:   map[kernel.AccountID]*Connection{},
		byCharacter: map[kernel.CharacterID]*Connection{},
		bySession:   map[kernel.SessionID]*Connection{},
		joining:     keylock.New[kernel.CharacterID](),
		rejoins:     make(chan struct{}, rejoinsAtOnce),
		ghosts:      map[string]time.Time{},
	}
}

// Node is this node's ID.
func (r *Connections) Node() string { return r.opts.Node }

// Open starts the session of the connection named connID; once draining, it ends at once with server_restart.
func (r *Connections) Open(connID string, o Outbox) *Connection {
	s := &Connection{connID: connID, outbox: o, out: out{outbox: o}, social: semaphore.NewWeighted(1)}
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
func (r *Connections) Close(s *Connection) (online bool) {
	ctx, span := r.tracer.Start(context.Background(), "account.close", trace.WithNewRoot())
	defer span.End()
	defer func() {
		r.mu.Lock()
		r.unclosed--
		r.mu.Unlock()
	}()
	if r.remove(s) {
		r.leaveAs(ctx, s)
		r.unqueue(ctx, s)
	}
	if !s.Active() {
		return false
	}
	r.offline(ctx, s)
	return true
}

// remove forgets a session whose connection closed and stops its rejoin; joined reports whether it claimed a character, which its caller then takes off its map.
func (r *Connections) remove(s *Connection) (joined bool) {
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
func (r *Connections) ByConn(connID string) *Connection {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.byConn[connID]
}

// ByCharacter is the session playing the character; nil when none.
func (r *Connections) ByCharacter(id kernel.CharacterID) *Connection {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.byCharacter[id]
}

// ByAccount is the account's active session; nil when none.
func (r *Connections) ByAccount(id kernel.AccountID) *Connection {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.byAccount[id]
}

// BySession is the login session's active session; nil when none.
func (r *Connections) BySession(id kernel.SessionID) *Connection {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.bySession[id]
}

// Playing is every session that claimed a character: its world join succeeded.
func (r *Connections) Playing() []*Connection {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]*Connection, 0, len(r.byCharacter))
	for _, s := range r.byCharacter {
		out = append(out, s)
	}
	return out
}

// LoggedIn is every session whose join finished, one per login session.
func (r *Connections) LoggedIn() []*Connection {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]*Connection, 0, len(r.bySession))
	for _, s := range r.bySession {
		out = append(out, s)
	}
	return out
}

// WorldSubscribed reports whether the world's stream broke, past its first subscription, and the sessions to rejoin.
func (r *Connections) WorldSubscribed() (broke bool, active []*Connection) {
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
func (r *Connections) Draining() bool { return r.draining.Load() }

// Drain refuses new sessions from now on and returns the open ones.
func (r *Connections) Drain() []*Connection {
	r.draining.Store(true)
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]*Connection, 0, len(r.byConn))
	for _, s := range r.byConn {
		out = append(out, s)
	}
	return out
}

// Pending is what the drain waits for: sessions not yet closed, including those still leaving, and running jobs.
func (r *Connections) Pending() (sessions, jobs int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.unclosed, r.jobs
}

const connectionsTracer = "finga/internal/application/account/connections"

// job runs fn on its own goroutine under a root span named name, counted in Pending until it returns.
func (r *Connections) job(ctx context.Context, name string, fn func(ctx context.Context), attrs ...any) {
	r.mu.Lock()
	r.jobs++
	r.mu.Unlock()
	ctx, span := r.tracer.Start(ctx, name, trace.WithNewRoot())
	r.deps.Panics.Go(ctx, panics.FlowJob, func() {
		defer func() {
			span.End()
			r.mu.Lock()
			r.jobs--
			r.mu.Unlock()
		}()
		fn(ctx)
	}, attrs...)
}
