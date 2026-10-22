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

	"finga/internal/application/character"
	"finga/internal/application/gamedata"
	"finga/internal/application/item"
	"finga/internal/application/world"
	"finga/internal/application/world/sim"
	"finga/internal/model"
	"finga/internal/protocol"
	"finga/pkg/auth"
	"finga/pkg/errs"
	"finga/pkg/ratelimit"
	"finga/pkg/wsconn"
)

// CharacterSource is what the gateway needs from the character module.
type CharacterSource interface {
	Load(ctx context.Context, id model.CharacterID, account model.AccountID) (character.Character, error)
	Derived(c character.Character) (character.Derived, error)
}

// ItemSource is what the gateway needs from the item module: the inventory
// for Welcome, and the two inventory commands.
type ItemSource interface {
	Inventory(ctx context.Context, owner model.CharacterID) ([]item.Item, error)
	Move(ctx context.Context, actor item.Actor, in item.MoveInput) (item.Result, error)
	Discard(ctx context.Context, actor item.Actor, in item.DiscardInput) (item.Result, error)
}

// MapInstance is what the gateway needs from a map actor.
type MapInstance interface {
	Join(ctx context.Context, p world.Player, sink world.Sink) error
	Leave(id model.CharacterID) bool
	Input(id model.CharacterID, seq uint32, in sim.Input) bool
	Attack(id model.CharacterID) bool
	PickUp(id model.CharacterID) bool
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
	Items       ItemSource // nil: Welcome carries no items and inventory commands are unavailable
	World       World
	Logger      *slog.Logger
	Observer    Observer
	CORSOrigins []string
	Clock       func() time.Time
	// HelloTimeout is how long a connection has to send Hello; 5 s by default.
	HelloTimeout time.Duration
}

const (
	defaultHelloTimeout = 5 * time.Second
	joinTimeout         = 5 * time.Second
	commandTimeout      = 5 * time.Second
)

// Gateway serves /ws.
type Gateway struct {
	deps   Dependencies
	codec  *wsconn.JSONCodec
	limits map[string]limit
	now    func() time.Time

	mu         sync.Mutex
	byConn     map[string]*session
	byAccount  map[model.AccountID]*session
	helloGrace time.Duration
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
	return &Gateway{
		deps:       deps,
		codec:      protocol.NewCodec(),
		limits:     rateLimits(),
		now:        deps.Clock,
		byConn:     map[string]*session{},
		byAccount:  map[model.AccountID]*session{},
		helloGrace: deps.HelloTimeout,
	}
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
			g.send(c, wsconn.Message{ReqID: m.ReqID, Data: protocol.Response{OK: false, Code: string(errs.RateLimited), Message: "slow down"}})
		}
		return
	}
	switch d := m.Data.(type) {
	case protocol.Input:
		s.inst().Input(s.character.ID, m.Seq, sim.Input{Left: d.Left, Right: d.Right, Up: d.Up, Down: d.Down, Jump: d.Jump})
	case protocol.Attack:
		s.inst().Attack(s.character.ID)
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
	case protocol.Ping:
		g.send(c, wsconn.Message{Data: protocol.Pong{ClientTime: d.ClientTime, ServerTime: g.now().UnixMilli()}})
	case protocol.Hello:
		c.Close(wsconn.CloseKicked, "hello twice")
	default:
		// Portals, chat and party arrive in later steps.
		if m.ReqID != "" {
			g.send(c, wsconn.Message{ReqID: m.ReqID, Data: protocol.Response{OK: false, Code: string(errs.Unavailable), Message: "not available yet"}})
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
	g.mu.Unlock()
	if s == nil {
		return
	}
	s.helloTimer.Stop()
	if s.active() {
		s.inst().Leave(s.character.ID)
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
	g.byAccount[account] = s
	s.account = account
	s.sessionID = model.SessionID(p.SessionID)
	s.character = c
	s.setInstance(inst)
	g.mu.Unlock()
	if old != nil && old != s {
		g.kick(old.conn, protocol.KickLoggedInElsewhere)
	}
	s.helloTimer.Stop()

	g.send(s.conn, wsconn.Message{Data: protocol.Welcome{
		Character:  characterView(c, derived),
		Inventory:  inventory,
		MapID:      c.MapID,
		ServerTime: g.now().UnixMilli(),
	}})
	player := world.Player{ID: c.ID, Account: account, Name: c.Name, Class: c.Class, Level: c.Level, Exp: c.Exp, Stats: c.Stats, HP: c.HP, MP: c.MP, X: c.X, Y: c.Y}
	snk := &sink{conn: s.conn, obs: g.deps.Observer, onTeleport: func(mapID string) {
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
	g.deps.Observer.OnlineChanged(1)
	g.deps.Logger.InfoContext(ctx, "ws joined", "conn", s.conn.ID(), "character_id", c.ID, "map_id", c.MapID)
}

// inventoryCommand runs an inventory command on the connection's own
// goroutine (it blocks on the database, never a tick), answers with a
// Response, and sends the changed slots as InventoryChanged.
func (g *Gateway) inventoryCommand(s *session, reqID string, run func(ctx context.Context, actor item.Actor) (item.Result, error)) {
	if g.deps.Items == nil {
		g.send(s.conn, wsconn.Message{ReqID: reqID, Data: protocol.Response{OK: false, Code: string(errs.Unavailable), Message: "not available yet"}})
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	res, err := run(ctx, item.Actor{Account: s.account, Character: s.character.ID})
	if err != nil {
		g.send(s.conn, wsconn.Message{ReqID: reqID, Data: protocol.Response{OK: false, Code: string(errs.CodeOf(err)), Message: errs.MessageOf(err)}})
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

func characterView(c character.Character, d character.Derived) protocol.CharacterView {
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
