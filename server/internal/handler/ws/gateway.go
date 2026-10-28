// Package ws is the gateway (Low-level design, pages 3, 4 and 10):
// WebSocket sessions, the Hello handshake, one connection per account,
// per-account rate limits, and routing of each message to the service that
// owns it, with the identity taken from the connection, never from the
// message. Players join through the world service, and everything the
// world sends them arrives on one delivery stream per gateway, which this
// package turns into protocol messages. When that stream breaks (the world
// restarted, or this gateway fell behind) every socket stays open and each
// player is joined again once the stream is back.
package ws

import (
	"context"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	accountv1 "finga/api/gen/account/v1"
	itemv1 "finga/api/gen/item/v1"
	socialv1 "finga/api/gen/social/v1"
	worldv1 "finga/api/gen/world/v1"
	"finga/internal/handler/errmap"
	"finga/internal/model"
	"finga/internal/protocol"
	"finga/pkg/auth"
	"finga/pkg/grpcx"
	"finga/pkg/ratelimit"
	"finga/pkg/wsconn"
)

// Observer counts traffic; internal/telemetry implements it. nil is fine.
type Observer interface {
	MessageIn(msgType string)
	MessageOut(msgType string)
	SlowConsumer()
	OnlineChanged(delta int)
}

// SessionWatcher streams the session ends the account service reports;
// its generated client satisfies it.
type SessionWatcher interface {
	WatchSessions(ctx context.Context, in *accountv1.WatchSessionsRequest, opts ...grpc.CallOption) (grpc.ServerStreamingClient[accountv1.WatchSessionsResponse], error)
}

// Dependencies are the services and settings the gateway is built from.
type Dependencies struct {
	// ID names this gateway node to the services that stream to it; random
	// when empty.
	ID          string
	Sessions    auth.Guard
	Watcher     SessionWatcher               // the account service; nil: no session ends are streamed, the minute check still runs
	World       worldv1.WorldServiceClient   // joins, commands, and the delivery stream Run follows
	Items       itemv1.ItemServiceClient     // the item service; nil: Welcome carries no items and inventory commands are unavailable
	Social      socialv1.SocialServiceClient // parties and chat; nil: both are unavailable
	Logger      *slog.Logger
	Observer    Observer
	CORSOrigins []string
	Clock       func() time.Time
	// HelloTimeout is how long a connection has to send Hello; 5 s by default.
	HelloTimeout time.Duration
	// JoinTimeout bounds a join, waiting out a world that is starting or
	// restarting; 5 s by default.
	JoinTimeout time.Duration
	// SessionCheckInterval is how often Run checks each connection's session
	// and kicks the expired ones; a minute by default.
	SessionCheckInterval time.Duration
}

const (
	defaultHelloTimeout         = 5 * time.Second
	defaultSessionCheckInterval = time.Minute
	defaultJoinTimeout          = 5 * time.Second
	commandTimeout              = 5 * time.Second
	joinRetryFirst              = 20 * time.Millisecond
	joinRetryMax                = 500 * time.Millisecond
)

// Gateway serves /ws.
type Gateway struct {
	deps   Dependencies
	codec  *wsconn.JSONCodec
	limits map[string]limit
	now    func() time.Time

	mu          sync.Mutex
	byConn      map[string]*session
	byAccount   map[model.AccountID]*session
	byCharacter map[model.CharacterID]*session // active sessions, for party and chat delivery
	bySession   map[model.SessionID]*session   // active connections by login session
	socialBoot  string                         // the social service's boot ID, from its stream
	worldSubs   uint64                         // subscriptions to the world's stream so far
	helloGrace  time.Duration
}

