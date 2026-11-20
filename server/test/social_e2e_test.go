package e2e_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	clientv1 "finga/api/gen/client/v1"
	"finga/internal/shared/gamedata"
	"finga/pkg/wsconn"
	"finga/test/e2etest"
)

func TestPlayTogether(t *testing.T) {
	t.Parallel()
	s := newStack(t)
	// Three hunters in the slime field; Dan stands on the forest's way back to the town to leave later.
	names := [][2]string{{"ann", "Ann"}, {"ben", "Ben"}, {"cat", "Cat"}, {"dan", "Dan"}}
	at := []gamedata.Point{{X: e2etest.SlimeField.X, Z: e2etest.SlimeField.Z - 1}, e2etest.SlimeField, {X: e2etest.SlimeField.X, Z: e2etest.SlimeField.Z + 1}, e2etest.ForestFromTown}
	var ls []*e2etest.Looter
	for k, n := range names {
		token, id := s.CreateCharacter(t, n[0], n[1], "warrior")
		s.Place(t, id, "forest", at[k], 144, 0)
		p, snap := s.Join(t, token, id)
		ls = append(ls, e2etest.NewLooter(p, snap))
	}
	ann, ben, cat, dan := ls[0], ls[1], ls[2], ls[3]

	t.Run("E3/invite-accept-and-everyone-sees-the-party", func(t *testing.T) {
		for k, m := range []*e2etest.Looter{ben, cat, dan} {
			if k > 0 {
				time.Sleep(600 * time.Millisecond) // party commands are limited to 2 per second
			}
			if r := ann.Command(t, "inv-"+m.ID, &clientv1.PartyInvite{CharacterId: m.ID}); !r.GetOk() {
				t.Fatalf("invite %s: %+v", m.ID, r)
			}
			inv := e2etest.Take(t, m, "PartyInvited", &m.Invites, 5*time.Second, nil)
			if inv.From != ann.ID || inv.FromName != "Ann" || inv.GetInviteId() == "" {
				t.Fatalf("invited %+v", inv)
			}
			if r := m.Command(t, "acc-"+m.ID, &clientv1.PartyRespond{InviteId: inv.GetInviteId(), Accept: true}); !r.GetOk() {
				t.Fatalf("accept: %+v", r)
			}
			for _, l := range ls[:k+2] {
				ps := l.PartyState(t, k+2)
				if ps.GetParty().GetLeader() != ann.ID || ps.GetParty().GetMembers()[0].GetCharacterId() != ann.ID || !ps.GetParty().GetMembers()[k+1].GetOnline() || ps.GetParty().GetMembers()[k+1].GetMapId() != "forest" || ps.GetParty().GetMembers()[k+1].GetClass() != "warrior" {
					t.Fatalf("%s sees %+v", l.ID, ps.Party)
				}
			}
		}
		// A member cannot invite, and a full party's leader cannot invite anyone: PARTY_FULL answers first.
		if r := ben.Command(t, "inv-x", &clientv1.PartyInvite{CharacterId: cat.ID}); r.GetOk() || r.Code != code("NOT_PARTY_LEADER") {
			t.Fatalf("member invites: %+v", r)
		}
		if r := ann.Command(t, "inv-y", &clientv1.PartyInvite{CharacterId: cat.ID}); r.GetOk() || r.Code != code("PARTY_FULL") {
			t.Fatalf("inviting from a full party: %+v", r)
		}
	})

	t.Run("E1/map-chat-reaches-the-map-cleaned-and-limited", func(t *testing.T) {
		ann.Say(t, clientv1.ChatChannel_CHAT_CHANNEL_MAP, "hello\tforest\n")
		for _, l := range ls {
			cm := l.Hears(t, "helloforest")
			if cm.Channel != clientv1.ChatChannel_CHAT_CHANNEL_MAP || cm.Sender != ann.ID || cm.SenderName != "Ann" {
				t.Fatalf("%s heard %+v", l.ID, cm)
			}
		}
		ann.Say(t, clientv1.ChatChannel_CHAT_CHANNEL_MAP, strings.Repeat("x", 121))
		if r := ann.Response(t); r.GetOk() || r.Code != code("CHAT_TOO_LONG") {
			t.Fatalf("121 characters: %+v", r)
		}
		for _, channel := range []clientv1.ChatChannel{clientv1.ChatChannel_CHAT_CHANNEL_UNSPECIFIED, 7} {
			ann.Say(t, channel, "which channel?")
			if r := ann.Response(t); r.GetOk() || r.Code != code("BAD_CHAT_CHANNEL") {
				t.Fatalf("channel %v: %+v", channel, r)
			}
		}
		// Ten lines at once, well past the burst of 3: refilling one a second, even a slow runner refuses some, and delivers none it refused.
		const lines = 10
		for k := range lines {
			ben.Say(t, clientv1.ChatChannel_CHAT_CHANNEL_MAP, fmt.Sprintf("spam %d", k))
		}
		tally := func() (heard, refused int) {
			for _, cm := range ann.ChatLines {
				if strings.HasPrefix(cm.Text, "spam ") {
					heard++
				}
			}
			for _, r := range ben.Responses {
				if r.Req == "" && (r.R.GetOk() || r.R.Code != code("CHAT_RATE_LIMITED")) {
					t.Fatalf("a spam line answered %+v", r.R)
				} else if r.Req == "" {
					refused++
				}
			}
			return heard, refused
		}
		for deadline := time.Now().Add(10 * time.Second); ; {
			if heard, refused := tally(); heard+refused >= lines {
				break
			} else if time.Now().After(deadline) {
				t.Fatalf("of %d lines, Ann heard %d and Ben had %d refused", lines, heard, refused)
			}
			ben.Pump(25*time.Millisecond, nil)
			ann.Pump(25*time.Millisecond, nil)
		}
		ben.Pump(250*time.Millisecond, nil)
		ann.Pump(250*time.Millisecond, nil)
		if heard, refused := tally(); refused == 0 || heard < 3 || heard+refused != lines {
			t.Fatalf("of %d lines, Ann heard %d and Ben had %d refused; want the burst of 3 or more heard, some refused, and no refused line heard", lines, heard, refused)
		}
		ann.ChatLines = slices.DeleteFunc(ann.ChatLines, func(cm *clientv1.ChatMessage) bool { return strings.HasPrefix(cm.Text, "spam ") })
		ben.Responses = slices.DeleteFunc(ben.Responses, func(r e2etest.Reply) bool { return r.Req == "" })
	})

	t.Run("E1-E3/party-chat-crosses-maps-and-map-chat-does-not", func(t *testing.T) {
		dan.Hop(t, e2etest.ForestFromTown, "town", e2etest.TownPortal)
		for _, l := range ls {
			ps := e2etest.Take(t, l, "PartyState with Dan in the town", &l.PartyStates, 5*time.Second, func(ps *clientv1.PartyState) bool {
				return ps.Party != nil && len(ps.GetParty().GetMembers()) == 4 && ps.GetParty().GetMembers()[3].GetMapId() == "town"
			})
			if ps.GetParty().GetMembers()[3].GetCharacterId() != dan.ID {
				t.Fatalf("%s sees %+v", l.ID, ps.Party)
			}
		}
		ann.Say(t, clientv1.ChatChannel_CHAT_CHANNEL_PARTY, "regroup at the well")
		for _, l := range ls {
			if cm := l.Hears(t, "regroup at the well"); cm.Channel != clientv1.ChatChannel_CHAT_CHANNEL_PARTY || cm.Sender != ann.ID {
				t.Fatalf("%s heard %+v", l.ID, cm)
			}
		}
		ann.Say(t, clientv1.ChatChannel_CHAT_CHANNEL_MAP, "forest only")
		for _, l := range ls[:3] {
			l.Hears(t, "forest only")
		}
		if !dan.DoesNotHear("forest only", 700*time.Millisecond) {
			t.Fatal("a map line reached the town")
		}
	})

	t.Run("E3/a-kill-shares-exp-with-the-members-alive-on-the-map", func(t *testing.T) {
		// Ann, Ben and Cat share floor(8 × 1.2) = 9, 3 each; Dan, in the town, gets nothing.
		before := len(ann.Exp)
		ann.HuntUntil(90*time.Second, func() bool { return len(ann.Exp) != before }, nil) // the gain is taken from what Ann heard
		if got := ann.Exp[before:]; len(got) != 1 || got[0].GetAmount() != 3 {
			t.Fatalf("the killer gained %v, want 3 once", got)
		}
		for _, l := range []*e2etest.Looter{ben, cat} {
			if g := e2etest.Take(t, l, "ExpGained", &l.Exp, 5*time.Second, nil); g.Amount != 3 {
				t.Fatalf("%s gained %+v", l.ID, g)
			}
		}
		if dan.Await(700*time.Millisecond, func(m wsconn.Message) bool { return m.Type == "ExpGained" }) {
			t.Fatal("a member on another map gained EXP")
		}
	})

	t.Run("E3/leave-kick-and-the-lead-passes-to-the-longest-standing", func(t *testing.T) {
		if r := ben.Command(t, "leave", &clientv1.PartyLeave{}); !r.GetOk() {
			t.Fatalf("leave: %+v", r)
		}
		ben.PartyState(t, 0)
		for _, l := range []*e2etest.Looter{ann, cat, dan} {
			l.PartyState(t, 3)
		}
		if r := cat.Command(t, "kick", &clientv1.PartyKick{CharacterId: dan.ID}); r.GetOk() || r.Code != code("NOT_PARTY_LEADER") {
			t.Fatalf("a member kicks: %+v", r)
		}
		if r := ann.Command(t, "leave", &clientv1.PartyLeave{}); !r.GetOk() {
			t.Fatalf("leader leaves: %+v", r)
		}
		for _, l := range []*e2etest.Looter{cat, dan} {
			if ps := l.PartyState(t, 2); ps.GetParty().GetLeader() != cat.ID {
				t.Fatalf("%s sees leader %s, want Cat", l.ID, ps.GetParty().GetLeader())
			}
		}
		if r := cat.Command(t, "kick2", &clientv1.PartyKick{CharacterId: dan.ID}); !r.GetOk() {
			t.Fatalf("the new leader kicks: %+v", r)
		}
		dan.PartyState(t, 0)
		cat.PartyState(t, 1)
	})
}

