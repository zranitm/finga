package ws_test

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"finga/internal/facade"
	"finga/internal/gamedata"
	"finga/internal/kernel"
	"finga/internal/sim"
	"finga/modules/account"
	"finga/modules/gateway/internal/handler/ws/receive"
	"finga/modules/gateway/internal/local"
	"finga/modules/item"
	"finga/modules/social"
	"finga/modules/world"
	"finga/pkg/hub"
	"finga/pkg/observability/tracing"
	"finga/pkg/transient"
	"finga/pkg/uuidv7"
)

// traces samples as the gateway does, and spans records every span it samples.
var traces, spans = gatewayTraces()

func gatewayTraces() (*tracing.Provider, *tracetest.SpanRecorder) {
	tp, err := tracing.New(context.Background(), tracing.Config{}, tracing.Options{Service: "gateway", Root: receive.EntryPoint})
	if err != nil {
		panic(err)
	}
	rec := tracetest.NewSpanRecorder()
	tp.RegisterSpanProcessor(rec)
	return tp, rec
}

// worldCalls and itemCalls trace a fake's calls as the module's facade does, so a kept command's trace holds them.
var worldCalls, itemCalls = moduleCalls("world"), moduleCalls("item")

func moduleCalls(module string) *facade.Calls {
	calls, err := facade.New(facade.Options{Module: module, Traces: traces, Registry: prometheus.NewRegistry()})
	if err != nil {
		panic(err)
	}
	return calls
}

// refusal is a module's failure under rule, the kernel's error over the module's own, as its facade wraps it.
func refusal(rule error) error { return fmt.Errorf("%w: refused", rule) }

// errDown is a module's failure while what it depends on is out of reach.
var errDown = fmt.Errorf("%w: connection refused", transient.ErrUnavailable)

func traceID(ctx context.Context) string { return trace.SpanContextFromContext(ctx).TraceID().String() }

// copied is a copy of what a fake recorded, read under its lock.
func copied[T any](mu *sync.Mutex, s *[]T) []T {
	mu.Lock()
	defer mu.Unlock()
	return slices.Clone(*s)
}

// stream is a module's stream to the gateway, on a hub the fake owns as the module does.
type stream[T any] struct {
	hub        *hub.Hub[T]
	subscribed chan struct{} // receives once per subscription, while it has room
	mu         sync.Mutex
	gateways   []string // each subscription's gateway ID, in order
}

func newStream[T any]() *stream[T] {
	return &stream[T]{hub: hub.New[T](1024), subscribed: make(chan struct{}, 8)}
}

// subscribe opens the gateway's subscription, closing an older one, as the module's Subscribe does.
func (s *stream[T]) subscribe(gateway string) *hub.Sub[T] {
	sub := s.hub.Subscribe(gateway)
	s.mu.Lock()
	s.gateways = append(s.gateways, gateway)
	s.mu.Unlock()
	select {
	case s.subscribed <- struct{}{}:
	default:
	}
	return sub
}

func (s *stream[T]) subscriptions() []string { return copied(&s.mu, &s.gateways) }

// open reports whether the gateway holds a subscription.
func (s *stream[T]) open(gateway string) bool {
	_, ok := s.hub.Get(gateway)
	return ok
}

// send queues v on the gateway's subscription; with none open, it is lost, as the module's would be.
func (s *stream[T]) send(v T) { s.hub.Broadcast(v) }

// cut closes the gateway's subscription, as a full queue does: the adapter subscribes again and hands over its Subscribed.
func (s *stream[T]) cut(t testing.TB) {
	t.Helper()
	sub, ok := s.hub.Get(gatewayID)
	if !ok {
		t.Fatal("cut a stream the gateway holds no subscription to")
	}
	sub.Close()
}

// fakeAccount is the account module: the tokens it knows, the sessions it ended, and its stream of session ends.
type fakeAccount struct {
	local.AccountModule // the lobby's calls, which no connection makes
	ends                *stream[account.Event]
	mu                  sync.Mutex
	tokens              map[string]account.Principal
	rotations           int
	expired             map[kernel.SessionID]bool
	rotateErr           error // every Rotate fails with it
}

