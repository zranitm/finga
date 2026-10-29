package application

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"finga/internal/kernel"
	"finga/internal/protocol"
	"finga/pkg/auth"
	"finga/pkg/ratelimit"
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
	// HelloTimeout is how long a connection has to send Hello; 5 s by default.
	HelloTimeout time.Duration
	// JoinTimeout bounds a join, waiting out a world that is starting or
	// restarting; 5 s by default.
	JoinTimeout time.Duration
	// CommandQueue bounds each connection's command queue; 64 by default.
	CommandQueue int
	// SessionCheckInterval is how often Run checks each connection's session
	// and kicks the expired ones; a minute by default.
	SessionCheckInterval time.Duration
}

const (
	defaultHelloTimeout         = 5 * time.Second
	defaultSessionCheckInterval = time.Minute
	defaultJoinTimeout          = 5 * time.Second
	defaultCommandQueue         = 64
	commandTimeout              = 5 * time.Second
	joinRetryFirst              = 20 * time.Millisecond
	joinRetryMax                = 500 * time.Millisecond
)

// Gateway is the connections' use cases; the WebSocket handler feeds it
// each connection's open, messages and close.
type Gateway struct {
	deps   Dependencies
	limits map[string]model.Limit
	now    func() time.Time

	mu          sync.Mutex
	byConn      map[string]*session
	byAccount   map[kernel.AccountID]*session
	byCharacter map[kernel.CharacterID]*session // active sessions, for party and chat delivery
	bySession   map[kernel.SessionID]*session   // active connections by login session
	socialBoot  string                          // the social service's boot ID, from its stream
	worldSubs   uint64                          // subscriptions to the world's stream so far
	helloGrace  time.Duration
	draining    atomic.Bool // Drain has begun: no new connections
}

