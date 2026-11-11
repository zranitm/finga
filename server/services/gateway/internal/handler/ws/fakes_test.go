package ws_test

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	characterv1 "finga/api/gen/character/v1"
	itemv1 "finga/api/gen/item/v1"
	socialv1 "finga/api/gen/social/v1"
	worldv1 "finga/api/gen/world/v1"
	"finga/internal/kernel"
	"finga/pkg/grpcx"
	"finga/pkg/grpcx/grpctest"
	"finga/pkg/tracing"
	"finga/services/gateway/internal/handler/ws/receive"
	"finga/services/gateway/internal/model"
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

// dialFake serves a fake and dials it, both ends traced as the host traces them.
func dialFake(t *testing.T, register func(*grpc.Server)) *grpc.ClientConn {
	t.Helper()
	tr := grpcx.Tracing{Provider: traces}
	srv := grpcx.NewServer(grpcx.ServerOptions{Tracing: &tr})
	register(srv)
	return grpctest.Conn(t, srv, grpcx.TraceCalls(tr))
}

type fakeSessions struct {
	mu        sync.Mutex
	tokens    map[string]model.Principal
	rotations int
	expired   map[kernel.SessionID]bool
	rotateErr error // every Rotate fails with it
}

func (f *fakeSessions) Validate(_ context.Context, token string) (model.Principal, error) {
	p, ok := f.tokens[token]
	if !ok {
		return model.Principal{}, &model.ServiceError{Public: model.Unauthenticated, Err: errors.New("bad token")} // as the account client says it
	}
	return p, nil
}

// fakeWorld serves the world over in-memory gRPC; Join streams before and a Snapshot before answering.
type fakeWorld struct {
	worldv1.UnimplementedWorldServiceServer
	mu         sync.Mutex
	chars      map[kernel.CharacterID]fakeCharacter
	conns      map[kernel.CharacterID]string // the connection each character last joined through
	joinErr    error
	leaveFails int                        // the next Leave calls fail as a world too busy to take them
	leaveGate  chan struct{}              // when set, Leave answers only once it closes
	before     []*worldv1.DeliverResponse // sent ahead of each Snapshot, as an older connection's
	joined     []*worldv1.JoinRequest
	left       []*worldv1.LeaveRequest
	leaveIDs   []string // each Leave's trace ID
	inputs     []*worldv1.InputRequest
	attacks    []*worldv1.Presence
	pickups    []*worldv1.Presence
	portals    []*worldv1.Presence
	says       []*worldv1.SayRequest
	equips     []*worldv1.EquipRequest
	unequips   []*worldv1.UnequipRequest
	equipErr   error // Equip and Unequip fail with it
	uses       []*worldv1.UseItemRequest
	useErr     error // UseItem fails with it
	spends     []*worldv1.SpendPointsRequest
	resets     []*worldv1.ResetPointsRequest
	invests    []*worldv1.SpendSkillPointsRequest
	pointsErr  error                 // SpendPoints and ResetPoints fail with it
	quests     []*worldv1.QuestState // the log Join reports
	questCalls []proto.Message       // Talk, AcceptQuest, AbandonQuest and HandInQuest requests
	questErr   error                 // the four fail with it
	calls      []string              // commands in arrival order
	traceIDs   []string              // each Join's and Attack's, in arrival order
	hold       chan struct{}         // set: Subscribed goes out only during the first join
	joinGate   chan struct{}         // set: a join reports on joining, then waits for the gate to close
	joining    chan struct{}
	block      chan struct{} // when set, Attack waits for it (or its context)
	blocked    chan error    // Attack reports here that it is waiting, then why it stopped
	tick       uint64
	tickRate   uint32 // what Join reports; 30 when unset
	boot       string
	live       bool
	events     chan *worldv1.DeliverResponse
	end        chan struct{}
	subscribed chan struct{} // receives once per subscription
}

