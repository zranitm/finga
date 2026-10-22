package ws_test

import (
	"context"
	"errors"
	"sync"

	"github.com/google/uuid"

	"finga/internal/application/character"
	"finga/internal/application/item"
	"finga/internal/application/world"
	"finga/internal/application/world/sim"
	"finga/internal/handler/ws"
	"finga/internal/model"
	"finga/pkg/auth"
)

type fakeSessions struct {
	tokens map[string]auth.Principal
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

func (f *fakeCharacters) Derived(c character.Character) (character.Derived, error) {
	return character.Derived{MaxHP: c.HP, MaxMP: c.MP}, nil
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
