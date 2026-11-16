// Package social is the social module's facade: parties and chat; its types and kernel errors are the module's contract.
package social

import (
	"context"

	"golang.org/x/sync/semaphore"

	"finga/internal/facade"
	"finga/internal/gamedata"
	"finga/internal/host"
	"finga/internal/kernel"
	"finga/modules/social/config"
	"finga/modules/social/internal/application"
	"finga/modules/social/internal/model"
	"finga/modules/social/internal/repository"
	"finga/pkg/panics"
	"finga/pkg/retention"
	"finga/pkg/retention/retentionprom"
)

// Module is the social module; every call to it goes through these methods.
type Module struct {
	calls   *facade.Calls
	parties parties
	chat    chat
	notes   *notifier
	panics  *panics.Recorder // a sweep's, which runs on a timer
	// registering lets one Online, or one step of a gone gateway's sweep, run at a time, so a sweep never undoes a newer registration
	registering *semaphore.Weighted
}

// parties is the module's party application, as the facade calls it.
type parties interface {
	Online(ctx context.Context, m model.Member) error
	Offline(ctx context.Context, id kernel.CharacterID, conn string) error
	Invite(ctx context.Context, from, to kernel.CharacterID) (model.Invite, error)
	Respond(ctx context.Context, who kernel.CharacterID, inviteID model.InviteID, accept bool) error
	Leave(ctx context.Context, who kernel.CharacterID) error
	Kick(ctx context.Context, leader, target kernel.CharacterID) error
	Promote(ctx context.Context, leader, target kernel.CharacterID) error
	Update(ctx context.Context, id kernel.CharacterID, mapID string, class kernel.Class, level, hp, maxHP int) error
	Memberships(ctx context.Context, visit func(party model.PartyID, members []kernel.CharacterID)) error
}

// chat is the module's chat application, as the facade calls it.
type chat interface {
	Send(ctx context.Context, from model.Sender, channel model.Channel, text string) (model.Message, []kernel.CharacterID, error)
}

// Deps are what the module is built from.
type Deps struct {
	Module *host.Module // its logs, panics, tasks and metrics
	Config config.Config
	Data   *gamedata.Data // how long a chat line may be
}

// New builds the module and adds its member retention task; parties live in memory, so a restart forgets them.
func New(_ context.Context, d Deps) (*Module, error) {
	retentionMetrics, err := retentionprom.New(d.Module.Registry(), "social")
	if err != nil {
		return nil, err
	}
	notes := newNotifier()
	ps := application.NewPartyService(application.PartyDeps{Store: repository.NewMemory(), Notifier: notes})
	c := d.Config.Members
	gone := retention.Policy{Keep: c.Keep, Every: c.PruneEvery, Batch: c.PruneBatch, Gap: c.PruneGap}
	d.Module.Task("member retention", func(ctx context.Context) error {
		return retention.Run(ctx, "members", gone, ps.PruneMembers, retentionMetrics, d.Module.Logger)
	}, host.Restartable)
	calls, err := d.Module.Calls(errorTable)
	if err != nil {
		return nil, err
	}
	return &Module{
		calls:       calls,
		parties:     ps,
		chat:        application.NewChatService(application.ChatDeps{Parties: ps, LineMax: d.Data.ChatLineMax()}),
		notes:       notes,
		panics:      d.Module.Panics,
		registering: semaphore.NewWeighted(1),
	}, nil
}

// Online records a character as connected through a gateway; one from an older join than theirs changes nothing.
func (m *Module) Online(ctx context.Context, p Presence) (err error) {
	ctx, call := m.calls.Start(ctx, "Online")
	defer call.End(&err)
	if err = m.registering.Acquire(ctx, 1); err != nil {
		return err
	}
	defer m.registering.Release(1)
	return m.parties.Online(ctx, p.model())
}

// Offline marks a character disconnected, unless they reconnected through another connection since.
func (m *Module) Offline(ctx context.Context, id kernel.CharacterID, conn string) (err error) {
	ctx, call := m.calls.Start(ctx, "Offline")
	defer call.End(&err)
	return m.parties.Offline(ctx, id, conn)
}

// Invite asks a character to join the inviter's party.
func (m *Module) Invite(ctx context.Context, from, to kernel.CharacterID) (_ InviteID, err error) {
	ctx, call := m.calls.Start(ctx, "Invite")
	defer call.End(&err)
	inv, err := m.parties.Invite(ctx, from, to)
	return inv.ID, err
}

// Respond accepts or declines an invite.
func (m *Module) Respond(ctx context.Context, who kernel.CharacterID, invite InviteID, accept bool) (err error) {
	ctx, call := m.calls.Start(ctx, "Respond")
	defer call.End(&err)
	return m.parties.Respond(ctx, who, invite, accept)
}

// LeaveParty takes a character out of their party.
func (m *Module) LeaveParty(ctx context.Context, who kernel.CharacterID) (err error) {
	ctx, call := m.calls.Start(ctx, "LeaveParty")
	defer call.End(&err)
	return m.parties.Leave(ctx, who)
}

// Kick removes a member; only the leader may.
func (m *Module) Kick(ctx context.Context, leader, target kernel.CharacterID) (err error) {
	ctx, call := m.calls.Start(ctx, "Kick")
	defer call.End(&err)
	return m.parties.Kick(ctx, leader, target)
}

// Promote hands leadership to another member.
func (m *Module) Promote(ctx context.Context, leader, target kernel.CharacterID) (err error) {
	ctx, call := m.calls.Start(ctx, "Promote")
	defer call.End(&err)
	return m.parties.Promote(ctx, leader, target)
}

// Chat cleans and rate-limits a line, and delivers a party line to every member; a map line goes back to the caller.
func (m *Module) Chat(ctx context.Context, l Line) (_ Message, err error) {
	ctx, call := m.calls.Start(ctx, "Chat")
	defer call.End(&err)
	msg, to, err := m.chat.Send(ctx, model.Sender{ID: l.From, Name: l.FromName}, model.Channel(l.Channel), l.Text)
	if err != nil {
		return Message{}, err
	}
	for _, id := range to {
		m.notes.send(id, ChatLine{To: id, Sender: msg.Sender, SenderName: msg.SenderName, Text: msg.Text})
	}
	return messageOf(msg), nil
}

// UpdateMember records what the world saw of a member.
func (m *Module) UpdateMember(ctx context.Context, u MemberUpdate) (err error) {
	ctx, call := m.calls.Start(ctx, "UpdateMember")
	defer call.End(&err)
	return m.parties.Update(ctx, u.ID, u.MapID, u.Class, u.Level, u.HP, u.MaxHP)
}

// Memberships visits every party with its members, for a world's first view of them.
func (m *Module) Memberships(ctx context.Context, visit func(party PartyID, members []kernel.CharacterID)) (err error) {
	ctx, call := m.calls.Start(ctx, "Memberships")
	defer call.End(&err)
	return m.parties.Memberships(ctx, visit)
}