func newFakeAccount() *fakeAccount {
	return &fakeAccount{ends: newStream[account.Event](), expired: map[kernel.SessionID]bool{}, tokens: map[string]account.Principal{
		tokenAlice: {AccountID: alice, SessionID: sessAlice},
		tokenBob:   {AccountID: bob, SessionID: sessBob},
	}}
}

func (f *fakeAccount) Validate(_ context.Context, token string) (account.Principal, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p, ok := f.tokens[token]
	if !ok {
		return account.Principal{}, refusal(kernel.ErrUnauthenticated)
	}
	return p, nil
}

// Rotate hands out a fresh token per call; Check consults the expired set.
func (f *fakeAccount) Rotate(_ context.Context, sessionID kernel.SessionID) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rotations++
	if f.rotateErr != nil {
		return "", f.rotateErr
	}
	return "rotated-" + sessionID.String()[:8] + "-" + fmt.Sprint(f.rotations), nil
}

func (f *fakeAccount) Check(_ context.Context, sessionID kernel.SessionID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.expired[sessionID] {
		return refusal(kernel.ErrUnauthenticated)
	}
	return nil
}

func (f *fakeAccount) expire(sessionID kernel.SessionID) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.expired[sessionID] = true
}

func (f *fakeAccount) Subscribe(gateway string) *hub.Sub[account.Event] {
	return f.ends.subscribe(gateway)
}

// fakeWorld is the world module; Join sends what came before and a Snapshot on the stream before answering.
type fakeWorld struct {
	deliveries *stream[world.Delivery]
	mu         sync.Mutex
	chars      map[kernel.CharacterID]fakeCharacter
	conns      map[kernel.CharacterID]string // the connection each character last joined through
	joinErr    error
	leaveFails int              // the next Leave calls fail as a map too busy to take them
	leaveGate  chan struct{}    // when set, Leave answers only once it closes
	before     []world.Delivery // sent ahead of each Snapshot, as an older connection's
	joined     []joinCall
	left       []world.Presence
	leaveIDs   []string // each Leave's trace ID
	inputs     []inputCall
	attacks    []attackCall
	portals    []world.Presence
	says       []sayCall
	equips     []equipCall
	unequips   []unequipCall
	equipErr   error // Equip and Unequip fail with it
	uses       []useCall
	useErr     error // UseItem fails with it
	spends     []spendCall
	resets     []resetCall
	invests    []investCall
	casts      []castCall
	castErr    error // UseSkill fails with it
	promotes   []promoteCall
	promoteErr error                  // Promote fails with it
	pointsErr  error                  // SpendPoints, ResetPoints and SpendSkillPoints fail with it
	quests     map[string]world.Quest // the log Join reports
	timers     []skillTimer           // the skill timers Join reports
	questCalls []questCall            // Talk, AcceptQuest, AbandonQuest and HandInQuest
	questErr   error                  // the four fail with it
	calls      []string               // commands in arrival order
	traceIDs   []string               // each Join's and Attack's, in arrival order
	hold       chan struct{}          // set: the first Subscribe returns only during the first join
	joinGate   chan struct{}          // set: a join reports on joining, then waits for the gate to close
	joining    chan struct{}
	block      chan struct{} // when set, Attack waits for it (or its context)
	blocked    chan error    // Attack reports here that it is waiting, then why it stopped
	tick       uint64
	tickRate   int          // what Join reports; 30 when unset
	leaveErr   error        // every Leave fails with it, as a module refusing for good
	ops        atomic.Int64 // every call the gateway made but Subscribe, which it may make at any time
}

type fakeCharacter struct {
	account kernel.AccountID
	view    *world.View
}

