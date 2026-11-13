// Package account is the account module's facade: accounts, sessions and settings; its types and kernel errors are the module's contract.
package account

import (
	"context"

	"finga/internal/facade"
	"finga/internal/host"
	"finga/internal/kernel"
	"finga/pkg/hub"
	"finga/pkg/retention"
	"finga/pkg/retention/retentionprom"
	"finga/services/account/config"
	"finga/services/account/internal/application"
	"finga/services/account/internal/model"
	"finga/services/account/internal/password"
	"finga/services/account/internal/repository"
)

// Module is the account module; every call to it goes through these methods.
type Module struct {
	calls    *facade.Calls
	svc      service
	sessions *hub.Hub[Event]
}

// service is the module's application, as the facade calls it.
type service interface {
	Register(ctx context.Context, in application.RegisterInput) (model.Account, error)
	Login(ctx context.Context, in application.LoginInput) (string, error)
	Validate(ctx context.Context, token string) (model.Principal, error)
	Rotate(ctx context.Context, sessionID kernel.SessionID) (string, error)
	Check(ctx context.Context, sessionID kernel.SessionID) error
	Revoke(ctx context.Context, sessionID kernel.SessionID) error
	RevokeAll(ctx context.Context, accountID kernel.AccountID) error
	Settings(ctx context.Context, id kernel.AccountID) (model.Settings, error)
	UpdateSettings(ctx context.Context, id kernel.AccountID, s model.Settings) error
}

// Deps are what the module is built from.
type Deps struct {
	Module *host.Module // its logs, panics, tasks, metrics and the process's database
	Config config.Config
}

// New builds the module and adds its session retention task; it fails fast on an unreachable database.
func New(ctx context.Context, d Deps) (*Module, error) {
	pool, dbMetrics, err := d.Module.Database(ctx)
	if err != nil {
		return nil, err
	}
	retentionMetrics, err := retentionprom.New(d.Module.Registry(), "account")
	if err != nil {
		return nil, err
	}
	pw := d.Config.Password
	sessions := hub.New[Event](sessionsQueue)
	svc, err := application.NewService(application.Deps{
		Store:    repository.NewPostgres(pool, dbMetrics),
		Notifier: notifier{sessions: sessions},
		Hasher:   password.NewHasher(password.Params{Time: pw.Time, Memory: pw.MemoryKiB, Threads: pw.Threads, SaltLen: password.Default.SaltLen, KeyLen: password.Default.KeyLen}, pw.Concurrent, pw.Wait),
	})
	if err != nil {
		return nil, err
	}
	s := d.Config.Sessions
	ended := retention.Policy{Keep: s.Keep, Every: s.PruneEvery, Batch: s.PruneBatch, Gap: s.PruneGap}
	d.Module.Task("session retention", func(ctx context.Context) error {
		return retention.Run(ctx, "sessions", ended, svc.PruneSessions, retentionMetrics, d.Module.Logger)
	}, host.Restartable)
	calls, err := d.Module.Calls(errorTable)
	if err != nil {
		return nil, err
	}
	return &Module{calls: calls, svc: svc, sessions: sessions}, nil
}

// Register creates an account.
func (m *Module) Register(ctx context.Context, c Credentials) (_ Account, err error) {
	ctx, call := m.calls.Start(ctx, "Register")
	defer call.End(&err)
	a, err := m.svc.Register(ctx, application.RegisterInput{Username: c.Username, Password: c.Password})
	return accountOf(a), err
}

// Login issues a session and its first token.
func (m *Module) Login(ctx context.Context, c Credentials) (_ string, err error) {
	ctx, call := m.calls.Start(ctx, "Login")
	defer call.End(&err)
	return m.svc.Login(ctx, application.LoginInput{Username: c.Username, Password: c.Password})
}

// Validate resolves a token to its session.
func (m *Module) Validate(ctx context.Context, token string) (_ Principal, err error) {
	ctx, call := m.calls.Start(ctx, "Validate")
	defer call.End(&err)
	p, err := m.svc.Validate(ctx, token)
	return Principal(p), err
}

// Rotate issues a session's next token.
func (m *Module) Rotate(ctx context.Context, id kernel.SessionID) (_ string, err error) {
	ctx, call := m.calls.Start(ctx, "Rotate")
	defer call.End(&err)
	return m.svc.Rotate(ctx, id)
}

// Check reports whether a session is still active.
func (m *Module) Check(ctx context.Context, id kernel.SessionID) (err error) {
	ctx, call := m.calls.Start(ctx, "Check")
	defer call.End(&err)
	return m.svc.Check(ctx, id)
}

// Revoke ends one session.
func (m *Module) Revoke(ctx context.Context, id kernel.SessionID) (err error) {
	ctx, call := m.calls.Start(ctx, "Revoke")
	defer call.End(&err)
	return m.svc.Revoke(ctx, id)
}

// RevokeAll ends every session of an account.
func (m *Module) RevokeAll(ctx context.Context, id kernel.AccountID) (err error) {
	ctx, call := m.calls.Start(ctx, "RevokeAll")
	defer call.End(&err)
	return m.svc.RevokeAll(ctx, id)
}

// Settings reads an account's settings.
func (m *Module) Settings(ctx context.Context, id kernel.AccountID) (_ Settings, err error) {
	ctx, call := m.calls.Start(ctx, "Settings")
	defer call.End(&err)
	s, err := m.svc.Settings(ctx, id)
	return settingsOf(s), err
}

// UpdateSettings saves an account's settings.
func (m *Module) UpdateSettings(ctx context.Context, id kernel.AccountID, s Settings) (err error) {
	ctx, call := m.calls.Start(ctx, "UpdateSettings")
	defer call.End(&err)
	return m.svc.UpdateSettings(ctx, id, s.model())
}