type fakeCharacter struct {
	account kernel.AccountID
	view    *worldv1.CharacterView
}

func newFakeWorld() *fakeWorld {
	return &fakeWorld{
		chars: map[kernel.CharacterID]fakeCharacter{
			aylaID:  {alice, &worldv1.CharacterView{Id: aylaID.String(), Name: "Ayla", Class: "warrior", Level: 1, Hp: 204, MaxHp: 204, Mp: 28, MaxMp: 28, MapId: "town", X: 416, Y: 608, Stats: &worldv1.Stats{Strength: 12}}},
			robinID: {alice, &worldv1.CharacterView{Id: robinID.String(), Name: "Robin", Class: "bowman", Level: 1, Hp: 105, MaxHp: 105, Mp: 33, MaxMp: 33, MapId: "town", X: 400, Y: 608}},
			brunoID: {bob, &worldv1.CharacterView{Id: brunoID.String(), Name: "Bruno", Class: "warrior", Level: 1, Hp: 204, MaxHp: 204, Mp: 28, MaxMp: 28, MapId: "town", X: 500, Y: 608}},
		},
		conns: map[kernel.CharacterID]string{},
		boot:  "boot-1", events: make(chan *worldv1.DeliverResponse, 256), end: make(chan struct{}), subscribed: make(chan struct{}, 8),
	}
}

func (f *fakeWorld) client(t *testing.T) worldv1.WorldServiceClient {
	t.Helper()
	return worldv1.NewWorldServiceClient(dialFake(t, func(srv *grpc.Server) { worldv1.RegisterWorldServiceServer(srv, f) }))
}

func (f *fakeWorld) Deliver(_ *worldv1.DeliverRequest, stream grpc.ServerStreamingServer[worldv1.DeliverResponse]) error {
	f.mu.Lock()
	boot, end, hold := f.boot, f.end, f.hold
	f.live = true
	f.mu.Unlock()
	if hold != nil {
		f.subscribed <- struct{}{} // joins work already; the gateway has not read Subscribed yet
		<-hold
	}
	if err := stream.Send(&worldv1.DeliverResponse{Event: &worldv1.DeliverResponse_Subscribed{Subscribed: &worldv1.Subscribed{BootId: boot}}}); err != nil {
		return err
	}
	if hold == nil {
		f.subscribed <- struct{}{}
	}
	for {
		select {
		case m := <-f.events:
			if err := stream.Send(m); err != nil {
				return err
			}
		case <-end:
			return status.Error(codes.Unavailable, "restarting")
		case <-stream.Context().Done():
			return nil
		}
	}
}

// restart ends the stream; the next subscription carries boot and ticks count from one again.
func (f *fakeWorld) restart(boot string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	close(f.end)
	f.end = make(chan struct{})
	f.boot, f.live, f.tick = boot, false, 0
}