// The world's calls as the fake recorded them.
type (
	joinCall struct {
		gateway string
		account kernel.AccountID
		p       world.Presence
	}
	inputCall struct {
		p    world.Presence
		seq  uint32
		keys sim.Input
		view uint32
	}
	attackCall struct {
		p   world.Presence
		seq uint32
	}
	sayCall struct {
		p          world.Presence
		name, text string
	}
	equipCall struct {
		p         world.Presence
		key       string
		item      kernel.ItemID
		equipSlot string
	}
	unequipCall struct {
		p              world.Presence
		key, equipSlot string
		toSlot         *int
	}
	useCall struct {
		p        world.Presence
		key      string
		item     kernel.ItemID
		template string
	}
	spendCall struct {
		p        world.Presence
		expected int
		points   gamedata.Stats
	}
	resetCall struct {
		p        world.Presence
		expected int
	}
	investCall struct {
		p        world.Presence
		expected int
		points   map[string]int
	}
	castCall struct {
		p     world.Presence
		skill string
		seq   uint32
	}
	promoteCall struct {
		p     world.Presence
		npc   string
		class kernel.Class
	}
	questCall struct {
		method     string
		p          world.Presence
		npc, quest string
	}
)

// skillTimer is a cooldown or a buff as the character's checkpoint holds it, left from the join on.
type skillTimer struct {
	skill  string
	buff   bool // else a cooldown
	left   time.Duration
	points int
}

func newFakeWorld() *fakeWorld {
	return &fakeWorld{
		deliveries: newStream[world.Delivery](),
		chars: map[kernel.CharacterID]fakeCharacter{
			aylaID:  {alice, &world.View{ID: aylaID, Name: "Ayla", Class: "warrior", Level: 1, HP: 204, MaxHP: 204, MapID: "town", X: 416, Y: 608, Stats: gamedata.Stats{STR: 12}}},
			robinID: {alice, &world.View{ID: robinID, Name: "Robin", Class: "bowman", Level: 1, HP: 105, MaxHP: 105, MapID: "town", X: 400, Y: 608}},
			brunoID: {bob, &world.View{ID: brunoID, Name: "Bruno", Class: "warrior", Level: 1, HP: 204, MaxHP: 204, MapID: "town", X: 500, Y: 608}},
		},
		conns: map[kernel.CharacterID]string{},
	}
}

func (f *fakeWorld) Subscribe(gateway string) *hub.Sub[world.Delivery] {
	f.mu.Lock()
	hold := f.hold
	f.mu.Unlock()
	sub := f.deliveries.subscribe(gateway)
	if hold != nil {
		<-hold // joins work already; the adapter has not handed over WorldSubscribed yet
	}
	return sub
}

func (f *fakeWorld) Join(ctx context.Context, gateway string, acc kernel.AccountID, p world.Presence) (_ world.View, err error) {
	f.ops.Add(1)
	ctx, call := worldCalls.Start(ctx, "Join")
	defer call.End(&err)
	f.mu.Lock()
	f.traceIDs = append(f.traceIDs, traceID(ctx))
	hold := f.hold
	f.hold = nil
	f.mu.Unlock()
	if hold != nil {
		// WorldSubscribed goes out now, handed over while this join is still in flight.
		close(hold)
		time.Sleep(100 * time.Millisecond)
	}
	f.mu.Lock()
	gate, joining := f.joinGate, f.joining
	f.mu.Unlock()
	if gate != nil {
		joining <- struct{}{}
		select {
		case <-gate:
		case <-ctx.Done():
			go func() {
				<-gate
				_, _ = f.land(gateway, acc, p) // the map still applies the join it took; only the reply is lost
			}()
			return world.View{}, ctx.Err()
		}
	}
	return f.land(gateway, acc, p)
}

