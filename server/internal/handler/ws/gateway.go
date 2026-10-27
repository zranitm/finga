// Package ws is the gateway (Low-level design, pages 3 and 4): WebSocket
// sessions, the Hello handshake, one connection per account, per-account
// rate limits, and routing of each message to the character's map
// instance with the identity taken from the connection, never from the
// message. It is the only package that turns the world's result types into
// protocol messages.
package ws

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"

	"finga/internal/application/account"
	"finga/internal/application/character"
	"finga/internal/application/chat"
	"finga/internal/application/item"
	"finga/internal/application/party"
	"finga/internal/application/world"
	"finga/internal/gamedata"
	"finga/internal/handler/errmap"
	"finga/internal/model"
	"finga/internal/progression"
	"finga/internal/protocol"
	"finga/internal/sim"
	"finga/pkg/auth"
	"finga/pkg/ratelimit"
	"finga/pkg/wsconn"
)

// CharacterSource is what the gateway needs from the character module.
type CharacterSource interface {
	Load(ctx context.Context, id model.CharacterID, account model.AccountID) (character.Character, error)
	Derived(c character.Character) (progression.Derived, error)
}

// ItemSource is what the gateway needs from the item module: the inventory
// for Welcome, and the two inventory commands.
type ItemSource interface {
	Inventory(ctx context.Context, owner model.CharacterID) ([]item.Item, error)
	Move(ctx context.Context, actor item.Actor, in item.MoveInput) (item.Result, error)
	Discard(ctx context.Context, actor item.Actor, in item.DiscardInput) (item.Result, error)
}

// ChatSource cleans and limits chat lines and names the party recipients.
type ChatSource interface {
	Send(from chat.Sender, channel chat.Channel, text string) (chat.Message, []model.CharacterID, error)
}

// PartySource is what the gateway needs from the party service.
type PartySource interface {
	Online(m party.Member)
	Offline(id model.CharacterID)
	Of(id model.CharacterID) (party.Party, bool)
	Invite(from, to model.CharacterID) (party.Invite, error)
	Respond(who model.CharacterID, inviteID string, accept bool) error
	Leave(who model.CharacterID) error
	Kick(leader, target model.CharacterID) error
	Promote(leader, target model.CharacterID) error
}

