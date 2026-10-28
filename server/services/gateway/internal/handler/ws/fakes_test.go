package ws_test

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"testing"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	characterv1 "finga/api/gen/character/v1"
	itemv1 "finga/api/gen/item/v1"
	socialv1 "finga/api/gen/social/v1"
	worldv1 "finga/api/gen/world/v1"
	"finga/internal/model"
	"finga/pkg/auth"
	"finga/pkg/grpcx"
	"finga/pkg/grpcx/grpctest"
)

type fakeSessions struct {
	mu        sync.Mutex
	tokens    map[string]auth.Principal
	rotations int
	expired   map[uuid.UUID]bool
}

func (f *fakeSessions) Revoke(context.Context, uuid.UUID) error { return nil }
func (f *fakeSessions) Validate(_ context.Context, token string) (auth.Principal, error) {
	p, ok := f.tokens[token]
	if !ok {
		return auth.Principal{}, errors.New("unauthenticated: bad token")
	}
	return p, nil
}

// fakeWorld is the world service, served over an in-memory gRPC
// connection so the gateway's real client and stream run. Join checks the
// character is the account's (the character service's not-found reason
// otherwise), records the join, and sends before, then the character's
// Snapshot, on the stream before it answers, as the real world may.
// Commands and leaves are recorded; the test pushes events to a player.
// restart ends the current stream and starts the ticks again; the next
// subscription carries boot.
type fakeWorld struct {
	worldv1.UnimplementedWorldServiceServer
	mu         sync.Mutex
	chars      map[model.CharacterID]fakeCharacter
	joinErr    error
	before     []*worldv1.DeliverResponse // sent ahead of each Snapshot
	joined     []*worldv1.JoinRequest
	left       []*worldv1.LeaveRequest
	inputs     []*worldv1.InputRequest
	attacks    []*worldv1.Presence
	pickups    []*worldv1.Presence
	portals    []*worldv1.Presence
	says       []*worldv1.SayRequest
	calls      []string      // every command, in the order it arrived
	block      chan struct{} // when set, Attack waits for it (or its context)
	blocked    chan error    // Attack reports here that it is waiting, then why it stopped
	tick       uint64
	boot       string
	live       bool
	events     chan *worldv1.DeliverResponse
	end        chan struct{}
	subscribed chan struct{} // receives once per subscription
}

// fakeCharacter is a character as the character service has it saved.
type fakeCharacter struct {
	account model.AccountID
	view    *worldv1.CharacterView
}

func newFakeWorld() *fakeWorld {
	return &fakeWorld{
		chars: map[model.CharacterID]fakeCharacter{
			aylaID:  {alice, &worldv1.CharacterView{Id: aylaID.String(), Name: "Ayla", Class: "warrior", Level: 1, Hp: 204, MaxHp: 204, Mp: 28, MaxMp: 28, MapId: "town", X: 416, Y: 608, Stats: &characterv1.Stats{Strength: 12}}},
			robinID: {alice, &worldv1.CharacterView{Id: robinID.String(), Name: "Robin", Class: "bowman", Level: 1, Hp: 105, MaxHp: 105, Mp: 33, MaxMp: 33, MapId: "town", X: 400, Y: 608}},
			brunoID: {bob, &worldv1.CharacterView{Id: brunoID.String(), Name: "Bruno", Class: "warrior", Level: 1, Hp: 204, MaxHp: 204, Mp: 28, MaxMp: 28, MapId: "town", X: 500, Y: 608}},
		},
		boot: "boot-1", events: make(chan *worldv1.DeliverResponse, 256), end: make(chan struct{}), subscribed: make(chan struct{}, 8),
	}
}

func (f *fakeWorld) client(t *testing.T) worldv1.WorldServiceClient {
	t.Helper()
	srv := grpcx.NewServer(grpcx.ServerOptions{})
	worldv1.RegisterWorldServiceServer(srv, f)
	return worldv1.NewWorldServiceClient(grpctest.Conn(t, srv))
}

