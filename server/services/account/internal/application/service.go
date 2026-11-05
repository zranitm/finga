// Package application is the account service's use cases: sign up, log in, sessions and settings.
package application

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"

	"finga/internal/kernel"
	"finga/pkg/auth"
	"finga/pkg/auth/password"
	"finga/pkg/ratelimit"
	"finga/services/account/internal/model"
)

// RegisterInput is a sign-up.
type RegisterInput struct {
	Username string
	Password string
}

// LoginInput is a login.
type LoginInput struct {
	Username string
	Password string
}

// Options tune the service.
type Options struct {
	Hasher *password.Hasher // the Argon2id cost and how many hashes run at once, from the config

	// OnSessionRevoked and OnAccountRevoked, if set, hear of ended sessions so connections can close.
	OnSessionRevoked func(ctx context.Context, sessionID uuid.UUID, reason string)
	OnAccountRevoked func(ctx context.Context, accountID uuid.UUID, reason string)
}

// The session policy (step 16).
const (
	idleTTL            = 7 * 24 * time.Hour  // a session ends after this long unused
	maxLifetime        = 30 * 24 * time.Hour // and never lives longer than this from login
	extendEvery        = 10 * time.Minute    // the idle expiry is pushed at most this often
	rotationGrace      = 60 * time.Second    // the token retired by a rotation works once within this
	loginRatePerMinute = 5                   // per username
	loginBurst         = 5
)

const (
	tokenBytes      = 32
	usernameMin     = 3
	usernameMax     = 16
	passwordMin     = 8
	passwordMax     = 128
	loginMaxRetries = 1 // once more, when the one_active_session index catches a race

	dummySecret = "finga-dummy-password-for-timing" // hashed once; verified for unknown usernames
)

// The reasons OnSessionRevoked and OnAccountRevoked give.
const (
	ReasonTokenReuse = "token reuse"
	ReasonLogout     = "logged out"
	ReasonNewLogin   = "logged in elsewhere"
)

var usernameRe = regexp.MustCompile(`^[A-Za-z0-9_]+$`)

// Service is the account service's use cases. It implements pkg/auth.
type Service struct {
	store      Store
	opts       Options
	dummy      string            // verified for unknown usernames, so they take as long
	byUser     ratelimit.Limiter // per-IP limits belong to the edge (nginx)
	presetList []model.Preset
}

var (
	_ auth.Authenticator = (*Service)(nil)
	_ auth.Sessions      = (*Service)(nil)
)

// NewService wires the service over its store.
func NewService(store Store, opts Options) *Service {
	dummy, err := opts.Hasher.Hash(context.Background(), dummySecret)
	if err != nil {
		panic("account: dummy hash: " + err.Error()) // the parameters were just validated by Hash's caller
	}
	return &Service{
		store:      store,
		opts:       opts,
		dummy:      dummy,
		byUser:     ratelimit.NewBuckets(loginRatePerMinute/60.0, loginBurst),
		presetList: []model.Preset{model.PresetClassic, model.PresetWASD},
	}
}

// Register creates an account; the repository only ever sees the password hash.
func (s *Service) Register(ctx context.Context, in RegisterInput) (model.Account, error) {
	if len(in.Username) < usernameMin || len(in.Username) > usernameMax || !usernameRe.MatchString(in.Username) {
		return model.Account{}, model.ErrBadUsername
	}
	if len(in.Password) < passwordMin || len(in.Password) > passwordMax {
		return model.Account{}, model.ErrBadPassword
	}
	hash, err := s.opts.Hasher.Hash(ctx, in.Password)
	if err != nil {
		return model.Account{}, busy(err)
	}
	a := model.NewAccount{ID: kernel.NewAccountID(), Username: in.Username, PasswordHash: hash, Settings: model.DefaultSettings(), CreatedAt: time.Now()}
	err = s.store.InTx(ctx, func(uow UnitOfWork) error {
		return uow.Accounts().Insert(ctx, a)
	})
	if err != nil {
		return model.Account{}, err
	}
	return model.Account{ID: a.ID, Username: a.Username, Settings: a.Settings, CreatedAt: a.CreatedAt}, nil
}

