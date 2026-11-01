package application_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"finga/internal/kernel"
	"finga/services/social/internal/application"
	"finga/services/social/internal/model"
)

type recorder struct {
	mu      sync.Mutex
	states  []stateCall
	invites []model.Invite
}

type stateCall struct {
	to    []kernel.CharacterID
	party *model.Party
}

func (r *recorder) PartyChanged(members []kernel.CharacterID, p *model.Party) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.states = append(r.states, stateCall{to: members, party: p})
}

func (r *recorder) Invited(_ kernel.CharacterID, inv model.Invite) {
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
	ctx   context.Context
	store *fakeStore
	svc   *application.PartyService
	rec   *recorder
	now   time.Time
	ids   map[string]kernel.CharacterID
	name  map[kernel.CharacterID]string
}

func newFixture(t *testing.T, names ...string) *fixture {
	t.Helper()
	f := &fixture{ctx: t.Context(), store: newFakeStore(), rec: &recorder{}, now: time.Unix(1_700_000_000, 0), ids: map[string]kernel.CharacterID{}, name: map[kernel.CharacterID]string{}}
	f.svc = application.NewPartyService(f.store, application.PartyOptions{Clock: func() time.Time { return f.now }})
	f.svc.Notify(f.rec)
	for k, n := range names {
		id := kernel.NewCharacterID()
		f.ids[n] = id
		f.name[id] = n
		must(t, f.svc.Online(f.ctx, model.Member{ID: id, Name: n, Level: 1 + k, HP: 100, MaxHP: 100, MapID: "forest"}))
	}
	return f
}

func (f *fixture) id(n string) kernel.CharacterID { return f.ids[n] }

func (f *fixture) of(t *testing.T, id kernel.CharacterID) (model.Party, bool) {
	t.Helper()
	p, ok, err := f.svc.Of(f.ctx, id)
	must(t, err)
	return p, ok
}

func (f *fixture) sameParty(t *testing.T, a, b kernel.CharacterID) bool {
	t.Helper()
	same, err := f.svc.SameParty(f.ctx, a, b)
	must(t, err)
	return same
}

func (f *fixture) membersOf(t *testing.T, id kernel.CharacterID) []kernel.CharacterID {
	t.Helper()
	ids, err := f.svc.Members(f.ctx, id)
	must(t, err)
	return ids
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func (f *fixture) invite(t *testing.T, from, to string) {
	t.Helper()
	inv, err := f.svc.Invite(f.ctx, f.id(from), f.id(to))
	if err != nil {
		t.Fatalf("%s invites %s: %v", from, to, err)
	}
	if err := f.svc.Respond(f.ctx, f.id(to), inv.ID, true); err != nil {
		t.Fatalf("%s accepts: %v", to, err)
	}
}

func (f *fixture) names(ids []kernel.CharacterID) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, f.name[id])
	}
	return out
}

