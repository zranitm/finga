package character_test

import (
	"context"
	"maps"
	"slices"
	"sort"
	"strings"
	"sync"

	"finga/internal/model"
	"finga/services/character/internal/character"
)

// fakeStore is an in-memory Store with the one constraint the service
// relies on: a unique name ignoring case.
type fakeStore struct {
	mu      sync.Mutex
	chars   map[model.CharacterID]character.Character
	order   []model.CharacterID
	txCount int
}

func newFakeStore() *fakeStore {
	return &fakeStore{chars: map[model.CharacterID]character.Character{}}
}

func (s *fakeStore) InTx(_ context.Context, fn func(uow character.UnitOfWork) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.txCount++
	// A failing function rolls back: the maps are restored.
	chars := maps.Clone(s.chars)
	order := slices.Clone(s.order)
	if err := fn(fakeUOW{s}); err != nil {
		s.chars, s.order = chars, order
		return err
	}
	return nil
}

func (s *fakeStore) List(_ context.Context, account model.AccountID) ([]character.Character, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []character.Character
	for _, id := range s.order {
		if c := s.chars[id]; c.AccountID == account {
			out = append(out, c)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].UpdatedAt.Before(out[j].UpdatedAt) })
	return out, nil
}

func (s *fakeStore) Load(_ context.Context, id model.CharacterID, account model.AccountID) (character.Character, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.chars[id]
	if !ok || c.AccountID != account {
		return character.Character{}, character.ErrNotFound
	}
	return c, nil
}

type fakeUOW struct{ s *fakeStore }

func (u fakeUOW) Characters() character.Repository { return fakeRepo(u) }

// fakeStarter records grants and fails the first failures calls with err.
type fakeStarter struct {
	mu             sync.Mutex
	store          *fakeStore // when set, each call records whether the character had committed
	calls          []grant
	committedFirst []bool
	failures       int
	err            error
	retryable      bool
}

type grant struct {
	account   model.AccountID
	character model.CharacterID
	class     model.Class
}

func (f *fakeStarter) GrantStarter(_ context.Context, account model.AccountID, id model.CharacterID, class model.Class) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, grant{account, id, class})
	if f.store != nil {
		f.store.mu.Lock()
		_, committed := f.store.chars[id]
		f.store.mu.Unlock()
		f.committedFirst = append(f.committedFirst, committed)
	}
	if len(f.calls) <= f.failures {
		return f.err
	}
	return nil
}

func (f *fakeStarter) Retryable(error) bool { return f.retryable }

type fakeRepo struct{ s *fakeStore }

func (r fakeRepo) Insert(_ context.Context, c character.Character) error {
	for _, existing := range r.s.chars {
		if strings.EqualFold(existing.Name, c.Name) {
			return character.ErrNameTaken
		}
	}
	r.s.chars[c.ID] = c
	r.s.order = append(r.s.order, c.ID)
	return nil
}

func (r fakeRepo) SaveCheckpoint(_ context.Context, cp character.Checkpoint) error {
	c, ok := r.s.chars[cp.ID]
	if !ok {
		return character.ErrNotFound
	}
	c.MapID, c.X, c.Y, c.HP, c.MP, c.Exp, c.UpdatedAt = cp.MapID, cp.X, cp.Y, cp.HP, cp.MP, cp.Exp, cp.At
	r.s.chars[cp.ID] = c
	return nil
}

func (r fakeRepo) SaveLevelUp(_ context.Context, lu character.LevelUp) error {
	c, ok := r.s.chars[lu.ID]
	if !ok {
		return character.ErrNotFound
	}
	c.Level, c.Exp, c.Stats, c.HP, c.MP, c.UpdatedAt = lu.Level, lu.Exp, lu.Stats, lu.HP, lu.MP, lu.At
	r.s.chars[lu.ID] = c
	return nil
}
