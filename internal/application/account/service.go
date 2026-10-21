package account

import (
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

	"finga/internal/model"
	"finga/pkg/auth"
	"finga/pkg/auth/password"
	"finga/pkg/ratelimit"
)

// Options tune the service. Zero values take the defaults below.
type Options struct {
	SessionTTL         time.Duration    // fixed lifetime until step 16 (TD-24)
	LoginRatePerMinute float64          // per username and per IP (page 4: 5 per minute)
	LoginBurst         float64          // attempts allowed at once
	Now                func() time.Time // the clock; tests inject one
	Hash               password.Params  // argon2id parameters
}

const (
	defaultSessionTTL = 7 * 24 * time.Hour
	defaultLoginRate  = 5
	defaultLoginBurst = 5

	tokenBytes      = 32
	usernameMin     = 3
	usernameMax     = 16
	passwordMin     = 8
	passwordMax     = 128
	loginKeyUser    = "user:"
	loginKeyIP      = "ip:"
	loginMaxRetries = 1 // once more, when the one_active_session index catches a race
)

var usernameRe = regexp.MustCompile(`^[A-Za-z0-9_]+$`)

// Service is the account module's use cases. It implements pkg/auth.
type Service struct {
	store      Store
	ttl        time.Duration
	now        func() time.Time
	hash       password.Params
	byUser     ratelimit.Limiter
	byIP       ratelimit.Limiter
	presetList []Preset
}

var (
	_ auth.Authenticator = (*Service)(nil)
	_ auth.Sessions      = (*Service)(nil)
)

// NewService wires the service over its store.
func NewService(store Store, opts Options) *Service {
	if opts.SessionTTL <= 0 {
		opts.SessionTTL = defaultSessionTTL
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
	perSecond := opts.LoginRatePerMinute / 60
	return &Service{
		store:      store,
		ttl:        opts.SessionTTL,
		now:        opts.Now,
		hash:       opts.Hash,
		byUser:     ratelimit.NewBuckets(perSecond, opts.LoginBurst, ratelimit.WithClock(opts.Now)),
		byIP:       ratelimit.NewBuckets(perSecond, opts.LoginBurst, ratelimit.WithClock(opts.Now)),
		presetList: []Preset{PresetClassic, PresetWASD},
	}
}

// Register creates an account. The password is hashed here; the
// repository never sees it.
func (s *Service) Register(ctx context.Context, in RegisterInput) (Account, error) {
	if len(in.Username) < usernameMin || len(in.Username) > usernameMax || !usernameRe.MatchString(in.Username) {
		return Account{}, ErrBadUsername
	}
	if len(in.Password) < passwordMin || len(in.Password) > passwordMax {
		return Account{}, ErrBadPassword
	}
	hash, err := password.Hash(in.Password, s.hash)
	if err != nil {
		return Account{}, err
	}
	a := NewAccount{ID: model.NewAccountID(), Username: in.Username, PasswordHash: hash, Settings: DefaultSettings(), CreatedAt: s.now()}
	err = s.store.InTx(ctx, func(uow UnitOfWork) error {
		return uow.Accounts().Insert(ctx, a)
	})
	if err != nil {
		return Account{}, err
	}
	return Account{ID: a.ID, Username: a.Username, Settings: a.Settings, CreatedAt: a.CreatedAt}, nil
}

// Login throttles per username and per IP, verifies the credentials, and
// issues a session. The token is returned once and never stored.
func (s *Service) Login(ctx context.Context, in LoginInput) (string, error) {
	if !s.byUser.Allow(loginKeyUser+strings.ToLower(in.Username), 1) {
		return "", ErrRateLimited
	}
	if in.IP != "" && !s.byIP.Allow(loginKeyIP+in.IP, 1) {
		return "", ErrRateLimited
	}
	id, err := s.Verify(ctx, in.Username, in.Password)
	if err != nil {
		return "", err
	}
	return s.Issue(ctx, id)
}

// Verify implements auth.Authenticator. An unknown user and a wrong
// password fail the same way, so responses don't reveal which usernames
// exist.
func (s *Service) Verify(ctx context.Context, username, pw string) (uuid.UUID, error) {
	creds, err := s.store.FindByUsername(ctx, username)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return uuid.Nil, ErrUnauthenticated
		}
		return uuid.Nil, err
	}
	ok, err := password.Verify(creds.PasswordHash, pw)
	if err != nil {
		return uuid.Nil, fmt.Errorf("account %s: %w", creds.Account.ID, err)
	}
	if !ok {
		return uuid.Nil, ErrUnauthenticated
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
	session := Session{ID: model.NewSessionID(), AccountID: model.AccountID(accountID), TokenHash: hash, CreatedAt: now, ExpiresAt: now.Add(s.ttl)}
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
		if errors.Is(err, errActiveSession) && attempt < loginMaxRetries {
			continue
		}
		if err != nil {
			return "", err
		}
		return token, nil
	}
}

// Validate implements auth.Sessions. Unknown, expired and revoked tokens
// all fail with ErrUnauthenticated.
func (s *Service) Validate(ctx context.Context, token string) (auth.Principal, error) {
	hash := sha256.Sum256([]byte(token))
	session, err := s.store.FindActiveSession(ctx, hash[:])
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return auth.Principal{}, ErrUnauthenticated
		}
		return auth.Principal{}, err
	}
	if !s.now().Before(session.ExpiresAt) {
		return auth.Principal{}, ErrUnauthenticated
	}
	return auth.Principal{AccountID: uuid.UUID(session.AccountID), SessionID: uuid.UUID(session.ID)}, nil
}

// Revoke implements auth.Sessions.
func (s *Service) Revoke(ctx context.Context, sessionID uuid.UUID) error {
	now := s.now()
	return s.store.InTx(ctx, func(uow UnitOfWork) error {
		return uow.Sessions().Revoke(ctx, model.SessionID(sessionID), now)
	})
}

// Settings reads an account's settings.
func (s *Service) Settings(ctx context.Context, id model.AccountID) (Settings, error) {
	return s.store.Settings(ctx, id)
}

// UpdateSettings saves an account's settings; the preset must be known.
func (s *Service) UpdateSettings(ctx context.Context, id model.AccountID, st Settings) error {
	known := false
	for _, p := range s.presetList {
		known = known || p == st.ControlPreset
	}
	if !known {
		return ErrBadPreset
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
