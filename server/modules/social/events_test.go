package social

import (
	"reflect"
	"testing"

	"finga/internal/kernel"
	"finga/modules/social/internal/model"
	"finga/pkg/hub"
)

// drain is every event queued on sub, in order.
func drain[E any](sub *hub.Sub[E]) []E {
	var out []E
	for {
		select {
		case e := <-sub.C():
			out = append(out, e)
		default:
			return out
		}
	}
}

// Each of the party module's notes reaches the gateway its player is online through, and every world, as the facade's events.
func TestTheModulesNotesReachTheirSubscribers(t *testing.T) {
	m := newModule(t, &fakeParties{}, &fakeChat{})
	n := m.notes
	gwA, gwB := m.Subscribe("gw-a"), m.Subscribe("gw-b")
	worlds := []*hub.Sub[MembershipEvent]{m.SubscribeMemberships("world-1"), m.SubscribeMemberships("world-2")}
	cy := kernel.NewCharacterID() // online through no gateway
	n.Reachable(ann, "gw-a", "conn-ann")
	n.Reachable(bo, "gw-b", "conn-bo")

	p := &model.Party{ID: party, Leader: ann, Members: []model.Member{
		{ID: ann, Name: "Ann", Class: "mage", Level: 3, HP: 90, MaxHP: 100, MapID: "forest", Online: true, Connection: "conn-ann", Gateway: "gw-a", JoinSeq: 4},
		{ID: bo, Name: "Bo", Class: "warrior", Level: 5, HP: 10, MaxHP: 120, MapID: "cave"},
	}}
	n.PartyChanged([]kernel.CharacterID{ann, bo, cy}, p)
	n.Invited(ann, model.Invite{ID: invite, From: bo, FromName: "Bo", To: ann})
	n.Unreachable(bo, "conn-old") // an older connection's, which keeps Bo's deliveries
	n.PartyChanged([]kernel.CharacterID{bo}, nil)
	n.Unreachable(bo, "conn-bo")
	n.Alone(bo) // through no gateway now

	state := &Party{Leader: ann, Members: []PartyMember{
		{ID: ann, Name: "Ann", Class: "mage", Level: 3, HP: 90, MaxHP: 100, MapID: "forest", Online: true},
		{ID: bo, Name: "Bo", Class: "warrior", Level: 5, HP: 10, MaxHP: 120, MapID: "cave"},
	}}
	if got, want := drain(gwA), []Delivery{PartyState{To: ann, Party: state}, Invited{To: ann, Invite: invite, From: bo, FromName: "Bo"}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("gw-a heard %+v, want %+v", got, want)
	}
	if got, want := drain(gwB), []Delivery{PartyState{To: bo, Party: state}, PartyState{To: bo}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("gw-b heard %+v, want %+v", got, want)
	}
	want := []MembershipEvent{
		Membership{Characters: []kernel.CharacterID{ann, bo, cy}, Members: []kernel.CharacterID{ann, bo}, Party: party},
		Membership{Characters: []kernel.CharacterID{bo}},
	}
	for _, w := range worlds {
		if got := drain(w); !reflect.DeepEqual(got, want) {
			t.Fatalf("a world heard %+v, want %+v", got, want)
		}
	}

	if m.Subscribe("gw-a") == gwA {
		t.Fatal("a second subscription is the first")
	}
	select {
	case <-gwA.Done():
	default:
		t.Fatal("subscribing again under the same gateway left the older subscription open")
	}
}

// A party line reaches each member through their gateway; a map line, and a refused one, reach nobody.
func TestAPartyLineReachesEachMember(t *testing.T) {
	c := &fakeChat{}
	m := newModule(t, &fakeParties{}, c)
	gwA, gwB := m.Subscribe("gw-a"), m.Subscribe("gw-b")
	m.notes.Reachable(ann, "gw-a", "conn-ann")
	m.notes.Reachable(bo, "gw-b", "conn-bo")
	if _, err := m.Chat(t.Context(), Line{From: ann, FromName: "Ann", Channel: ChannelParty, Text: "hi"}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Chat(t.Context(), Line{From: ann, FromName: "Ann", Channel: ChannelMap, Text: "hi"}); err != nil {
		t.Fatal(err)
	}
	c.err = model.ErrRate
	if _, err := m.Chat(t.Context(), Line{From: ann, FromName: "Ann", Channel: ChannelParty, Text: "hi"}); err == nil {
		t.Fatal("a line past the rate was taken")
	}
	for to, sub := range map[kernel.CharacterID]*hub.Sub[Delivery]{ann: gwA, bo: gwB} {
		if got, want := drain(sub), []Delivery{ChatLine{To: to, Sender: ann, SenderName: "Ann", Text: "hi"}}; !reflect.DeepEqual(got, want) {
			t.Fatalf("heard %+v, want %+v", got, want)
		}
	}
}

// An announcement goes once to each gateway anyone is online through, however many are on it, and to none with nobody online.
func TestAnAnnouncementGoesOnceToEachGatewayWithPlayers(t *testing.T) {
	m := newModule(t, &fakeParties{}, &fakeChat{})
	gwA, gwB, gwC := m.Subscribe("gw-a"), m.Subscribe("gw-b"), m.Subscribe("gw-c")
	m.notes.Reachable(ann, "gw-a", "conn-ann")
	m.notes.Reachable(kernel.NewCharacterID(), "gw-a", "conn-cy")
	m.notes.Reachable(bo, "gw-b", "conn-bo")
	m.notes.Reachable(kernel.NewCharacterID(), "gw-d", "conn-di") // a gateway with no subscription
	news := BossAppeared{Boss: "king_slime", MapID: "forest"}
	if err := m.Announce(t.Context(), news); err != nil {
		t.Fatal(err)
	}
	for name, sub := range map[string]*hub.Sub[Delivery]{"gw-a": gwA, "gw-b": gwB} {
		if got := drain(sub); !reflect.DeepEqual(got, []Delivery{news}) {
			t.Fatalf("%s heard %+v; want the notice once", name, got)
		}
	}
	if got := drain(gwC); len(got) != 0 {
		t.Fatalf("gw-c, with nobody online, heard %+v", got)
	}
}
