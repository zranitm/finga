package application

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"finga/internal/kernel"
	"finga/internal/protocol"
	"finga/pkg/auth"
	"finga/services/gateway/internal/model"
)

// Dependencies are the services and settings the gateway is built from.
type Dependencies struct {
	// ID names this gateway node to the services that stream to it; random
	// when empty.
	ID          string
	Sessions    auth.Guard
	SessionEnds SessionEnds // the account service's stream; nil: none is followed, the minute check still runs
	World       World       // joins, commands, and the delivery stream Run follows
	Items       Items       // nil: Welcome carries no items and inventory commands are unavailable
	Social      Social      // parties and chat; nil: both are unavailable
	Logger      *slog.Logger
	Observer    Observer
	Clock       func() time.Time
	// JoinTimeout bounds a join, waiting out a world that is starting or
	// restarting; 5 s by default.
	JoinTimeout time.Duration
	// SessionCheckInterval is how often Run checks each connection's session
	// and kicks the expired ones; a minute by default.
	SessionCheckInterval time.Duration
}

const (
	defaultSessionCheckInterval = time.Minute
	defaultJoinTimeout          = 5 * time.Second
	commandTimeout              = 5 * time.Second
	joinRetryFirst              = 20 * time.Millisecond
	joinRetryMax                = 500 * time.Millisecond
)

// Gateway is the connections' use cases (page 10): a connection's
// session from Open to Closed, Hello, the players' commands (commands.go),
// and the services' streams, whose pushes it forwards to each player's
// connection. The WebSocket handler calls it for each connection's
// messages, as the REST handler calls the Lobby.
type Gateway struct {
	deps Dependencies
	now  func() time.Time

	mu          sync.Mutex
	byConn      map[string]*Session
	byAccount   map[kernel.AccountID]*Session
	byCharacter map[kernel.CharacterID]*Session // active sessions, for party and chat delivery
	bySession   map[kernel.SessionID]*Session   // active connections by login session
	socialBoot  string                          // the social service's boot ID, from its stream
	worldSubs   uint64                          // subscriptions to the world's stream so far
	draining    atomic.Bool                     // Drain has begun: no new connections
}

// NewGateway builds the connections' use cases.
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
		deps.ID = uuid.NewString()
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

// Run follows the world's delivery stream, the social service's and the
// account service's session ends (each when set), and checks every
// connection's login session on an interval, until ctx is done, kicking the
// ones whose session ended (expired, logged out, reused token) with
// session_expired. The minute check catches what a broken stream missed.
// It never returns an error.
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
			s.conn.Kick(protocol.KickSessionExpired)
		}
	}
}

// sessionEnded acts on one message of the account service's stream.
func (g *Gateway) sessionEnded(ev model.SessionEnd) {
	switch {
	case !ev.Session.IsZero():
		g.KickSession(ev.Session)
	case !ev.Account.IsZero():
		g.KickAccount(ev.Account, ev.NewLogin)
	}
}

// KickSession closes every connection on a login session: the account
// service reported it ended by reuse or logout.
func (g *Gateway) KickSession(sessionID kernel.SessionID) {
	g.mu.Lock()
	s := g.bySession[sessionID]
	g.mu.Unlock()
	if s != nil {
		s.conn.Kick(protocol.KickSessionExpired)
	}
}

// KickAccount closes every connection of an account: a logout everywhere,
// or a new login elsewhere (newLogin), which the player sees as
// logged_in_elsewhere; anything else is session_expired.
func (g *Gateway) KickAccount(accountID kernel.AccountID, newLogin bool) {
	g.mu.Lock()
	s := g.byAccount[accountID]
	g.mu.Unlock()
	if s == nil {
		return
	}
	why := protocol.KickSessionExpired
	if newLogin {
		why = protocol.KickLoggedInElsewhere
	}
	s.conn.Kick(why)
}