func (f *fixture) members(t *testing.T, of string) (leader string, members []string) {
	t.Helper()
	p, ok := f.of(t, f.id(of))
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
		inv, err := f.svc.Invite(f.ctx, f.id("Ayla"), f.id("Robin"))
		if err != nil || inv.From != f.id("Ayla") || inv.To != f.id("Robin") || inv.FromName != "Ayla" || !inv.Expires.Equal(f.now.Add(30*time.Second)) {
			t.Fatalf("invite %+v, %v", inv, err)
		}
		if len(f.rec.invites) != 1 || f.rec.invites[0].ID != inv.ID {
			t.Fatalf("notified invites %+v", f.rec.invites)
		}
		if _, ok := f.of(t, f.id("Ayla")); ok {
			t.Fatal("a party exists before anyone accepted")
		}
		if _, err := f.svc.Invite(f.ctx, f.id("Ayla"), f.id("Robin")); !errors.Is(err, model.ErrAlreadyAsked) {
			t.Fatalf("second invite: %v", err)
		}
		// Declining removes the invite; a later response finds nothing.
		if err := f.svc.Respond(f.ctx, f.id("Robin"), inv.ID, false); err != nil {
			t.Fatal(err)
		}
		if err := f.svc.Respond(f.ctx, f.id("Robin"), inv.ID, true); !errors.Is(err, model.ErrInviteExpired) {
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
		if _, err := f.svc.Invite(f.ctx, f.id("Robin"), f.id("Cass")); !errors.Is(err, model.ErrNotLeader) {
			t.Fatalf("member invites: %v", err)
		}
		f.invite(t, "Cass", "Dinh")
		if _, err := f.svc.Invite(f.ctx, f.id("Ayla"), f.id("Dinh")); !errors.Is(err, model.ErrInParty) {
			t.Fatalf("inviting a member of another party: %v", err)
		}
		if _, err := f.svc.Invite(f.ctx, f.id("Ayla"), f.id("Ayla")); !errors.Is(err, model.ErrSelf) {
			t.Fatalf("self invite: %v", err)
		}
	})
	t.Run("a fifth member is refused", func(t *testing.T) {
		if err := f.svc.Leave(f.ctx, f.id("Dinh")); err != nil {
			t.Fatal(err)
		}
		if err := f.svc.Leave(f.ctx, f.id("Cass")); err != nil {
			t.Fatal(err)
		}
		f.invite(t, "Ayla", "Cass")
		f.invite(t, "Ayla", "Dinh")
		if _, err := f.svc.Invite(f.ctx, f.id("Ayla"), f.id("Eve")); !errors.Is(err, model.ErrFull) {
			t.Fatalf("fifth: %v", err)
		}
	})
	t.Run("kick: leader only, not themselves, only members", func(t *testing.T) {
		if err := f.svc.Kick(f.ctx, f.id("Robin"), f.id("Cass")); !errors.Is(err, model.ErrNotLeader) {
			t.Fatalf("member kicks: %v", err)
		}
		if err := f.svc.Kick(f.ctx, f.id("Ayla"), f.id("Ayla")); !errors.Is(err, model.ErrNotMember) {
			t.Fatalf("self kick: %v", err)
		}
		if err := f.svc.Kick(f.ctx, f.id("Ayla"), f.id("Eve")); !errors.Is(err, model.ErrNotMember) {
			t.Fatalf("kicking a stranger: %v", err)
		}
		f.rec.reset()
		if err := f.svc.Kick(f.ctx, f.id("Ayla"), f.id("Dinh")); err != nil {
			t.Fatal(err)
		}
		if _, ok := f.of(t, f.id("Dinh")); ok {
			t.Fatal("Dinh still in a party")
		}
		if n := len(f.rec.states); n != 2 || f.rec.states[0].party != nil || f.names(f.rec.states[0].to)[0] != "Dinh" || !equal(f.names(f.rec.states[1].to), []string{"Ayla", "Robin", "Cass"}) {
			t.Fatalf("notifications %+v", f.rec.states)
		}
	})
	t.Run("promote hands the lead over", func(t *testing.T) {
		if err := f.svc.Promote(f.ctx, f.id("Robin"), f.id("Cass")); !errors.Is(err, model.ErrNotLeader) {
			t.Fatalf("member promotes: %v", err)
		}
		if err := f.svc.Promote(f.ctx, f.id("Ayla"), f.id("Cass")); err != nil {
			t.Fatal(err)
		}
		if leader, _ := f.members(t, "Ayla"); leader != "Cass" {
			t.Fatalf("leader %s", leader)
		}
		if _, err := f.svc.Invite(f.ctx, f.id("Ayla"), f.id("Eve")); !errors.Is(err, model.ErrNotLeader) {
			t.Fatal("the old leader can still invite")
		}
	})
	t.Run("when the leader leaves, the longest-standing member leads", func(t *testing.T) {
		if err := f.svc.Leave(f.ctx, f.id("Cass")); err != nil {
			t.Fatal(err)
		}
		leader, members := f.members(t, "Ayla")
		if leader != "Ayla" || !equal(members, []string{"Ayla", "Robin"}) {
			t.Fatalf("leader %s members %v", leader, members)
		}
		// Robin joined before Cass rejoined, so Robin leads when Ayla leaves.
		f.invite(t, "Ayla", "Cass")
		if err := f.svc.Leave(f.ctx, f.id("Ayla")); err != nil {
			t.Fatal(err)
		}
		if leader, members := f.members(t, "Cass"); leader != "Robin" || !equal(members, []string{"Robin", "Cass"}) {
			t.Fatalf("leader %s members %v", leader, members)
		}
	})
	t.Run("the party disbands when empty", func(t *testing.T) {
		if err := f.svc.Leave(f.ctx, f.id("Robin")); err != nil {
			t.Fatal(err)
		}
		if leader, members := f.members(t, "Cass"); leader != "Cass" || len(members) != 1 {
			t.Fatalf("leader %s members %v", leader, members)
		}
		f.rec.reset()
		if err := f.svc.Leave(f.ctx, f.id("Cass")); err != nil {
			t.Fatal(err)
		}
		if _, ok := f.of(t, f.id("Cass")); ok {
			t.Fatal("a party of nobody")
		}
		if err := f.svc.Leave(f.ctx, f.id("Cass")); !errors.Is(err, model.ErrNoParty) {
			t.Fatalf("leaving twice: %v", err)
		}
		if len(f.rec.states) != 1 || f.rec.states[0].party != nil {
			t.Fatalf("notifications %+v", f.rec.states)
		}
	})
}