func (f *fakeWorld) Join(ctx context.Context, req *worldv1.JoinRequest) (*worldv1.JoinResponse, error) {
	f.mu.Lock()
	f.traceIDs = append(f.traceIDs, trace.SpanContextFromContext(ctx).TraceID().String())
	hold := f.hold
	f.hold = nil
	f.mu.Unlock()
	if hold != nil {
		// Subscribed goes out now, read while this join is still in flight.
		close(hold)
		time.Sleep(100 * time.Millisecond)
	}
	f.mu.Lock()
	gate, joining := f.joinGate, f.joining
	f.mu.Unlock()
	if gate != nil {
		joining <- struct{}{}
		<-gate
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.joinErr != nil {
		return nil, f.joinErr
	}
	if !f.live {
		return nil, grpctest.Error(codes.FailedPrecondition, worldv1.Reason_REASON_NOT_SUBSCRIBED)
	}
	id, _ := kernel.ParseCharacterID(req.GetCharacterId())
	c, ok := f.chars[id]
	if !ok || c.account.String() != req.GetAccountId() {
		return nil, grpctest.Error(codes.NotFound, characterv1.Reason_REASON_CHARACTER_NOT_FOUND)
	}
	f.joined = append(f.joined, req)
	f.conns[id] = req.GetConnectionId()
	for _, m := range f.before {
		m.CharacterId, m.ConnectionId = id.String(), "conn-older"
		for _, r := range m.GetFrame().GetRecipients() {
			r.CharacterId, r.ConnectionId = id.String(), "conn-older"
		}
		f.events <- m
	}
	f.tick++
	v := c.view
	f.events <- &worldv1.DeliverResponse{CharacterId: id.String(), ConnectionId: req.GetConnectionId(), Tick: f.tick, Event: &worldv1.DeliverResponse_Snapshot{Snapshot: &worldv1.Snapshot{
		MapId: v.GetMapId(), Players: []*worldv1.PlayerView{{Id: v.GetId(), Name: v.GetName(), Class: v.GetClass(), Level: v.GetLevel(), Hp: v.GetHp(), MaxHp: v.GetMaxHp(), X: v.GetX(), Y: v.GetY(), Facing: 1}},
	}}}
	hz := cmp.Or(f.tickRate, 30)
	return &worldv1.JoinResponse{Character: v, TickRateHz: hz, JoinSeq: uint64(len(f.joined)), Quests: f.quests}, nil
}

// save is the character's checkpoint, which the next join loads.
func (f *fakeWorld) save(id kernel.CharacterID, mapID string, x, y float64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c := f.chars[id]
	v := proto.Clone(c.view).(*worldv1.CharacterView)
	v.MapId, v.X, v.Y = mapID, x, y
	c.view = v
	f.chars[id] = c
}

func (f *fakeWorld) joins() []*worldv1.JoinRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]*worldv1.JoinRequest(nil), f.joined...)
}

func (f *fakeWorld) leaves() []*worldv1.LeaveRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]*worldv1.LeaveRequest(nil), f.left...)
}

func (f *fakeWorld) leaveTraceIDs() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.leaveIDs...)
}

func (f *fakeWorld) Leave(ctx context.Context, req *worldv1.LeaveRequest) (*worldv1.LeaveResponse, error) {
	f.mu.Lock()
	f.left = append(f.left, req)
	f.leaveIDs = append(f.leaveIDs, trace.SpanContextFromContext(ctx).TraceID().String())
	if f.leaveFails > 0 {
		f.leaveFails--
		f.mu.Unlock()
		return nil, grpctest.Error(codes.Unavailable, worldv1.Reason_REASON_BUSY)
	}
	gate := f.leaveGate
	f.mu.Unlock()
	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return &worldv1.LeaveResponse{}, nil
}

func (f *fakeWorld) Input(_ context.Context, req *worldv1.InputRequest) (*worldv1.InputResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.inputs = append(f.inputs, req)
	f.calls = append(f.calls, "input "+strconv.FormatUint(uint64(req.GetSeq()), 10))
	return &worldv1.InputResponse{}, nil
}

func (f *fakeWorld) inputList() []*worldv1.InputRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]*worldv1.InputRequest(nil), f.inputs...)
}

func (f *fakeWorld) attackList() []*worldv1.Presence {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]*worldv1.Presence(nil), f.attacks...)
}

func (f *fakeWorld) pickupList() []*worldv1.Presence {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]*worldv1.Presence(nil), f.pickups...)
}

func (f *fakeWorld) portalList() []*worldv1.Presence {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]*worldv1.Presence(nil), f.portals...)
}

func (f *fakeWorld) sayList() []*worldv1.SayRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]*worldv1.SayRequest(nil), f.says...)
}

func (f *fakeWorld) inputCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.inputs)
}

