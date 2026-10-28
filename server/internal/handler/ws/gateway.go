// Package ws is the gateway (Low-level design, pages 3 and 4): WebSocket
// sessions, the Hello handshake, one connection per account, per-account
// rate limits, and routing of each message to the character's map
// instance with the identity taken from the connection, never from the
// message. It is the only package that turns the world's result types into
// protocol messages.
package ws

import (
	"context"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc"

	accountv1 "finga/api/gen/account/v1"
	characterv1 "finga/api/gen/character/v1"
	itemv1 "finga/api/gen/item/v1"
	socialv1 "finga/api/gen/social/v1"
	"finga/internal/application/world"
	"finga/internal/gamedata"
	"finga/internal/handler/errmap"
	"finga/internal/model"
	"finga/internal/protocol"
	"finga/internal/sim"
	"finga/pkg/auth"
	"finga/pkg/grpcx"
	"finga/pkg/ratelimit"
	"finga/pkg/wsconn"
)

// CharacterSource is what the gateway needs from the character service
// (page 10); its generated client satisfies it.
type CharacterSource interface {
	Load(ctx context.Context, in *characterv1.LoadRequest, opts ...grpc.CallOption) (*characterv1.LoadResponse, error)
}

// MapInstance is what the gateway needs from a map actor.
type MapInstance interface {
	Join(ctx context.Context, p world.Player, sink world.Sink) error
	Leave(id model.CharacterID) bool
	Input(id model.CharacterID, seq uint32, in sim.Input) bool
	Attack(id model.CharacterID) bool
	PickUp(id model.CharacterID) bool
	EnterPortal(id model.CharacterID) bool
	MapID() string
	Chat(id model.CharacterID, name, text string) bool
}

// World finds map instances.
type World interface {
	Instance(mapID string) (MapInstance, error)
}

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
	Watcher     SessionWatcher // the account service; nil: no session ends are streamed, the minute check still runs
	Characters  CharacterSource
	Items       itemv1.ItemServiceClient     // the item service; nil: Welcome carries no items and inventory commands are unavailable
	Social      socialv1.SocialServiceClient // parties and chat; nil: both are unavailable
	World       World
	Logger      *slog.Logger
	Observer    Observer
	CORSOrigins []string
	Clock       func() time.Time
	// HelloTimeout is how long a connection has to send Hello; 5 s by default.
	HelloTimeout time.Duration
	// SessionCheckInterval is how often Run checks each connection's session
	// and kicks the expired ones; a minute by default.
	SessionCheckInterval time.Duration
}