// NewGateway builds the connections' use cases.
func NewGateway(deps Dependencies) *Gateway {
	if deps.Clock == nil {
		deps.Clock = time.Now
	}
	if deps.HelloTimeout <= 0 {
		deps.HelloTimeout = defaultHelloTimeout
	}
	if deps.JoinTimeout <= 0 {
		deps.JoinTimeout = defaultJoinTimeout
	}
	if deps.CommandQueue <= 0 {
		deps.CommandQueue = defaultCommandQueue
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
		limits:      model.RateLimits(),
		now:         deps.Clock,
		byConn:      map[string]*session{},
		byAccount:   map[kernel.AccountID]*session{},
		byCharacter: map[kernel.CharacterID]*session{},
		bySession:   map[kernel.SessionID]*session{},
		helloGrace:  deps.HelloTimeout,
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
	active := make([]*session, 0, len(g.bySession))
	for _, s := range g.bySession {
		active = append(active, s)
	}
	g.mu.Unlock()
	for _, s := range active {
		if err := g.deps.Sessions.Check(ctx, uuid.UUID(s.sessionID)); err != nil {
			g.kick(s.conn, protocol.KickSessionExpired)
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
		g.kick(s.conn, protocol.KickSessionExpired)
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
	g.kick(s.conn, why)
}

// sessionOf finds the active session of a character, if any.
func (g *Gateway) sessionOf(id kernel.CharacterID) *session {
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
		g.kick(c, protocol.KickServerRestart)
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

// Open starts a connection: the Hello timer and its command goroutine. A
// connection that stays silent is closed.
func (g *Gateway) Open(c Conn) {
	s := &session{conn: c, limiters: map[string]*ratelimit.Buckets{}, queue: make(chan model.Message, g.deps.CommandQueue), drained: make(chan struct{})}
	s.ctx, s.cancel = context.WithCancel(context.Background())
	s.out.send = func(m model.Message) { g.send(c, m) }
	s.helloTimer = time.AfterFunc(g.helloGrace, func() {
		if !s.active() {
			c.Close(model.CloseKicked, "no hello")
		}
	})
	g.mu.Lock()
	g.byConn[c.ID()] = s
	draining := g.draining.Load() // read under the lock Drain collects connections with
	g.mu.Unlock()
	go g.commands(s)
	if draining {
		g.kick(c, protocol.KickServerRestart) // upgraded as Drain began
	}
}

// Receive takes one decoded message on the connection's read loop: it
// applies the rate limits and queues the message for the connection's
// command goroutine, never waiting on a service. Before Hello only Hello is
// accepted; Ping is answered here. A full queue answers rate_limited to a
// command with a request ID and drops the rest.
func (g *Gateway) Receive(c Conn, m model.Message) {
	g.deps.Observer.MessageIn(m.Type)
	g.mu.Lock()
	s := g.byConn[c.ID()]
	g.mu.Unlock()
	if s == nil {
		return
	}
	if !s.helloSeen {
		if _, ok := m.Data.(protocol.Hello); !ok {
			c.Close(model.CloseKicked, "hello expected")
			return
		}
		s.helloSeen = true
		s.queue <- m // the queue is empty: nothing came before Hello
		return
	}
	switch d := m.Data.(type) {
	case protocol.Ping:
		g.send(c, model.Message{Data: protocol.Pong{ClientTime: d.ClientTime, ServerTime: g.now().UnixMilli()}})
		return
	case protocol.Hello:
		c.Close(model.CloseKicked, "hello twice")
		return
	}
	if lim, limited := g.limits[m.Type]; limited && !s.allow(m.Type, lim, g.now) {
		if m.ReqID != "" {
			g.send(c, model.Message{ReqID: m.ReqID, Data: refusal(model.RateLimited)})
		}
		return
	}
	select {
	case s.queue <- m:
	default:
		if m.ReqID != "" {
			g.send(c, model.Message{ReqID: m.ReqID, Data: refusal(model.RateLimited)})
		}
	}
}

// commands drains a connection's queue in order, one message at a time,
// until the connection closes; then it stops, and Closed waits for it.
func (g *Gateway) commands(s *session) {
	defer close(s.drained)
	for {
		select {
		case <-s.ctx.Done():
			return
		case m := <-s.queue:
			if s.ctx.Err() != nil {
				return
			}
			g.command(s, m)
		}
	}
}

// command runs one queued message: Hello first, then, once it has
// succeeded, each command with the connection's identity.
func (g *Gateway) command(s *session, m model.Message) {
	if hello, ok := m.Data.(protocol.Hello); ok {
		g.hello(s, hello)
		return
	}
	if !s.active() {
		return // Hello failed, and the connection is closing
	}
	c := s.conn
	switch d := m.Data.(type) {
	case protocol.Input:
		g.worldCommand(s, func(ctx context.Context, p model.Presence) error {
			return g.deps.World.Input(ctx, p, m.Seq, d)
		})
	case protocol.Attack:
		g.worldCommand(s, g.deps.World.Attack)
	case protocol.EnterPortal:
		g.worldCommand(s, g.deps.World.EnterPortal)
	case protocol.PickUp:
		g.worldCommand(s, g.deps.World.PickUp)
	case protocol.InventoryMove:
		g.inventoryCommand(s, m.ReqID, func(ctx context.Context, a model.Actor) ([]protocol.InventorySlot, error) {
			id, err := kernel.ParseItemID(d.ItemID)
			if err != nil {
				return nil, err
			}
			d.ItemID = id.String()
			return g.deps.Items.Move(ctx, a, d)
		})
	case protocol.InventoryDiscard:
		g.inventoryCommand(s, m.ReqID, func(ctx context.Context, a model.Actor) ([]protocol.InventorySlot, error) {
			id, err := kernel.ParseItemID(d.ItemID)
			if err != nil {
				return nil, err
			}
			d.ItemID = id.String()
			return g.deps.Items.Discard(ctx, a, d)
		})
	case protocol.ChatSend:
		g.chat(s, d)
	case protocol.PartyInvite:
		g.partyCommand(s, m.ReqID, func(ctx context.Context, me kernel.CharacterID) error {
			to, err := kernel.ParseCharacterID(d.CharacterID)
			if err != nil {
				return err
			}
			return g.deps.Social.Invite(ctx, me, to)
		})
	case protocol.PartyRespond:
		g.partyCommand(s, m.ReqID, func(ctx context.Context, me kernel.CharacterID) error {
			return g.deps.Social.Respond(ctx, me, d.InviteID, d.Accept)
		})
	case protocol.PartyLeave:
		g.partyCommand(s, m.ReqID, func(ctx context.Context, me kernel.CharacterID) error {
			return g.deps.Social.Leave(ctx, me) // Social is read once partyCommand has checked it is set
		})
	case protocol.PartyKick:
		g.partyCommand(s, m.ReqID, func(ctx context.Context, me kernel.CharacterID) error {
			target, err := kernel.ParseCharacterID(d.CharacterID)
			if err != nil {
				return err
			}
			return g.deps.Social.Kick(ctx, me, target)
		})
	case protocol.PartyPromote:
		g.partyCommand(s, m.ReqID, func(ctx context.Context, me kernel.CharacterID) error {
			target, err := kernel.ParseCharacterID(d.CharacterID)
			if err != nil {
				return err
			}
			return g.deps.Social.Promote(ctx, me, target)
		})
	default:
		// Market messages arrive in Stage 2.
		if m.ReqID != "" {
			g.send(c, model.Message{ReqID: m.ReqID, Data: refusal(model.NotImplemented)})
		}
	}
}

// Closed ends a connection the transport closed (slow when it fell too far
// behind; reason for the log): it cancels the command in flight, waits for
// the command goroutine, then leaves the map and clears the registry.
func (g *Gateway) Closed(c Conn, slow bool, reason string) {
	g.mu.Lock()
	s := g.byConn[c.ID()]
	g.mu.Unlock()
	if s == nil {
		return
	}
	s.cancel()
	<-s.drained
	g.mu.Lock()
	delete(g.byConn, c.ID())
	var joined bool
	if s != nil {
		s.closed = true
		joined = !s.character.ID.IsZero()
		if g.byAccount[s.account] == s {
			delete(g.byAccount, s.account)
		}
		if g.byCharacter[s.character.ID] == s {
			delete(g.byCharacter, s.character.ID)
		}
		if g.bySession[s.sessionID] == s {
			delete(g.bySession, s.sessionID)
		}
	}
	g.mu.Unlock()
	s.helloTimer.Stop()
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
	if slow {
		g.deps.Observer.SlowConsumer()
	}
	g.deps.Logger.Info("ws closed", "conn", c.ID(), "reason", reason)
}

// leave tells the world the connection's player is gone.
func (g *Gateway) leave(s *session) {
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	if err := g.deps.World.Leave(ctx, s.presence(), s.conn.ID()); err != nil {
		g.deps.Logger.WarnContext(ctx, "world leave failed", "character_id", s.character.ID, "err", err)
	}
}

// hello runs the handshake: version, token, then the join through the
// world (which checks the character is the account's), the inventory and
// the party, then Welcome. The session is registered for its character
// before the join, so the Snapshot, which may come back on the world's
// stream before the join answers, waits in the session and follows
// Welcome. An older connection of the account loses once the join holds.
func (g *Gateway) hello(s *session, h protocol.Hello) {
	ctx, cancel := context.WithTimeout(s.ctx, g.deps.JoinTimeout)
	defer cancel()
	if h.ProtocolVersion != protocol.Version {
		g.kick(s.conn, protocol.KickOutdatedProtocol)
		return
	}
	p, err := g.deps.Sessions.Validate(ctx, h.Token)
	if err != nil {
		g.kick(s.conn, protocol.KickUnauthenticated)
		return
	}
	// Every Hello rotates the token (step 16): the client keeps the one in
	// Welcome; the one it sent works once more for a minute.
	token, err := g.deps.Sessions.Rotate(ctx, p.SessionID)
	if err != nil {
		if model.PublicOf(err) == model.Unauthenticated {
			g.kick(s.conn, protocol.KickUnauthenticated)
			return
		}
		g.deps.Logger.ErrorContext(ctx, "token rotation failed", "session_id", p.SessionID, "err", err)
		s.conn.Close(model.CloseInternal, "session")
		return
	}
	charID, err := kernel.ParseCharacterID(h.CharacterID)
	if err != nil {
		g.kick(s.conn, protocol.KickUnauthenticated)
		return
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
	s.helloTimer.Stop()

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
			g.kick(s.conn, protocol.KickUnauthenticated)
			return
		}
		g.deps.Logger.ErrorContext(ctx, "join failed", "character_id", charID, "err", err)
		s.conn.Close(model.CloseInternal, "join")
		return
	}
	c := joined.Character
	inventory := []protocol.ItemView{}
	if g.deps.Items != nil {
		items, err := g.deps.Items.Inventory(ctx, charID)
		if err != nil {
			g.deps.Logger.ErrorContext(ctx, "inventory failed", "character_id", charID, "err", err)
			s.conn.Close(model.CloseInternal, "inventory")
			return
		}
		inventory = items
	}
	welcome := protocol.Welcome{
		Token:      token,
		Character:  c,
		Inventory:  inventory,
		MapID:      joined.MapID,
		ServerTime: g.now().UnixMilli(),
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
	s.out.release(joined.MapID, model.Message{Data: welcome})
	// An older connection of the account, or a second Hello on the same
	// session, loses: the newest connection wins.
	for _, prev := range []*session{old, sameSession, previous} {
		if prev != nil && prev != s {
			g.kick(prev.conn, protocol.KickLoggedInElsewhere)
		}
	}
	if missed {
		go g.rejoin(s)
	}
	g.online(ctx, s)
	g.deps.Observer.OnlineChanged(1)
	g.deps.Logger.InfoContext(ctx, "ws joined", "conn", s.conn.ID(), "character_id", charID, "map_id", joined.MapID)
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
		var active []*session
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
	if p, isRefusal := ev.Message.Data.(protocol.Response); isRefusal && p.Code == model.Internal.Code {
		g.deps.Logger.Warn("pickup failed for a reason the world could not name", "conn", s.conn.ID(), "character_id", ev.Character)
	}
	if !s.out.deliver(ev.Message) {
		s.conn.Close(model.CloseInternal, "join")
	}
}

// rejoin joins a connected player again after the world's stream broke,
// from the character's last checkpoint: MapChange first when that is
// another map than the session's, then the Snapshot. The socket stays
// open; a join that fails because the stream broke again waits for the
// next subscription, which rejoins, and any other failure (the world still
// unreachable when the join gives up, say) closes the connection so the
// client reconnects.
func (g *Gateway) rejoin(s *session) {
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
		s.conn.Close(model.CloseInternal, "rejoin")
		return
	}
	var first []model.Message
	if joined.MapID != before {
		first = append(first, model.Message{Data: protocol.MapChange{MapID: joined.MapID, X: joined.X, Y: joined.Y}})
	}
	s.out.release(joined.MapID, first...)
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

// worldCommand sends a command to the world with the connection's
// character and map. A failure is dropped: the client's next input
// supersedes it, and a world that went away is handled by the rejoin.
func (g *Gateway) worldCommand(s *session, call func(ctx context.Context, p model.Presence) error) {
	ctx, cancel := context.WithTimeout(s.ctx, commandTimeout)
	defer cancel()
	_ = call(ctx, s.presence())
}

// online tells the social service a player is connected through this
// gateway, so party traffic finds it here. A failure is logged: parties are
// optional, and the next registration (a social restart) tries again.
func (g *Gateway) online(ctx context.Context, s *session) {
	if g.deps.Social == nil {
		return
	}
	if err := g.deps.Social.Online(ctx, g.deps.ID, s.character, s.mapID()); err != nil {
		g.deps.Logger.WarnContext(ctx, "social online failed", "character_id", s.character.ID, "err", err)
	}
}

// chat cleans a line through the social service, then hands a map line to
// the sender's map instance (everyone there hears it, the sender too); the
// social service delivers a party line to each member itself, on any map
// and through any gateway. A refused line answers a Response (ChatSend
// carries no request ID).
func (g *Gateway) chat(s *session, d protocol.ChatSend) {
	if g.deps.Social == nil {
		g.send(s.conn, model.Message{Data: refusal(model.ChatUnavailable)})
		return
	}
	ctx, cancel := context.WithTimeout(s.ctx, commandTimeout)
	defer cancel()
	line, err := g.deps.Social.Chat(ctx, s.character, d.Channel, d.Text)
	if err != nil {
		g.send(s.conn, model.Message{Data: g.refuse(s, "ChatSend", err)})
		return
	}
	if line.Channel == protocol.ChatMap {
		g.worldCommand(s, func(ctx context.Context, p model.Presence) error {
			return g.deps.World.Say(ctx, p, s.character.Name, line.Text)
		})
	}
}

// partyCommand runs a party command with the connection's identity and
// answers a Response; the social service pushes PartyState itself.
func (g *Gateway) partyCommand(s *session, reqID string, run func(ctx context.Context, me kernel.CharacterID) error) {
	if g.deps.Social == nil {
		g.send(s.conn, model.Message{ReqID: reqID, Data: refusal(model.PartiesUnavailable)})
		return
	}
	ctx, cancel := context.WithTimeout(s.ctx, commandTimeout)
	defer cancel()
	if err := run(ctx, s.character.ID); err != nil {
		g.send(s.conn, model.Message{ReqID: reqID, Data: g.refuse(s, "party", err)})
		return
	}
	g.send(s.conn, model.Message{ReqID: reqID, Data: protocol.Response{OK: true}})
}

// socialDelivered turns one message of the social service's stream into
// what the client sees. A new boot ID on Subscribed means the social
// service restarted and every party is gone: each connected player gets
// PartyState with no party and is registered again.
func (g *Gateway) socialDelivered(ev model.SocialEvent) {
	if ev.Subscribed {
		g.mu.Lock()
		restarted := g.socialBoot != "" && g.socialBoot != ev.BootID
		g.socialBoot = ev.BootID
		active := make([]*session, 0, len(g.byCharacter))
		for _, s := range g.byCharacter {
			active = append(active, s)
		}
		g.mu.Unlock()
		if restarted {
			ctx, cancel := context.WithTimeout(context.Background(), g.deps.JoinTimeout)
			defer cancel()
			for _, s := range active {
				g.send(s.conn, model.Message{Data: protocol.PartyState{}})
				g.online(ctx, s)
			}
		}
		return
	}
	t := g.sessionOf(ev.Character)
	if t == nil {
		return // gone from this gateway; the social service learns it from Offline
	}
	g.send(t.conn, ev.Message)
}

// refusal is a failure as a Response.
func refusal(p model.Public) protocol.Response {
	return protocol.Response{OK: false, Code: p.Code, Message: p.Message}
}

// refuse answers a command's error with its own code and message, and logs
// the cause of an error nobody expected (a database failure), which the
// player only sees as internal.
func (g *Gateway) refuse(s *session, command string, err error) protocol.Response {
	p := model.PublicOf(err)
	if model.IsInternal(p) {
		g.deps.Logger.Error("command failed", "conn", s.conn.ID(), "character_id", s.character.ID, "command", command, "err", err)
	}
	return refusal(p)
}

// inventoryCommand runs an inventory command on the connection's own
// goroutine (it blocks on the database, never a tick), answers with a
// Response, and sends the changed slots as InventoryChanged.
func (g *Gateway) inventoryCommand(s *session, reqID string, run func(ctx context.Context, a model.Actor) ([]protocol.InventorySlot, error)) {
	if g.deps.Items == nil {
		g.send(s.conn, model.Message{ReqID: reqID, Data: refusal(model.InventoryUnavailable)})
		return
	}
	ctx, cancel := context.WithTimeout(s.ctx, commandTimeout)
	defer cancel()
	changed, err := run(ctx, model.Actor{Account: s.account, Character: s.character.ID})
	if err != nil {
		g.send(s.conn, model.Message{ReqID: reqID, Data: g.refuse(s, "inventory", err)})
		return
	}
	g.send(s.conn, model.Message{ReqID: reqID, Data: protocol.Response{OK: true}})
	if len(changed) > 0 {
		g.send(s.conn, model.Message{Data: protocol.InventoryChanged{Slots: changed}})
	}
}

// kick tells the peer why and closes.
func (g *Gateway) kick(c Conn, reason protocol.KickReason) {
	g.send(c, model.Message{Data: protocol.Kicked{Reason: reason}})
	c.Close(model.CloseKicked, string(reason))
}

func (g *Gateway) send(c Conn, m model.Message) {
	if m.Type == "" {
		m.Type = typeName(m.Data)
	}
	g.deps.Observer.MessageOut(m.Type)
	c.Send(m) // a full queue closes the connection; Closed cleans up
}

// noopObserver is the Observer when none is given.
type noopObserver struct{}

// MessageIn does nothing.
func (noopObserver) MessageIn(string) {}

// MessageOut does nothing.
func (noopObserver) MessageOut(string) {}

// SlowConsumer does nothing.
func (noopObserver) SlowConsumer() {}

// OnlineChanged does nothing.
func (noopObserver) OnlineChanged(int) {}