// New builds the gateway.
func New(deps Dependencies) *Gateway {
	if deps.Clock == nil {
		deps.Clock = time.Now
	}
	if deps.HelloTimeout <= 0 {
		deps.HelloTimeout = defaultHelloTimeout
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
		codec:       protocol.NewCodec(),
		limits:      rateLimits(),
		now:         deps.Clock,
		byConn:      map[string]*session{},
		byAccount:   map[model.AccountID]*session{},
		byCharacter: map[model.CharacterID]*session{},
		bySession:   map[model.SessionID]*session{},
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
		go grpcx.Follow(ctx, func(ctx context.Context) (grpc.ServerStreamingClient[worldv1.DeliverResponse], error) {
			return g.deps.World.Deliver(ctx, &worldv1.DeliverRequest{GatewayId: g.deps.ID})
		}, g.worldDelivered, grpcx.FollowOptions{OnBreak: func(err error) {
			g.deps.Logger.WarnContext(ctx, "world stream broke; subscribing again", "err", err)
		}})
	}
	if g.deps.Social != nil {
		go grpcx.Follow(ctx, func(ctx context.Context) (grpc.ServerStreamingClient[socialv1.DeliverResponse], error) {
			return g.deps.Social.Deliver(ctx, &socialv1.DeliverRequest{GatewayId: g.deps.ID})
		}, g.socialDelivered, grpcx.FollowOptions{OnBreak: func(err error) {
			g.deps.Logger.WarnContext(ctx, "social stream broke; subscribing again", "err", err)
		}})
	}
	if g.deps.Watcher != nil {
		go grpcx.Follow(ctx, func(ctx context.Context) (grpc.ServerStreamingClient[accountv1.WatchSessionsResponse], error) {
			return g.deps.Watcher.WatchSessions(ctx, &accountv1.WatchSessionsRequest{GatewayId: g.deps.ID})
		}, g.sessionEnded, grpcx.FollowOptions{OnBreak: func(err error) {
			g.deps.Logger.WarnContext(ctx, "session stream broke; subscribing again", "err", err)
		}})
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
func (g *Gateway) sessionEnded(m *accountv1.WatchSessionsResponse) {
	switch {
	case m.GetSessionRevoked() != nil:
		id, err := model.ParseSessionID(m.GetSessionRevoked().GetSessionId())
		if err == nil {
			g.KickSession(id)
		}
	case m.GetAccountRevoked() != nil:
		ev := m.GetAccountRevoked()
		id, err := model.ParseAccountID(ev.GetAccountId())
		if err == nil {
			g.KickAccount(id, ev.GetReason() == accountv1.RevokeReason_REVOKE_REASON_NEW_LOGIN)
		}
	}
}

// KickSession closes every connection on a login session: the account
// service reported it ended by reuse or logout.
func (g *Gateway) KickSession(sessionID model.SessionID) {
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
func (g *Gateway) KickAccount(accountID model.AccountID, newLogin bool) {
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
func (g *Gateway) sessionOf(id model.CharacterID) *session {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.byCharacter[id]
}

// ServeHTTP upgrades the request and runs the session until it closes.
func (g *Gateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	wsconn.Serve(w, r, g, wsconn.Options{Codec: g.codec, AllowedOrigins: g.deps.CORSOrigins})
}

// OnOpen starts the Hello timer: a connection that stays silent is closed.
func (g *Gateway) OnOpen(c wsconn.Conn) {
	s := &session{conn: c, limiters: map[string]*ratelimit.Buckets{}}
	s.out.send = func(m wsconn.Message) { g.send(c, m) }
	s.helloTimer = time.AfterFunc(g.helloGrace, func() {
		if !s.active() {
			c.Close(wsconn.CloseKicked, "no hello")
		}
	})
	g.mu.Lock()
	g.byConn[c.ID()] = s
	g.mu.Unlock()
}

// OnMessage routes one message. Before Hello only Hello is accepted.
func (g *Gateway) OnMessage(c wsconn.Conn, m wsconn.Message) {
	g.deps.Observer.MessageIn(m.Type)
	g.mu.Lock()
	s := g.byConn[c.ID()]
	g.mu.Unlock()
	if s == nil {
		return
	}
	if !s.active() {
		hello, ok := m.Data.(protocol.Hello)
		if !ok {
			c.Close(wsconn.CloseKicked, "hello expected")
			return
		}
		g.hello(s, hello)
		return
	}
	if lim, limited := g.limits[m.Type]; limited && !s.allow(m.Type, lim, g.now) {
		if m.ReqID != "" {
			g.send(c, wsconn.Message{ReqID: m.ReqID, Data: refusal(errmap.RateLimited)})
		}
		return
	}
	switch d := m.Data.(type) {
	case protocol.Input:
		g.worldCommand(s, func(ctx context.Context, p *worldv1.Presence) error {
			_, err := g.deps.World.Input(ctx, &worldv1.InputRequest{Presence: p, Seq: m.Seq, Keys: &worldv1.Keys{Left: d.Left, Right: d.Right, Up: d.Up, Down: d.Down, Jump: d.Jump}})
			return err
		})
	case protocol.Attack:
		g.worldCommand(s, func(ctx context.Context, p *worldv1.Presence) error {
			_, err := g.deps.World.Attack(ctx, &worldv1.AttackRequest{Presence: p})
			return err
		})
	case protocol.EnterPortal:
		g.worldCommand(s, func(ctx context.Context, p *worldv1.Presence) error {
			_, err := g.deps.World.EnterPortal(ctx, &worldv1.EnterPortalRequest{Presence: p})
			return err
		})
	case protocol.PickUp:
		g.worldCommand(s, func(ctx context.Context, p *worldv1.Presence) error {
			_, err := g.deps.World.PickUp(ctx, &worldv1.PickUpRequest{Presence: p})
			return err
		})
	case protocol.InventoryMove:
		g.inventoryCommand(s, m.ReqID, func(ctx context.Context, actor *itemv1.Actor) ([]*itemv1.Slot, error) {
			id, err := model.ParseItemID(d.ItemID)
			if err != nil {
				return nil, err
			}
			res, err := g.deps.Items.Move(ctx, &itemv1.MoveRequest{Actor: actor, IdempotencyKey: d.IdempotencyKey, ItemId: id.String(), Tab: d.Tab, Slot: int32(d.Slot)})
			return res.GetChanged(), err
		})
	case protocol.InventoryDiscard:
		g.inventoryCommand(s, m.ReqID, func(ctx context.Context, actor *itemv1.Actor) ([]*itemv1.Slot, error) {
			id, err := model.ParseItemID(d.ItemID)
			if err != nil {
				return nil, err
			}
			res, err := g.deps.Items.Discard(ctx, &itemv1.DiscardRequest{Actor: actor, IdempotencyKey: d.IdempotencyKey, ItemId: id.String(), Quantity: int32(d.Quantity)})
			return res.GetChanged(), err
		})
	case protocol.ChatSend:
		g.chat(s, d)
	case protocol.PartyInvite:
		g.partyCommand(s, m.ReqID, func(ctx context.Context, me string) error {
			to, err := model.ParseCharacterID(d.CharacterID)
			if err != nil {
				return err
			}
			_, err = g.deps.Social.Invite(ctx, &socialv1.InviteRequest{CharacterId: me, TargetId: to.String()})
			return err
		})
	case protocol.PartyRespond:
		g.partyCommand(s, m.ReqID, func(ctx context.Context, me string) error {
			_, err := g.deps.Social.Respond(ctx, &socialv1.RespondRequest{CharacterId: me, InviteId: d.InviteID, Accept: d.Accept})
			return err
		})
	case protocol.PartyLeave:
		g.partyCommand(s, m.ReqID, func(ctx context.Context, me string) error {
			_, err := g.deps.Social.LeaveParty(ctx, &socialv1.LeavePartyRequest{CharacterId: me})
			return err
		})
	case protocol.PartyKick:
		g.partyCommand(s, m.ReqID, func(ctx context.Context, me string) error {
			target, err := model.ParseCharacterID(d.CharacterID)
			if err != nil {
				return err
			}
			_, err = g.deps.Social.Kick(ctx, &socialv1.KickRequest{CharacterId: me, TargetId: target.String()})
			return err
		})
	case protocol.PartyPromote:
		g.partyCommand(s, m.ReqID, func(ctx context.Context, me string) error {
			target, err := model.ParseCharacterID(d.CharacterID)
			if err != nil {
				return err
			}
			_, err = g.deps.Social.Promote(ctx, &socialv1.PromoteRequest{CharacterId: me, TargetId: target.String()})
			return err
		})
	case protocol.Ping:
		g.send(c, wsconn.Message{Data: protocol.Pong{ClientTime: d.ClientTime, ServerTime: g.now().UnixMilli()}})
	case protocol.Hello:
		c.Close(wsconn.CloseKicked, "hello twice")
	default:
		// Market messages arrive in Stage 2.
		if m.ReqID != "" {
			g.send(c, wsconn.Message{ReqID: m.ReqID, Data: refusal(errmap.NotImplemented)})
		}
	}
}

// OnClose leaves the map and clears the registry.
func (g *Gateway) OnClose(c wsconn.Conn, err error) {
	g.mu.Lock()
	s := g.byConn[c.ID()]
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
	if s == nil {
		return
	}
	s.helloTimer.Stop()
	if joined {
		// The world removes the player only if it joined through this
		// connection; a rejoin that finishes after this leaves again.
		g.leave(s)
	}
	if s.active() {
		if g.deps.Social != nil {
			ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
			if _, err := g.deps.Social.Offline(ctx, &socialv1.OfflineRequest{CharacterId: s.character.ID.String()}); err != nil {
				g.deps.Logger.WarnContext(ctx, "social offline failed", "character_id", s.character.ID, "err", err)
			}
			cancel()
		}
		g.deps.Observer.OnlineChanged(-1)
	}
	var ce *wsconn.CloseError
	if isCloseError(err, &ce) && ce.Code == wsconn.CloseSlowConsumer {
		g.deps.Observer.SlowConsumer()
	}
	g.deps.Logger.Info("ws closed", "conn", c.ID(), "reason", errText(err))
}

// leave tells the world the connection's player is gone.
func (g *Gateway) leave(s *session) {
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	if _, err := g.deps.World.Leave(ctx, &worldv1.LeaveRequest{Presence: s.presence(), ConnectionId: s.conn.ID()}); err != nil {
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
	ctx, cancel := context.WithTimeout(context.Background(), g.deps.JoinTimeout)
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
		if errmap.For(err) == errmap.Unauthenticated {
			g.kick(s.conn, protocol.KickUnauthenticated)
			return
		}
		g.deps.Logger.ErrorContext(ctx, "token rotation failed", "session_id", p.SessionID, "err", err)
		s.conn.Close(wsconn.CloseInternal, "session")
		return
	}
	charID, err := model.ParseCharacterID(h.CharacterID)
	if err != nil {
		g.kick(s.conn, protocol.KickUnauthenticated)
		return
	}
	account, sessionID := model.AccountID(p.AccountID), model.SessionID(p.SessionID)

	// A rejoin (the world's stream broke) waits until this join is out.
	s.rejoinMu.Lock()
	defer s.rejoinMu.Unlock()
	s.out.hold()
	g.mu.Lock()
	previous := g.byCharacter[charID]
	g.byCharacter[charID] = s
	s.account, s.sessionID, s.character.ID = account, sessionID, charID
	subs := g.worldSubs
	g.mu.Unlock()
	s.helloTimer.Stop()

	res, err := g.join(ctx, &worldv1.JoinRequest{GatewayId: g.deps.ID, ConnectionId: s.conn.ID(), AccountId: account.String(), CharacterId: charID.String()})
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
		s.character.ID = model.CharacterID{}
		g.mu.Unlock()
		// Someone else's character, or none, is a bad Hello; a world or
		// character service that cannot answer is a server fault the client
		// retries.
		if errmap.For(err).Code == characterNotFound {
			g.kick(s.conn, protocol.KickUnauthenticated)
			return
		}
		g.deps.Logger.ErrorContext(ctx, "join failed", "character_id", charID, "err", err)
		s.conn.Close(wsconn.CloseInternal, "join")
		return
	}
	c := res.GetCharacter()
	inventory := []protocol.ItemView{}
	if g.deps.Items != nil {
		res, err := g.deps.Items.Inventory(ctx, &itemv1.InventoryRequest{CharacterId: charID.String()})
		if err != nil {
			g.deps.Logger.ErrorContext(ctx, "inventory failed", "character_id", charID, "err", err)
			s.conn.Close(wsconn.CloseInternal, "inventory")
			return
		}
		inventory = itemViews(res.GetItems())
	}
	welcome := protocol.Welcome{
		Token:      token,
		Character:  characterView(c),
		Inventory:  inventory,
		MapID:      c.GetMapId(),
		ServerTime: g.now().UnixMilli(),
	}
	if g.deps.Social != nil {
		// Parties are optional: a social service that cannot be reached
		// leaves Welcome without one rather than the player out of the game.
		res, err := g.deps.Social.PartyOf(ctx, &socialv1.PartyOfRequest{CharacterId: charID.String()})
		if err != nil {
			g.deps.Logger.WarnContext(ctx, "party lookup failed", "character_id", charID, "err", err)
		}
		welcome.Party = partyView(res.GetParty())
	}

	g.mu.Lock()
	old := g.byAccount[account]
	sameSession := g.bySession[sessionID]
	g.byAccount[account] = s
	g.bySession[sessionID] = s
	s.character = player{ID: charID, Name: c.GetName(), Level: int(c.GetLevel()), HP: int(c.GetHp()), MaxHP: int(c.GetMaxHp())}
	s.setActive()
	missed := g.worldSubs != subs // the stream broke during the join
	g.mu.Unlock()
	s.out.release(c.GetMapId(), wsconn.Message{Data: welcome})
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
	g.deps.Logger.InfoContext(ctx, "ws joined", "conn", s.conn.ID(), "character_id", charID, "map_id", c.GetMapId())
}

// worldDelivered acts on one message of the world's stream: a delivery
// goes to its player's session; Subscribed after the first means the
// stream broke and every player this gateway had joined is gone from the
// world, so each is joined again. Nothing here waits on the world: the
// joins' Snapshots come back on this same stream.
func (g *Gateway) worldDelivered(m *worldv1.DeliverResponse) {
	if m.GetSubscribed() != nil {
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
	id, err := model.ParseCharacterID(m.GetCharacterId())
	if err != nil {
		return
	}
	s := g.sessionOf(id)
	if s == nil {
		return // gone from this gateway; the world learns it from Leave
	}
	msg, ok := delivery(m)
	if !ok {
		return
	}
	if p, isRefusal := msg.Data.(protocol.Response); isRefusal && p.Code == errmap.Internal.Code {
		g.deps.Logger.Warn("pickup failed for a reason the world could not name", "conn", s.conn.ID(), "character_id", id)
	}
	if !s.out.deliver(msg) {
		s.conn.Close(wsconn.CloseInternal, "join")
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
	ctx, cancel := context.WithTimeout(context.Background(), g.deps.JoinTimeout)
	defer cancel()
	before := s.mapID()
	s.out.hold()
	res, err := g.join(ctx, &worldv1.JoinRequest{GatewayId: g.deps.ID, ConnectionId: s.conn.ID(), AccountId: s.account.String(), CharacterId: s.character.ID.String()})
	if err != nil {
		s.out.abort()
		if r, _ := grpcx.ReasonOf(err); r == grpcx.Key(worldv1.Reason_REASON_NOT_SUBSCRIBED) {
			// The stream broke again: the next subscription rejoins.
			g.deps.Logger.WarnContext(ctx, "rejoin waits for the world's stream", "character_id", s.character.ID)
			return
		}
		g.deps.Logger.ErrorContext(ctx, "rejoin failed", "character_id", s.character.ID, "err", err)
		s.conn.Close(wsconn.CloseInternal, "rejoin")
		return
	}
	c := res.GetCharacter()
	var first []wsconn.Message
	if c.GetMapId() != before {
		first = append(first, wsconn.Message{Data: protocol.MapChange{MapID: c.GetMapId(), X: c.GetX(), Y: c.GetY()}})
	}
	s.out.release(c.GetMapId(), first...)
	g.mu.Lock()
	closed := s.closed
	g.mu.Unlock()
	if closed {
		g.leave(s)
		return
	}
	g.deps.Logger.InfoContext(ctx, "ws rejoined", "conn", s.conn.ID(), "character_id", s.character.ID, "map_id", c.GetMapId())
}

// join asks the world to join a player, waiting out a world that is not
// reachable yet (starting, restarting) or has not seen this gateway's
// stream yet, until ctx ends. A join is safe to repeat: the world replaces
// a presence that joined through the same connection.
func (g *Gateway) join(ctx context.Context, req *worldv1.JoinRequest) (*worldv1.JoinResponse, error) {
	wait := joinRetryFirst
	for {
		res, err := g.deps.World.Join(ctx, req)
		if err == nil || !waitable(err) {
			return res, err
		}
		t := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			t.Stop()
			return nil, err
		case <-t.C:
		}
		wait = min(wait*2, joinRetryMax)
	}
}

// waitable is a join failure that time may cure: the world is unreachable,
// or its stream to this gateway is not open yet.
func waitable(err error) bool {
	if r, ok := grpcx.ReasonOf(err); ok {
		return r == grpcx.Key(worldv1.Reason_REASON_NOT_SUBSCRIBED)
	}
	return status.Code(err) == codes.Unavailable
}

// worldCommand sends a command to the world with the connection's
// character and map. A failure is dropped: the client's next input
// supersedes it, and a world that went away is handled by the rejoin.
func (g *Gateway) worldCommand(s *session, call func(ctx context.Context, p *worldv1.Presence) error) {
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
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
	c := s.character
	member := &socialv1.Member{Id: c.ID.String(), Name: c.Name, Level: int32(c.Level), Hp: int32(c.HP), MaxHp: int32(c.MaxHP), MapId: s.mapID()}
	if _, err := g.deps.Social.Online(ctx, &socialv1.OnlineRequest{GatewayId: g.deps.ID, Member: member}); err != nil {
		g.deps.Logger.WarnContext(ctx, "social online failed", "character_id", c.ID, "err", err)
	}
}

// chat cleans a line through the social service, then hands a map line to
// the sender's map instance (everyone there hears it, the sender too); the
// social service delivers a party line to each member itself, on any map
// and through any gateway. A refused line answers a Response (ChatSend
// carries no request ID).
func (g *Gateway) chat(s *session, d protocol.ChatSend) {
	if g.deps.Social == nil {
		g.send(s.conn, wsconn.Message{Data: refusal(errmap.ChatUnavailable)})
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	res, err := g.deps.Social.Chat(ctx, &socialv1.ChatRequest{SenderId: s.character.ID.String(), SenderName: s.character.Name, Channel: chatChannels[d.Channel], Text: d.Text})
	if err != nil {
		g.send(s.conn, wsconn.Message{Data: g.refuse(s, "ChatSend", err)})
		return
	}
	if res.GetChannel() == socialv1.ChatChannel_CHAT_CHANNEL_MAP {
		g.worldCommand(s, func(ctx context.Context, p *worldv1.Presence) error {
			_, err := g.deps.World.Say(ctx, &worldv1.SayRequest{Presence: p, SenderName: s.character.Name, Text: res.GetText()})
			return err
		})
	}
}

// chatChannels are the client's channels in the contract; anything else is
// unspecified, which the social service refuses.
var chatChannels = map[protocol.ChatChannel]socialv1.ChatChannel{
	protocol.ChatMap:   socialv1.ChatChannel_CHAT_CHANNEL_MAP,
	protocol.ChatParty: socialv1.ChatChannel_CHAT_CHANNEL_PARTY,
}

// partyCommand runs a party command with the connection's identity and
// answers a Response; the social service pushes PartyState itself.
func (g *Gateway) partyCommand(s *session, reqID string, run func(ctx context.Context, me string) error) {
	if g.deps.Social == nil {
		g.send(s.conn, wsconn.Message{ReqID: reqID, Data: refusal(errmap.PartiesUnavailable)})
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	if err := run(ctx, s.character.ID.String()); err != nil {
		g.send(s.conn, wsconn.Message{ReqID: reqID, Data: g.refuse(s, "party", err)})
		return
	}
	g.send(s.conn, wsconn.Message{ReqID: reqID, Data: protocol.Response{OK: true}})
}

// socialDelivered turns one message of the social service's stream into
// what the client sees. A new boot ID on Subscribed means the social
// service restarted and every party is gone: each connected player gets
// PartyState with no party and is registered again.
func (g *Gateway) socialDelivered(m *socialv1.DeliverResponse) {
	if sub := m.GetSubscribed(); sub != nil {
		g.mu.Lock()
		restarted := g.socialBoot != "" && g.socialBoot != sub.GetBootId()
		g.socialBoot = sub.GetBootId()
		active := make([]*session, 0, len(g.byCharacter))
		for _, s := range g.byCharacter {
			active = append(active, s)
		}
		g.mu.Unlock()
		if restarted {
			ctx, cancel := context.WithTimeout(context.Background(), g.deps.JoinTimeout)
			defer cancel()
			for _, s := range active {
				g.send(s.conn, wsconn.Message{Data: protocol.PartyState{}})
				g.online(ctx, s)
			}
		}
		return
	}
	id, err := model.ParseCharacterID(m.GetCharacterId())
	if err != nil {
		return
	}
	t := g.sessionOf(id)
	if t == nil {
		return // gone from this gateway; the social service learns it from Offline
	}
	switch {
	case m.GetPartyState() != nil:
		g.send(t.conn, wsconn.Message{Data: protocol.PartyState{Party: partyView(m.GetPartyState().GetParty())}})
	case m.GetPartyInvited() != nil:
		inv := m.GetPartyInvited()
		g.send(t.conn, wsconn.Message{Data: protocol.PartyInvited{InviteID: inv.GetInviteId(), From: inv.GetFromId(), FromName: inv.GetFromName()}})
	case m.GetChatLine() != nil:
		line := m.GetChatLine()
		g.send(t.conn, wsconn.Message{Data: protocol.ChatMessage{Channel: protocol.ChatParty, Sender: line.GetSenderId(), SenderName: line.GetSenderName(), Text: line.GetText()}})
	}
}

// refusal is a failure as a Response.
func refusal(p errmap.Public) protocol.Response {
	return protocol.Response{OK: false, Code: p.Code, Message: p.Message}
}

// refuse answers a command's error with its own code and message, and logs
// the cause of an error nobody expected (a database failure), which the
// player only sees as internal.
func (g *Gateway) refuse(s *session, command string, err error) protocol.Response {
	p := errmap.For(err)
	if errmap.IsInternal(p) {
		g.deps.Logger.Error("command failed", "conn", s.conn.ID(), "character_id", s.character.ID, "command", command, "err", err)
	}
	return refusal(p)
}

func partyView(p *socialv1.Party) *protocol.PartyView {
	if p == nil {
		return nil
	}
	v := &protocol.PartyView{Leader: p.GetLeaderId(), Members: make([]protocol.PartyMember, 0, len(p.GetMembers()))}
	for _, m := range p.GetMembers() {
		v.Members = append(v.Members, protocol.PartyMember{CharacterID: m.GetId(), Name: m.GetName(), Level: int(m.GetLevel()), HP: int(m.GetHp()), MaxHP: int(m.GetMaxHp()), MapID: m.GetMapId(), Online: m.GetOnline()})
	}
	return v
}

// inventoryCommand runs an inventory command on the connection's own
// goroutine (it blocks on the database, never a tick), answers with a
// Response, and sends the changed slots as InventoryChanged.
func (g *Gateway) inventoryCommand(s *session, reqID string, run func(ctx context.Context, actor *itemv1.Actor) ([]*itemv1.Slot, error)) {
	if g.deps.Items == nil {
		g.send(s.conn, wsconn.Message{ReqID: reqID, Data: refusal(errmap.InventoryUnavailable)})
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	changed, err := run(ctx, &itemv1.Actor{AccountId: s.account.String(), CharacterId: s.character.ID.String()})
	if err != nil {
		g.send(s.conn, wsconn.Message{ReqID: reqID, Data: g.refuse(s, "inventory", err)})
		return
	}
	g.send(s.conn, wsconn.Message{ReqID: reqID, Data: protocol.Response{OK: true}})
	if len(changed) > 0 {
		g.send(s.conn, wsconn.Message{Data: protocol.InventoryChanged{Slots: inventorySlots(changed)}})
	}
}

// kick tells the peer why and closes.
func (g *Gateway) kick(c wsconn.Conn, reason protocol.KickReason) {
	g.send(c, wsconn.Message{Data: protocol.Kicked{Reason: reason}})
	c.Close(wsconn.CloseKicked, string(reason))
}

func (g *Gateway) send(c wsconn.Conn, m wsconn.Message) {
	if m.Type == "" {
		m.Type = typeName(m.Data)
	}
	g.deps.Observer.MessageOut(m.Type)
	_ = c.Send(m) // a full queue closes the connection; OnClose cleans up
}

// characterNotFound is the code of a character that is not the account's.
const characterNotFound = "character_not_found"

func itemViews(items []*itemv1.Item) []protocol.ItemView {
	out := make([]protocol.ItemView, 0, len(items))
	for _, it := range items {
		out = append(out, itemView(it))
	}
	return out
}

func itemView(it *itemv1.Item) protocol.ItemView {
	return protocol.ItemView{ItemID: it.GetId(), Template: it.GetTemplateId(), Tab: it.GetTab(), Slot: int(it.GetSlot()), Quantity: int(it.GetQuantity())}
}

// inventorySlots are the item service's changed slots as the client sees
// them.
func inventorySlots(slots []*itemv1.Slot) []protocol.InventorySlot {
	out := make([]protocol.InventorySlot, 0, len(slots))
	for _, s := range slots {
		slot := protocol.InventorySlot{Tab: s.GetTab(), Slot: int(s.GetSlot())}
		if s.GetItem() != nil {
			v := itemView(s.GetItem())
			slot.Item = &v
		}
		out = append(out, slot)
	}
	return out
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