// land joins the character as its map applies the join.
func (f *fakeWorld) land(gateway string, acc kernel.AccountID, p world.Presence) (world.View, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.joinErr != nil {
		return world.View{}, f.joinErr
	}
	if !f.deliveries.open(gateway) {
		return world.View{}, kernel.ErrNotSubscribed
	}
	c, ok := f.chars[p.Character]
	if !ok || c.account != acc {
		return world.View{}, refusal(kernel.ErrCharacterNotFound)
	}
	f.joined = append(f.joined, joinCall{gateway: gateway, account: acc, p: p})
	f.conns[p.Character] = p.Conn
	for _, d := range f.before {
		d.Character, d.Conn = p.Character, "conn-older"
		if fr, ok := d.Event.(world.Frame); ok {
			to := make([]world.Recipient, 0, len(fr.Recipients))
			for _, r := range fr.Recipients {
				r.Character, r.Connection = p.Character, "conn-older"
				to = append(to, r)
			}
			fr.Recipients = to
			d.Event = fr
		}
		f.deliveries.send(d)
	}
	f.tick++
	v := *c.view
	f.deliveries.send(world.Delivery{Character: p.Character, Conn: p.Conn, Tick: f.tick, Event: world.Snapshot{
		MapID: v.MapID, Players: []world.PlayerView{{ID: v.ID, Name: v.Name, Class: v.Class, Level: v.Level, HP: v.HP, MaxHP: v.MaxHP, State: sim.State{X: v.X, Y: v.Y, Facing: 1}}},
	}})
	v.TickRateHz = cmp.Or(f.tickRate, 30)
	v.JoinSeq = uint64(len(f.joined))
	v.Quests = maps.Clone(f.quests)
	v.Timers = world.SkillTimers{Cooldowns: map[string]time.Time{}, Buffs: map[string]world.BuffTimer{}}
	for _, s := range f.timers {
		if s.buff {
			v.Timers.Buffs[s.skill] = world.BuffTimer{Until: time.Now().Add(s.left), Points: s.points}
		} else {
			v.Timers.Cooldowns[s.skill] = time.Now().Add(s.left)
		}
	}
	return v, nil
}

// save is the character's checkpoint, which the next join loads.
func (f *fakeWorld) save(id kernel.CharacterID, mapID string, x, y float64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c := f.chars[id]
	v := *c.view
	v.MapID, v.X, v.Y = mapID, x, y
	c.view = &v
	f.chars[id] = c
}

func (f *fakeWorld) joins() []joinCall            { return copied(&f.mu, &f.joined) }
func (f *fakeWorld) leaves() []world.Presence     { return copied(&f.mu, &f.left) }
func (f *fakeWorld) leaveTraceIDs() []string      { return copied(&f.mu, &f.leaveIDs) }
func (f *fakeWorld) inputList() []inputCall       { return copied(&f.mu, &f.inputs) }
func (f *fakeWorld) attackList() []attackCall     { return copied(&f.mu, &f.attacks) }
func (f *fakeWorld) portalList() []world.Presence { return copied(&f.mu, &f.portals) }
func (f *fakeWorld) sayList() []sayCall           { return copied(&f.mu, &f.says) }
func (f *fakeWorld) traceIDList() []string        { return copied(&f.mu, &f.traceIDs) }
func (f *fakeWorld) callList() []string           { return copied(&f.mu, &f.calls) }
func (f *fakeWorld) useCalls() []useCall          { return copied(&f.mu, &f.uses) }
func (f *fakeWorld) castCalls() []castCall        { return copied(&f.mu, &f.casts) }
func (f *fakeWorld) promoteCalls() []promoteCall  { return copied(&f.mu, &f.promotes) }
func (f *fakeWorld) skillPointsCalls() []investCall {
	return copied(&f.mu, &f.invests)
}
func (f *fakeWorld) questList() []questCall { return copied(&f.mu, &f.questCalls) }

func (f *fakeWorld) inputCount() int { return len(f.inputList()) }

func (f *fakeWorld) pointsCalls() ([]spendCall, []resetCall) {
	return copied(&f.mu, &f.spends), copied(&f.mu, &f.resets)
}

func (f *fakeWorld) equipCalls() ([]equipCall, []unequipCall) {
	return copied(&f.mu, &f.equips), copied(&f.mu, &f.unequips)
}

