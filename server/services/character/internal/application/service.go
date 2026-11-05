// Package application is the character service's use cases: creation, loading and saves.
package application

import (
	"context"
	"log/slog"
	"regexp"
	"time"

	"github.com/cenkalti/backoff/v5"

	"finga/internal/kernel"
	"finga/internal/progression"
	"finga/services/character/internal/model"
)

// CreateInput is a new character.
type CreateInput struct {
	Name  string
	Class kernel.Class
}

// Options tune the service.
type Options struct {
	// Starter grants a new character its starter items after it commits; nil grants nothing.
	Starter StarterItems
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

// Service is the character service's use cases.
type Service struct {
	store   Store
	content Content
	opts    Options
}

// NewService wires the service over its store and the game data.
func NewService(store Store, content Content, opts Options) *Service {
	if opts.Logger == nil {
		opts.Logger = slog.New(slog.DiscardHandler)
	}
	return &Service{store: store, content: content, opts: opts}
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
	town := s.content.Town()
	derived := progression.DeriveStats(formulas, class.BaseStats, 1, progression.Equipment{})
	c := model.Character{
		ID: kernel.NewCharacterID(), AccountID: account, Name: in.Name, Class: class.ID,
		Level: 1, Exp: 0, Stats: class.BaseStats, HP: derived.MaxHP, MP: derived.MaxMP,
		MapID: town.ID, X: town.PlayerSpawn.X, Y: town.PlayerSpawn.Y, UpdatedAt: time.Now(),
	}
	err := s.store.InTx(ctx, func(uow UnitOfWork) error {
		n, err := uow.Characters().CountLocked(ctx, account)
		if err != nil {
			return err
		}
		if n >= maxCharacters {
			return model.ErrSlotsFull
		}
		return uow.Characters().Insert(ctx, c)
	})
	if err != nil {
		return model.Character{}, err
	}
	s.grantStarter(ctx, c)
	return c, nil
}

// grantStarter retries until starterTimeout; keyed by character ID, a retry never grants twice.
func (s *Service) grantStarter(ctx context.Context, c model.Character) {
	if s.opts.Starter == nil {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, starterTimeout)
	defer cancel()
	var last error
	_, err := backoff.Retry(ctx, func() (struct{}, error) {
		last = s.opts.Starter.GrantStarter(ctx, c.AccountID, c.ID, c.Class)
		if last != nil && !s.opts.Starter.Retryable(last) {
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
		s.opts.Logger.WarnContext(ctx, "starter items not granted yet; the next join grants them", "character_id", c.ID, "err", last)
	default:
		s.opts.Logger.ErrorContext(ctx, "starter items refused", "character_id", c.ID, "class", c.Class, "err", err)
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
	cp.At = time.Now()
	return s.store.InTx(ctx, func(uow UnitOfWork) error {
		return uow.Characters().SaveCheckpoint(ctx, cp)
	})
}

// SaveLevelUp persists a level-up right away. At is set here.
func (s *Service) SaveLevelUp(ctx context.Context, lu model.LevelUp) error {
	lu.At = time.Now()
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