// Login throttles per username, verifies the credentials and issues a session token.
func (s *Service) Login(ctx context.Context, in LoginInput) (string, error) {
	if !s.byUser.Allow(strings.ToLower(in.Username)) {
		return "", model.ErrRateLimited
	}
	id, err := s.Verify(ctx, in.Username, in.Password)
	if err != nil {
		return "", err
	}
	return s.Issue(ctx, id)
}

// Verify implements auth.Authenticator.
// An unknown username is checked against a dummy hash, so timing reveals nothing.
func (s *Service) Verify(ctx context.Context, username, pw string) (uuid.UUID, error) {
	creds, err := s.store.FindByUsername(ctx, username)
	if err != nil {
		if errors.Is(err, model.ErrNotFound) {
			if _, err := s.opts.Hasher.Verify(ctx, s.dummy, pw); errors.Is(err, password.ErrBusy) {
				return uuid.Nil, model.ErrBusy
			}
			return uuid.Nil, model.ErrUnauthenticated
		}
		return uuid.Nil, err
	}
	ok, err := s.opts.Hasher.Verify(ctx, creds.PasswordHash, pw)
	if errors.Is(err, password.ErrBusy) {
		return uuid.Nil, model.ErrBusy
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("account %s: %w", creds.Account.ID, err)
	}
	if !ok {
		return uuid.Nil, model.ErrUnauthenticated
	}
	return uuid.UUID(creds.Account.ID), nil
}

// Issue implements auth.Sessions, replacing the account's older sessions in one transaction.
// The account row lock queues concurrent logins; a unique-index race retries once.
func (s *Service) Issue(ctx context.Context, accountID uuid.UUID) (string, error) {
	token, hash, err := newToken()
	if err != nil {
		return "", err
	}
	now := time.Now()
	session := model.Session{ID: kernel.NewSessionID(), AccountID: kernel.AccountID(accountID), TokenHash: hash, CreatedAt: now, ExpiresAt: s.expiry(now, now)}
	for attempt := 0; ; attempt++ {
		err = s.store.InTx(ctx, func(uow UnitOfWork) error {
			if err := uow.Accounts().Lock(ctx, session.AccountID); err != nil {
				return err
			}
			if err := uow.Sessions().RevokeAll(ctx, session.AccountID, now); err != nil {
				return err
			}
			return uow.Sessions().Insert(ctx, session)
		})
		if errors.Is(err, model.ErrSessionRace) && attempt < loginMaxRetries {
			continue
		}
		if err != nil {
			return "", err
		}
		if s.opts.OnAccountRevoked != nil {
			s.opts.OnAccountRevoked(ctx, accountID, ReasonNewLogin) // the older session's connection, if any
		}
		return token, nil
	}
}

// Validate implements auth.Sessions and slides the expiry on success.
// The last rotated-out token works once within the grace; any other use revokes the session.
func (s *Service) Validate(ctx context.Context, token string) (auth.Principal, error) {
	hash := sha256.Sum256([]byte(token))
	session, err := s.store.FindActiveSession(ctx, hash[:])
	if err != nil {
		if errors.Is(err, model.ErrNotFound) {
			return auth.Principal{}, model.ErrUnauthenticated
		}
		return auth.Principal{}, err
	}
	now := time.Now()
	if !now.Before(session.ExpiresAt) {
		return auth.Principal{}, model.ErrUnauthenticated
	}
	if !bytes.Equal(session.TokenHash, hash[:]) {
		// The previous token: once, inside the grace; otherwise reuse.
		if session.RotatedAt == nil || !now.Before(session.RotatedAt.Add(rotationGrace)) {
			if err := s.revoke(ctx, session.ID, now, ReasonTokenReuse); err != nil {
				return auth.Principal{}, err
			}
			return auth.Principal{}, model.ErrUnauthenticated
		}
		err := s.store.InTx(ctx, func(uow UnitOfWork) error {
			return uow.Sessions().ConsumePrevious(ctx, session.ID)
		})
		if err != nil {
			return auth.Principal{}, err
		}
	}
	if err := s.extend(ctx, session, now); err != nil {
		return auth.Principal{}, err
	}
	return auth.Principal{AccountID: uuid.UUID(session.AccountID), SessionID: uuid.UUID(session.ID)}, nil
}

