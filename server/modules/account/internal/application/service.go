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

	"finga/internal/kernel"
	"finga/modules/account/internal/model"
	"finga/modules/account/internal/password"
	"finga/pkg/ratelimit"
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

// Deps are what the service is built over.
type Deps struct {
	Store    Store
	Notifier Notifier         // hears of every ended session
	Hasher   *password.Hasher // the Argon2id cost and how many hashes run at once, from the config
}

// Session lifetimes, token rotation and login throttling.
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

// The reasons the Notifier is given.
const (
	ReasonTokenReuse = "token reuse"
	ReasonLogout     = "logged out"
	ReasonNewLogin   = "logged in elsewhere"
)

var usernameRe = regexp.MustCompile(`^[A-Za-z0-9_]+$`)

// Service is the account service's use cases. It implements pkg/auth.
type Service struct {
	store      Store
	notifier   Notifier
	hasher     *password.Hasher
	dummy      string            // verified for unknown usernames, so they take as long
	byUser     ratelimit.Limiter // per-IP limits belong to the edge (nginx)
	presetList []model.Preset
}

// NewService wires the service over deps; it fails when the hasher cannot make the dummy hash.
func NewService(deps Deps) (*Service, error) {
	dummy, err := deps.Hasher.Hash(context.Background(), dummySecret)
	if err != nil {
		return nil, fmt.Errorf("account: dummy hash: %w", err)
	}
	return &Service{
		store:      deps.Store,
		notifier:   deps.Notifier,
		hasher:     deps.Hasher,
		dummy:      dummy,
		byUser:     ratelimit.NewBuckets(loginRatePerMinute/60.0, loginBurst),
		presetList: []model.Preset{model.PresetClassic, model.PresetWASD},
	}, nil
}

// Register creates an account; the repository only ever sees the password hash.
func (s *Service) Register(ctx context.Context, in RegisterInput) (model.Account, error) {
	if len(in.Username) < usernameMin || len(in.Username) > usernameMax || !usernameRe.MatchString(in.Username) {
		return model.Account{}, model.ErrBadUsername
	}
	if len(in.Password) < passwordMin || len(in.Password) > passwordMax {
		return model.Account{}, model.ErrBadPassword
	}
	hash, err := s.hasher.Hash(ctx, in.Password)
	if err != nil {
		return model.Account{}, busy(err)
	}
	a := model.NewAccount{ID: kernel.NewAccountID(), Username: in.Username, PasswordHash: hash, Settings: model.DefaultSettings()}
	var created time.Time
	err = s.store.InTx(ctx, func(uow UnitOfWork) error {
		var err error
		created, err = uow.Accounts().Insert(ctx, a)
		return err
	})
	if err != nil {
		return model.Account{}, err
	}
	return model.Account{ID: a.ID, Username: a.Username, Settings: a.Settings, CreatedAt: created}, nil
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

// Verify returns the account the credentials name: an unknown username checks a dummy hash, and a match at another cost is rehashed.
func (s *Service) Verify(ctx context.Context, username, pw string) (kernel.AccountID, error) {
	creds, err := s.store.FindByUsername(ctx, username)
	if err != nil {
		if errors.Is(err, model.ErrNotFound) {
			if _, err := s.hasher.Verify(ctx, s.dummy, pw); errors.Is(err, password.ErrBusy) {
				return kernel.AccountID{}, model.ErrBusy
			}
			return kernel.AccountID{}, model.ErrUnauthenticated
		}
		return kernel.AccountID{}, err
	}
	ok, err := s.hasher.Verify(ctx, creds.PasswordHash, pw)
	if errors.Is(err, password.ErrBusy) {
		return kernel.AccountID{}, model.ErrBusy
	}
	if err != nil {
		return kernel.AccountID{}, fmt.Errorf("account %s: %w", creds.Account.ID, err)
	}
	if !ok {
		return kernel.AccountID{}, model.ErrUnauthenticated
	}
	if s.hasher.NeedsRehash(creds.PasswordHash) {
		if err := s.rehash(ctx, creds, pw); err != nil {
			return kernel.AccountID{}, err
		}
	}
	return creds.Account.ID, nil
}

// rehash stores the verified password at the configured cost; with no hash slot free, the next login does.
func (s *Service) rehash(ctx context.Context, creds model.Credentials, pw string) error {
	hash, err := s.hasher.Hash(ctx, pw)
	if errors.Is(err, password.ErrBusy) {
		return nil
	}
	if err != nil {
		return err
	}
	return s.store.InTx(ctx, func(uow UnitOfWork) error {
		return uow.Accounts().ReplaceHash(ctx, creds.Account.ID, creds.PasswordHash, hash)
	})
}

// Issue starts a session and returns its token, replacing older sessions under the account lock; a unique-index race retries once.
func (s *Service) Issue(ctx context.Context, accountID kernel.AccountID) (string, error) {
	token, hash, err := newToken()
	if err != nil {
		return "", err
	}
	now := time.Now()
	session := model.Session{ID: kernel.NewSessionID(), AccountID: accountID, TokenHash: hash, ExpiresAt: s.expiry(now, now)}
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
		s.notifier.AccountRevoked(ctx, accountID, ReasonNewLogin) // the older session's connection, if any
		return token, nil
	}
}