func (f *fakeWorld) Leave(ctx context.Context, p world.Presence) error {
	f.ops.Add(1)
	f.mu.Lock()
	f.left = append(f.left, p)
	f.leaveIDs = append(f.leaveIDs, traceID(ctx))
	if f.leaveFails > 0 {
		f.leaveFails--
		f.mu.Unlock()
		return refusal(kernel.ErrMapBusy)
	}
	if f.leaveErr != nil {
		err := f.leaveErr
		f.mu.Unlock()
		return err
	}
	gate := f.leaveGate
	f.mu.Unlock()
	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

func (f *fakeWorld) Input(_ context.Context, p world.Presence, seq uint32, in sim.Input, view uint32) error {
	f.ops.Add(1)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.inputs = append(f.inputs, inputCall{p: p, seq: seq, keys: in, view: view})
	f.calls = append(f.calls, fmt.Sprintf("input %d", seq))
	return nil
}

func (f *fakeWorld) Attack(ctx context.Context, p world.Presence, seq uint32) (err error) {
	f.ops.Add(1)
	ctx, call := worldCalls.Start(ctx, "Attack")
	defer call.End(&err)
	f.mu.Lock()
	f.traceIDs = append(f.traceIDs, traceID(ctx))
	f.attacks = append(f.attacks, attackCall{p: p, seq: seq})
	f.calls = append(f.calls, "attack")
	block, blocked := f.block, f.blocked
	f.mu.Unlock()
	if block != nil {
		blocked <- nil
		select {
		case <-block:
			blocked <- nil
		case <-ctx.Done():
			blocked <- ctx.Err()
			return ctx.Err()
		}
	}
	return nil
}

func (f *fakeWorld) EnterPortal(_ context.Context, p world.Presence) error {
	f.ops.Add(1)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.portals = append(f.portals, p)
	f.calls = append(f.calls, "portal")
	return nil
}

func (f *fakeWorld) Say(_ context.Context, p world.Presence, name, text string) error {
	f.ops.Add(1)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.says = append(f.says, sayCall{p: p, name: name, text: text})
	return nil
}

func (f *fakeWorld) Equip(_ context.Context, p world.Presence, key string, it kernel.ItemID, equipSlot string) error {
	f.ops.Add(1)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.equips = append(f.equips, equipCall{p: p, key: key, item: it, equipSlot: equipSlot})
	return f.equipErr
}

func (f *fakeWorld) Unequip(_ context.Context, p world.Presence, key, equipSlot string, toSlot *int) error {
	f.ops.Add(1)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.unequips = append(f.unequips, unequipCall{p: p, key: key, equipSlot: equipSlot, toSlot: toSlot})
	return f.equipErr
}

func (f *fakeWorld) UseItem(_ context.Context, p world.Presence, key string, it kernel.ItemID, template string) error {
	f.ops.Add(1)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.uses = append(f.uses, useCall{p: p, key: key, item: it, template: template})
	return f.useErr
}

func (f *fakeWorld) SpendPoints(_ context.Context, p world.Presence, expected int, points gamedata.Stats) error {
	f.ops.Add(1)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.spends = append(f.spends, spendCall{p: p, expected: expected, points: points})
	return f.pointsErr
}

func (f *fakeWorld) ResetPoints(_ context.Context, p world.Presence, expected int) error {
	f.ops.Add(1)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.resets = append(f.resets, resetCall{p: p, expected: expected})
	return f.pointsErr
}

func (f *fakeWorld) SpendSkillPoints(_ context.Context, p world.Presence, expected int, points map[string]int) error {
	f.ops.Add(1)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.invests = append(f.invests, investCall{p: p, expected: expected, points: points})
	return f.pointsErr
}

func (f *fakeWorld) UseSkill(_ context.Context, p world.Presence, skill string, seq uint32) error {
	f.ops.Add(1)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.casts = append(f.casts, castCall{p: p, skill: skill, seq: seq})
	return f.castErr
}

func (f *fakeWorld) Promote(_ context.Context, p world.Presence, npc string, class kernel.Class) error {
	f.ops.Add(1)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.promotes = append(f.promotes, promoteCall{p: p, npc: npc, class: class})
	return f.promoteErr
}

func (f *fakeWorld) Talk(_ context.Context, p world.Presence, npc string) error {
	f.ops.Add(1)
	return f.questCall(questCall{method: "Talk", p: p, npc: npc})
}

func (f *fakeWorld) AcceptQuest(_ context.Context, p world.Presence, npc, quest string) error {
	f.ops.Add(1)
	return f.questCall(questCall{method: "AcceptQuest", p: p, npc: npc, quest: quest})
}

func (f *fakeWorld) AbandonQuest(_ context.Context, p world.Presence, quest string) error {
	f.ops.Add(1)
	return f.questCall(questCall{method: "AbandonQuest", p: p, quest: quest})
}

func (f *fakeWorld) HandInQuest(_ context.Context, p world.Presence, npc, quest string) error {
	f.ops.Add(1)
	return f.questCall(questCall{method: "HandInQuest", p: p, npc: npc, quest: quest})
}

func (f *fakeWorld) questCall(c questCall) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.questCalls = append(f.questCalls, c)
	return f.questErr
}

