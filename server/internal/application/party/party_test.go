package party_test

import (
	"errors"
	"sync"
	"testing"
	"time"

	"finga/internal/application/party"
	"finga/internal/model"
)

// recorder keeps every notification in order.
type recorder struct {
	mu      sync.Mutex
	states  []stateCall
	invites []party.Invite
}

type stateCall struct {
	to    []model.CharacterID
	party *party.Party
}

func (r *recorder) PartyChanged(members []model.CharacterID, p *party.Party) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.states = append(r.states, stateCall{to: members, party: p})
}

func (r *recorder) Invited(_ model.CharacterID, inv party.Invite) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.invites = append(r.invites, inv)
}

func (r *recorder) last() stateCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.states[len(r.states)-1]
}

func (r *recorder) reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.states, r.invites = nil, nil
}

type fixture struct {
	svc  *party.Service
	rec  *recorder
	now  time.Time
	ids  map[string]model.CharacterID
	name map[model.CharacterID]string
}

func newFixture(t *testing.T, names ...string) *fixture {
	t.Helper()
	f := &fixture{rec: &recorder{}, now: time.Unix(1_700_000_000, 0), ids: map[string]model.CharacterID{}, name: map[model.CharacterID]string{}}
	f.svc = party.NewService(party.Options{Clock: func() time.Time { return f.now }})
	f.svc.Notify(f.rec)
	for k, n := range names {
		id := model.NewCharacterID()
		f.ids[n] = id
		f.name[id] = n
		f.svc.Online(party.Member{ID: id, Name: n, Level: 1 + k, HP: 100, MaxHP: 100, MapID: "forest"})
	}
	return f
}

func (f *fixture) id(n string) model.CharacterID { return f.ids[n] }

// invite sends and accepts in one go.
func (f *fixture) invite(t *testing.T, from, to string) {
	t.Helper()
	inv, err := f.svc.Invite(f.id(from), f.id(to))
	if err != nil {
		t.Fatalf("%s invites %s: %v", from, to, err)
	}
	if err := f.svc.Respond(f.id(to), inv.ID, true); err != nil {
		t.Fatalf("%s accepts: %v", to, err)
	}
}

func (f *fixture) names(ids []model.CharacterID) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, f.name[id])
	}
	return out
}

func (f *fixture) members(t *testing.T, of string) (leader string, members []string) {
	t.Helper()
	p, ok := f.svc.Of(f.id(of))
	if !ok {
		return "", nil
	}
	for _, m := range p.Members {
		members = append(members, m.Name)
	}
	return f.name[p.Leader], members
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if a[k] != b[k] {
			return false
		}
	}
	return true
}

