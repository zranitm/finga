package ws_test

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/google/uuid"

	"finga/internal/application/character"
	"finga/internal/application/chat"
	"finga/internal/application/item"
	"finga/internal/application/party"
	"finga/internal/application/world"
	"finga/internal/handler/ws"
	"finga/internal/model"
	"finga/internal/progression"
	"finga/internal/sim"
	"finga/pkg/auth"
)

type fakeSessions struct {
	mu        sync.Mutex
	tokens    map[string]auth.Principal
	rotations int
	expired   map[uuid.UUID]bool
}

func (f *fakeSessions) Issue(context.Context, uuid.UUID) (string, error) {
	return "", errors.New("unused")
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

func newFakeInstance() *fakeInstance { return &fakeInstance{sinks: map[model.CharacterID]world.Sink{}} }

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

// fakeItems answers inventory commands with scripted results.
type fakeItems struct {
	mu        sync.Mutex
	inventory []item.Item
	moves     []item.MoveInput
	discards  []item.DiscardInput
	actors    []item.Actor
	result    item.Result
	err       error
}

func (f *fakeItems) Inventory(context.Context, model.CharacterID) ([]item.Item, error) {
	return f.inventory, nil
}

func (f *fakeItems) Move(_ context.Context, actor item.Actor, in item.MoveInput) (item.Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.moves = append(f.moves, in)
	f.actors = append(f.actors, actor)
	return f.result, f.err
}

func (f *fakeItems) Discard(_ context.Context, actor item.Actor, in item.DiscardInput) (item.Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.discards = append(f.discards, in)
	f.actors = append(f.actors, actor)
	return f.result, f.err
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

// fakeChat routes the way the chat service would, without cleaning.
type fakeChat struct {
	err        error
	recipients []model.CharacterID // for the party channel
}

func (f *fakeChat) Send(from chat.Sender, channel chat.Channel, text string) (chat.Message, []model.CharacterID, error) {
	if f.err != nil {
		return chat.Message{}, nil, f.err
	}
	msg := chat.Message{Channel: channel, Sender: from.ID, SenderName: from.Name, Text: text}
	if channel == chat.Party {
		return msg, f.recipients, nil
	}
	return msg, nil, nil
}

// fakeParties records calls and answers with scripted results.
type fakeParties struct {
	mu      sync.Mutex
	online  []party.Member
	offline []model.CharacterID
	calls   []string
	of      map[model.CharacterID]party.Party
	err     error
}

func (f *fakeParties) record(s string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, s)
}

func (f *fakeParties) Online(m party.Member) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.online = append(f.online, m)
}

func (f *fakeParties) Offline(id model.CharacterID) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.offline = append(f.offline, id)
}

func (f *fakeParties) Of(id model.CharacterID) (party.Party, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p, ok := f.of[id]
	return p, ok
}

func (f *fakeParties) Invite(from, to model.CharacterID) (party.Invite, error) {
	f.record("invite " + from.String() + " " + to.String())
	return party.Invite{ID: "inv-1", From: from, To: to}, f.err
}

func (f *fakeParties) Respond(who model.CharacterID, inviteID string, accept bool) error {
	f.record("respond " + who.String() + " " + inviteID + " " + map[bool]string{true: "yes", false: "no"}[accept])
	return f.err
}

func (f *fakeParties) Leave(who model.CharacterID) error {
	f.record("leave " + who.String())
	return f.err
}

func (f *fakeParties) Kick(leader, target model.CharacterID) error {
	f.record("kick " + leader.String() + " " + target.String())
	return f.err
}

func (f *fakeParties) Promote(leader, target model.CharacterID) error {
	f.record("promote " + leader.String() + " " + target.String())
	return f.err
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
