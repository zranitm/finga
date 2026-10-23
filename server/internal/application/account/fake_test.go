package account_test

import (
	"bytes"
	"context"
	"strings"
	"sync"
	"time"

	"finga/internal/application/account"
	"finga/internal/model"
)

// fakeStore is an in-memory Store whose InTx hands over a fake unit of
// work. It mimics the constraints the service relies on: a unique username
// ignoring case. The one-active-session index is only exercised against
// real Postgres.
type fakeStore struct {
	mu       sync.Mutex
	accounts map[model.AccountID]account.NewAccount
	sessions []account.Session
	// what reached the repository, for assertions
	insertedTokenHashes [][]byte
	txCount             int
	failInTx            error // returned by InTx when set
}

func newFakeStore() *fakeStore {
	return &fakeStore{accounts: map[model.AccountID]account.NewAccount{}}
}

func (s *fakeStore) InTx(_ context.Context, fn func(uow account.UnitOfWork) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.txCount++
	if s.failInTx != nil {
		return s.failInTx
	}
	return fn(fakeUOW{s: s})
}

func (s *fakeStore) FindByUsername(_ context.Context, username string) (account.Credentials, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, a := range s.accounts {
		if strings.EqualFold(a.Username, username) {
			return account.Credentials{Account: toAccount(a), PasswordHash: a.PasswordHash}, nil
		}
	}
	return account.Credentials{}, account.ErrNotFound
}

func (s *fakeStore) FindActiveSession(_ context.Context, tokenHash []byte) (account.Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, se := range s.sessions {
		if (bytes.Equal(se.TokenHash, tokenHash) || bytes.Equal(se.PreviousTokenHash, tokenHash)) && se.RevokedAt == nil {
			return se, nil
		}
	}
	return account.Session{}, account.ErrNotFound
}

func (s *fakeStore) Settings(_ context.Context, id model.AccountID) (account.Settings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.accounts[id]
	if !ok {
		return account.Settings{}, account.ErrNotFound
	}
	return a.Settings, nil
}

func (s *fakeStore) activeSessions(id model.AccountID) int {
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

func toAccount(a account.NewAccount) account.Account {
	return account.Account{ID: a.ID, Username: a.Username, Settings: a.Settings, CreatedAt: a.CreatedAt}
}

type fakeUOW struct{ s *fakeStore }

func (u fakeUOW) Accounts() account.Repository        { return fakeAccounts(u) }
func (u fakeUOW) Sessions() account.SessionRepository { return fakeSessions(u) }

type fakeAccounts struct{ s *fakeStore }

func (r fakeAccounts) Insert(_ context.Context, a account.NewAccount) error {
	for _, existing := range r.s.accounts {
		if strings.EqualFold(existing.Username, a.Username) {
			return account.ErrUsernameTaken
		}
	}
	r.s.accounts[a.ID] = a
	return nil
}

func (r fakeAccounts) Lock(_ context.Context, id model.AccountID) error {
	if _, ok := r.s.accounts[id]; !ok {
		return account.ErrNotFound
	}
	return nil
}

func (r fakeAccounts) UpdateSettings(_ context.Context, id model.AccountID, st account.Settings) error {
	a, ok := r.s.accounts[id]
	if !ok {
		return account.ErrNotFound
	}
	a.Settings = st
	r.s.accounts[id] = a
	return nil
}

type fakeSessions struct{ s *fakeStore }

func (r fakeSessions) Insert(_ context.Context, se account.Session) error {
	r.s.sessions = append(r.s.sessions, se)
	r.s.insertedTokenHashes = append(r.s.insertedTokenHashes, se.TokenHash)
	return nil
}

func (r fakeSessions) RevokeAll(_ context.Context, id model.AccountID, at time.Time) error {
	for i := range r.s.sessions {
		if r.s.sessions[i].AccountID == id && r.s.sessions[i].RevokedAt == nil {
			t := at
			r.s.sessions[i].RevokedAt = &t
		}
	}
	return nil
}

func (r fakeSessions) Revoke(_ context.Context, id model.SessionID, at time.Time) error {
	for i := range r.s.sessions {
		if r.s.sessions[i].ID == id && r.s.sessions[i].RevokedAt == nil {
			t := at
			r.s.sessions[i].RevokedAt = &t
		}
	}
	return nil
}

func (s *fakeStore) FindSession(_ context.Context, id model.SessionID) (account.Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, se := range s.sessions {
		if se.ID == id {
			return se, nil
		}
	}
	return account.Session{}, account.ErrNotFound
}

func (r fakeSessions) Rotate(_ context.Context, id model.SessionID, newHash []byte, at time.Time) error {
	for i := range r.s.sessions {
		if r.s.sessions[i].ID == id && r.s.sessions[i].RevokedAt == nil {
			t := at
			r.s.sessions[i].PreviousTokenHash = r.s.sessions[i].TokenHash
			r.s.sessions[i].TokenHash = newHash
			r.s.sessions[i].RotatedAt = &t
			return nil
		}
	}
	return account.ErrNotFound
}

func (r fakeSessions) ConsumePrevious(_ context.Context, id model.SessionID) error {
	for i := range r.s.sessions {
		if r.s.sessions[i].ID == id {
			r.s.sessions[i].RotatedAt = nil
		}
	}
	return nil
}

func (r fakeSessions) Extend(_ context.Context, id model.SessionID, until time.Time) error {
	for i := range r.s.sessions {
		if r.s.sessions[i].ID == id {
			r.s.sessions[i].ExpiresAt = until
		}
	}
	return nil
}
