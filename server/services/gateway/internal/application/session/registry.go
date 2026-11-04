package session

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"finga/internal/kernel"
	"finga/pkg/panics"
	"finga/pkg/uuidv7"
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
	// Leave removes the player, if it joined through conn.
	Leave(ctx context.Context, p model.Presence, conn string) error
}

// Social is the social service as joins use it: who is online, and on which map.
type Social interface {
	Online(ctx context.Context, gateway string, p model.Player, mapID string) error
}

// Deps are what the registry and its joins are built from.
type Deps struct {
	Gateway     string // this gateway's ID, on every join, registration and stream; empty mints one
	World       World
	Social      Social // nil: no parties
	Logger      *slog.Logger
	Panics      *panics.Recorder // a rejoin's panic; nil logs it to Logger
	JoinTimeout time.Duration    // a rejoin's whole join, retries included
}

// Registry is the gateway's sessions, found by connection, account, character and login session.
type Registry struct {
	deps Deps

	mu          sync.Mutex
	byConn      map[string]*Session
	byAccount   map[kernel.AccountID]*Session
	byCharacter map[kernel.CharacterID]*Session
	bySession   map[kernel.SessionID]*Session
	worldSubs   uint64 // more than one means the world's stream broke
	draining    atomic.Bool
}

// NewRegistry builds an empty registry, defaulting unset dependencies.
func NewRegistry(deps Deps) *Registry {
	if deps.Gateway == "" {
		deps.Gateway = uuidv7.NewString()
	}
	if deps.Panics == nil {
		deps.Panics = panics.NewRecorder(deps.Logger, nil)
	}
	return &Registry{
		deps:        deps,
		byConn:      map[string]*Session{},
		byAccount:   map[kernel.AccountID]*Session{},
		byCharacter: map[kernel.CharacterID]*Session{},
		bySession:   map[kernel.SessionID]*Session{},
	}
}

// Gateway is this gateway's ID.
func (r *Registry) Gateway() string { return r.deps.Gateway }

// Open starts the session of the connection named connID; once draining, it ends at once with server_restart.
func (r *Registry) Open(connID string, o Outbox) *Session {
	s := &Session{connID: connID, outbox: o, out: out{outbox: o}}
	s.ctx, s.cancel = context.WithCancel(context.Background())
	r.mu.Lock()
	r.byConn[connID] = s
	draining := r.draining.Load() // under the lock, so Drain cannot miss this connection
	r.mu.Unlock()
	if draining {
		o.End(model.EndServerRestart)
	}
	return s
}

// Remove forgets a session whose connection closed and stops its rejoin; joined reports whether it claimed a character.
func (r *Registry) Remove(s *Session) (joined bool) {
	s.cancel()
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.byConn, s.connID)
	s.closed = true
	if r.byAccount[s.account] == s {
		delete(r.byAccount, s.account)
	}
	if r.byCharacter[s.character.ID] == s {
		delete(r.byCharacter, s.character.ID)
	}
	if r.bySession[s.sessionID] == s {
		delete(r.bySession, s.sessionID)
	}
	return !s.character.ID.IsZero()
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

// Playing is every session that claimed a character, joined or still joining.
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

// Len is the number of open sessions.
func (r *Registry) Len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.byConn)
}