func (f *fakeWorld) Attack(ctx context.Context, req *worldv1.AttackRequest) (*worldv1.AttackResponse, error) {
	f.mu.Lock()
	f.traceIDs = append(f.traceIDs, trace.SpanContextFromContext(ctx).TraceID().String())
	f.attacks = append(f.attacks, req.GetPresence())
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
			return nil, ctx.Err()
		}
	}
	return &worldv1.AttackResponse{}, nil
}

func (f *fakeWorld) traceIDList() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.traceIDs...)
}

func (f *fakeWorld) callList() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

func (f *fakeWorld) PickUp(_ context.Context, req *worldv1.PickUpRequest) (*worldv1.PickUpResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pickups = append(f.pickups, req.GetPresence())
	f.calls = append(f.calls, "pickup")
	return &worldv1.PickUpResponse{}, nil
}

func (f *fakeWorld) EnterPortal(_ context.Context, req *worldv1.EnterPortalRequest) (*worldv1.EnterPortalResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.portals = append(f.portals, req.GetPresence())
	f.calls = append(f.calls, "portal")
	return &worldv1.EnterPortalResponse{}, nil
}

func (f *fakeWorld) Equip(_ context.Context, req *worldv1.EquipRequest) (*worldv1.EquipResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.equips = append(f.equips, req)
	return &worldv1.EquipResponse{}, f.equipErr
}

func (f *fakeWorld) Unequip(_ context.Context, req *worldv1.UnequipRequest) (*worldv1.UnequipResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.unequips = append(f.unequips, req)
	return &worldv1.UnequipResponse{}, f.equipErr
}

func (f *fakeWorld) UseItem(_ context.Context, req *worldv1.UseItemRequest) (*worldv1.UseItemResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.uses = append(f.uses, req)
	return &worldv1.UseItemResponse{}, f.useErr
}

func (f *fakeWorld) SpendPoints(_ context.Context, req *worldv1.SpendPointsRequest) (*worldv1.SpendPointsResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.spends = append(f.spends, req)
	return &worldv1.SpendPointsResponse{}, f.pointsErr
}

func (f *fakeWorld) ResetPoints(_ context.Context, req *worldv1.ResetPointsRequest) (*worldv1.ResetPointsResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.resets = append(f.resets, req)
	return &worldv1.ResetPointsResponse{}, f.pointsErr
}

func (f *fakeWorld) SpendSkillPoints(_ context.Context, req *worldv1.SpendSkillPointsRequest) (*worldv1.SpendSkillPointsResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.invests = append(f.invests, req)
	return &worldv1.SpendSkillPointsResponse{}, f.pointsErr
}

func (f *fakeWorld) skillPointsCalls() []*worldv1.SpendSkillPointsRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]*worldv1.SpendSkillPointsRequest(nil), f.invests...)
}

func (f *fakeWorld) Talk(_ context.Context, req *worldv1.TalkRequest) (*worldv1.TalkResponse, error) {
	return &worldv1.TalkResponse{}, f.questCall(req)
}

func (f *fakeWorld) AcceptQuest(_ context.Context, req *worldv1.AcceptQuestRequest) (*worldv1.AcceptQuestResponse, error) {
	return &worldv1.AcceptQuestResponse{}, f.questCall(req)
}

func (f *fakeWorld) AbandonQuest(_ context.Context, req *worldv1.AbandonQuestRequest) (*worldv1.AbandonQuestResponse, error) {
	return &worldv1.AbandonQuestResponse{}, f.questCall(req)
}

func (f *fakeWorld) HandInQuest(_ context.Context, req *worldv1.HandInQuestRequest) (*worldv1.HandInQuestResponse, error) {
	return &worldv1.HandInQuestResponse{}, f.questCall(req)
}

func (f *fakeWorld) questCall(req proto.Message) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.questCalls = append(f.questCalls, req)
	return f.questErr
}

func (f *fakeWorld) failQuests(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.questErr = err
}

func (f *fakeWorld) questList() []proto.Message {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]proto.Message(nil), f.questCalls...)
}

