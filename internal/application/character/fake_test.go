package character_test

import (
	"context"
	"sort"
	"strings"
	"sync"

	"finga/internal/application/character"
	"finga/internal/model"
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
	return fn(fakeUOW{s})
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
