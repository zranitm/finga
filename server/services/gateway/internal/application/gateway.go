package application

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cenkalti/backoff/v5"
	"github.com/google/uuid"

	clientv1 "finga/api/gen/client/v1"
	"finga/internal/kernel"
	"finga/pkg/auth"
	"finga/pkg/uuidv7"
	"finga/services/gateway/internal/model"
)

// Dependencies are the services and settings the gateway is built from.
type Dependencies struct {
	ID                   string
	Sessions             auth.Guard
	SessionEnds          SessionEnds // nil: only the periodic session check runs
	World                World
	Items                Items  // nil: no inventory
	Social               Social // nil: no parties or chat
	Logger               *slog.Logger
	Observer             Observer
	Clock                func() time.Time
	JoinTimeout          time.Duration
	SessionCheckInterval time.Duration
}

const (
	defaultSessionCheckInterval = time.Minute
	defaultJoinTimeout          = 5 * time.Second
	commandTimeout              = 5 * time.Second
	joinRetryFirst              = 20 * time.Millisecond
	joinRetryMax                = 500 * time.Millisecond
)

// Gateway runs the connection use cases and forwards the services' pushes to players.
type Gateway struct {
	deps Dependencies
	now  func() time.Time

	mu          sync.Mutex
	byConn      map[string]*Session
	byAccount   map[kernel.AccountID]*Session
	byCharacter map[kernel.CharacterID]*Session
	bySession   map[kernel.SessionID]*Session
	socialBoot  string // a new boot ID means the social service restarted
	worldSubs   uint64 // more than one means the world's stream broke
	draining    atomic.Bool
}

// NewGateway builds a Gateway, defaulting unset dependencies.
func NewGateway(deps Dependencies) *Gateway {
	if deps.Clock == nil {
		deps.Clock = time.Now
	}
	if deps.JoinTimeout <= 0 {
		deps.JoinTimeout = defaultJoinTimeout
	}
	if deps.Observer == nil {
		deps.Observer = noopObserver{}
	}
	if deps.SessionCheckInterval <= 0 {
		deps.SessionCheckInterval = defaultSessionCheckInterval
	}
	if deps.ID == "" {
		deps.ID = uuidv7.NewString()
	}
	return &Gateway{
		deps:        deps,
		now:         deps.Clock,
		byConn:      map[string]*Session{},
		byAccount:   map[kernel.AccountID]*Session{},
		byCharacter: map[kernel.CharacterID]*Session{},
		bySession:   map[kernel.SessionID]*Session{},
	}
}

// Run follows the services' streams and periodically kicks expired sessions until ctx is done.
func (g *Gateway) Run(ctx context.Context) error {
	if g.deps.World != nil {
		go g.deps.World.FollowDeliveries(ctx, g.deps.ID, g.worldDelivered)
	}
	if g.deps.Social != nil {
		go g.deps.Social.FollowDeliveries(ctx, g.deps.ID, g.socialDelivered)
	}
	if g.deps.SessionEnds != nil {
		go g.deps.SessionEnds.FollowSessionEnds(ctx, g.deps.ID, g.sessionEnded)
	}
	ticker := time.NewTicker(g.deps.SessionCheckInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			g.checkSessions(ctx)
		}
	}
}

func (g *Gateway) checkSessions(ctx context.Context) {
	g.mu.Lock()
	active := make([]*Session, 0, len(g.bySession))
	for _, s := range g.bySession {
		active = append(active, s)
	}
	g.mu.Unlock()
	for _, s := range active {
		if err := g.deps.Sessions.Check(ctx, uuid.UUID(s.sessionID)); err != nil {
			s.conn.Kick(clientv1.KickReason_KICK_REASON_SESSION_EXPIRED)
		}
	}
}

func (g *Gateway) sessionEnded(ev model.SessionEnd) {
	switch {
	case !ev.Session.IsZero():
		g.KickSession(ev.Session)
	case !ev.Account.IsZero():
		g.KickAccount(ev.Account, ev.NewLogin)
	}
}