func TestInvitesExpireAndNeedTheSameMap(t *testing.T) {
	f := newFixture(t, "Ayla", "Robin", "Cass")
	inv, err := f.svc.Invite(f.ctx, f.id("Ayla"), f.id("Robin"))
	if err != nil {
		t.Fatal(err)
	}
	f.now = f.now.Add(30 * time.Second)
	if err := f.svc.Respond(f.ctx, f.id("Robin"), inv.ID, true); !errors.Is(err, model.ErrInviteExpired) {
		t.Fatalf("after 30 s: %v", err)
	}
	inv, err = f.svc.Invite(f.ctx, f.id("Ayla"), f.id("Robin"))
	if err != nil {
		t.Fatalf("a new invite after the old one expired: %v", err)
	}
	if err := f.svc.Respond(f.ctx, f.id("Cass"), inv.ID, true); !errors.Is(err, model.ErrInviteExpired) {
		t.Fatalf("someone else's invite: %v", err)
	}
	f.now = f.now.Add(29 * time.Second)
	if err := f.svc.Respond(f.ctx, f.id("Robin"), inv.ID, true); err != nil {
		t.Fatalf("at 29 s: %v", err)
	}
	// Cass is on another map: not invitable; back on the forest she is.
	must(t, f.svc.Update(f.ctx, f.id("Cass"), "town", 3, 50, 100))
	if _, err := f.svc.Invite(f.ctx, f.id("Ayla"), f.id("Cass")); !errors.Is(err, model.ErrNotHere) {
		t.Fatalf("another map: %v", err)
	}
	must(t, f.svc.Update(f.ctx, f.id("Cass"), "forest", 3, 50, 100))
	if _, err := f.svc.Invite(f.ctx, f.id("Ayla"), f.id("Cass")); err != nil {
		t.Fatalf("same map: %v", err)
	}
	must(t, f.svc.Offline(f.ctx, f.id("Cass")))
	f.now = f.now.Add(time.Minute)
	if _, err := f.svc.Invite(f.ctx, f.id("Ayla"), f.id("Cass")); !errors.Is(err, model.ErrNotHere) {
		t.Fatalf("offline: %v", err)
	}
	if _, err := f.svc.Invite(f.ctx, f.id("Ayla"), kernel.NewCharacterID()); !errors.Is(err, model.ErrNotHere) {
		t.Fatalf("unknown: %v", err)
	}
}

