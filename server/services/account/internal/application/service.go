// Package application is the account service's use cases (Low-level
// design, page 11): sign up, log in, sessions with one active session per
// account (OD-08), and account settings. Its Service implements pkg/auth,
// so the handler and the gateway depend on the interface (TD-17). ports.go
// declares what it needs: its Store and the repositories bound to one
// transaction, which the repository layer implements.
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

// LoginInput is a login. IP is the caller's address for throttling; empty
// skips the per-IP limit.
type LoginInput struct {
	Username string
	Password string
	IP       string
}

// Options tune the service. Zero values take the defaults below.
type Options struct {
	IdleTTL            time.Duration    // a session ends after this long unused (TD-24: 7 days)
	MaxLifetime        time.Duration    // and never lives longer than this from login (30 days)
	ExtendEvery        time.Duration    // the idle expiry is pushed at most this often (10 minutes)
	RotationGrace      time.Duration    // the token retired by a rotation works once within this (60 s)
	LoginRatePerMinute float64          // per username and per IP (page 4: 5 per minute)
	LoginBurst         int              // attempts allowed at once
	Now                func() time.Time // the clock; tests inject one
	Hash               password.Params  // argon2id parameters

	// OnSessionRevoked and OnAccountRevoked are told when the service ends
	// sessions (reuse, logout, a new login elsewhere), so connections can
	// be closed. Both may be nil.
	OnSessionRevoked func(ctx context.Context, sessionID uuid.UUID, reason string)
	OnAccountRevoked func(ctx context.Context, accountID uuid.UUID, reason string)
}

const (
	defaultIdleTTL       = 7 * 24 * time.Hour
	defaultMaxLifetime   = 30 * 24 * time.Hour
	defaultExtendEvery   = 10 * time.Minute
	defaultRotationGrace = 60 * time.Second
	defaultLoginRate     = 5
	defaultLoginBurst    = 5

	tokenBytes      = 32
	usernameMin     = 3
	usernameMax     = 16
	passwordMin     = 8
	passwordMax     = 128
	loginKeyUser    = "user:"
	loginKeyIP      = "ip:"
	loginMaxRetries = 1 // once more, when the one_active_session index catches a race

	dummySecret = "finga-dummy-password-for-timing" // hashed once; verified for unknown usernames
)

// The reasons OnSessionRevoked and OnAccountRevoked give.
const (
	// ReasonTokenReuse: the session's previous token was used again after
	// the grace, so the session ended (step 16).
	ReasonTokenReuse = "token reuse"
	// ReasonLogout: the player logged out, here or everywhere.
	ReasonLogout = "logged out"
	// ReasonNewLogin: a new login ended the older session (one active
	// session per account).
	ReasonNewLogin = "logged in elsewhere"
)

var usernameRe = regexp.MustCompile(`^[A-Za-z0-9_]+$`)

// Service is the account service's use cases. It implements pkg/auth.
type Service struct {
	store      Store
	opts       Options
	now        func() time.Time
	hash       password.Params
	dummy      string // an argon2id hash verified for unknown usernames, so they take as long
	byUser     ratelimit.Limiter
	byIP       ratelimit.Limiter
	presetList []model.Preset
}

var (
	_ auth.Authenticator = (*Service)(nil)
	_ auth.Sessions      = (*Service)(nil)
)

