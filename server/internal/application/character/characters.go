package character

import (
	"context"
	"log/slog"
	"regexp"
	"time"

	"github.com/cenkalti/backoff/v5"

	model "finga/internal/model/character"
	"finga/internal/shared/gamedata"
	"finga/internal/shared/kernel"
	"finga/internal/shared/progression"
)

// CreateInput is a new character; every one starts as the class at the root of the tree.
type CreateInput struct {
	Name string
}

// casesDeps are what the use cases is built over.
type casesDeps struct {
	Store   Store
	Content Content
	Starter StarterItems // grants a new character its starter items after it commits
	Logger  *slog.Logger
}

// A starter grant retries from 50 ms, doubling up to a second, for 5 s; the world grants missed items on join.
const (
	starterTimeout    = 5 * time.Second
	starterBackoff    = 50 * time.Millisecond
	maxStarterBackoff = time.Second
)

const (
	nameMin       = 3
	nameMax       = 12
	maxCharacters = 3
)

var nameRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9]*$`)

// cases is the character module's use cases.
type cases struct {
	store   Store
	content Content
	starter StarterItems
	log     *slog.Logger
}

// newCases wires the use cases over deps.
func newCases(deps casesDeps) *cases {
	return &cases{store: deps.Store, content: deps.Content, starter: deps.Starter, log: deps.Logger}
}

// Create makes a level 1 character with starter items; a failed grant still succeeds, as a retry would meet name_taken.
func (s *cases) Create(ctx context.Context, account kernel.AccountID, in CreateInput) (model.Character, error) {
	if len(in.Name) < nameMin || len(in.Name) > nameMax || !nameRe.MatchString(in.Name) {
		return model.Character{}, model.ErrBadName
	}
	class := s.content.StartClass()
	town := s.content.StartTown()
	derived := progression.DeriveStats(s.content.StatRules(), class.ID, 1, class.BaseStats, progression.ItemStats{})
	c := model.Character{
		ID: kernel.NewCharacterID(), AccountID: account, Name: in.Name, Class: class.ID,
		Level: 1, Exp: 0, HP: derived.MaxHP,
		MapID: town.ID, X: town.PlayerSpawn.X, Z: town.PlayerSpawn.Z, Yaw: float32(town.SpawnYaw), SkillBar: make([]string, model.SkillBarKeys),
	}
	var stored model.Character
	err := s.store.InTx(ctx, func(uow UnitOfWork) error {
		n, err := uow.Characters().CountLocked(ctx, account)
		if err != nil {
			return err
		}
		if n >= maxCharacters {
			return model.ErrSlotsFull
		}
		stored, err = uow.Characters().Insert(ctx, c)
		return err
	})
	if err != nil {
		return model.Character{}, err
	}
	s.grantStarter(ctx, stored)
	return stored, nil
}

// grantStarter retries until starterTimeout; keyed by character ID, a retry never grants twice.
func (s *cases) grantStarter(ctx context.Context, c model.Character) {
	ctx, cancel := context.WithTimeout(ctx, starterTimeout)
	defer cancel()
	var last error
	_, err := backoff.Retry(ctx, func() (struct{}, error) {
		last = s.starter.GrantStarter(ctx, c.AccountID, c.ID)
		if last != nil && !s.starter.Retryable(last) {
			return struct{}{}, backoff.Permanent(last)
		}
		return struct{}{}, last
	},
		backoff.WithBackOff(&backoff.ExponentialBackOff{InitialInterval: starterBackoff, RandomizationFactor: backoff.DefaultRandomizationFactor, Multiplier: 2, MaxInterval: maxStarterBackoff}),
		backoff.WithMaxElapsedTime(0),
	)
	switch {
	case err == nil:
	case ctx.Err() != nil:
		s.log.WarnContext(ctx, "starter items not granted yet; the next join grants them", "character_id", c.ID, "err", last)
	default:
		s.log.ErrorContext(ctx, "starter items refused", "character_id", c.ID, "class", c.Class, "err", err)
	}
}

// List is the account's characters, oldest first.
func (s *cases) List(ctx context.Context, account kernel.AccountID) ([]model.Character, error) {
	return s.store.List(ctx, account)
}

// Load is one character, only if it belongs to the account.
func (s *cases) Load(ctx context.Context, id kernel.CharacterID, account kernel.AccountID) (model.Character, error) {
	return s.store.Load(ctx, id, account)
}

// Find is the characters with ids, of any account, for another module to read: pvp's board and arena builds.
func (s *cases) Find(ctx context.Context, ids []kernel.CharacterID) ([]model.Character, error) {
	return s.store.Find(ctx, ids)
}

// Quests is a loaded character's quests; the world reads them at join, while it is the only writer.
func (s *cases) Quests(ctx context.Context, id kernel.CharacterID) ([]model.Quest, error) {
	return s.store.Quests(ctx, id)
}

// Skills is a loaded character's skills with points; the world reads them at join, while it is the only writer.
func (s *cases) Skills(ctx context.Context, id kernel.CharacterID) ([]model.SkillPoints, error) {
	return s.store.Skills(ctx, id)
}

// Timers is a loaded character's skill timers; the world reads them at join, while it is the only writer.
func (s *cases) Timers(ctx context.Context, id kernel.CharacterID) ([]model.SkillTimer, error) {
	return s.store.Timers(ctx, id)
}

// Waypoints is a loaded character's found waypoints; the world reads them at join, while it is the only writer.
func (s *cases) Waypoints(ctx context.Context, id kernel.CharacterID) ([]model.Waypoint, error) {
	return s.store.Waypoints(ctx, id)
}

// SaveCheckpoint persists position, HP, energy, the whole progression, every quest, every skill, every skill timer and every waypoint found, in one transaction.
func (s *cases) SaveCheckpoint(ctx context.Context, cp model.Checkpoint) error {
	if _, ok := s.content.Class(cp.Class); !ok {
		return model.ErrClassUnavailable
	}
	seen := make(map[string]bool, len(cp.Quests))
	for _, q := range cp.Quests {
		if !q.Valid() || seen[q.ID] {
			return model.ErrBadQuest
		}
		seen[q.ID] = true
	}
	learned := make(map[string]bool, len(cp.Skills))
	for _, sk := range cp.Skills {
		if sk.ID == "" || sk.Points < 1 || learned[sk.ID] {
			return model.ErrBadSkill
		}
		learned[sk.ID] = true
	}
	if cp.UnspentSkillPoints < 0 {
		return model.ErrBadSkill
	}
	timed := make(map[model.SkillTimer]bool, len(cp.Timers))
	for _, t := range cp.Timers {
		key := model.SkillTimer{Skill: t.Skill, Kind: t.Kind}
		if !t.Valid() || timed[key] {
			return model.ErrBadSkillTimer
		}
		timed[key] = true
	}
	found := make(map[string]bool, len(cp.Waypoints))
	for _, w := range cp.Waypoints {
		if w.ID == "" || found[w.ID] {
			return model.ErrBadWaypoint
		}
		found[w.ID] = true
	}
	if !cp.Fits() {
		return model.ErrBadCheckpoint
	}
	return s.store.InTx(ctx, func(uow UnitOfWork) error {
		return uow.Characters().SaveCheckpoint(ctx, cp)
	})
}

// FindWaypoints adds the waypoints the character has not found, each by a known id once, in one transaction.
func (s *cases) FindWaypoints(ctx context.Context, id kernel.CharacterID, found []model.Waypoint) error {
	seen := make(map[string]bool, len(found))
	for _, w := range found {
		if w.ID == "" || seen[w.ID] {
			return model.ErrBadWaypoint
		}
		seen[w.ID] = true
	}
	return s.store.InTx(ctx, func(uow UnitOfWork) error {
		return uow.Characters().SaveWaypoints(ctx, id, found)
	})
}

// SaveLevelUp persists a level-up right away.
func (s *cases) SaveLevelUp(ctx context.Context, lu model.LevelUp) error {
	if !lu.Fits() {
		return model.ErrBadCheckpoint
	}
	return s.store.InTx(ctx, func(uow UnitOfWork) error {
		return uow.Characters().SaveLevelUp(ctx, lu)
	})
}

// SetSkillBar stores which skill sits on each key; each is empty or an active skill in the game data, whether or not the character has learned it, as the world refuses those it hasn't, and none a follow-up of the character's class, which rides on its root's key as the client keeps it.
func (s *cases) SetSkillBar(ctx context.Context, id kernel.CharacterID, account kernel.AccountID, bar []string) error {
	if len(bar) != model.SkillBarKeys {
		return model.ErrBadSkillBar
	}
	for _, skill := range bar {
		if sk, ok := s.content.Skill(skill); skill != "" && (!ok || sk.Kind != gamedata.Active) {
			return model.ErrBadSkillBar
		}
	}
	c, err := s.store.Load(ctx, id, account)
	if err != nil {
		return err
	}
	class, _ := s.content.Class(c.Class)
	for _, skill := range bar {
		if skill != "" && !class.Keyable(skill) {
			return model.ErrBadSkillBar
		}
	}
	return s.store.InTx(ctx, func(uow UnitOfWork) error {
		return uow.Characters().SetSkillBar(ctx, id, account, bar)
	})
}

// Own is a character's own stats: the starting stats everyone shares, plus the points they spent.
func (s *cases) Own(c model.Character) gamedata.Stats {
	return progression.AddStats(s.content.StartClass().BaseStats, c.Points)
}

// Derived computes a character's derived stats with no equipment; a class the game data doesn't have is ErrClassUnavailable.
func (s *cases) Derived(c model.Character) (progression.Derived, error) {
	if _, ok := s.content.Class(c.Class); !ok {
		return progression.Derived{}, model.ErrClassUnavailable
	}
	return progression.DeriveStats(s.content.StatRules(), c.Class, c.Level, s.Own(c), progression.ItemStats{}), nil
}