// fail sets the error a group of commands fails with, nil for none.
func (f *fakeWorld) fail(to *error, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	*to = err
}

func (f *fakeWorld) failEquips(err error)   { f.fail(&f.equipErr, err) }
func (f *fakeWorld) failUses(err error)     { f.fail(&f.useErr, err) }
func (f *fakeWorld) failPoints(err error)   { f.fail(&f.pointsErr, err) }
func (f *fakeWorld) failCasts(err error)    { f.fail(&f.castErr, err) }
func (f *fakeWorld) failPromotes(err error) { f.fail(&f.promoteErr, err) }
func (f *fakeWorld) failQuests(err error)   { f.fail(&f.questErr, err) }

// delta sends the character a tick's Frame through its connection, with ack, the latest input applied, and the energy it holds when one is given.
func (f *fakeWorld) delta(to kernel.CharacterID, tick uint64, ack uint32, fr world.Frame, energy ...int) {
	f.mu.Lock()
	r := world.Recipient{Character: to, Connection: f.conns[to], AckSeq: ack}
	f.mu.Unlock()
	for _, e := range energy {
		r.Energy = &e
	}
	fr.Recipients = []world.Recipient{r}
	f.deliveries.send(world.Delivery{Tick: tick, Event: fr})
}

// frameTo is an empty Frame whose one recipient is the character through conn, with ack as its AckSeq.
func frameTo(to kernel.CharacterID, conn string, tick uint64, ack uint32) world.Delivery {
	return world.Delivery{Tick: tick, Event: world.Frame{Recipients: []world.Recipient{{Character: to, Connection: conn, AckSeq: ack}}}}
}

// push sends the character an event through its connection.
func (f *fakeWorld) push(to kernel.CharacterID, tick uint64, e world.Event) {
	f.mu.Lock()
	conn := f.conns[to]
	f.mu.Unlock()
	f.deliveries.send(world.Delivery{Character: to, Conn: conn, Tick: tick, Event: e})
}

type fakeObserver struct {
	mu       sync.Mutex
	in       map[string]int
	out      map[string]int
	online   int
	changes  int // OnlineChanged calls
	slow     int
	waiting  int      // logins waiting to register with social
	welcomes []string // each Welcome's kind
}

func newFakeObserver() *fakeObserver {
	return &fakeObserver{in: map[string]int{}, out: map[string]int{}}
}

func (o *fakeObserver) MessageIn(t string)  { o.mu.Lock(); o.in[t]++; o.mu.Unlock() }
func (o *fakeObserver) MessageOut(t string) { o.mu.Lock(); o.out[t]++; o.mu.Unlock() }
func (o *fakeObserver) SlowConsumer()       { o.mu.Lock(); o.slow++; o.mu.Unlock() }
func (o *fakeObserver) WelcomeSent(kind string, bytes int) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if bytes > 0 {
		o.welcomes = append(o.welcomes, kind)
	}
}