// NewService wires the service over its store.
func NewService(store Store, opts Options) *Service {
	if opts.IdleTTL <= 0 {
		opts.IdleTTL = defaultIdleTTL
	}
	if opts.MaxLifetime <= 0 {
		opts.MaxLifetime = defaultMaxLifetime
	}
	if opts.ExtendEvery <= 0 {
		opts.ExtendEvery = defaultExtendEvery
	}
	if opts.RotationGrace <= 0 {
		opts.RotationGrace = defaultRotationGrace
	}
	if opts.LoginRatePerMinute <= 0 {
		opts.LoginRatePerMinute = defaultLoginRate
	}
	if opts.LoginBurst <= 0 {
		opts.LoginBurst = defaultLoginBurst
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Hash == (password.Params{}) {
		opts.Hash = password.Default
	}
	dummy, err := password.Hash(dummySecret, opts.Hash)
	if err != nil {
		panic("account: dummy hash: " + err.Error()) // the parameters were just validated by Hash's caller
	}
	perSecond := opts.LoginRatePerMinute / 60
	return &Service{
		store:      store,
		opts:       opts,
		now:        opts.Now,
		hash:       opts.Hash,
		dummy:      dummy,
		byUser:     ratelimit.NewBuckets(perSecond, opts.LoginBurst, ratelimit.WithClock(opts.Now)),
		byIP:       ratelimit.NewBuckets(perSecond, opts.LoginBurst, ratelimit.WithClock(opts.Now)),
		presetList: []model.Preset{model.PresetClassic, model.PresetWASD},
	}
}

// Register creates an account. The password is hashed here; the
// repository never sees it.
func (s *Service) Register(ctx context.Context, in RegisterInput) (model.Account, error) {
	if len(in.Username) < usernameMin || len(in.Username) > usernameMax || !usernameRe.MatchString(in.Username) {
		return model.Account{}, model.ErrBadUsername
	}
	if len(in.Password) < passwordMin || len(in.Password) > passwordMax {
		return model.Account{}, model.ErrBadPassword
	}
	hash, err := password.Hash(in.Password, s.hash)
	if err != nil {
		return model.Account{}, err
	}
	a := model.NewAccount{ID: kernel.NewAccountID(), Username: in.Username, PasswordHash: hash, Settings: model.DefaultSettings(), CreatedAt: s.now()}
	err = s.store.InTx(ctx, func(uow UnitOfWork) error {
		return uow.Accounts().Insert(ctx, a)
	})
	if err != nil {
		return model.Account{}, err
	}
	return model.Account{ID: a.ID, Username: a.Username, Settings: a.Settings, CreatedAt: a.CreatedAt}, nil
}

// Login throttles per username and per IP, verifies the credentials, and
// issues a session. The token is returned once and never stored.
func (s *Service) Login(ctx context.Context, in LoginInput) (string, error) {
	if !s.byUser.Allow(loginKeyUser + strings.ToLower(in.Username)) {
		return "", model.ErrRateLimited
	}
	if in.IP != "" && !s.byIP.Allow(loginKeyIP+in.IP) {
		return "", model.ErrRateLimited
	}
	id, err := s.Verify(ctx, in.Username, in.Password)
	if err != nil {
		return "", err
	}
	return s.Issue(ctx, id)
}

// Verify implements auth.Authenticator. An unknown user and a wrong
// password fail the same way and take the same time: an unknown username
// is verified against a dummy hash, so responses don't reveal which
// usernames exist, by content or by timing.
func (s *Service) Verify(ctx context.Context, username, pw string) (uuid.UUID, error) {
	creds, err := s.store.FindByUsername(ctx, username)
	if err != nil {
		if errors.Is(err, model.ErrNotFound) {
			_, _ = password.Verify(s.dummy, pw)
			return uuid.Nil, model.ErrUnauthenticated
		}
		return uuid.Nil, err
	}
	ok, err := password.Verify(creds.PasswordHash, pw)
	if err != nil {
		return uuid.Nil, fmt.Errorf("account %s: %w", creds.Account.ID, err)
	}
	if !ok {
		return uuid.Nil, model.ErrUnauthenticated
	}
	return uuid.UUID(creds.Account.ID), nil
}

// Issue implements auth.Sessions: one transaction revokes the account's
// older sessions and inserts the new one. The account row is locked first,
// so concurrent logins queue; the partial unique index is the backstop, and
// a violation runs the transaction once more.
func (s *Service) Issue(ctx context.Context, accountID uuid.UUID) (string, error) {
	token, hash, err := newToken()
	if err != nil {
		return "", err
	}
	now := s.now()
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

// Validate implements auth.Sessions. Unknown, expired and revoked tokens
// all fail with ErrUnauthenticated. The token retired by the last rotation
// is accepted once within the grace period; any other use of it is reuse
// and revokes the session. A valid use slides the expiry.
func (s *Service) Validate(ctx context.Context, token string) (auth.Principal, error) {
	hash := sha256.Sum256([]byte(token))
	session, err := s.store.FindActiveSession(ctx, hash[:])
	if err != nil {
		if errors.Is(err, model.ErrNotFound) {
			return auth.Principal{}, model.ErrUnauthenticated
		}
		return auth.Principal{}, err
	}
	now := s.now()
	if !now.Before(session.ExpiresAt) {
		return auth.Principal{}, model.ErrUnauthenticated
	}
	if !bytes.Equal(session.TokenHash, hash[:]) {
		// The previous token: once, inside the grace; otherwise reuse.
		if session.RotatedAt == nil || !now.Before(session.RotatedAt.Add(s.opts.RotationGrace)) {
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

// Rotate implements auth.Sessions: the session gets a new token, and the
// one it had becomes the previous token for the grace period.
func (s *Service) Rotate(ctx context.Context, sessionID uuid.UUID) (string, error) {
	token, hash, err := newToken()
	if err != nil {
		return "", err
	}
	now := s.now()
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
	if session.RevokedAt != nil || !s.now().Before(session.ExpiresAt) {
		return model.ErrUnauthenticated
	}
	return nil
}

// Revoke implements auth.Sessions: logging out of one session.
func (s *Service) Revoke(ctx context.Context, sessionID uuid.UUID) error {
	return s.revoke(ctx, kernel.SessionID(sessionID), s.now(), ReasonLogout)
}

// RevokeAll implements auth.Sessions: logging out everywhere.
func (s *Service) RevokeAll(ctx context.Context, accountID uuid.UUID) error {
	now := s.now()
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

// expiry is when a session used at now should end: the idle period from
// now, never past the lifetime from its creation.
func (s *Service) expiry(createdAt, now time.Time) time.Time {
	until := now.Add(s.opts.IdleTTL)
	if limit := createdAt.Add(s.opts.MaxLifetime); until.After(limit) {
		until = limit
	}
	return until
}

// extend slides the expiry, at most once every ExtendEvery and only when
// it moves it later.
func (s *Service) extend(ctx context.Context, session model.Session, now time.Time) error {
	lastExtended := session.ExpiresAt.Add(-s.opts.IdleTTL)
	if now.Sub(lastExtended) < s.opts.ExtendEvery {
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

// newToken is 32 random bytes as URL-safe base64, and its SHA-256.
func newToken() (token string, hash []byte, err error) {
	raw := make([]byte, tokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", nil, fmt.Errorf("account: token: %w", err)
	}
	token = base64.RawURLEncoding.EncodeToString(raw)
	sum := sha256.Sum256([]byte(token))
	return token, sum[:], nil
}