// sessionOf finds the active session of a character, if any.
func (g *Gateway) sessionOf(id kernel.CharacterID) *Session {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.byCharacter[id]
}

// Draining reports whether Drain has begun: the handler refuses new
// connections from then on.
func (g *Gateway) Draining() bool { return g.draining.Load() }

// drainPoll is how often Drain checks whether every connection has closed.
const drainPoll = 10 * time.Millisecond

// Drain is the gateway's part of a graceful shutdown, run once readiness
// has turned 503 and before the listener closes: it refuses new
// connections, tells every connection the server is restarting (Kicked
// server_restart, after which the client reconnects with jitter) and
// closes it, and returns once they have all closed or ctx ends.
func (g *Gateway) Drain(ctx context.Context) {
	g.draining.Store(true)
	g.mu.Lock()
	conns := make([]Conn, 0, len(g.byConn))
	for _, s := range g.byConn {
		conns = append(conns, s.conn)
	}
	g.mu.Unlock()
	for _, c := range conns {
		c.Kick(protocol.KickServerRestart)
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

// Open starts a connection's session; the WebSocket handler holds it for
// the connection and passes it to every use case. A connection that
// opened as Drain began is told the server is restarting.
func (g *Gateway) Open(c Conn) *Session {
	s := &Session{conn: c, out: out{conn: c}}
	s.ctx, s.cancel = context.WithCancel(context.Background())
	g.mu.Lock()
	g.byConn[c.ID()] = s
	draining := g.draining.Load() // read under the lock Drain collects connections with
	g.mu.Unlock()
	if draining {
		c.Kick(protocol.KickServerRestart) // upgraded as Drain began
	}
	return s
}

// Closed ends a session whose connection closed (reason for the log),
// once no command of the connection runs any more: it stops a rejoin in
// flight, leaves the map and clears the registry.
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
		// The world removes the player only if it joined through this
		// connection; a rejoin that finishes after this leaves again.
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

// leave tells the world the connection's player is gone.
func (g *Gateway) leave(s *Session) {
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	if err := g.deps.World.Leave(ctx, s.presence(), s.conn.ID()); err != nil {
		g.deps.Logger.WarnContext(ctx, "world leave failed", "character_id", s.character.ID, "err", err)
	}
}

// Hello joins the connection's player (page 10): the token, then the join
// through the world (which checks the character is the account's), the
// inventory and the party, then Welcome through the Conn. The session is
// registered for its character before the join, so the Snapshot, which
// may come back on the world's stream before the join answers, waits in
// the session and follows Welcome. An older connection of the account
// loses once the join holds. It fails with model.ErrHelloRejected when the
// token or the character is not the account's; any other failure is one
// the client may retry, logged here with its cause.
func (g *Gateway) Hello(ctx context.Context, s *Session, token string, charID kernel.CharacterID) error {
	ctx, cancel := context.WithTimeout(ctx, g.deps.JoinTimeout)
	defer cancel()
	p, err := g.deps.Sessions.Validate(ctx, token)
	if err != nil {
		return fmt.Errorf("%w: %w", model.ErrHelloRejected, err)
	}
	// Every Hello rotates the token (step 16): the client keeps the one in
	// Welcome; the one it sent works once more for a minute.
	rotated, err := g.deps.Sessions.Rotate(ctx, p.SessionID)
	if err != nil {
		if model.PublicOf(err) == model.Unauthenticated {
			return fmt.Errorf("%w: %w", model.ErrHelloRejected, err)
		}
		g.deps.Logger.ErrorContext(ctx, "token rotation failed", "session_id", p.SessionID, "err", err)
		return fmt.Errorf("rotate: %w", err)
	}
	account, sessionID := kernel.AccountID(p.AccountID), kernel.SessionID(p.SessionID)

	// A rejoin (the world's stream broke) waits until this join is out.
	s.rejoinMu.Lock()
	defer s.rejoinMu.Unlock()
	s.out.hold()
	g.mu.Lock()
	previous := g.byCharacter[charID]
	g.byCharacter[charID] = s
	s.account, s.sessionID, s.character.ID = account, sessionID, charID
	// A join only succeeds once the world holds this gateway's stream, so
	// the first subscription is never a break, even before the gateway has
	// read its Subscribed.
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
		// Someone else's character, or none, is a bad Hello; a world or
		// character service that cannot answer is a server fault the client
		// retries.
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
		// Parties are optional: a social service that cannot be reached
		// leaves Welcome without one rather than the player out of the game.
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
	s.character = model.Player{ID: charID, Name: c.Name, Level: c.Level, HP: c.HP, MaxHP: c.MaxHP}
	s.setActive()
	missed := g.worldSubs > subs // the stream broke during the join
	g.mu.Unlock()
	s.out.release(joined.MapID, func() { s.conn.Welcome(welcome) })
	// An older connection of the account, or a second Hello on the same
	// session, loses: the newest connection wins.
	for _, prev := range []*Session{old, sameSession, previous} {
		if prev != nil && prev != s {
			prev.conn.Kick(protocol.KickLoggedInElsewhere)
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

// worldDelivered acts on one message of the world's stream: a delivery
// goes to its player's session; Subscribed after the first means the
// stream broke and every player this gateway had joined is gone from the
// world, so each is joined again. Nothing here waits on the world: the
// joins' Snapshots come back on this same stream.
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
		return // gone from this gateway; the world learns it from Leave
	}
	if !s.out.deliver(ev.Delivery) {
		s.conn.Close("deliveries")
	}
}

// rejoin joins a connected player again after the world's stream broke,
// from the character's last checkpoint: MapChange first when that is
// another map than the session's, then the Snapshot. The socket stays
// open; a join that fails because the stream broke again waits for the
// next subscription, which rejoins, and any other failure (the world still
// unreachable when the join gives up, say) closes the connection so the
// client reconnects.
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
			g.leave(s) // the connection closed mid-join; the world may have joined it anyway
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

// join asks the world to join a player, waiting out a world that is not
// reachable yet (starting, restarting) or has not seen this gateway's
// stream yet, until ctx ends. A join is safe to repeat: the world replaces
// a presence that joined through the same connection.
func (g *Gateway) join(ctx context.Context, conn string, account kernel.AccountID, character kernel.CharacterID) (model.Joined, error) {
	wait := joinRetryFirst
	for {
		joined, err := g.deps.World.Join(ctx, g.deps.ID, conn, account, character)
		if err == nil || !waitable(err) {
			return joined, err
		}
		t := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			t.Stop()
			return model.Joined{}, err
		case <-t.C:
		}
		wait = min(wait*2, joinRetryMax)
	}
}

// waitable is a join failure that time may cure: the world is unreachable,
// or its stream to this gateway is not open yet.
func waitable(err error) bool {
	return errors.Is(err, model.ErrNotSubscribed) || errors.Is(err, model.ErrUnavailable)
}

// online tells the social service a player is connected through this
// gateway, so party traffic finds it here. A failure is logged: parties are
// optional, and the next registration (a social restart) tries again.
func (g *Gateway) online(ctx context.Context, s *Session) {
	if g.deps.Social == nil {
		return
	}
	if err := g.deps.Social.Online(ctx, g.deps.ID, s.character, s.mapID()); err != nil {
		g.deps.Logger.WarnContext(ctx, "social online failed", "character_id", s.character.ID, "err", err)
	}
}

// socialDelivered forwards one message of the social service's stream to
// its player's connection. A new boot ID on Subscribed means the social
// service restarted and every party is gone: each connected player gets
// PartyState with no party and is registered again.
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
		return // gone from this gateway; the social service learns it from Offline
	}
	t.conn.Deliver(ev.Delivery)
}

// noopObserver is the Observer when none is given.
type noopObserver struct{}

// OnlineChanged does nothing.
func (noopObserver) OnlineChanged(int) {}
