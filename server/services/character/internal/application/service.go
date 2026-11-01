// Package application is the character service's use cases: creation, loading and saves.
package application

import (
	"context"
	"log/slog"
	"regexp"
	"time"

	"finga/internal/kernel"
	"finga/internal/progression"
	"finga/services/character/internal/model"
)

// CreateInput is a new character.
type CreateInput struct {
	Name  string
	Class kernel.Class
}

// Options tune the service. Zero values take the defaults.
type Options struct {
	Now func() time.Time
	// Starter grants a new character its starter items after it commits; nil grants nothing.
	Starter StarterItems
	// StarterTimeout bounds a grant's retries (5 s); the world grants missed items on join.
	StarterTimeout time.Duration
	// StarterBackoff is the first retry wait (50 ms), doubling up to a second.
	StarterBackoff time.Duration
	Logger         *slog.Logger
}

const (
	defaultStarterTimeout = 5 * time.Second
	defaultStarterBackoff = 50 * time.Millisecond
	maxStarterBackoff     = time.Second
)

const (
	nameMin       = 3
	nameMax       = 12
	maxCharacters = 3
)

var nameRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9]*$`)

// Service is the character service's use cases.
type Service struct {
	store   Store
	content Content
	now     func() time.Time
	opts    Options
}

// NewService wires the service over its store and the game data.
func NewService(store Store, content Content, opts Options) *Service {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.StarterTimeout <= 0 {
		opts.StarterTimeout = defaultStarterTimeout
	}
	if opts.StarterBackoff <= 0 {
		opts.StarterBackoff = defaultStarterBackoff
	}
	if opts.Logger == nil {
		opts.Logger = slog.New(slog.DiscardHandler)
	}
	return &Service{store: store, content: content, now: opts.Now, opts: opts}
}

// Create makes a level 1 character at the town spawn, then grants its starter items.
// It succeeds even if the grant fails: a client retry would only meet name_taken.
func (s *Service) Create(ctx context.Context, account kernel.AccountID, in CreateInput) (model.Character, error) {
	if len(in.Name) < nameMin || len(in.Name) > nameMax || !nameRe.MatchString(in.Name) {
		return model.Character{}, model.ErrBadName
	}
	class, ok := s.content.Class(in.Class)
	if !ok {
		return model.Character{}, model.ErrClassUnavailable
	}
	formulas, ok := s.content.Formulas(in.Class)
	if !ok {
		return model.Character{}, model.ErrClassUnavailable
	}
	existing, err := s.store.List(ctx, account)
	if err != nil {
		return model.Character{}, err
	}
	if len(existing) >= maxCharacters {
		return model.Character{}, model.ErrSlotsFull
	}
	town := s.content.Town()
	derived := progression.DeriveStats(formulas, class.BaseStats, 1, progression.Equipment{})
	c := model.Character{
		ID: kernel.NewCharacterID(), AccountID: account, Name: in.Name, Class: class.ID,
		Level: 1, Exp: 0, Stats: class.BaseStats, HP: derived.MaxHP, MP: derived.MaxMP,
		MapID: town.ID, X: town.PlayerSpawn.X, Y: town.PlayerSpawn.Y, UpdatedAt: s.now(),
	}
	err = s.store.InTx(ctx, func(uow UnitOfWork) error {
		return uow.Characters().Insert(ctx, c)
	})
	if err != nil {
		return model.Character{}, err
	}
	s.grantStarter(ctx, c)
	return c, nil
}

// grantStarter retries until StarterTimeout; keyed by character ID, a retry never grants twice.
func (s *Service) grantStarter(ctx context.Context, c model.Character) {
	if s.opts.Starter == nil {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, s.opts.StarterTimeout)
	defer cancel()
	wait := s.opts.StarterBackoff
	for {
		err := s.opts.Starter.GrantStarter(ctx, c.AccountID, c.ID, c.Class)
		if err == nil {
			return
		}
		if !s.opts.Starter.Retryable(err) {
			s.opts.Logger.ErrorContext(ctx, "starter items refused", "character_id", c.ID, "class", c.Class, "err", err)
			return
		}
		t := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			t.Stop()
			s.opts.Logger.WarnContext(ctx, "starter items not granted yet; the next join grants them", "character_id", c.ID, "err", err)
			return
		case <-t.C:
		}
		wait = min(wait*2, maxStarterBackoff)
	}
}

// List is the account's characters, oldest first.
func (s *Service) List(ctx context.Context, account kernel.AccountID) ([]model.Character, error) {
	return s.store.List(ctx, account)
}

// Load is one character, only if it belongs to the account.
func (s *Service) Load(ctx context.Context, id kernel.CharacterID, account kernel.AccountID) (model.Character, error) {
	return s.store.Load(ctx, id, account)
}

// SaveCheckpoint persists position, HP, MP and EXP. At is set here.
func (s *Service) SaveCheckpoint(ctx context.Context, cp model.Checkpoint) error {
	cp.At = s.now()
	return s.store.InTx(ctx, func(uow UnitOfWork) error {
		return uow.Characters().SaveCheckpoint(ctx, cp)
	})
}

// SaveLevelUp persists a level-up right away. At is set here.
func (s *Service) SaveLevelUp(ctx context.Context, lu model.LevelUp) error {
	lu.At = s.now()
	return s.store.InTx(ctx, func(uow UnitOfWork) error {
		return uow.Characters().SaveLevelUp(ctx, lu)
	})
}

// Derived computes a character's derived stats with no equipment.
func (s *Service) Derived(c model.Character) (progression.Derived, error) {
	formulas, ok := s.content.Formulas(c.Class)
	if !ok {
		return progression.Derived{}, model.ErrClassUnavailable
	}
	return progression.DeriveStats(formulas, c.Stats, c.Level, progression.Equipment{}), nil
}