// KickSession closes the connection on a login session that ended.
func (g *Gateway) KickSession(sessionID kernel.SessionID) {
	g.mu.Lock()
	s := g.bySession[sessionID]
	g.mu.Unlock()
	if s != nil {
		s.conn.Kick(clientv1.KickReason_KICK_REASON_SESSION_EXPIRED)
	}
}

// KickAccount closes an account's connection: logged_in_elsewhere on a new login, else session_expired.
func (g *Gateway) KickAccount(accountID kernel.AccountID, newLogin bool) {
	g.mu.Lock()
	s := g.byAccount[accountID]
	g.mu.Unlock()
	if s == nil {
		return
	}
	why := clientv1.KickReason_KICK_REASON_SESSION_EXPIRED
	if newLogin {
		why = clientv1.KickReason_KICK_REASON_LOGGED_IN_ELSEWHERE
	}
	s.conn.Kick(why)
}

func (g *Gateway) sessionOf(id kernel.CharacterID) *Session {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.byCharacter[id]
}

// Draining reports whether Drain has begun; new connections are refused from then on.
func (g *Gateway) Draining() bool { return g.draining.Load() }

const drainPoll = 10 * time.Millisecond

// Drain refuses new connections, kicks open ones with server_restart and waits until they close or ctx ends.
func (g *Gateway) Drain(ctx context.Context) {
	g.draining.Store(true)
	g.mu.Lock()
	conns := make([]Conn, 0, len(g.byConn))
	for _, s := range g.byConn {
		conns = append(conns, s.conn)
	}
	g.mu.Unlock()
	for _, c := range conns {
		c.Kick(clientv1.KickReason_KICK_REASON_SERVER_RESTART)
	}
	t := time.NewTicker(drainPoll)
	defer t.Stop()
	for {
		g.mu.Lock()
		open := len(g.byConn)
		g.mu.Unlock()
		if open == 0 {
			return
		}
		select {
		case <-ctx.Done():
			g.deps.Logger.WarnContext(ctx, "drain ended with connections open", "open", open)
			return
		case <-t.C:
		}
	}
}

// Open starts a connection's session, which the handler passes to every use case.
func (g *Gateway) Open(c Conn) *Session {
	s := &Session{conn: c, out: out{conn: c}}
	s.ctx, s.cancel = context.WithCancel(context.Background())
	g.mu.Lock()
	g.byConn[c.ID()] = s
	draining := g.draining.Load() // under the lock, so Drain cannot miss this connection
	g.mu.Unlock()
	if draining {
		c.Kick(clientv1.KickReason_KICK_REASON_SERVER_RESTART)
	}
	return s
}

// Closed ends a session once its connection closed and none of its commands still runs.
func (g *Gateway) Closed(s *Session, reason string) {
	s.cancel()
	g.mu.Lock()
	delete(g.byConn, s.conn.ID())
	s.closed = true
	joined := !s.character.ID.IsZero()
	if g.byAccount[s.account] == s {
		delete(g.byAccount, s.account)
	}
	if g.byCharacter[s.character.ID] == s {
		delete(g.byCharacter, s.character.ID)
	}
	if g.bySession[s.sessionID] == s {
		delete(g.bySession, s.sessionID)
	}
	g.mu.Unlock()
	if joined {
		g.leave(s)
	}
	if s.active() {
		if g.deps.Social != nil {
			ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
			if err := g.deps.Social.Offline(ctx, s.character.ID); err != nil {
				g.deps.Logger.WarnContext(ctx, "social offline failed", "character_id", s.character.ID, "err", err)
			}
			cancel()
		}
		g.deps.Observer.OnlineChanged(-1)
	}
	g.deps.Logger.Info("ws closed", "conn", s.conn.ID(), "reason", reason)
}

func (g *Gateway) leave(s *Session) {
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	if err := g.deps.World.Leave(ctx, s.presence(), s.conn.ID()); err != nil {
		g.deps.Logger.WarnContext(ctx, "world leave failed", "character_id", s.character.ID, "err", err)
	}
}