// MapInstance is what the gateway needs from a map actor.
type MapInstance interface {
	Join(ctx context.Context, p world.Player, sink world.Sink) error
	Leave(id model.CharacterID) bool
	Input(id model.CharacterID, seq uint32, in sim.Input) bool
	Attack(id model.CharacterID) bool
	PickUp(id model.CharacterID) bool
	EnterPortal(id model.CharacterID) bool
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

// Dependencies are the services and settings the gateway is built from.
type Dependencies struct {
	Sessions    auth.Sessions
	Characters  CharacterSource
	Items       ItemSource  // nil: Welcome carries no items and inventory commands are unavailable
	Chat        ChatSource  // nil: ChatSend is unavailable
	Parties     PartySource // nil: party commands are unavailable; the gateway is its Notifier
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

// Run checks every connection's login session on an interval until ctx is
// done, kicking the ones whose session ended (expired, logged out, reused
// token) with session_expired. It never returns an error.
func (g *Gateway) Run(ctx context.Context) error {
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

// KickSession closes every connection on a login session; the account
// service calls it when a session ends by reuse or logout.
func (g *Gateway) KickSession(sessionID model.SessionID) {
	g.mu.Lock()
	s := g.bySession[sessionID]
	g.mu.Unlock()
	if s != nil {
		g.kick(s.conn, protocol.KickSessionExpired)
	}
}

// KickAccount closes every connection of an account: a logout everywhere,
// or a new login elsewhere (account.ReasonNewLogin), which the player sees
// as logged_in_elsewhere; anything else is session_expired.
func (g *Gateway) KickAccount(accountID model.AccountID, reason string) {
	g.mu.Lock()
	s := g.byAccount[accountID]
	g.mu.Unlock()
	if s == nil {
		return
	}
	why := protocol.KickSessionExpired
	if reason == account.ReasonNewLogin {
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
		g.inventoryCommand(s, m.ReqID, func(ctx context.Context, actor item.Actor) (item.Result, error) {
			id, err := model.ParseItemID(d.ItemID)
			if err != nil {
				return item.Result{}, err
			}
			return g.deps.Items.Move(ctx, actor, item.MoveInput{Key: d.IdempotencyKey, ItemID: id, Tab: gamedata.Tab(d.Tab), Slot: d.Slot})
		})
	case protocol.InventoryDiscard:
		g.inventoryCommand(s, m.ReqID, func(ctx context.Context, actor item.Actor) (item.Result, error) {
			id, err := model.ParseItemID(d.ItemID)
			if err != nil {
				return item.Result{}, err
			}
			return g.deps.Items.Discard(ctx, actor, item.DiscardInput{Key: d.IdempotencyKey, ItemID: id, Quantity: d.Quantity})
		})
	case protocol.ChatSend:
		g.chat(s, d)
	case protocol.PartyInvite:
		g.partyCommand(s, m.ReqID, func() error {
			to, err := model.ParseCharacterID(d.CharacterID)
			if err != nil {
				return err
			}
			_, err = g.deps.Parties.Invite(s.character.ID, to)
			return err
		})
	case protocol.PartyRespond:
		g.partyCommand(s, m.ReqID, func() error { return g.deps.Parties.Respond(s.character.ID, d.InviteID, d.Accept) })
	case protocol.PartyLeave:
		g.partyCommand(s, m.ReqID, func() error { return g.deps.Parties.Leave(s.character.ID) })
	case protocol.PartyKick:
		g.partyCommand(s, m.ReqID, func() error {
			target, err := model.ParseCharacterID(d.CharacterID)
			if err != nil {
				return err
			}
			return g.deps.Parties.Kick(s.character.ID, target)
		})
	case protocol.PartyPromote:
		g.partyCommand(s, m.ReqID, func() error {
			target, err := model.ParseCharacterID(d.CharacterID)
			if err != nil {
				return err
			}
			return g.deps.Parties.Promote(s.character.ID, target)
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
		if g.deps.Parties != nil {
			g.deps.Parties.Offline(s.character.ID)
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
		if errors.Is(err, account.ErrUnauthenticated) {
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
	c, err := g.deps.Characters.Load(ctx, charID, account)
	if err != nil {
		g.kick(s.conn, protocol.KickUnauthenticated)
		return
	}
	derived, err := g.deps.Characters.Derived(c)
	if err != nil {
		g.deps.Logger.ErrorContext(ctx, "derived stats failed", "character_id", c.ID, "err", err)
		s.conn.Close(wsconn.CloseInternal, "stats")
		return
	}
	inst, err := g.deps.World.Instance(c.MapID)
	if err != nil {
		g.deps.Logger.ErrorContext(ctx, "character on an unknown map", "character_id", c.ID, "map_id", c.MapID)
		s.conn.Close(wsconn.CloseInternal, "map")
		return
	}
	inventory := []protocol.ItemView{}
	if g.deps.Items != nil {
		items, err := g.deps.Items.Inventory(ctx, c.ID)
		if err != nil {
			g.deps.Logger.ErrorContext(ctx, "inventory failed", "character_id", c.ID, "err", err)
			s.conn.Close(wsconn.CloseInternal, "inventory")
			return
		}
		inventory = itemViews(items)
	}

	g.mu.Lock()
	old := g.byAccount[account]
	sameSession := g.bySession[model.SessionID(p.SessionID)]
	g.byAccount[account] = s
	g.byCharacter[c.ID] = s
	g.bySession[model.SessionID(p.SessionID)] = s
	s.account = account
	s.sessionID = model.SessionID(p.SessionID)
	s.character = c
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
		Character:  characterView(c, derived),
		Inventory:  inventory,
		MapID:      c.MapID,
		ServerTime: g.now().UnixMilli(),
	}
	if g.deps.Parties != nil {
		if p, ok := g.deps.Parties.Of(c.ID); ok {
			welcome.Party = partyView(&p)
		}
	}
	g.send(s.conn, wsconn.Message{Data: welcome})
	player := world.Player{ID: c.ID, Account: account, Name: c.Name, Class: c.Class, Level: c.Level, Exp: c.Exp, Stats: c.Stats, HP: c.HP, MP: c.MP, X: c.X, Y: c.Y}
	snk := &sink{conn: s.conn, obs: g.deps.Observer, logger: g.deps.Logger, onTeleport: func(mapID string) {
		if target, err := g.deps.World.Instance(mapID); err == nil {
			s.setInstance(target)
		}
	}}
	if err := inst.Join(ctx, player, snk); err != nil {
		g.deps.Logger.ErrorContext(ctx, "join failed", "character_id", c.ID, "map_id", c.MapID, "err", err)
		s.conn.Close(wsconn.CloseInternal, "join")
		return
	}
	s.setActive()
	if g.deps.Parties != nil {
		g.deps.Parties.Online(party.Member{ID: c.ID, Name: c.Name, Level: c.Level, HP: c.HP, MaxHP: derived.MaxHP, MapID: c.MapID})
	}
	g.deps.Observer.OnlineChanged(1)
	g.deps.Logger.InfoContext(ctx, "ws joined", "conn", s.conn.ID(), "character_id", c.ID, "map_id", c.MapID)
}

// chat cleans a line through the chat service, then hands a map line to
// the sender's map instance (everyone there hears it, the sender too) or
// delivers a party line to each member's connection, on any map. A
// refused line answers a Response (ChatSend carries no request ID).
func (g *Gateway) chat(s *session, d protocol.ChatSend) {
	if g.deps.Chat == nil {
		g.send(s.conn, wsconn.Message{Data: refusal(errmap.ChatUnavailable)})
		return
	}
	msg, to, err := g.deps.Chat.Send(chat.Sender{ID: s.character.ID, Name: s.character.Name}, chat.Channel(d.Channel), d.Text)
	if err != nil {
		g.send(s.conn, wsconn.Message{Data: g.refuse(s, "ChatSend", err)})
		return
	}
	if msg.Channel == chat.Map {
		s.inst().Chat(msg.Sender, msg.SenderName, msg.Text)
		return
	}
	line := protocol.ChatMessage{Channel: protocol.ChatParty, Sender: msg.Sender.String(), SenderName: msg.SenderName, Text: msg.Text}
	for _, id := range to {
		if t := g.sessionOf(id); t != nil {
			g.send(t.conn, wsconn.Message{Data: line})
		}
	}
}

// partyCommand runs a party command with the connection's identity and
// answers a Response; the party service pushes PartyState itself.
func (g *Gateway) partyCommand(s *session, reqID string, run func() error) {
	if g.deps.Parties == nil {
		g.send(s.conn, wsconn.Message{ReqID: reqID, Data: refusal(errmap.PartiesUnavailable)})
		return
	}
	if err := run(); err != nil {
		g.send(s.conn, wsconn.Message{ReqID: reqID, Data: g.refuse(s, "party", err)})
		return
	}
	g.send(s.conn, wsconn.Message{ReqID: reqID, Data: protocol.Response{OK: true}})
}

// PartyChanged implements party.Notifier: every listed member who is
// connected gets the new PartyState (party null when they have none).
func (g *Gateway) PartyChanged(members []model.CharacterID, p *party.Party) {
	state := protocol.PartyState{Party: partyView(p)}
	for _, id := range members {
		if t := g.sessionOf(id); t != nil {
			g.send(t.conn, wsconn.Message{Data: state})
		}
	}
}

// Invited implements party.Notifier: the recipient gets PartyInvited.
func (g *Gateway) Invited(to model.CharacterID, inv party.Invite) {
	if t := g.sessionOf(to); t != nil {
		g.send(t.conn, wsconn.Message{Data: protocol.PartyInvited{InviteID: inv.ID, From: inv.From.String(), FromName: inv.FromName}})
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

func partyView(p *party.Party) *protocol.PartyView {
	if p == nil {
		return nil
	}
	v := &protocol.PartyView{Leader: p.Leader.String(), Members: make([]protocol.PartyMember, 0, len(p.Members))}
	for _, m := range p.Members {
		v.Members = append(v.Members, protocol.PartyMember{CharacterID: m.ID.String(), Name: m.Name, Level: m.Level, HP: m.HP, MaxHP: m.MaxHP, MapID: m.MapID, Online: m.Online})
	}
	return v
}

// inventoryCommand runs an inventory command on the connection's own
// goroutine (it blocks on the database, never a tick), answers with a
// Response, and sends the changed slots as InventoryChanged.
func (g *Gateway) inventoryCommand(s *session, reqID string, run func(ctx context.Context, actor item.Actor) (item.Result, error)) {
	if g.deps.Items == nil {
		g.send(s.conn, wsconn.Message{ReqID: reqID, Data: refusal(errmap.InventoryUnavailable)})
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	res, err := run(ctx, item.Actor{Account: s.account, Character: s.character.ID})
	if err != nil {
		g.send(s.conn, wsconn.Message{ReqID: reqID, Data: g.refuse(s, "inventory", err)})
		return
	}
	g.send(s.conn, wsconn.Message{ReqID: reqID, Data: protocol.Response{OK: true}})
	if len(res.Changed) > 0 {
		g.send(s.conn, wsconn.Message{Data: protocol.InventoryChanged{Slots: inventorySlots(res.Changed)}})
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

func characterView(c character.Character, d progression.Derived) protocol.CharacterView {
	return protocol.CharacterView{
		ID: c.ID.String(), Name: c.Name, Class: c.Class.String(), Level: c.Level, Exp: c.Exp,
		Stats: protocol.Stats{STR: c.Stats.STR, DEX: c.Stats.DEX, INT: c.Stats.INT, AGI: c.Stats.AGI, VIT: c.Stats.VIT},
		HP:    c.HP, MaxHP: d.MaxHP, MP: c.MP, MaxMP: d.MaxMP,
	}
}

func itemViews(items []item.Item) []protocol.ItemView {
	out := make([]protocol.ItemView, 0, len(items))
	for _, it := range items {
		out = append(out, itemView(it))
	}
	return out
}

func itemView(it item.Item) protocol.ItemView {
	return protocol.ItemView{ItemID: it.ID.String(), Template: it.TemplateID, Tab: string(it.Tab), Slot: it.Slot, Quantity: it.Quantity}
}

func inventorySlots(slots []item.Slot) []protocol.InventorySlot {
	out := make([]protocol.InventorySlot, 0, len(slots))
	for _, s := range slots {
		slot := protocol.InventorySlot{Tab: string(s.Tab), Slot: s.Slot}
		if s.Item != nil {
			v := itemView(*s.Item)
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