func (f *fakeWorld) failPoints(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pointsErr = err
}

func (f *fakeWorld) pointsCalls() ([]*worldv1.SpendPointsRequest, []*worldv1.ResetPointsRequest) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]*worldv1.SpendPointsRequest(nil), f.spends...), append([]*worldv1.ResetPointsRequest(nil), f.resets...)
}

func (f *fakeWorld) failEquips(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.equipErr = err
}

func (f *fakeWorld) failUses(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.useErr = err
}

func (f *fakeWorld) useCalls() []*worldv1.UseItemRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]*worldv1.UseItemRequest(nil), f.uses...)
}

func (f *fakeWorld) equipCalls() ([]*worldv1.EquipRequest, []*worldv1.UnequipRequest) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]*worldv1.EquipRequest(nil), f.equips...), append([]*worldv1.UnequipRequest(nil), f.unequips...)
}

func (f *fakeWorld) Say(_ context.Context, req *worldv1.SayRequest) (*worldv1.SayResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.says = append(f.says, req)
	return &worldv1.SayResponse{}, nil
}

// delta sends a tick's Delta to the character, in a Frame as the world does.
func (f *fakeWorld) delta(to kernel.CharacterID, tick uint64, d *worldv1.Delta) {
	f.mu.Lock()
	conn := f.conns[to]
	f.mu.Unlock()
	f.events <- frameTo(to, conn, tick, d)
}

// frameTo is a Frame whose one recipient is the character through conn, with d's AckSeq as its own.
func frameTo(to kernel.CharacterID, conn string, tick uint64, d *worldv1.Delta) *worldv1.DeliverResponse {
	r := &worldv1.Recipient{CharacterId: to.String(), ConnectionId: conn, AckSeq: d.GetAckSeq()}
	d.AckSeq = 0
	return &worldv1.DeliverResponse{Tick: tick, Event: &worldv1.DeliverResponse_Frame{Frame: &worldv1.Frame{Delta: d, Recipients: []*worldv1.Recipient{r}}}}
}

