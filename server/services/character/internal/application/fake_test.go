package application_test

import (
	"context"
	"maps"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"finga/internal/kernel"
	"finga/services/character/internal/application"
	"finga/services/character/internal/model"
)

// fakeStore is an in-memory Store whose names are unique ignoring case.
type fakeStore struct {
	mu      sync.Mutex
	chars   map[kernel.CharacterID]model.Character
	quests  map[kernel.CharacterID]map[string]model.Quest
	skills  map[kernel.CharacterID]map[string]int
	timers  map[kernel.CharacterID][]model.SkillTimer
	order   []kernel.CharacterID
	txCount int
}

func newFakeStore() *fakeStore {
	return &fakeStore{chars: map[kernel.CharacterID]model.Character{}, quests: map[kernel.CharacterID]map[string]model.Quest{}, skills: map[kernel.CharacterID]map[string]int{}, timers: map[kernel.CharacterID][]model.SkillTimer{}}
}

func (s *fakeStore) InTx(_ context.Context, fn func(uow application.UnitOfWork) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.txCount++
	chars, quests, skills := maps.Clone(s.chars), map[kernel.CharacterID]map[string]model.Quest{}, map[kernel.CharacterID]map[string]int{}
	for id, qs := range s.quests {
		quests[id] = maps.Clone(qs)
	}
	for id, sk := range s.skills {
		skills[id] = maps.Clone(sk)
	}
	order, timers := slices.Clone(s.order), maps.Clone(s.timers)
	if err := fn(fakeUOW{s}); err != nil {
		s.chars, s.quests, s.skills, s.order, s.timers = chars, quests, skills, order, timers
		return err
	}
	return nil
}

func (s *fakeStore) Quests(_ context.Context, id kernel.CharacterID) ([]model.Quest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := slices.Collect(maps.Values(s.quests[id]))
	slices.SortFunc(out, func(a, b model.Quest) int { return strings.Compare(a.ID, b.ID) })
	return out, nil
}

func (s *fakeStore) Skills(_ context.Context, id kernel.CharacterID) ([]model.SkillPoints, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []model.SkillPoints
	for _, skill := range slices.Sorted(maps.Keys(s.skills[id])) {
		out = append(out, model.SkillPoints{ID: skill, Points: s.skills[id][skill]})
	}
	return out, nil
}

func (s *fakeStore) Timers(_ context.Context, id kernel.CharacterID) ([]model.SkillTimer, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.timers[id]), nil
}

func (s *fakeStore) List(_ context.Context, account kernel.AccountID) ([]model.Character, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []model.Character
	for _, id := range s.order {
		if c := s.chars[id]; c.AccountID == account {
			out = append(out, c)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].UpdatedAt.Before(out[j].UpdatedAt) })
	return out, nil
}

func (s *fakeStore) Load(_ context.Context, id kernel.CharacterID, account kernel.AccountID) (model.Character, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.chars[id]
	if !ok || c.AccountID != account {
		return model.Character{}, model.ErrNotFound
	}
	return c, nil
}

type fakeUOW struct{ s *fakeStore }

func (u fakeUOW) Characters() application.Repository { return fakeRepo(u) }

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
	account   kernel.AccountID
	character kernel.CharacterID
}

func (f *fakeStarter) GrantStarter(_ context.Context, account kernel.AccountID, id kernel.CharacterID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, grant{account, id})
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

func (r fakeRepo) CountLocked(_ context.Context, account kernel.AccountID) (int, error) {
	n := 0
	for _, c := range r.s.chars {
		if c.AccountID == account {
			n++
		}
	}
	return n, nil
}

// Insert, SaveCheckpoint and SaveLevelUp stamp updated_at, as the database does.
func (r fakeRepo) Insert(_ context.Context, c model.Character) (model.Character, error) {
	for _, existing := range r.s.chars {
		if strings.EqualFold(existing.Name, c.Name) {
			return model.Character{}, model.ErrNameTaken
		}
	}
	c.UpdatedAt = time.Now()
	r.s.chars[c.ID] = c
	r.s.order = append(r.s.order, c.ID)
	return c, nil
}

func (r fakeRepo) SaveCheckpoint(_ context.Context, cp model.Checkpoint) error {
	c, ok := r.s.chars[cp.ID]
	if !ok {
		return model.ErrNotFound
	}
	c.Class, c.MapID, c.X, c.Y, c.HP, c.MP, c.Level, c.Exp, c.UpdatedAt = cp.Class, cp.MapID, cp.X, cp.Y, cp.HP, cp.MP, cp.Level, cp.Exp, time.Now()
	c.Points, c.UnspentPoints, c.FreeResetUsed, c.UnspentSkillPoints = cp.Points, cp.UnspentPoints, cp.FreeResetUsed, cp.UnspentSkillPoints
	r.s.chars[cp.ID] = c
	if r.s.skills[cp.ID] == nil {
		r.s.skills[cp.ID] = map[string]int{}
	}
	for _, sk := range cp.Skills {
		r.s.skills[cp.ID][sk.ID] = sk.Points
	}
	r.s.quests[cp.ID] = map[string]model.Quest{}
	for _, q := range cp.Quests {
		r.s.quests[cp.ID][q.ID] = q
	}
	r.s.timers[cp.ID] = slices.Clone(cp.Timers)
	return nil
}

func (r fakeRepo) SetSkillBar(_ context.Context, id kernel.CharacterID, account kernel.AccountID, bar []string) error {
	c, ok := r.s.chars[id]
	if !ok || c.AccountID != account {
		return model.ErrNotFound
	}
	c.SkillBar = slices.Clone(bar)
	r.s.chars[id] = c
	return nil
}

func (r fakeRepo) SaveLevelUp(_ context.Context, lu model.LevelUp) error {
	c, ok := r.s.chars[lu.ID]
	if !ok {
		return model.ErrNotFound
	}
	c.Level, c.Exp, c.Points, c.HP, c.MP, c.UpdatedAt = lu.Level, lu.Exp, lu.Points, lu.HP, lu.MP, time.Now()
	r.s.chars[lu.ID] = c
	return nil
}