// Parties live in memory, so a restart forgets them; the players reconnect, and a party forms again.
func TestARestartForgetsPartiesAndAPartyFormsAgain(t *testing.T) {
	t.Parallel()
	s := newStack(t)
	var ls []*e2etest.Looter
	for _, n := range [][2]string{{"eve", "Eve"}, {"fin", "Fin"}} {
		token, id := s.CreateCharacter(t, n[0], n[1], "warrior")
		p, snap := s.Join(t, token, id)
		ls = append(ls, e2etest.NewLooter(p, snap))
	}
	party := func(req string) {
		t.Helper()
		eve, fin := ls[0], ls[1]
		if r := eve.Command(t, "inv-"+req, &clientv1.PartyInvite{CharacterId: fin.ID}); !r.GetOk() {
			t.Fatalf("invite: %+v", r)
		}
		inv := e2etest.Take(t, fin, "PartyInvited", &fin.Invites, 5*time.Second, nil)
		if r := fin.Command(t, "acc-"+req, &clientv1.PartyRespond{InviteId: inv.GetInviteId(), Accept: true}); !r.GetOk() {
			t.Fatalf("accept: %+v", r)
		}
		for _, l := range ls {
			l.PartyState(t, 2)
		}
	}
	party("1")

	s.Restart()
	for k, l := range ls {
		back, _ := s.Hello(t, l.Token, l.ID) // registered with the fresh module on the Hello
		ls[k] = back
	}
	if r := ls[0].Command(t, "leave", &clientv1.PartyLeave{}); r.GetOk() || r.GetCode() != code("NO_PARTY") {
		t.Fatalf("leaving the party the restart forgot answered %+v", r)
	}
	time.Sleep(time.Second) // party commands are limited to 2 per second
	party("2")
}