func (f *fakeWorld) push(to kernel.CharacterID, m *worldv1.DeliverResponse) {
	f.mu.Lock()
	m.CharacterId, m.ConnectionId = to.String(), f.conns[to]
	f.mu.Unlock()
	f.events <- m
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

// fakeItems serves the item service over in-memory gRPC, recording inventory commands.
type fakeItems struct {
	itemv1.UnimplementedItemServiceServer
	mu        sync.Mutex
	inventory []*itemv1.Item
	moves     []*itemv1.MoveRequest
	discards  []*itemv1.DiscardRequest
	changed   []*itemv1.Slot
	revision  uint64 // every answer's
	err       error
	gate      chan struct{} // when set, Inventory waits for it to close
	invErr    error         // Inventory fails with it
}

func (f *fakeItems) client(t *testing.T) itemv1.ItemServiceClient {
	t.Helper()
	return itemv1.NewItemServiceClient(dialFake(t, func(srv *grpc.Server) { itemv1.RegisterItemServiceServer(srv, f) }))
}

func (f *fakeItems) Inventory(ctx context.Context, _ *itemv1.InventoryRequest) (*itemv1.InventoryResponse, error) {
	if f.gate != nil {
		select {
		case <-f.gate:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.invErr != nil {
		return nil, f.invErr
	}
	return &itemv1.InventoryResponse{Items: f.inventory, Revision: f.revision}, nil
}

func (f *fakeItems) Move(_ context.Context, req *itemv1.MoveRequest) (*itemv1.MoveResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.moves = append(f.moves, req)
	if f.err != nil {
		return nil, f.err
	}
	return &itemv1.MoveResponse{Changed: f.changed, Revision: f.revision}, nil
}

func (f *fakeItems) Discard(_ context.Context, req *itemv1.DiscardRequest) (*itemv1.DiscardResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.discards = append(f.discards, req)
	if f.err != nil {
		return nil, f.err
	}
	return &itemv1.DiscardResponse{Changed: f.changed, Revision: f.revision}, nil
}

// fakeSocial serves the social service over in-memory gRPC, delivering party lines to recipients.
type fakeSocial struct {
	socialv1.UnimplementedSocialServiceServer
	mu         sync.Mutex
	boot       string
	online     []*socialv1.OnlineRequest
	onlineIDs  []string // each Online's trace ID
	offline    []*socialv1.OfflineRequest
	hang       map[string]bool // Online for these character IDs waits until its context ends
	downFor    int             // the next Online calls fail as a service restarting
	gate       chan struct{}   // when set, Online lands only once it closes
	landed     []string        // "online <id>" and "offline <id>", in the order they took effect
	calls      []string
	of         map[string]*socialv1.Party // the party Online sends each character; none sent for one left out
	err        error                      // for party commands
	chatErr    error
	recipients []kernel.CharacterID // a party line goes to these
	events     chan *socialv1.DeliverResponse
	end        chan struct{}
	subscribed chan struct{} // receives once per subscription
}

func newFakeSocial() *fakeSocial {
	return &fakeSocial{boot: "boot-1", of: map[string]*socialv1.Party{}, events: make(chan *socialv1.DeliverResponse, 64), end: make(chan struct{}), subscribed: make(chan struct{}, 8)}
}

func (f *fakeSocial) client(t *testing.T) socialv1.SocialServiceClient {
	t.Helper()
	return socialv1.NewSocialServiceClient(dialFake(t, func(srv *grpc.Server) { socialv1.RegisterSocialServiceServer(srv, f) }))
}

func (f *fakeSocial) Deliver(_ *socialv1.DeliverRequest, stream grpc.ServerStreamingServer[socialv1.DeliverResponse]) error {
	f.mu.Lock()
	boot, end := f.boot, f.end
	f.mu.Unlock()
	if err := stream.Send(&socialv1.DeliverResponse{Event: &socialv1.DeliverResponse_Subscribed{Subscribed: &socialv1.Subscribed{BootId: boot}}}); err != nil {
		return err
	}
	f.subscribed <- struct{}{}
	for {
		select {
		case m := <-f.events:
			if err := stream.Send(m); err != nil {
				return err
			}
		case <-end:
			return status.Error(codes.Unavailable, "restarting")
		case <-stream.Context().Done():
			return nil
		}
	}
}

// restart ends the current stream; the next subscription carries boot.
func (f *fakeSocial) restart(boot string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	close(f.end)
	f.end = make(chan struct{})
	f.boot = boot
}

func (f *fakeSocial) push(to kernel.CharacterID, m *socialv1.DeliverResponse) {
	m.CharacterId = to.String()
	f.events <- m
}

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

func (f *fakeSocial) onlineTraceIDs() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.onlineIDs...)
}

func (f *fakeSocial) Online(ctx context.Context, req *socialv1.OnlineRequest) (*socialv1.OnlineResponse, error) {
	f.mu.Lock()
	f.online = append(f.online, req)
	f.onlineIDs = append(f.onlineIDs, trace.SpanContextFromContext(ctx).TraceID().String())
	hang, gate := f.hang[req.GetMember().GetId()], f.gate
	down := f.downFor > 0
	if down {
		f.downFor--
	}
	f.mu.Unlock()
	if down {
		return nil, status.Error(codes.Unavailable, "restarting")
	}
	if hang {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.landed = append(f.landed, "online "+req.GetMember().GetId())
	// As the service does, for the characters a test gave an entry: the member hears their party, an empty one for nil.
	if party, ok := f.of[req.GetMember().GetId()]; ok {
		f.events <- &socialv1.DeliverResponse{CharacterId: req.GetMember().GetId(), Event: &socialv1.DeliverResponse_PartyState{PartyState: &socialv1.PartyState{Party: party}}}
	}
	return &socialv1.OnlineResponse{}, nil
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

func (f *fakeSocial) onlineCalls() []*socialv1.OnlineRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]*socialv1.OnlineRequest(nil), f.online...)
}

