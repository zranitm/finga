// Package character is the character module's facade: characters, their progression and their saves; its types and kernel errors are the module's contract.
package character

import (
	"context"
	"errors"

	"finga/internal/facade"
	"finga/internal/gamedata"
	"finga/internal/host"
	"finga/internal/kernel"
	"finga/internal/progression"
	"finga/modules/character/internal/application"
	"finga/modules/character/internal/local"
	"finga/modules/character/internal/model"
	"finga/modules/character/internal/repository"
)

// Module is the character module; every call to it goes through these methods.
type Module struct {
	calls *facade.Calls
	svc   service
}

// service is the module's application, as the facade calls it.
type service interface {
	Create(ctx context.Context, account kernel.AccountID, in application.CreateInput) (model.Character, error)
	List(ctx context.Context, account kernel.AccountID) ([]model.Character, error)
	Load(ctx context.Context, id kernel.CharacterID, account kernel.AccountID) (model.Character, error)
	Quests(ctx context.Context, id kernel.CharacterID) ([]model.Quest, error)
	Skills(ctx context.Context, id kernel.CharacterID) ([]model.SkillPoints, error)
	Timers(ctx context.Context, id kernel.CharacterID) ([]model.SkillTimer, error)
	Own(c model.Character) gamedata.Stats
	Derived(c model.Character) (progression.Derived, error)
	SaveCheckpoint(ctx context.Context, cp model.Checkpoint) error
	SaveLevelUp(ctx context.Context, lu model.LevelUp) error
	SetSkillBar(ctx context.Context, id kernel.CharacterID, account kernel.AccountID, bar []string) error
}

// Items is the item module in the same process, as character creation calls it.
type Items = local.Items

// Deps are what the module is built from; Item grants a new character its starter items.
type Deps struct {
	Module *host.Module   // its logs, panics, metrics and the process's database
	Data   *gamedata.Data // classes, stat rules, the start town and skills
	Item   Items
}

// New builds the module; it fails fast on an unreachable database.
func New(ctx context.Context, d Deps) (*Module, error) {
	if d.Item == nil {
		return nil, errors.New("character: give Item")
	}
	pool, dbMetrics, err := d.Module.Database(ctx)
	if err != nil {
		return nil, err
	}
	calls, err := d.Module.Calls(errorTable)
	if err != nil {
		return nil, err
	}
	svc := application.NewService(application.Deps{Store: repository.NewPostgres(pool, dbMetrics), Content: d.Data, Starter: local.NewStarter(d.Item), Logger: d.Module.Logger})
	return &Module{calls: calls, svc: svc}, nil
}

// Create makes a level 1 character of the account and grants its starter items.
func (m *Module) Create(ctx context.Context, account kernel.AccountID, name string) (_ Character, err error) {
	ctx, call := m.calls.Start(ctx, "Create")
	defer call.End(&err)
	c, err := m.svc.Create(ctx, account, application.CreateInput{Name: name})
	if err != nil {
		return Character{}, err
	}
	return m.characterOf(c), nil
}

// List is the account's characters, oldest first.
func (m *Module) List(ctx context.Context, account kernel.AccountID) (_ []Character, err error) {
	ctx, call := m.calls.Start(ctx, "List")
	defer call.End(&err)
	cs, err := m.svc.List(ctx, account)
	if err != nil {
		return nil, err
	}
	out := make([]Character, 0, len(cs))
	for _, c := range cs {
		out = append(out, m.characterOf(c))
	}
	return out, nil
}

// Load is one of the account's characters, with its quests, skills and skill timers; a class the game data doesn't have is refused.
func (m *Module) Load(ctx context.Context, id kernel.CharacterID, account kernel.AccountID) (_ Loaded, err error) {
	ctx, call := m.calls.Start(ctx, "Load")
	defer call.End(&err)
	c, err := m.svc.Load(ctx, id, account)
	if err != nil {
		return Loaded{}, err
	}
	if _, err := m.svc.Derived(c); err != nil {
		return Loaded{}, err
	}
	qs, err := m.svc.Quests(ctx, id)
	if err != nil {
		return Loaded{}, err
	}
	sks, err := m.svc.Skills(ctx, id)
	if err != nil {
		return Loaded{}, err
	}
	ts, err := m.svc.Timers(ctx, id)
	if err != nil {
		return Loaded{}, err
	}
	out := Loaded{Character: m.characterOf(c), Quests: make([]Quest, 0, len(qs)), Skills: make([]SkillPoints, 0, len(sks)), Timers: make([]SkillTimer, 0, len(ts))}
	for _, q := range qs {
		out.Quests = append(out.Quests, questOf(q))
	}
	for _, sk := range sks {
		out.Skills = append(out.Skills, SkillPoints(sk))
	}
	for _, t := range ts {
		out.Timers = append(out.Timers, timerOf(t))
	}
	return out, nil
}

// SaveCheckpoint saves position, HP, energy and the whole progression.
func (m *Module) SaveCheckpoint(ctx context.Context, cp Checkpoint) (err error) {
	ctx, call := m.calls.Start(ctx, "SaveCheckpoint")
	defer call.End(&err)
	return m.svc.SaveCheckpoint(ctx, cp.model())
}

// SaveLevelUp saves a level-up right away.
func (m *Module) SaveLevelUp(ctx context.Context, lu LevelUp) (err error) {
	ctx, call := m.calls.Start(ctx, "SaveLevelUp")
	defer call.End(&err)
	return m.svc.SaveLevelUp(ctx, model.LevelUp(lu))
}

// SetSkillBar stores which skill sits on each key, for one of the account's characters.
func (m *Module) SetSkillBar(ctx context.Context, id kernel.CharacterID, account kernel.AccountID, bar []string) (err error) {
	ctx, call := m.calls.Start(ctx, "SetSkillBar")
	defer call.End(&err)
	return m.svc.SetSkillBar(ctx, id, account, bar)
}

// characterOf adds the own stats and the maximums, left 0 when the class is unknown (Load refuses that case).
func (m *Module) characterOf(c model.Character) Character {
	d, _ := m.svc.Derived(c)
	return Character{
		ID: c.ID, AccountID: c.AccountID, Name: c.Name, Class: c.Class, Level: c.Level, Exp: c.Exp,
		Points: c.Points, UnspentPoints: c.UnspentPoints, FreeResetUsed: c.FreeResetUsed, OwnStats: m.svc.Own(c), UnspentSkillPoints: c.UnspentSkillPoints,
		HP: c.HP, MaxHP: d.MaxHP, Energy: c.Energy, MapID: c.MapID, X: c.X, Y: c.Y, UpdatedAt: c.UpdatedAt, SkillBar: c.SkillBar,
	}
}
