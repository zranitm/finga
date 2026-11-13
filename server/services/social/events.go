package social

import (
	"context"
	"sync"

	"finga/internal/kernel"
	"finga/pkg/hub"
	"finga/services/social/internal/application"
	"finga/services/social/internal/model"
)

// Queue sizes for a slow gateway or world before its subscription is closed.
const (
	deliveriesQueue  = 4096
	membershipsQueue = 4096
)

// Delivery is what a gateway hears: a PartyState, an Invited or a ChatLine for one of its players, or a BossAppeared for all of them.
//
//sumtype:decl
type Delivery interface{ delivery() }

// PartyState is a player's party as it is now; Party is nil for none.
type PartyState struct {
	To    kernel.CharacterID
	Party *Party
}

// Party is a party as its members see it: the leader and the members in join order.
type Party struct {
	Leader  kernel.CharacterID
	Members []PartyMember
}

// PartyMember is a member as the party sees them, online or not.
type PartyMember struct {
	Member
	Online bool
}

// Invited is an invite for a player.
type Invited struct {
	To       kernel.CharacterID
	Invite   InviteID
	From     kernel.CharacterID
	FromName string
}

// ChatLine is a party line for one of its members.
type ChatLine struct {
	To         kernel.CharacterID
	Sender     kernel.CharacterID
	SenderName string
	Text       string
}

// Announcement is news for every player online, which each gateway hears once.
type Announcement interface {
	Delivery
	announcement()
}

// BossAppeared says a field boss appeared on a map.
type BossAppeared struct {
	Boss  string // its mob type
	MapID string
}

func (PartyState) delivery()       {}
func (Invited) delivery()          {}
func (ChatLine) delivery()         {}
func (BossAppeared) delivery()     {}
func (BossAppeared) announcement() {}

// MembershipEvent is what a world hears: a Membership.
//
//sumtype:decl
type MembershipEvent interface{ membershipEvent() }

// Membership sets each of Characters' party to Members under Party; no members and the zero Party for none. Each sets parties outright, so an overlap is harmless.
type Membership struct {
	Characters []kernel.CharacterID
	Members    []kernel.CharacterID
	Party      PartyID
}

func (Membership) membershipEvent() {}

// Subscribe opens a gateway's deliveries from now on; a newer one under the same ID closes it, as a full queue does.
func (m *Module) Subscribe(gateway string) *hub.Sub[Delivery] {
	return m.notes.deliveries.Subscribe(gateway)
}

// SubscribeMemberships opens a world's membership changes from now on, to follow a snapshot from Memberships taken after it.
func (m *Module) SubscribeMemberships(world string) *hub.Sub[MembershipEvent] {
	return m.notes.memberships.Subscribe(world)
}

// Announce sends news once to each gateway anyone is online through, which hands it to every player it has: a burst that grows with gateways, not players.
func (m *Module) Announce(ctx context.Context, a Announcement) (err error) {
	_, call := m.calls.Start(ctx, "Announce")
	defer call.End(&err)
	m.notes.announce(a)
	return nil
}

// notifier hands the party module's notes to the subscribed gateways and worlds, each player's to the gateway they are online through; it never blocks.
type notifier struct {
	deliveries  *hub.Hub[Delivery]
	memberships *hub.Hub[MembershipEvent]

	mu       sync.Mutex
	presence map[kernel.CharacterID]presence
}

// presence is where a character is online: the gateway and the connection.
type presence struct{ gateway, conn string }

var _ application.Notifier = (*notifier)(nil)

func newNotifier() *notifier {
	return &notifier{
		deliveries:  hub.New[Delivery](deliveriesQueue),
		memberships: hub.New[MembershipEvent](membershipsQueue),
		presence:    map[kernel.CharacterID]presence{},
	}
}

// PartyChanged tells each member the party's state, and every world its membership; it runs under the party lock.
func (n *notifier) PartyChanged(members []kernel.CharacterID, p *model.Party) {
	party := partyOf(p)
	for _, id := range members {
		n.send(id, PartyState{To: id, Party: party})
	}
	change := Membership{Characters: members}
	if p != nil {
		for _, m := range p.Members {
			change.Members = append(change.Members, m.ID)
		}
		change.Party = p.ID
	}
	n.memberships.Broadcast(change)
}

// Alone tells the member they are in no party; the worlds already hold them in none.
func (n *notifier) Alone(id kernel.CharacterID) { n.send(id, PartyState{To: id}) }

// Invited tells the invitee.
func (n *notifier) Invited(to kernel.CharacterID, inv model.Invite) {
	n.send(to, Invited{To: to, Invite: inv.ID, From: inv.From, FromName: inv.FromName})
}

// Reachable routes the character's deliveries to the gateway; it runs under the party lock, which decided the member is online there.
func (n *notifier) Reachable(id kernel.CharacterID, gateway, conn string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.presence[id] = presence{gateway, conn}
}

// Unreachable forgets where the character is, if still through conn.
func (n *notifier) Unreachable(id kernel.CharacterID, conn string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.presence[id].conn == conn {
		delete(n.presence, id)
	}
}

// announce sends a once to each gateway anyone is online through, so it never fills one's queue.
func (n *notifier) announce(a Announcement) {
	n.mu.Lock()
	gateways := map[string]bool{}
	for _, p := range n.presence {
		gateways[p.gateway] = true
	}
	n.mu.Unlock()
	for gateway := range gateways {
		if sub, ok := n.deliveries.Get(gateway); ok {
			sub.Send(a)
		}
	}
}

// through is each character online through the gateway, with its connection.
func (n *notifier) through(gateway string) map[kernel.CharacterID]string {
	n.mu.Lock()
	defer n.mu.Unlock()
	out := map[kernel.CharacterID]string{}
	for id, p := range n.presence {
		if p.gateway == gateway {
			out[id] = p.conn
		}
	}
	return out
}

// subscribed is whether the gateway has a subscription open.
func (n *notifier) subscribed(gateway string) bool {
	_, ok := n.deliveries.Get(gateway)
	return ok
}

// send delivers d to the character's gateway, if they are online there and it has a subscription.
func (n *notifier) send(to kernel.CharacterID, d Delivery) {
	n.mu.Lock()
	p, ok := n.presence[to]
	n.mu.Unlock()
	if !ok {
		return
	}
	if sub, ok := n.deliveries.Get(p.gateway); ok {
		sub.Send(d)
	}
}

func partyOf(p *model.Party) *Party {
	if p == nil {
		return nil
	}
	out := &Party{Leader: p.Leader, Members: make([]PartyMember, 0, len(p.Members))}
	for _, m := range p.Members {
		out.Members = append(out.Members, PartyMember{ID: m.ID, Name: m.Name, Class: m.Class, Level: m.Level, HP: m.HP, MaxHP: m.MaxHP, MapID: m.MapID, Online: m.Online})
	}
	return out
}