func (f *fakeSocial) Offline(_ context.Context, req *socialv1.OfflineRequest) (*socialv1.OfflineResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.offline = append(f.offline, req)
	f.landed = append(f.landed, "offline "+req.GetCharacterId())
	return &socialv1.OfflineResponse{}, nil
}

func (f *fakeSocial) Invite(_ context.Context, req *socialv1.InviteRequest) (*socialv1.InviteResponse, error) {
	if err := f.record("invite " + req.GetCharacterId() + " " + req.GetTargetId()); err != nil {
		return nil, err
	}
	return &socialv1.InviteResponse{InviteId: "inv-1"}, nil
}

func (f *fakeSocial) Respond(_ context.Context, req *socialv1.RespondRequest) (*socialv1.RespondResponse, error) {
	return &socialv1.RespondResponse{}, f.record("respond " + req.GetCharacterId() + " " + req.GetInviteId() + " " + map[bool]string{true: "yes", false: "no"}[req.GetAccept()])
}

func (f *fakeSocial) LeaveParty(_ context.Context, req *socialv1.LeavePartyRequest) (*socialv1.LeavePartyResponse, error) {
	return &socialv1.LeavePartyResponse{}, f.record("leave " + req.GetCharacterId())
}

func (f *fakeSocial) Kick(_ context.Context, req *socialv1.KickRequest) (*socialv1.KickResponse, error) {
	return &socialv1.KickResponse{}, f.record("kick " + req.GetCharacterId() + " " + req.GetTargetId())
}

func (f *fakeSocial) Promote(_ context.Context, req *socialv1.PromoteRequest) (*socialv1.PromoteResponse, error) {
	return &socialv1.PromoteResponse{}, f.record("promote " + req.GetCharacterId() + " " + req.GetTargetId())
}

func (f *fakeSocial) Chat(_ context.Context, req *socialv1.ChatRequest) (*socialv1.ChatResponse, error) {
	f.mu.Lock()
	err, recipients := f.chatErr, f.recipients
	f.mu.Unlock()
	if err != nil {
		return nil, err
	}
	if req.GetChannel() == socialv1.ChatChannel_CHAT_CHANNEL_UNSPECIFIED {
		return nil, grpctest.Error(codes.InvalidArgument, socialv1.Reason_REASON_BAD_CHAT_CHANNEL)
	}
	if req.GetChannel() == socialv1.ChatChannel_CHAT_CHANNEL_PARTY {
		for _, id := range recipients {
			f.push(id, &socialv1.DeliverResponse{Event: &socialv1.DeliverResponse_ChatLine{ChatLine: &socialv1.ChatLine{SenderId: req.GetSenderId(), SenderName: req.GetSenderName(), Text: req.GetText()}}})
		}
	}
	return &socialv1.ChatResponse{Channel: req.GetChannel(), Text: req.GetText()}, nil
}

// Rotate hands out a fresh token per call; Check consults the expired set.
func (f *fakeSessions) Rotate(_ context.Context, sessionID kernel.SessionID) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rotations++
	if f.rotateErr != nil {
		return "", f.rotateErr
	}
	return "rotated-" + sessionID.String()[:8] + "-" + fmt.Sprint(f.rotations), nil
}

func (f *fakeSessions) Check(_ context.Context, sessionID kernel.SessionID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.expired[sessionID] {
		return &model.ServiceError{Public: model.Unauthenticated, Err: errors.New("session expired")}
	}
	return nil
}

func (f *fakeSessions) expire(sessionID kernel.SessionID) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.expired == nil {
		f.expired = map[kernel.SessionID]bool{}
	}
	f.expired[sessionID] = true
}
