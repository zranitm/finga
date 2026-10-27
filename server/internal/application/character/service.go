package character

import (
	"context"
	"regexp"
	"time"

	"finga/internal/model"
	"finga/internal/progression"
)

// Options tune the service.
type Options struct {
	Now func() time.Time
}

const (
	nameMin = 3
	nameMax = 12
	// maxCharacters is the Stage 1 cap per account (OD-09 default), so the
	// character-select screen stays bounded before A3 arrives.
	maxCharacters = 3
)

// A name starts with a letter and continues with letters and digits.
var nameRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9]*$`)

// Service is the character module's use cases.
type Service struct {
	store   Store
	content Content
	now     func() time.Time
}

// NewService wires the service over its store and the game data.
func NewService(store Store, content Content, opts Options) *Service {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	return &Service{store: store, content: content, now: opts.Now}
}

// Create makes a level 1 character of a class available in this stage,
// with its base stats, full HP and MP, standing at the town's player
// spawn. Step 12 adds the starter items to the same transaction.
func (s *Service) Create(ctx context.Context, account model.AccountID, in CreateInput) (Character, error) {
	if len(in.Name) < nameMin || len(in.Name) > nameMax || !nameRe.MatchString(in.Name) {
		return Character{}, ErrBadName
	}
	class, ok := s.content.Class(in.Class)
	if !ok {
		return Character{}, ErrClassUnavailable
	}
	formulas, ok := s.content.Formulas(in.Class)
	if !ok {
		return Character{}, ErrClassUnavailable
	}
	existing, err := s.store.List(ctx, account)
	if err != nil {
		return Character{}, err
	}
	if len(existing) >= maxCharacters {
		return Character{}, ErrSlotsFull
	}
	town := s.content.Town()
	derived := progression.DeriveStats(formulas, class.BaseStats, 1, progression.Equipment{})
	c := Character{
		ID: model.NewCharacterID(), AccountID: account, Name: in.Name, Class: class.ID,
		Level: 1, Exp: 0, Stats: class.BaseStats, HP: derived.MaxHP, MP: derived.MaxMP,
		MapID: town.ID, X: town.PlayerSpawn.X, Y: town.PlayerSpawn.Y, UpdatedAt: s.now(),
	}
	err = s.store.InTx(ctx, func(uow UnitOfWork) error {
		if err := uow.Characters().Insert(ctx, c); err != nil {
			return err
		}
		return uow.Starter().GrantStarter(ctx, c.ID, c.Class)
	})
	if err != nil {
		return Character{}, err
	}
	return c, nil
}

// List is the account's characters, oldest first.
func (s *Service) List(ctx context.Context, account model.AccountID) ([]Character, error) {
	return s.store.List(ctx, account)
}

// Load is one character, only if it belongs to the account.
func (s *Service) Load(ctx context.Context, id model.CharacterID, account model.AccountID) (Character, error) {
	return s.store.Load(ctx, id, account)
}

// SaveCheckpoint persists position, HP, MP and EXP. At is set here.
func (s *Service) SaveCheckpoint(ctx context.Context, cp Checkpoint) error {
	cp.At = s.now()
	return s.store.InTx(ctx, func(uow UnitOfWork) error {
		return uow.Characters().SaveCheckpoint(ctx, cp)
	})
}

// SaveLevelUp persists a level-up right away. At is set here.
func (s *Service) SaveLevelUp(ctx context.Context, lu LevelUp) error {
	lu.At = s.now()
	return s.store.InTx(ctx, func(uow UnitOfWork) error {
		return uow.Characters().SaveLevelUp(ctx, lu)
	})
}

// Derived computes a character's derived stats with no equipment.
func (s *Service) Derived(c Character) (progression.Derived, error) {
	formulas, ok := s.content.Formulas(c.Class)
	if !ok {
		return progression.Derived{}, ErrClassUnavailable
	}
	return progression.DeriveStats(formulas, c.Stats, c.Level, progression.Equipment{}), nil
}