// Rotate implements auth.Sessions; the old token stays valid once within the grace period.
func (s *Service) Rotate(ctx context.Context, sessionID uuid.UUID) (string, error) {
	token, hash, err := newToken()
	if err != nil {
		return "", err
	}
	now := time.Now()
	err = s.store.InTx(ctx, func(uow UnitOfWork) error {
		return uow.Sessions().Rotate(ctx, kernel.SessionID(sessionID), hash, now)
	})
	if err != nil {
		if errors.Is(err, model.ErrNotFound) {
			return "", model.ErrUnauthenticated
		}
		return "", err
	}
	return token, nil
}

// Check implements auth.Sessions: the session is active and not expired.
func (s *Service) Check(ctx context.Context, sessionID uuid.UUID) error {
	session, err := s.store.FindSession(ctx, kernel.SessionID(sessionID))
	if err != nil {
		if errors.Is(err, model.ErrNotFound) {
			return model.ErrUnauthenticated
		}
		return err
	}
	if session.RevokedAt != nil || !time.Now().Before(session.ExpiresAt) {
		return model.ErrUnauthenticated
	}
	return nil
}

// Revoke implements auth.Sessions: logging out of one session.
func (s *Service) Revoke(ctx context.Context, sessionID uuid.UUID) error {
	return s.revoke(ctx, kernel.SessionID(sessionID), time.Now(), ReasonLogout)
}

// RevokeAll implements auth.Sessions: logging out everywhere.
func (s *Service) RevokeAll(ctx context.Context, accountID uuid.UUID) error {
	now := time.Now()
	err := s.store.InTx(ctx, func(uow UnitOfWork) error {
		return uow.Sessions().RevokeAll(ctx, kernel.AccountID(accountID), now)
	})
	if err != nil {
		return err
	}
	if s.opts.OnAccountRevoked != nil {
		s.opts.OnAccountRevoked(ctx, accountID, ReasonLogout)
	}
	return nil
}

func (s *Service) revoke(ctx context.Context, id kernel.SessionID, now time.Time, reason string) error {
	err := s.store.InTx(ctx, func(uow UnitOfWork) error {
		return uow.Sessions().Revoke(ctx, id, now)
	})
	if err != nil {
		return err
	}
	if s.opts.OnSessionRevoked != nil {
		s.opts.OnSessionRevoked(ctx, uuid.UUID(id), reason)
	}
	return nil
}

// expiry is the idle period from now, capped at the lifetime from creation.
func (s *Service) expiry(createdAt, now time.Time) time.Time {
	until := now.Add(idleTTL)
	if limit := createdAt.Add(maxLifetime); until.After(limit) {
		until = limit
	}
	return until
}

// extend slides the expiry, at most once every ExtendEvery.
func (s *Service) extend(ctx context.Context, session model.Session, now time.Time) error {
	lastExtended := session.ExpiresAt.Add(-idleTTL)
	if now.Sub(lastExtended) < extendEvery {
		return nil
	}
	until := s.expiry(session.CreatedAt, now)
	if !until.After(session.ExpiresAt) {
		return nil
	}
	return s.store.InTx(ctx, func(uow UnitOfWork) error {
		return uow.Sessions().Extend(ctx, session.ID, until)
	})
}

// Settings reads an account's settings.
func (s *Service) Settings(ctx context.Context, id kernel.AccountID) (model.Settings, error) {
	return s.store.Settings(ctx, id)
}

// UpdateSettings saves an account's settings; the preset must be known.
func (s *Service) UpdateSettings(ctx context.Context, id kernel.AccountID, st model.Settings) error {
	known := false
	for _, p := range s.presetList {
		known = known || p == st.ControlPreset
	}
	if !known {
		return model.ErrBadPreset
	}
	return s.store.InTx(ctx, func(uow UnitOfWork) error {
		return uow.Accounts().UpdateSettings(ctx, id, st)
	})
}

func newToken() (token string, hash []byte, err error) {
	raw := make([]byte, tokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", nil, fmt.Errorf("account: token: %w", err)
	}
	token = base64.RawURLEncoding.EncodeToString(raw)
	sum := sha256.Sum256([]byte(token))
	return token, sum[:], nil
}

// busy turns a hash that found no free slot into the module's error.
func busy(err error) error {
	if errors.Is(err, password.ErrBusy) {
		return model.ErrBusy
	}
	return err
}