// Hello joins the connection's character and sends Welcome; model.ErrHelloRejected means a bad token or character.
func (g *Gateway) Hello(ctx context.Context, s *Session, token string, charID kernel.CharacterID) error {
	ctx, cancel := context.WithTimeout(ctx, g.deps.JoinTimeout)
	defer cancel()
	p, err := g.deps.Sessions.Validate(ctx, token)
	if err != nil {
		return fmt.Errorf("%w: %w", model.ErrHelloRejected, err)
	}
	// The client keeps Welcome's token; the one it sent works once more for a minute.
	rotated, err := g.deps.Sessions.Rotate(ctx, p.SessionID)
	if err != nil {
		if model.PublicOf(err) == model.Unauthenticated {
			return fmt.Errorf("%w: %w", model.ErrHelloRejected, err)
		}
		g.deps.Logger.ErrorContext(ctx, "token rotation failed", "session_id", p.SessionID, "err", err)
		return fmt.Errorf("rotate: %w", err)
	}
	account, sessionID := kernel.AccountID(p.AccountID), kernel.SessionID(p.SessionID)

	// Rejoins wait, and deliveries are held so a Snapshot that beats the join follows Welcome.
	s.rejoinMu.Lock()
	defer s.rejoinMu.Unlock()
	s.out.hold()
	g.mu.Lock()
	previous := g.byCharacter[charID]
	g.byCharacter[charID] = s
	s.account, s.sessionID, s.character.ID = account, sessionID, charID
	// A join succeeds only once the world has our stream, so the first subscription is never a break.
	subs := max(g.worldSubs, 1)
	g.mu.Unlock()

	joined, err := g.join(ctx, s.conn.ID(), account, charID)
	if err != nil {
		s.out.abort()
		g.mu.Lock()
		if g.byCharacter[charID] == s {
			if previous != nil {
				g.byCharacter[charID] = previous
			} else {
				delete(g.byCharacter, charID)
			}
		}
		s.character.ID = kernel.CharacterID{}
		g.mu.Unlock()
		if model.PublicOf(err).Code == model.CharacterNotFound {
			return fmt.Errorf("%w: %w", model.ErrHelloRejected, err)
		}
		g.deps.Logger.ErrorContext(ctx, "join failed", "character_id", charID, "err", err)
		return fmt.Errorf("join: %w", err)
	}
	c := joined.Character
	welcome := model.Welcome{Token: rotated, Character: c, MapID: joined.MapID, ServerTime: g.now()}
	if g.deps.Items != nil {
		items, err := g.deps.Items.Inventory(ctx, charID)
		if err != nil {
			g.deps.Logger.ErrorContext(ctx, "inventory failed", "character_id", charID, "err", err)
			return fmt.Errorf("inventory: %w", err)
		}
		welcome.Inventory = items
	}
	if g.deps.Social != nil {
		// Parties are optional: a social failure must not block the join.
		party, err := g.deps.Social.PartyOf(ctx, charID)
		if err != nil {
			g.deps.Logger.WarnContext(ctx, "party lookup failed", "character_id", charID, "err", err)
		}
		welcome.Party = party
	}

	g.mu.Lock()
	old := g.byAccount[account]
	sameSession := g.bySession[sessionID]
	g.byAccount[account] = s
	g.bySession[sessionID] = s
	s.character = model.Player{ID: charID, Name: c.GetName(), Level: int(c.GetLevel()), HP: int(c.GetHp()), MaxHP: int(c.GetMaxHp())}
	s.setActive()
	missed := g.worldSubs > subs // the stream broke during the join
	g.mu.Unlock()
	s.out.release(joined.MapID, func() { s.conn.Welcome(welcome) })
	// The newest connection wins.
	for _, prev := range []*Session{old, sameSession, previous} {
		if prev != nil && prev != s {
			prev.conn.Kick(clientv1.KickReason_KICK_REASON_LOGGED_IN_ELSEWHERE)
		}
	}
	if missed {
		go g.rejoin(s)
	}
	g.online(ctx, s)
	g.deps.Observer.OnlineChanged(1)
	g.deps.Logger.InfoContext(ctx, "ws joined", "conn", s.conn.ID(), "character_id", charID, "map_id", joined.MapID)
	return nil
}

