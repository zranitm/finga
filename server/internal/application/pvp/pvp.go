// Package pvp is the pvp module's facade: the queue, offers, ratings, the board and arena builds; its types and kernel errors are the module's contract.
package pvp

import (
	"context"
	"math/rand/v2"

	adapter "finga/internal/adapter/pvp"
	"finga/internal/application/pvp/service"
	repository "finga/internal/repository/pvp"
	"finga/internal/shared/facade"
	"finga/internal/shared/gamedata"
	"finga/internal/shared/host"
	"finga/internal/shared/kernel"
	"finga/pkg/hub"
)

// pushQueue bounds a gateway's stream of pvp pushes.
const pushQueue = 4096

// Module is the pvp module; every call to it goes through these methods.
type Module struct {
	calls  *facade.Calls
	svc    *service.Service
	pushes *hub.Hub[Push]
}

// Deps are what the module is built from.
type Deps struct {
	Module    *host.Module // its logs, panics, tasks, metrics and database
	Data      *gamedata.Data
	World     adapter.WorldModule
	Social    adapter.SocialModule
	Character adapter.CharacterModule
}

// New builds the module and adds its matcher and its follower of the world's outcomes; the queue lives in memory, so a restart empties it.
func New(ctx context.Context, d Deps) (*Module, error) {
	pool, dbMetrics, err := d.Module.Database(ctx)
	if err != nil {
		return nil, err
	}
	pushes := hub.New[Push](pushQueue)
	world, social := adapter.NewWorld(d.World), adapter.NewSocial(d.Social, d.Module.Panics)
	svc := service.New(service.Deps{
		Data: d.Data, World: world, Social: social, Characters: adapter.NewCharacters(d.Character), Store: repository.NewPostgres(pool, dbMetrics), Notifier: notifier{pushes}, Logger: d.Module.Logger, Panics: d.Module.Panics,
		Rand: rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64())), //nolint:gosec // bots' classes and names, not a secret
	})
	d.Module.Task("matcher", svc.Run, host.Restartable)
	d.Module.Task("parties", func(ctx context.Context) error {
		social.FollowParties(ctx, svc.PartyChanged, svc.Resync)
		return nil
	}, host.Restartable)
	d.Module.Task("outcomes", func(ctx context.Context) error { return svc.Outcomes(ctx, world.FollowResults) }, host.Restartable)
	calls, err := d.Module.Calls(errorTable)
	if err != nil {
		return nil, err
	}
	return &Module{calls: calls, svc: svc, pushes: pushes}, nil
}

// Queue puts the character in a mode's queue.
func (m *Module) Queue(ctx context.Context, p Player, mode string) (err error) {
	ctx, call := m.calls.Start(ctx, "Queue")
	defer call.End(&err)
	return m.svc.Queue(ctx, p, mode)
}

// LeaveQueue takes the character out of the queue, declining an offer standing.
func (m *Module) LeaveQueue(ctx context.Context, who kernel.CharacterID) (err error) {
	ctx, call := m.calls.Start(ctx, "LeaveQueue")
	defer call.End(&err)
	return m.svc.Leave(ctx, who)
}

// Answer accepts or declines an offer.
func (m *Module) Answer(ctx context.Context, who kernel.CharacterID, offer string, accept bool) (err error) {
	ctx, call := m.calls.Start(ctx, "Answer")
	defer call.End(&err)
	return m.svc.Answer(ctx, who, offer, accept)
}

// Offline takes a character who went offline out of the queue.
func (m *Module) Offline(ctx context.Context, who kernel.CharacterID) (err error) {
	ctx, call := m.calls.Start(ctx, "Offline")
	defer call.End(&err)
	m.svc.Offline(ctx, who)
	return nil
}

// SetBuild saves the arena build of one of the account's characters, which their match, if they're in one, fights with from its next round.
func (m *Module) SetBuild(ctx context.Context, account kernel.AccountID, id kernel.CharacterID, bar []string, traits map[string]string) (err error) {
	ctx, call := m.calls.Start(ctx, "SetBuild")
	defer call.End(&err)
	return m.svc.SetBuild(ctx, account, id, bar, traits)
}

// Profile is one of the account's characters' standing in each rated mode, and their arena build.
func (m *Module) Profile(ctx context.Context, account kernel.AccountID, id kernel.CharacterID) (_ Profile, err error) {
	ctx, call := m.calls.Start(ctx, "Profile")
	defer call.End(&err)
	return m.svc.Profile(ctx, account, id)
}

// Leaderboard is a rated mode's top placed characters, highest first.
func (m *Module) Leaderboard(ctx context.Context, mode string) (_ []Row, err error) {
	ctx, call := m.calls.Start(ctx, "Leaderboard")
	defer call.End(&err)
	return m.svc.Leaderboard(ctx, mode)
}

// Subscribe opens a gateway's pushes from now on: every player's, which the gateway hands to those it has.
func (m *Module) Subscribe(gateway string) *hub.Sub[Push] {
	return m.pushes.Subscribe(gateway)
}

// notifier sends each push to every gateway; it never blocks.
type notifier struct{ pushes *hub.Hub[Push] }

// Push sends d for to.
func (n notifier) Push(to kernel.CharacterID, d service.Delivery) {
	n.pushes.Broadcast(Push{To: to, Delivery: d})
}