func (f *fakeWorld) Deliver(_ *worldv1.DeliverRequest, stream grpc.ServerStreamingServer[worldv1.DeliverResponse]) error {
	f.mu.Lock()
	boot, end := f.boot, f.end
	f.live = true
	f.mu.Unlock()
	if err := stream.Send(&worldv1.DeliverResponse{Event: &worldv1.DeliverResponse_Subscribed{Subscribed: &worldv1.Subscribed{BootId: boot}}}); err != nil {
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

// restart ends the current stream, as a world that went away would: the
// next subscription carries boot, and the ticks count from one again.
func (f *fakeWorld) restart(boot string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	close(f.end)
	f.end = make(chan struct{})
	f.boot, f.live, f.tick = boot, false, 0
}

func (f *fakeWorld) Join(_ context.Context, req *worldv1.JoinRequest) (*worldv1.JoinResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.joinErr != nil {
		return nil, f.joinErr
	}
	if !f.live {
		return nil, grpcx.Error(codes.FailedPrecondition, worldv1.Reason_REASON_NOT_SUBSCRIBED)
	}
	id, _ := model.ParseCharacterID(req.GetCharacterId())
	c, ok := f.chars[id]
	if !ok || c.account.String() != req.GetAccountId() {
		return nil, grpcx.Error(codes.NotFound, characterv1.Reason_REASON_CHARACTER_NOT_FOUND)
	}
	f.joined = append(f.joined, req)
	for _, m := range f.before {
		m.CharacterId = id.String()
		f.events <- m
	}
	f.tick++
	v := c.view
	f.events <- &worldv1.DeliverResponse{CharacterId: id.String(), Tick: f.tick, Event: &worldv1.DeliverResponse_Snapshot{Snapshot: &worldv1.Snapshot{
		MapId: v.GetMapId(), Players: []*worldv1.PlayerView{{Id: v.GetId(), Name: v.GetName(), Class: v.GetClass(), Level: v.GetLevel(), Hp: v.GetHp(), MaxHp: v.GetMaxHp(), X: v.GetX(), Y: v.GetY(), Facing: 1}},
	}}}
	return &worldv1.JoinResponse{Character: v}, nil
}

// save is the character's checkpoint, which the next join loads.
func (f *fakeWorld) save(id model.CharacterID, mapID string, x, y float64) {
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

func (f *fakeWorld) Leave(_ context.Context, req *worldv1.LeaveRequest) (*worldv1.LeaveResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.left = append(f.left, req)
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

func (f *fakeWorld) Say(_ context.Context, req *worldv1.SayRequest) (*worldv1.SayResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.says = append(f.says, req)
	return &worldv1.SayResponse{}, nil
}

// push sends an event to a player on the stream.
func (f *fakeWorld) push(to model.CharacterID, m *worldv1.DeliverResponse) {
	m.CharacterId = to.String()
	f.events <- m
}

type fakeObserver struct {
	mu     sync.Mutex
	in     map[string]int
	out    map[string]int
	online int
	slow   int
}

func newFakeObserver() *fakeObserver {
	return &fakeObserver{in: map[string]int{}, out: map[string]int{}}
}

func (o *fakeObserver) MessageIn(t string)  { o.mu.Lock(); o.in[t]++; o.mu.Unlock() }
func (o *fakeObserver) MessageOut(t string) { o.mu.Lock(); o.out[t]++; o.mu.Unlock() }
func (o *fakeObserver) SlowConsumer()       { o.mu.Lock(); o.slow++; o.mu.Unlock() }
func (o *fakeObserver) OnlineChanged(d int) { o.mu.Lock(); o.online += d; o.mu.Unlock() }

// fakeItems is the item service, served over an in-memory gRPC connection
// so the gateway's real client runs: it records the inventory commands and
// answers them with changed, or err.
type fakeItems struct {
	itemv1.UnimplementedItemServiceServer
	mu        sync.Mutex
	inventory []*itemv1.Item
	moves     []*itemv1.MoveRequest
	discards  []*itemv1.DiscardRequest
	changed   []*itemv1.Slot
	err       error
}

// client serves the fake and returns the gateway's client to it.
func (f *fakeItems) client(t *testing.T) itemv1.ItemServiceClient {
	t.Helper()
	srv := grpcx.NewServer(grpcx.ServerOptions{})
	itemv1.RegisterItemServiceServer(srv, f)
	return itemv1.NewItemServiceClient(grpctest.Conn(t, srv))
}

func (f *fakeItems) Inventory(context.Context, *itemv1.InventoryRequest) (*itemv1.InventoryResponse, error) {
	return &itemv1.InventoryResponse{Items: f.inventory}, nil
}

func (f *fakeItems) Move(_ context.Context, req *itemv1.MoveRequest) (*itemv1.MoveResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.moves = append(f.moves, req)
	if f.err != nil {
		return nil, f.err
	}
	return &itemv1.MoveResponse{Changed: f.changed}, nil
}

func (f *fakeItems) Discard(_ context.Context, req *itemv1.DiscardRequest) (*itemv1.DiscardResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.discards = append(f.discards, req)
	if f.err != nil {
		return nil, f.err
	}
	return &itemv1.DiscardResponse{Changed: f.changed}, nil
}

// fakeSocial is the social service, served over an in-memory gRPC
// connection: it records the calls the gateway makes, answers PartyOf from
// a script, routes chat the way the service would (a party line is
// delivered on the stream to each recipient), and streams whatever the
// test pushes on events. Closing end ends the current stream; the next
// subscription gets boot.
type fakeSocial struct {
	socialv1.UnimplementedSocialServiceServer
	mu         sync.Mutex
	boot       string
	online     []*socialv1.OnlineRequest
	offline    []string
	calls      []string
	of         map[string]*socialv1.Party
	err        error // for party commands
	chatErr    error
	recipients []model.CharacterID // a party line goes to these
	events     chan *socialv1.DeliverResponse
	end        chan struct{}
	subscribed chan struct{} // receives once per subscription
}

func newFakeSocial() *fakeSocial {
	return &fakeSocial{boot: "boot-1", of: map[string]*socialv1.Party{}, events: make(chan *socialv1.DeliverResponse, 64), end: make(chan struct{}), subscribed: make(chan struct{}, 8)}
}

func (f *fakeSocial) client(t *testing.T) socialv1.SocialServiceClient {
	t.Helper()
	srv := grpcx.NewServer(grpcx.ServerOptions{})
	socialv1.RegisterSocialServiceServer(srv, f)
	return socialv1.NewSocialServiceClient(grpctest.Conn(t, srv))
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

func (f *fakeSocial) push(to model.CharacterID, m *socialv1.DeliverResponse) {
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

func (f *fakeSocial) Online(_ context.Context, req *socialv1.OnlineRequest) (*socialv1.OnlineResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.online = append(f.online, req)
	return &socialv1.OnlineResponse{}, nil
}

func (f *fakeSocial) onlineCalls() []*socialv1.OnlineRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]*socialv1.OnlineRequest(nil), f.online...)
}

func (f *fakeSocial) Offline(_ context.Context, req *socialv1.OfflineRequest) (*socialv1.OfflineResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.offline = append(f.offline, req.GetCharacterId())
	return &socialv1.OfflineResponse{}, nil
}

func (f *fakeSocial) PartyOf(_ context.Context, req *socialv1.PartyOfRequest) (*socialv1.PartyOfResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return &socialv1.PartyOfResponse{Party: f.of[req.GetCharacterId()]}, nil
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
	if req.GetChannel() == socialv1.ChatChannel_CHAT_CHANNEL_PARTY {
		for _, id := range recipients {
			f.push(id, &socialv1.DeliverResponse{Event: &socialv1.DeliverResponse_ChatLine{ChatLine: &socialv1.ChatLine{SenderId: req.GetSenderId(), SenderName: req.GetSenderName(), Text: req.GetText()}}})
		}
	}
	return &socialv1.ChatResponse{Channel: req.GetChannel(), Text: req.GetText()}, nil
}

// Rotate hands out a fresh token per call; Check consults the expired set.
func (f *fakeSessions) Rotate(_ context.Context, sessionID uuid.UUID) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rotations++
	return "rotated-" + sessionID.String()[:8] + "-" + fmt.Sprint(f.rotations), nil
}

func (f *fakeSessions) Check(_ context.Context, sessionID uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.expired[sessionID] {
		return errors.New("unauthenticated: session expired")
	}
	return nil
}

func (f *fakeSessions) RevokeAll(context.Context, uuid.UUID) error { return nil }

func (f *fakeSessions) expire(sessionID uuid.UUID) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.expired == nil {
		f.expired = map[uuid.UUID]bool{}
	}
	f.expired[sessionID] = true
}