func TestPartyLifecycle(t *testing.T) {
	f := newFixture(t, "Ayla", "Robin", "Cass", "Dinh", "Eve")

	t.Run("an invite reaches the target and nothing exists until it is accepted", func(t *testing.T) {
		inv, err := f.svc.Invite(f.id("Ayla"), f.id("Robin"))
		if err != nil || inv.From != f.id("Ayla") || inv.To != f.id("Robin") || inv.FromName != "Ayla" || !inv.Expires.Equal(f.now.Add(30*time.Second)) {
			t.Fatalf("invite %+v, %v", inv, err)
		}
		if len(f.rec.invites) != 1 || f.rec.invites[0].ID != inv.ID {
			t.Fatalf("notified invites %+v", f.rec.invites)
		}
		if _, ok := f.svc.Of(f.id("Ayla")); ok {
			t.Fatal("a party exists before anyone accepted")
		}
		if _, err := f.svc.Invite(f.id("Ayla"), f.id("Robin")); !errors.Is(err, party.ErrAlreadyAsked) {
			t.Fatalf("second invite: %v", err)
		}
		// Declining removes the invite; a later response finds nothing.
		if err := f.svc.Respond(f.id("Robin"), inv.ID, false); err != nil {
			t.Fatal(err)
		}
		if err := f.svc.Respond(f.id("Robin"), inv.ID, true); !errors.Is(err, party.ErrInviteExpired) {
			t.Fatalf("responding twice: %v", err)
		}
	})
	t.Run("accepting creates the party with the inviter as leader and tells both", func(t *testing.T) {
		f.rec.reset()
		f.invite(t, "Ayla", "Robin")
		leader, members := f.members(t, "Robin")
		if leader != "Ayla" || !equal(members, []string{"Ayla", "Robin"}) {
			t.Fatalf("leader %s members %v", leader, members)
		}
		last := f.rec.last()
		if !equal(f.names(last.to), []string{"Ayla", "Robin"}) || last.party == nil || last.party.Leader != f.id("Ayla") || last.party.Members[1].Level != 2 {
			t.Fatalf("notified %+v", last)
		}
	})
	t.Run("only the leader invites; a member of another party cannot be invited", func(t *testing.T) {
		if _, err := f.svc.Invite(f.id("Robin"), f.id("Cass")); !errors.Is(err, party.ErrNotLeader) {
			t.Fatalf("member invites: %v", err)
		}
		f.invite(t, "Cass", "Dinh")
		if _, err := f.svc.Invite(f.id("Ayla"), f.id("Dinh")); !errors.Is(err, party.ErrInParty) {
			t.Fatalf("inviting a member of another party: %v", err)
		}
		if _, err := f.svc.Invite(f.id("Ayla"), f.id("Ayla")); !errors.Is(err, party.ErrSelf) {
			t.Fatalf("self invite: %v", err)
		}
	})
	t.Run("a fifth member is refused", func(t *testing.T) {
		if err := f.svc.Leave(f.id("Dinh")); err != nil {
			t.Fatal(err)
		}
		if err := f.svc.Leave(f.id("Cass")); err != nil {
			t.Fatal(err)
		}
		f.invite(t, "Ayla", "Cass")
		f.invite(t, "Ayla", "Dinh")
		if _, err := f.svc.Invite(f.id("Ayla"), f.id("Eve")); !errors.Is(err, party.ErrFull) {
			t.Fatalf("fifth: %v", err)
		}
	})
	t.Run("kick: leader only, not themselves, only members", func(t *testing.T) {
		if err := f.svc.Kick(f.id("Robin"), f.id("Cass")); !errors.Is(err, party.ErrNotLeader) {
			t.Fatalf("member kicks: %v", err)
		}
		if err := f.svc.Kick(f.id("Ayla"), f.id("Ayla")); !errors.Is(err, party.ErrNotMember) {
			t.Fatalf("self kick: %v", err)
		}
		if err := f.svc.Kick(f.id("Ayla"), f.id("Eve")); !errors.Is(err, party.ErrNotMember) {
			t.Fatalf("kicking a stranger: %v", err)
		}
		f.rec.reset()
		if err := f.svc.Kick(f.id("Ayla"), f.id("Dinh")); err != nil {
			t.Fatal(err)
		}
		if _, ok := f.svc.Of(f.id("Dinh")); ok {
			t.Fatal("Dinh still in a party")
		}
		if n := len(f.rec.states); n != 2 || f.rec.states[0].party != nil || f.names(f.rec.states[0].to)[0] != "Dinh" || !equal(f.names(f.rec.states[1].to), []string{"Ayla", "Robin", "Cass"}) {
			t.Fatalf("notifications %+v", f.rec.states)
		}
	})
	t.Run("promote hands the lead over", func(t *testing.T) {
		if err := f.svc.Promote(f.id("Robin"), f.id("Cass")); !errors.Is(err, party.ErrNotLeader) {
			t.Fatalf("member promotes: %v", err)
		}
		if err := f.svc.Promote(f.id("Ayla"), f.id("Cass")); err != nil {
			t.Fatal(err)
		}
		if leader, _ := f.members(t, "Ayla"); leader != "Cass" {
			t.Fatalf("leader %s", leader)
		}
		if _, err := f.svc.Invite(f.id("Ayla"), f.id("Eve")); !errors.Is(err, party.ErrNotLeader) {
			t.Fatal("the old leader can still invite")
		}
	})
	t.Run("when the leader leaves, the longest-standing member leads", func(t *testing.T) {
		if err := f.svc.Leave(f.id("Cass")); err != nil {
			t.Fatal(err)
		}
		leader, members := f.members(t, "Ayla")
		if leader != "Ayla" || !equal(members, []string{"Ayla", "Robin"}) {
			t.Fatalf("leader %s members %v", leader, members)
		}
		// Robin joined before Cass did; Cass rejoins, then Ayla leaves:
		// Robin leads, not Cass.
		f.invite(t, "Ayla", "Cass")
		if err := f.svc.Leave(f.id("Ayla")); err != nil {
			t.Fatal(err)
		}
		if leader, members := f.members(t, "Cass"); leader != "Robin" || !equal(members, []string{"Robin", "Cass"}) {
			t.Fatalf("leader %s members %v", leader, members)
		}
	})
	t.Run("the party disbands when empty", func(t *testing.T) {
		if err := f.svc.Leave(f.id("Robin")); err != nil {
			t.Fatal(err)
		}
		if leader, members := f.members(t, "Cass"); leader != "Cass" || len(members) != 1 {
			t.Fatalf("leader %s members %v", leader, members)
		}
		f.rec.reset()
		if err := f.svc.Leave(f.id("Cass")); err != nil {
			t.Fatal(err)
		}
		if _, ok := f.svc.Of(f.id("Cass")); ok {
			t.Fatal("a party of nobody")
		}
		if err := f.svc.Leave(f.id("Cass")); !errors.Is(err, party.ErrNoParty) {
			t.Fatalf("leaving twice: %v", err)
		}
		if len(f.rec.states) != 1 || f.rec.states[0].party != nil {
			t.Fatalf("notifications %+v", f.rec.states)
		}
	})
}