// welcomed is each Welcome's kind so far.
func (o *fakeObserver) welcomed() []string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return slices.Clone(o.welcomes)
}
func (o *fakeObserver) OnlineChanged(d int) {
	o.mu.Lock()
	o.online += d
	o.changes++
	o.mu.Unlock()
}

func (o *fakeObserver) RegistrationsWaiting(d int) { o.mu.Lock(); o.waiting += d; o.mu.Unlock() }

func (o *fakeObserver) registering() int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.waiting
}

// onlineAfter lets every goroutine run until it blocks, expects n online count changes, and returns the count.
func (o *fakeObserver) onlineAfter(t *testing.T, n int) int {
	t.Helper()
	synctest.Wait()
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.changes != n {
		t.Fatalf("%d changes of the online count, want %d", o.changes, n)
	}
	return o.online
}

// fakeItems is the item module, recording inventory commands.
type fakeItems struct {
	mu          sync.Mutex
	inventory   []item.Item
	moves       []itemCall[item.MoveRequest]
	discards    []itemCall[item.DiscardRequest]
	discardMany []itemCall[item.DiscardManyRequest]
	changed     []item.Slot
	revision    uint64 // every answer's
	err         error
	gate        chan struct{} // when set, Inventory waits for it to close
	ops         atomic.Int64  // every call the gateway made
}

// itemCall is an inventory command as the item module got it.
type itemCall[R any] struct {
	actor item.Actor
	req   R
}

func (f *fakeItems) Inventory(ctx context.Context, _ kernel.CharacterID) (_ item.Inventory, err error) {
	f.ops.Add(1)
	ctx, call := itemCalls.Start(ctx, "Inventory")
	defer call.End(&err)
	if f.gate != nil {
		select {
		case <-f.gate:
		case <-ctx.Done():
			return item.Inventory{}, ctx.Err()
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return item.Inventory{Items: slices.Clone(f.inventory), Revision: f.revision}, nil
}

func (f *fakeItems) Move(_ context.Context, a item.Actor, r item.MoveRequest) (item.Result, error) {
	f.ops.Add(1)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.moves = append(f.moves, itemCall[item.MoveRequest]{a, r})
	return f.result()
}

func (f *fakeItems) Discard(_ context.Context, a item.Actor, r item.DiscardRequest) (item.Result, error) {
	f.ops.Add(1)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.discards = append(f.discards, itemCall[item.DiscardRequest]{a, r})
	return f.result()
}

func (f *fakeItems) DiscardMany(_ context.Context, a item.Actor, r item.DiscardManyRequest) (item.Result, error) {
	f.ops.Add(1)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.discardMany = append(f.discardMany, itemCall[item.DiscardManyRequest]{a, r})
	return f.result()
}

// result is every command's answer; called with mu held.
func (f *fakeItems) result() (item.Result, error) {
	if f.err != nil {
		return item.Result{}, f.err
	}
	return item.Result{Changed: f.changed, Revision: f.revision}, nil
}

// invite1, invite2 and invite9 are invites the tests deliver and answer.
var invite1, invite2, invite9 = newInviteID(), newInviteID(), newInviteID()

func newInviteID() social.InviteID {
	id, err := social.ParseInviteID(uuidv7.NewString())
	if err != nil {
		panic(err)
	}
	return id
}

// fakeSocial is the social module, delivering party lines to recipients.
type fakeSocial struct {
	deliveries *stream[social.Delivery]
	mu         sync.Mutex
	online     []social.Presence
	onlineIDs  []string // each Online's trace ID
	offline    []offlineCall
	hang       map[kernel.CharacterID]bool // Online for these waits until its context ends
	downFor    int                         // the next Online calls fail as a module whose database is out of reach
	gate       chan struct{}               // when set, Online lands only once it closes
	landed     []string                    // "online <id>" and "offline <id>", in the order they took effect
	calls      []string
	of         map[kernel.CharacterID]*social.Party // the party Online sends each character; none sent for one left out
	err        error                                // for party commands
	chatErr    error
	offlineErr error
	recipients []kernel.CharacterID // a party line goes to these
}

type offlineCall struct {
	id   kernel.CharacterID
	conn string
}

func newFakeSocial() *fakeSocial {
	return &fakeSocial{deliveries: newStream[social.Delivery](), of: map[kernel.CharacterID]*social.Party{}}
}

func (f *fakeSocial) Subscribe(gateway string) *hub.Sub[social.Delivery] {
	return f.deliveries.subscribe(gateway)
}

// GatewayLeft is the adapter telling of a break; the module's sweep after its grace is not faked.
func (f *fakeSocial) GatewayLeft(string) {}

func (f *fakeSocial) record(s string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, s)
	return f.err
}

func (f *fakeSocial) lastCall() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[len(f.calls)-1]
}

