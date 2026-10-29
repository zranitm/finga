package application_test

import (
	"bytes"
	"context"
	"strings"
	"sync"
	"time"

	"finga/internal/kernel"
	"finga/services/account/internal/application"
	"finga/services/account/internal/model"
)

// fakeStore is an in-memory Store whose InTx hands over a fake unit of
// work. It mimics the constraints the service relies on: a unique username
// ignoring case. The one-active-session index is only exercised against
// real Postgres.
type fakeStore struct {
	mu       sync.Mutex
	accounts map[kernel.AccountID]model.NewAccount
	sessions []model.Session
	// what reached the repository, for assertions
	insertedTokenHashes [][]byte
	txCount             int
	failInTx            error // returned by InTx when set
}

func newFakeStore() *fakeStore {
	return &fakeStore{accounts: map[kernel.AccountID]model.NewAccount{}}
}

func (s *fakeStore) InTx(_ context.Context, fn func(uow application.UnitOfWork) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.txCount++
	if s.failInTx != nil {
		return s.failInTx
	}
	return fn(fakeUOW{s: s})
}

func (s *fakeStore) FindByUsername(_ context.Context, username string) (model.Credentials, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, a := range s.accounts {
		if strings.EqualFold(a.Username, username) {
			return model.Credentials{Account: toAccount(a), PasswordHash: a.PasswordHash}, nil
		}
	}
	return model.Credentials{}, model.ErrNotFound
}

func (s *fakeStore) FindActiveSession(_ context.Context, tokenHash []byte) (model.Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, se := range s.sessions {
		if (bytes.Equal(se.TokenHash, tokenHash) || bytes.Equal(se.PreviousTokenHash, tokenHash)) && se.RevokedAt == nil {
			return se, nil
		}
	}
	return model.Session{}, model.ErrNotFound
}

func (s *fakeStore) Settings(_ context.Context, id kernel.AccountID) (model.Settings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.accounts[id]
	if !ok {
		return model.Settings{}, model.ErrNotFound
	}
	return a.Settings, nil
}

func (s *fakeStore) activeSessions(id kernel.AccountID) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, se := range s.sessions {
		if se.AccountID == id && se.RevokedAt == nil {
			n++
		}
	}
	return n
}

func toAccount(a model.NewAccount) model.Account {
	return model.Account{ID: a.ID, Username: a.Username, Settings: a.Settings, CreatedAt: a.CreatedAt}
}

type fakeUOW struct{ s *fakeStore }

func (u fakeUOW) Accounts() application.Repository        { return fakeAccounts(u) }
func (u fakeUOW) Sessions() application.SessionRepository { return fakeSessions(u) }

type fakeAccounts struct{ s *fakeStore }

func (r fakeAccounts) Insert(_ context.Context, a model.NewAccount) error {
	for _, existing := range r.s.accounts {
		if strings.EqualFold(existing.Username, a.Username) {
			return model.ErrUsernameTaken
		}
	}
	r.s.accounts[a.ID] = a
	return nil
}

func (r fakeAccounts) Lock(_ context.Context, id kernel.AccountID) error {
	if _, ok := r.s.accounts[id]; !ok {
		return model.ErrNotFound
	}
	return nil
}

func (r fakeAccounts) UpdateSettings(_ context.Context, id kernel.AccountID, st model.Settings) error {
	a, ok := r.s.accounts[id]
	if !ok {
		return model.ErrNotFound
	}
	a.Settings = st
	r.s.accounts[id] = a
	return nil
}

type fakeSessions struct{ s *fakeStore }

func (r fakeSessions) Insert(_ context.Context, se model.Session) error {
	r.s.sessions = append(r.s.sessions, se)
	r.s.insertedTokenHashes = append(r.s.insertedTokenHashes, se.TokenHash)
	return nil
}

func (r fakeSessions) RevokeAll(_ context.Context, id kernel.AccountID, at time.Time) error {
	for i := range r.s.sessions {
		if r.s.sessions[i].AccountID == id && r.s.sessions[i].RevokedAt == nil {
			t := at
			r.s.sessions[i].RevokedAt = &t
		}
	}
	return nil
}

func (r fakeSessions) Revoke(_ context.Context, id kernel.SessionID, at time.Time) error {
	for i := range r.s.sessions {
		if r.s.sessions[i].ID == id && r.s.sessions[i].RevokedAt == nil {
			t := at
			r.s.sessions[i].RevokedAt = &t
		}
	}
	return nil
}

func (s *fakeStore) FindSession(_ context.Context, id kernel.SessionID) (model.Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, se := range s.sessions {
		if se.ID == id {
			return se, nil
		}
	}
	return model.Session{}, model.ErrNotFound
}

func (r fakeSessions) Rotate(_ context.Context, id kernel.SessionID, newHash []byte, at time.Time) error {
	for i := range r.s.sessions {
		if r.s.sessions[i].ID == id && r.s.sessions[i].RevokedAt == nil {
			t := at
			r.s.sessions[i].PreviousTokenHash = r.s.sessions[i].TokenHash
			r.s.sessions[i].TokenHash = newHash
			r.s.sessions[i].RotatedAt = &t
			return nil
		}
	}
	return model.ErrNotFound
}

func (r fakeSessions) ConsumePrevious(_ context.Context, id kernel.SessionID) error {
	for i := range r.s.sessions {
		if r.s.sessions[i].ID == id {
			r.s.sessions[i].RotatedAt = nil
		}
	}
	return nil
}

func (r fakeSessions) Extend(_ context.Context, id kernel.SessionID, until time.Time) error {
	for i := range r.s.sessions {
		if r.s.sessions[i].ID == id {
			r.s.sessions[i].ExpiresAt = until
		}
	}
	return nil
}