func TestMembersStayThroughDisconnectsAndStateFollowsThem(t *testing.T) {
	f := newFixture(t, "Ayla", "Robin")
	f.invite(t, "Ayla", "Robin")
	f.rec.reset()
	must(t, f.svc.Offline(f.ctx, f.id("Robin")))
	last := f.rec.last()
	if last.party == nil || last.party.Members[1].Online || !equal(f.names(last.to), []string{"Ayla", "Robin"}) {
		t.Fatalf("after Robin went offline: %+v", last)
	}
	if !f.sameParty(t, f.id("Ayla"), f.id("Robin")) || len(f.membersOf(t, f.id("Robin"))) != 2 {
		t.Fatal("Robin left the party by disconnecting")
	}
	// Reconnecting on another map at a new level: the row follows.
	must(t, f.svc.Online(f.ctx, model.Member{ID: f.id("Robin"), Name: "Robin", Level: 5, HP: 10, MaxHP: 300, MapID: "cave"}))
	last = f.rec.last()
	if m := last.party.Members[1]; !m.Online || m.Level != 5 || m.MapID != "cave" || m.HP != 10 || m.MaxHP != 300 {
		t.Fatalf("after Robin came back: %+v", m)
	}
	must(t, f.svc.Update(f.ctx, f.id("Ayla"), "cave", 2, 250, 250))
	if m := f.rec.last().party.Members[0]; m.Level != 2 || m.MapID != "cave" || m.HP != 250 {
		t.Fatalf("after Ayla's update: %+v", m)
	}
	cass := kernel.NewCharacterID()
	if f.membersOf(t, cass) != nil || f.sameParty(t, f.id("Ayla"), cass) {
		t.Fatal("a stranger has a party")
	}
	must(t, f.svc.Update(f.ctx, cass, "cave", 1, 1, 1)) // unknown: ignored
	must(t, f.svc.Offline(f.ctx, cass))
}

func TestMembershipsVisitsEveryPartyOnce(t *testing.T) {
	f := newFixture(t, "ann", "bo", "cy", "di", "ed")
	f.invite(t, "ann", "bo")
	f.invite(t, "cy", "di")
	var seen [][]kernel.CharacterID
	must(t, f.svc.Memberships(f.ctx, func(members []kernel.CharacterID) { seen = append(seen, members) }))
	if len(seen) != 2 {
		t.Fatalf("visited %d parties, want 2", len(seen))
	}
	got := map[kernel.CharacterID]int{}
	for _, members := range seen {
		if len(members) != 2 {
			t.Fatalf("members %v", members)
		}
		for _, id := range members {
			got[id]++
		}
	}
	for _, n := range []string{"ann", "bo", "cy", "di"} {
		if got[f.id(n)] != 1 {
			t.Fatalf("%s visited %d times", n, got[f.id(n)])
		}
	}
	if got[f.id("ed")] != 0 {
		t.Fatal("a character with no party was visited")
	}
}

func TestAnInviteRefusedForAFullPartyIsSpent(t *testing.T) {
	f := newFixture(t, "Ayla", "Robin", "Cass", "Dinh", "Eve")
	f.invite(t, "Ayla", "Robin")
	f.invite(t, "Ayla", "Cass")
	inv, err := f.svc.Invite(f.ctx, f.id("Ayla"), f.id("Eve"))
	must(t, err)
	f.invite(t, "Ayla", "Dinh")
	if err := f.svc.Respond(f.ctx, f.id("Eve"), inv.ID, true); !errors.Is(err, model.ErrFull) {
		t.Fatalf("joining a full party: %v", err)
	}
	if err := f.svc.Respond(f.ctx, f.id("Eve"), inv.ID, true); !errors.Is(err, model.ErrInviteExpired) {
		t.Fatalf("the refused invite again: %v", err)
	}
}

func TestAStoreFailureIsReturnedAndNobodyIsTold(t *testing.T) {
	f := newFixture(t, "Ayla", "Robin")
	f.rec.reset()
	broken := errors.New("store unavailable")
	f.store.fail = broken
	_, invErr := f.svc.Invite(f.ctx, f.id("Ayla"), f.id("Robin"))
	_, _, ofErr := f.svc.Of(f.ctx, f.id("Ayla"))
	for name, err := range map[string]error{
		"Online":      f.svc.Online(f.ctx, model.Member{ID: f.id("Ayla"), Name: "Ayla"}),
		"Invite":      invErr,
		"Leave":       f.svc.Leave(f.ctx, f.id("Ayla")),
		"Of":          ofErr,
		"Memberships": f.svc.Memberships(f.ctx, func([]kernel.CharacterID) {}),
	} {
		if !errors.Is(err, broken) {
			t.Errorf("%s: %v, want the store's error", name, err)
		}
	}
	if len(f.rec.states) != 0 || len(f.rec.invites) != 0 {
		t.Fatalf("notified %+v %+v", f.rec.states, f.rec.invites)
	}
}