func TestInvitesExpireAndNeedTheSameMap(t *testing.T) {
	f := newFixture(t, "Ayla", "Robin", "Cass")
	inv, err := f.svc.Invite(f.id("Ayla"), f.id("Robin"))
	if err != nil {
		t.Fatal(err)
	}
	f.now = f.now.Add(30 * time.Second)
	if err := f.svc.Respond(f.id("Robin"), inv.ID, true); !errors.Is(err, party.ErrInviteExpired) {
		t.Fatalf("after 30 s: %v", err)
	}
	inv, err = f.svc.Invite(f.id("Ayla"), f.id("Robin"))
	if err != nil {
		t.Fatalf("a new invite after the old one expired: %v", err)
	}
	if err := f.svc.Respond(f.id("Cass"), inv.ID, true); !errors.Is(err, party.ErrInviteExpired) {
		t.Fatalf("someone else's invite: %v", err)
	}
	f.now = f.now.Add(29 * time.Second)
	if err := f.svc.Respond(f.id("Robin"), inv.ID, true); err != nil {
		t.Fatalf("at 29 s: %v", err)
	}
	// Cass is on another map: not invitable; back on the forest she is.
	f.svc.Update(f.id("Cass"), "town", 3, 50, 100)
	if _, err := f.svc.Invite(f.id("Ayla"), f.id("Cass")); !errors.Is(err, party.ErrNotHere) {
		t.Fatalf("another map: %v", err)
	}
	f.svc.Update(f.id("Cass"), "forest", 3, 50, 100)
	if _, err := f.svc.Invite(f.id("Ayla"), f.id("Cass")); err != nil {
		t.Fatalf("same map: %v", err)
	}
	// Offline characters cannot be invited either.
	f.svc.Offline(f.id("Cass"))
	f.now = f.now.Add(time.Minute)
	if _, err := f.svc.Invite(f.id("Ayla"), f.id("Cass")); !errors.Is(err, party.ErrNotHere) {
		t.Fatalf("offline: %v", err)
	}
	if _, err := f.svc.Invite(f.id("Ayla"), model.NewCharacterID()); !errors.Is(err, party.ErrNotHere) {
		t.Fatalf("unknown: %v", err)
	}
}

func TestMembersStayThroughDisconnectsAndStateFollowsThem(t *testing.T) {
	f := newFixture(t, "Ayla", "Robin")
	f.invite(t, "Ayla", "Robin")
	f.rec.reset()
	f.svc.Offline(f.id("Robin"))
	last := f.rec.last()
	if last.party == nil || last.party.Members[1].Online || !equal(f.names(last.to), []string{"Ayla", "Robin"}) {
		t.Fatalf("after Robin went offline: %+v", last)
	}
	if !f.svc.SameParty(f.id("Ayla"), f.id("Robin")) || len(f.svc.Members(f.id("Robin"))) != 2 {
		t.Fatal("Robin left the party by disconnecting")
	}
	// Reconnecting on another map at a new level: the row follows.
	f.svc.Online(party.Member{ID: f.id("Robin"), Name: "Robin", Level: 5, HP: 10, MaxHP: 300, MapID: "cave"})
	last = f.rec.last()
	if m := last.party.Members[1]; !m.Online || m.Level != 5 || m.MapID != "cave" || m.HP != 10 || m.MaxHP != 300 {
		t.Fatalf("after Robin came back: %+v", m)
	}
	// The world reports a level-up and a map change.
	f.svc.Update(f.id("Ayla"), "cave", 2, 250, 250)
	if m := f.rec.last().party.Members[0]; m.Level != 2 || m.MapID != "cave" || m.HP != 250 {
		t.Fatalf("after Ayla's update: %+v", m)
	}
	// Strangers: no party, no members, not the same party.
	cass := model.NewCharacterID()
	if f.svc.Members(cass) != nil || f.svc.SameParty(f.id("Ayla"), cass) {
		t.Fatal("a stranger has a party")
	}
	f.svc.Update(cass, "cave", 1, 1, 1) // unknown: ignored
	f.svc.Offline(cass)
}