// worldDelivered routes a delivery to its session. A repeat Subscribed means the stream broke,
// so every player rejoins off this goroutine: their Snapshots arrive on this same stream.
func (g *Gateway) worldDelivered(ev model.WorldEvent) {
	if ev.Subscribed {
		g.mu.Lock()
		again := g.worldSubs > 0
		g.worldSubs++
		var active []*Session
		for _, s := range g.byCharacter {
			if s.active() {
				active = append(active, s)
			}
		}
		g.mu.Unlock()
		if again {
			for _, s := range active {
				go g.rejoin(s)
			}
		}
		return
	}
	s := g.sessionOf(ev.Character)
	if s == nil {
		return
	}
	if !s.out.deliver(ev.Delivery) {
		s.conn.Close("deliveries")
	}
}

// rejoin joins a player again after the world's stream broke; other failures close the connection.
func (g *Gateway) rejoin(s *Session) {
	s.rejoinMu.Lock()
	defer s.rejoinMu.Unlock()
	ctx, cancel := context.WithTimeout(s.ctx, g.deps.JoinTimeout)
	defer cancel()
	before := s.mapID()
	s.out.hold()
	joined, err := g.join(ctx, s.conn.ID(), s.account, s.character.ID)
	if err != nil {
		s.out.abort()
		if s.ctx.Err() != nil {
			g.leave(s) // closed mid-join; the world may have joined it anyway
			return
		}
		if errors.Is(err, model.ErrNotSubscribed) {
			// The stream broke again: the next subscription rejoins.
			g.deps.Logger.WarnContext(ctx, "rejoin waits for the world's stream", "character_id", s.character.ID)
			return
		}
		g.deps.Logger.ErrorContext(ctx, "rejoin failed", "character_id", s.character.ID, "err", err)
		s.conn.Close("rejoin")
		return
	}
	var first func()
	if joined.MapID != before {
		first = func() { s.conn.MapChange(joined.MapID, joined.X, joined.Y) }
	}
	s.out.release(joined.MapID, first)
	g.mu.Lock()
	closed := s.closed
	g.mu.Unlock()
	if closed {
		g.leave(s)
		return
	}
	g.deps.Logger.InfoContext(ctx, "ws rejoined", "conn", s.conn.ID(), "character_id", s.character.ID, "map_id", joined.MapID)
}

// join retries while the world cannot take the join yet, until ctx ends; a join is safe to repeat.
func (g *Gateway) join(ctx context.Context, conn string, account kernel.AccountID, character kernel.CharacterID) (model.Joined, error) {
	var last error
	joined, err := backoff.Retry(ctx, func() (model.Joined, error) {
		joined, err := g.deps.World.Join(ctx, g.deps.ID, conn, account, character)
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

// online registers the player with the social service; failures are only logged.
func (g *Gateway) online(ctx context.Context, s *Session) {
	if g.deps.Social == nil {
		return
	}
	if err := g.deps.Social.Online(ctx, g.deps.ID, s.character, s.mapID()); err != nil {
		g.deps.Logger.WarnContext(ctx, "social online failed", "character_id", s.character.ID, "err", err)
	}
}

// socialDelivered forwards a delivery; a new boot ID means the service restarted and every party is gone.
func (g *Gateway) socialDelivered(ev model.SocialEvent) {
	if ev.Subscribed {
		g.mu.Lock()
		restarted := g.socialBoot != "" && g.socialBoot != ev.BootID
		g.socialBoot = ev.BootID
		active := make([]*Session, 0, len(g.byCharacter))
		for _, s := range g.byCharacter {
			active = append(active, s)
		}
		g.mu.Unlock()
		if restarted {
			ctx, cancel := context.WithTimeout(context.Background(), g.deps.JoinTimeout)
			defer cancel()
			for _, s := range active {
				s.conn.PartyGone()
				g.online(ctx, s)
			}
		}
		return
	}
	t := g.sessionOf(ev.Character)
	if t == nil {
		return
	}
	t.conn.Deliver(ev.Delivery)
}

type noopObserver struct{}

// OnlineChanged does nothing.
func (noopObserver) OnlineChanged(int) {}