func (f *fakeSocial) onlineTraceIDs() []string { return copied(&f.mu, &f.onlineIDs) }

func (f *fakeSocial) Online(ctx context.Context, p social.Presence) error {
	id := p.Member.ID
	f.mu.Lock()
	f.online = append(f.online, p)
	f.onlineIDs = append(f.onlineIDs, traceID(ctx))
	hang, gate := f.hang[id], f.gate
	down := f.downFor > 0
	if down {
		f.downFor--
	}
	f.mu.Unlock()
	if down {
		return errDown
	}
	if hang {
		<-ctx.Done()
		return ctx.Err()
	}
	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.landed = append(f.landed, "online "+id.String())
	// As the module does, for the characters a test gave an entry: the member hears their party, an empty one for nil.
	if party, ok := f.of[id]; ok {
		f.deliveries.send(social.PartyState{To: id, Party: party})
	}
	return nil
}

// lastLanded is the last of the character's Online and Offline to take effect: "online", "offline" or "".
func (f *fakeSocial) lastLanded(id kernel.CharacterID) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	last := ""
	for _, l := range f.landed {
		if what, who, _ := strings.Cut(l, " "); who == id.String() {
			last = what
		}
	}
	return last
}

func (f *fakeSocial) set(change func(f *fakeSocial)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	change(f)
}

func (f *fakeSocial) onlineCalls() []social.Presence { return copied(&f.mu, &f.online) }

func (f *fakeSocial) Offline(_ context.Context, id kernel.CharacterID, conn string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.offline = append(f.offline, offlineCall{id: id, conn: conn})
	f.landed = append(f.landed, "offline "+id.String())
	return f.offlineErr
}

func (f *fakeSocial) Invite(_ context.Context, from, to kernel.CharacterID) (social.InviteID, error) {
	return invite1, f.record("invite " + from.String() + " " + to.String())
}

func (f *fakeSocial) Respond(_ context.Context, who kernel.CharacterID, invite social.InviteID, accept bool) error {
	return f.record("respond " + who.String() + " " + invite.String() + " " + map[bool]string{true: "yes", false: "no"}[accept])
}

func (f *fakeSocial) LeaveParty(_ context.Context, who kernel.CharacterID) error {
	return f.record("leave " + who.String())
}

func (f *fakeSocial) Kick(_ context.Context, leader, target kernel.CharacterID) error {
	return f.record("kick " + leader.String() + " " + target.String())
}

func (f *fakeSocial) Promote(_ context.Context, leader, target kernel.CharacterID) error {
	return f.record("promote " + leader.String() + " " + target.String())
}

func (f *fakeSocial) Chat(_ context.Context, l social.Line) (social.Message, error) {
	f.mu.Lock()
	err, recipients := f.chatErr, f.recipients
	f.mu.Unlock()
	if err != nil {
		return social.Message{}, err
	}
	switch l.Channel {
	case social.ChannelMap:
	case social.ChannelParty:
		for _, id := range recipients {
			f.deliveries.send(social.ChatLine{To: id, Sender: l.From, SenderName: l.FromName, Text: l.Text})
		}
	default:
		return social.Message{}, refusal(kernel.ErrBadChatChannel)
	}
	return social.Message{Channel: l.Channel, Sender: l.From, SenderName: l.FromName, Text: l.Text}, nil
}
