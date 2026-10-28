package ws_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	itemv1 "finga/api/gen/item/v1"
	socialv1 "finga/api/gen/social/v1"
	"finga/internal/application/character"
	"finga/internal/application/world"
	"finga/internal/handler/ws"
	"finga/internal/model"
	"finga/internal/progression"
	"finga/internal/sim"
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

type fakeCharacters struct {
	chars map[model.CharacterID]character.Character
}

func (f *fakeCharacters) Load(_ context.Context, id model.CharacterID, account model.AccountID) (character.Character, error) {
	c, ok := f.chars[id]
	if !ok || c.AccountID != account {
		return character.Character{}, character.ErrNotFound
	}
	return c, nil
}

func (f *fakeCharacters) Derived(c character.Character) (progression.Derived, error) {
	return progression.Derived{MaxHP: c.HP, MaxMP: c.MP}, nil
}

// fakeInstance records joins, leaves and inputs and lets the test push
// events to a player's sink.
type fakeInstance struct {
	mapID   string
	mu      sync.Mutex
	joined  []world.Player
	left    []model.CharacterID
	inputs  []recordedInput
	attacks []model.CharacterID
	pickups []model.CharacterID
	portals []model.CharacterID
	chats   []recordedChat
	sinks   map[model.CharacterID]world.Sink
	joinErr error
}

type recordedInput struct {
	id  model.CharacterID
	seq uint32
	in  sim.Input
}

func newFakeInstance() *fakeInstance {
	return &fakeInstance{mapID: "town", sinks: map[model.CharacterID]world.Sink{}}
}

// MapID is the map the fake runs.
func (f *fakeInstance) MapID() string { return f.mapID }

func (f *fakeInstance) Join(_ context.Context, p world.Player, s world.Sink) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.joinErr != nil {
		return f.joinErr
	}
	f.joined = append(f.joined, p)
	f.sinks[p.ID] = s
	s.Send(world.Snapshot{Tick: 1, MapID: "town", Players: []world.PlayerView{{ID: p.ID, Name: p.Name, Class: p.Class, Level: p.Level, HP: p.HP, MaxHP: p.HP, State: sim.State{X: p.X, Y: p.Y, Facing: 1}}}})
	return nil
}

func (f *fakeInstance) Leave(id model.CharacterID) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.left = append(f.left, id)
	delete(f.sinks, id)
	return true
}

func (f *fakeInstance) Input(id model.CharacterID, seq uint32, in sim.Input) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.inputs = append(f.inputs, recordedInput{id, seq, in})
	return true
}

func (f *fakeInstance) Attack(id model.CharacterID) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.attacks = append(f.attacks, id)
	return true
}

func (f *fakeInstance) inputCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.inputs)
}

func (f *fakeInstance) push(id model.CharacterID, e world.Event) bool {
	f.mu.Lock()
	s := f.sinks[id]
	f.mu.Unlock()
	if s == nil {
		return false
	}
	return s.Send(e)
}

type fakeWorld struct {
	instances map[string]ws.MapInstance
}

func (f *fakeWorld) Instance(mapID string) (ws.MapInstance, error) {
	i, ok := f.instances[mapID]
	if !ok {
		return nil, world.ErrNoMap
	}
	return i, nil
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

func (f *fakeInstance) PickUp(id model.CharacterID) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pickups = append(f.pickups, id)
	return true
}

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

func (f *fakeInstance) EnterPortal(id model.CharacterID) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.portals = append(f.portals, id)
	return true
}

func (f *fakeInstance) Chat(id model.CharacterID, name, text string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.chats = append(f.chats, recordedChat{id, name, text})
	return true
}

type recordedChat struct {
	id         model.CharacterID
	name, text string
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