const (
	defaultHelloTimeout         = 5 * time.Second
	defaultSessionCheckInterval = time.Minute
	joinTimeout                 = 5 * time.Second
	commandTimeout              = 5 * time.Second
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

// Run follows the account service's session ends (when a Watcher is set)
// and checks every connection's login session on an interval, until ctx is
// done, kicking the ones whose session ended (expired, logged out, reused
// token) with session_expired. The minute check catches what a broken
// stream missed. It never returns an error.
func (g *Gateway) Run(ctx context.Context) error {
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
		s.inst().Input(s.character.ID, m.Seq, sim.Input{Left: d.Left, Right: d.Right, Up: d.Up, Down: d.Down, Jump: d.Jump})
	case protocol.Attack:
		s.inst().Attack(s.character.ID)
	case protocol.EnterPortal:
		s.inst().EnterPortal(s.character.ID)
	case protocol.PickUp:
		s.inst().PickUp(s.character.ID)
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
	if s != nil && s.active() && g.byAccount[s.account] == s {
		delete(g.byAccount, s.account)
	}
	if s != nil && s.active() && g.byCharacter[s.character.ID] == s {
		delete(g.byCharacter, s.character.ID)
	}
	if s != nil && s.active() && g.bySession[s.sessionID] == s {
		delete(g.bySession, s.sessionID)
	}
	g.mu.Unlock()
	if s == nil {
		return
	}
	s.helloTimer.Stop()
	if s.active() {
		s.inst().Leave(s.character.ID)
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

// hello runs the handshake: version, token, character ownership, then the
// registry (kicking an older connection of the account), Welcome, and the
// join to the character's map instance.
func (g *Gateway) hello(s *session, h protocol.Hello) {
	ctx, cancel := context.WithTimeout(context.Background(), joinTimeout)
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
	account := model.AccountID(p.AccountID)
	res, err := g.deps.Characters.Load(ctx, &characterv1.LoadRequest{CharacterId: charID.String(), AccountId: account.String()})
	if err != nil {
		// Someone else's character, or none, is a bad Hello; a character
		// service that cannot answer is a server fault the client retries.
		if errmap.For(err).Code == characterNotFound {
			g.kick(s.conn, protocol.KickUnauthenticated)
			return
		}
		g.deps.Logger.ErrorContext(ctx, "character load failed", "character_id", charID, "err", err)
		s.conn.Close(wsconn.CloseInternal, "character")
		return
	}
	c := res.GetCharacter()
	class, err := model.ParseClass(c.GetClass())
	if err != nil {
		g.deps.Logger.ErrorContext(ctx, "character of an unknown class", "character_id", charID, "class", c.GetClass())
		s.conn.Close(wsconn.CloseInternal, "class")
		return
	}
	inst, err := g.deps.World.Instance(c.GetMapId())
	if err != nil {
		g.deps.Logger.ErrorContext(ctx, "character on an unknown map", "character_id", charID, "map_id", c.GetMapId())
		s.conn.Close(wsconn.CloseInternal, "map")
		return
	}
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

	g.mu.Lock()
	old := g.byAccount[account]
	sameSession := g.bySession[model.SessionID(p.SessionID)]
	g.byAccount[account] = s
	g.byCharacter[charID] = s
	g.bySession[model.SessionID(p.SessionID)] = s
	s.account = account
	s.sessionID = model.SessionID(p.SessionID)
	s.character = player{ID: charID, Name: c.GetName(), Level: int(c.GetLevel()), HP: int(c.GetHp()), MaxHP: int(c.GetMaxHp())}
	s.setInstance(inst)
	g.mu.Unlock()
	// An older connection of the account, or a second Hello on the same
	// session, loses: the newest connection wins.
	for _, prev := range []*session{old, sameSession} {
		if prev != nil && prev != s {
			g.kick(prev.conn, protocol.KickLoggedInElsewhere)
		}
	}
	s.helloTimer.Stop()

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
	g.send(s.conn, wsconn.Message{Data: welcome})
	st := c.GetStats()
	joining := world.Player{
		ID: charID, Account: account, Name: c.GetName(), Class: class, Level: int(c.GetLevel()), Exp: c.GetExp(),
		Stats: gamedata.Stats{STR: int(st.GetStrength()), DEX: int(st.GetDexterity()), INT: int(st.GetIntelligence()), AGI: int(st.GetAgility()), VIT: int(st.GetVitality())},
		HP:    int(c.GetHp()), MP: int(c.GetMp()), X: c.GetX(), Y: c.GetY(),
	}
	snk := &sink{conn: s.conn, obs: g.deps.Observer, logger: g.deps.Logger, onTeleport: func(mapID string) {
		if target, err := g.deps.World.Instance(mapID); err == nil {
			s.setInstance(target)
		}
	}}
	if err := inst.Join(ctx, joining, snk); err != nil {
		g.deps.Logger.ErrorContext(ctx, "join failed", "character_id", charID, "map_id", c.GetMapId(), "err", err)
		s.conn.Close(wsconn.CloseInternal, "join")
		return
	}
	s.setActive()
	g.online(ctx, s)
	g.deps.Observer.OnlineChanged(1)
	g.deps.Logger.InfoContext(ctx, "ws joined", "conn", s.conn.ID(), "character_id", charID, "map_id", c.GetMapId())
}

// online tells the social service a player is connected through this
// gateway, so party traffic finds it here. A failure is logged: parties are
// optional, and the next registration (a social restart) tries again.
func (g *Gateway) online(ctx context.Context, s *session) {
	if g.deps.Social == nil {
		return
	}
	c := s.character
	member := &socialv1.Member{Id: c.ID.String(), Name: c.Name, Level: int32(c.Level), Hp: int32(c.HP), MaxHp: int32(c.MaxHP), MapId: s.inst().MapID()}
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
		s.inst().Chat(s.character.ID, s.character.Name, res.GetText())
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
			ctx, cancel := context.WithTimeout(context.Background(), joinTimeout)
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

func characterView(c *characterv1.Character) protocol.CharacterView {
	st := c.GetStats()
	return protocol.CharacterView{
		ID: c.GetId(), Name: c.GetName(), Class: c.GetClass(), Level: int(c.GetLevel()), Exp: c.GetExp(),
		Stats: protocol.Stats{STR: int(st.GetStrength()), DEX: int(st.GetDexterity()), INT: int(st.GetIntelligence()), AGI: int(st.GetAgility()), VIT: int(st.GetVitality())},
		HP:    int(c.GetHp()), MaxHP: int(c.GetMaxHp()), MP: int(c.GetMp()), MaxMP: int(c.GetMaxMp()),
	}
}

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