// Validate resolves a token to its session and slides the expiry; the rotated-out token passes once in the grace period.
func (s *Service) Validate(ctx context.Context, token string) (model.Principal, error) {
	hash := sha256.Sum256([]byte(token))
	session, err := s.store.FindActiveSession(ctx, hash[:])
	if err != nil {
		if errors.Is(err, model.ErrNotFound) {
			return model.Principal{}, model.ErrUnauthenticated
		}
		return model.Principal{}, err
	}
	now := time.Now()
	if !now.Before(session.ExpiresAt) {
		return model.Principal{}, model.ErrUnauthenticated
	}
	if !bytes.Equal(session.TokenHash, hash[:]) {
		if err := s.consumePrevious(ctx, session.ID, hash[:], now); err != nil {
			return model.Principal{}, err
		}
	}
	if err := s.extend(ctx, session, now); err != nil {
		return model.Principal{}, err
	}
	return model.Principal{AccountID: session.AccountID, SessionID: session.ID}, nil
}

// consumePrevious lets the old token through once in the grace, under the row lock; any other use revokes the session.
func (s *Service) consumePrevious(ctx context.Context, id kernel.SessionID, hash []byte, now time.Time) error {
	reuse := false
	err := s.store.InTx(ctx, func(uow UnitOfWork) error {
		cur, err := uow.Sessions().Lock(ctx, id)
		if err != nil {
			return err
		}
		if cur.RevokedAt != nil || !bytes.Equal(cur.PreviousTokenHash, hash) {
			return model.ErrUnauthenticated // revoked, or rotated again, since the read
		}
		if cur.RotatedAt == nil || !now.Before(cur.RotatedAt.Add(rotationGrace)) {
			reuse = true
			return nil
		}
		return uow.Sessions().ConsumePrevious(ctx, id)
	})
	if errors.Is(err, model.ErrNotFound) {
		return model.ErrUnauthenticated
	}
	if err != nil || !reuse {
		return err
	}
	if err := s.revoke(ctx, id, now, ReasonTokenReuse); err != nil {
		return err
	}
	return model.ErrUnauthenticated
}

// Rotate issues a session's next token; the old one stays valid once within the grace period.
func (s *Service) Rotate(ctx context.Context, sessionID kernel.SessionID) (string, error) {
	token, hash, err := newToken()
	if err != nil {
		return "", err
	}
	now := time.Now()
	err = s.store.InTx(ctx, func(uow UnitOfWork) error {
		return uow.Sessions().Rotate(ctx, sessionID, hash, now)
	})
	if err != nil {
		if errors.Is(err, model.ErrNotFound) {
			return "", model.ErrUnauthenticated
		}
		return "", err
	}
	return token, nil
}

// Check reports whether the session is active and not expired.
func (s *Service) Check(ctx context.Context, sessionID kernel.SessionID) error {
	session, err := s.store.FindSession(ctx, sessionID)
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

// Revoke logs out of one session.
func (s *Service) Revoke(ctx context.Context, sessionID kernel.SessionID) error {
	return s.revoke(ctx, sessionID, time.Now(), ReasonLogout)
}

// RevokeAll logs out everywhere.
func (s *Service) RevokeAll(ctx context.Context, accountID kernel.AccountID) error {
	now := time.Now()
	err := s.store.InTx(ctx, func(uow UnitOfWork) error {
		return uow.Sessions().RevokeAll(ctx, accountID, now)
	})
	if err != nil {
		return err
	}
	s.notifier.AccountRevoked(ctx, accountID, ReasonLogout)
	return nil
}

func (s *Service) revoke(ctx context.Context, id kernel.SessionID, now time.Time, reason string) error {
	err := s.store.InTx(ctx, func(uow UnitOfWork) error {
		return uow.Sessions().Revoke(ctx, id, now)
	})
	if err != nil {
		return err
	}
	s.notifier.SessionRevoked(ctx, id, reason)
	return nil
}

// PruneSessions deletes at most limit sessions that ended, revoked or expired, before before; it reports how many.
func (s *Service) PruneSessions(ctx context.Context, before time.Time, limit int) (int64, error) {
	var n int64
	err := s.store.InTx(ctx, func(uow UnitOfWork) error {
		var err error
		n, err = uow.Sessions().DeleteEnded(ctx, before, limit)
		return err
	})
	return n, err
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
