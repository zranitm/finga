package e2e_test

import (
	"strings"
	"testing"
	"time"

	"finga/internal/protocol"
	"finga/pkg/wsconn"
)

// take removes and returns the first recorded element matching ok, waiting
// up to d for one to arrive. Messages of other kinds are kept for later.
func take[T any](t *testing.T, l *looter, what string, recorded *[]T, d time.Duration, ok func(T) bool) T {
	t.Helper()
	var got T
	found := l.until(d, func() bool {
		for k, v := range *recorded {
			if ok == nil || ok(v) {
				got = v
				*recorded = append((*recorded)[:k], (*recorded)[k+1:]...)
				return true
			}
		}
		return false
	})
	if !found {
		t.Fatalf("%s: no %s within %s (recorded %+v)", l.id, what, d, *recorded)
	}
	return got
}

// partyState waits for a PartyState with n members (0: no party).
func (l *looter) partyState(t *testing.T, n int) protocol.PartyState {
	t.Helper()
	return take(t, l, "PartyState", &l.partyStates, 5*time.Second, func(ps protocol.PartyState) bool {
		if n == 0 {
			return ps.Party == nil
		}
		return ps.Party != nil && len(ps.Party.Members) == n
	})
}

func (l *looter) command(t *testing.T, req string, d any) protocol.Response {
	t.Helper()
	if err := l.trySend(wsconn.Message{ReqID: req, Data: d}); err != nil {
		t.Fatal(err)
	}
	return take(t, l, "Response "+req, &l.responses, 5*time.Second, func(r reply) bool { return r.req == req }).r
}

// response waits for a Response without a request ID (a refused chat line).
func (l *looter) response(t *testing.T) protocol.Response {
	t.Helper()
	return take(t, l, "Response", &l.responses, 5*time.Second, func(r reply) bool { return r.req == "" }).r
}

func (l *looter) say(t *testing.T, channel protocol.ChatChannel, text string) {
	t.Helper()
	if err := l.trySend(wsconn.Message{Data: protocol.ChatSend{Channel: channel, Text: text}}); err != nil {
		t.Fatal(err)
	}
}

func (l *looter) hears(t *testing.T, text string) protocol.ChatMessage {
	t.Helper()
	return take(t, l, "ChatMessage "+text, &l.chatLines, 5*time.Second, func(cm protocol.ChatMessage) bool { return cm.Text == text })
}

func (l *looter) doesNotHear(text string, d time.Duration) bool {
	return !l.until(d, func() bool {
		for _, cm := range l.chatLines {
			if cm.Text == text {
				return true
			}
		}
		return false
	})
}

func TestPlayTogether(t *testing.T) {
	s := newStack(t)
	// Four hunters in the forest: three in the slime field, Dan on the
	// portal back to the town (x 64) so he can leave the map later.
	names := [][2]string{{"ann", "Ann"}, {"ben", "Ben"}, {"cat", "Cat"}, {"dan", "Dan"}}
	xs := []float64{300, 330, 360, 64}
	var ls []*looter
	for k, n := range names {
		token, id := s.createCharacter(t, n[0], n[1], "warrior")
		s.place(t, id, "forest", xs[k], 608, 204, 0)
		p, snap := s.join(t, token, id)
		ls = append(ls, newLooter(p, snap))
	}
	ann, ben, cat, dan := ls[0], ls[1], ls[2], ls[3]

	t.Run("E3/invite-accept-and-everyone-sees-the-party", func(t *testing.T) {
		for k, m := range []*looter{ben, cat, dan} {
			if k > 0 {
				time.Sleep(600 * time.Millisecond) // party commands are limited to 2 per second (page 4)
			}
			if r := ann.command(t, "inv-"+m.id, protocol.PartyInvite{CharacterID: m.id}); !r.OK {
				t.Fatalf("invite %s: %+v", m.id, r)
			}
			inv := take(t, m, "PartyInvited", &m.invites, 5*time.Second, nil)
			if inv.From != ann.id || inv.FromName != "Ann" || inv.InviteID == "" {
				t.Fatalf("invited %+v", inv)
			}
			if r := m.command(t, "acc-"+m.id, protocol.PartyRespond{InviteID: inv.InviteID, Accept: true}); !r.OK {
				t.Fatalf("accept: %+v", r)
			}
			for _, l := range ls[:k+2] {
				ps := l.partyState(t, k+2)
				if ps.Party.Leader != ann.id || ps.Party.Members[0].CharacterID != ann.id || !ps.Party.Members[k+1].Online || ps.Party.Members[k+1].MapID != "forest" {
					t.Fatalf("%s sees %+v", l.id, ps.Party)
				}
			}
		}
		// A member cannot invite; the leader of a full party (four of four)
		// cannot invite anyone, a member included: the party is full is the
		// first rule that answers. Inviting a member of a party with room is
		// already_in_party, covered by the party service tests.
		if r := ben.command(t, "inv-x", protocol.PartyInvite{CharacterID: cat.id}); r.OK || r.Code != "not_party_leader" {
			t.Fatalf("member invites: %+v", r)
		}
		if r := ann.command(t, "inv-y", protocol.PartyInvite{CharacterID: cat.id}); r.OK || r.Code != "party_full" {
			t.Fatalf("inviting from a full party: %+v", r)
		}
	})

	t.Run("E1/map-chat-reaches-the-map-cleaned-and-limited", func(t *testing.T) {
		ann.say(t, protocol.ChatMap, "hello\tforest\n")
		for _, l := range ls {
			cm := l.hears(t, "helloforest")
			if cm.Channel != protocol.ChatMap || cm.Sender != ann.id || cm.SenderName != "Ann" {
				t.Fatalf("%s heard %+v", l.id, cm)
			}
		}
		ann.say(t, protocol.ChatMap, strings.Repeat("x", 121))
		if r := ann.response(t); r.OK || r.Code != "chat_too_long" {
			t.Fatalf("121 characters: %+v", r)
		}
		// Burst of three, then the fourth within the second is refused.
		for k := range 3 {
			ben.say(t, protocol.ChatMap, "spam "+string(rune('a'+k)))
		}
		ben.say(t, protocol.ChatMap, "spam d")
		if r := ben.response(t); r.OK || r.Code != "chat_rate_limited" {
			t.Fatalf("fourth line: %+v", r)
		}
		ann.hears(t, "spam c")
		if !ann.doesNotHear("spam d", 500*time.Millisecond) {
			t.Fatal("the refused line was delivered")
		}
	})

	t.Run("E1-E3/party-chat-crosses-maps-and-map-chat-does-not", func(t *testing.T) {
		dan.hop(t, 64, "town", 1216)
		for _, l := range ls {
			ps := take(t, l, "PartyState with Dan in the town", &l.partyStates, 5*time.Second, func(ps protocol.PartyState) bool {
				return ps.Party != nil && len(ps.Party.Members) == 4 && ps.Party.Members[3].MapID == "town"
			})
			if ps.Party.Members[3].CharacterID != dan.id {
				t.Fatalf("%s sees %+v", l.id, ps.Party)
			}
		}
		ann.say(t, protocol.ChatParty, "regroup at the well")
		for _, l := range ls {
			if cm := l.hears(t, "regroup at the well"); cm.Channel != protocol.ChatParty || cm.Sender != ann.id {
				t.Fatalf("%s heard %+v", l.id, cm)
			}
		}
		ann.say(t, protocol.ChatMap, "forest only")
		for _, l := range ls[:3] {
			l.hears(t, "forest only")
		}
		if !dan.doesNotHear("forest only", 700*time.Millisecond) {
			t.Fatal("a map line reached the town")
		}
	})

	t.Run("E3/a-kill-shares-exp-with-the-members-alive-on-the-map", func(t *testing.T) {
		// Ann, Ben and Cat share: total floor(5 × 1.2) = 6, 2 each; Dan is
		// in the town and gets nothing.
		ann.face(true)
		deadline := time.Now().Add(90 * time.Second)
		next := time.Now()
		var got int64
		for got == 0 && time.Now().Before(deadline) {
			if !time.Now().Before(next) {
				if err := ann.attack(); err != nil {
					t.Fatal(err)
				}
				next = time.Now().Add(850 * time.Millisecond)
			}
			ann.pump(100*time.Millisecond, func(m wsconn.Message) bool {
				if m.Type == "ExpGained" {
					got = m.Data.(protocol.ExpGained).Amount
					return true
				}
				return false
			})
		}
		if got != 2 {
			t.Fatalf("the killer gained %d, want 2", got)
		}
		for _, l := range []*looter{ben, cat} {
			if g := take(t, l, "ExpGained", &l.exp, 5*time.Second, nil); g.Amount != 2 {
				t.Fatalf("%s gained %+v", l.id, g)
			}
		}
		if dan.pump(700*time.Millisecond, func(m wsconn.Message) bool { return m.Type == "ExpGained" }) {
			t.Fatal("a member on another map gained EXP")
		}
	})

	t.Run("E3/leave-kick-and-the-lead-passes-to-the-longest-standing", func(t *testing.T) {
		if r := ben.command(t, "leave", protocol.PartyLeave{}); !r.OK {
			t.Fatalf("leave: %+v", r)
		}
		ben.partyState(t, 0)
		for _, l := range []*looter{ann, cat, dan} {
			l.partyState(t, 3)
		}
		if r := cat.command(t, "kick", protocol.PartyKick{CharacterID: dan.id}); r.OK || r.Code != "not_party_leader" {
			t.Fatalf("a member kicks: %+v", r)
		}
		if r := ann.command(t, "leave", protocol.PartyLeave{}); !r.OK {
			t.Fatalf("leader leaves: %+v", r)
		}
		for _, l := range []*looter{cat, dan} {
			if ps := l.partyState(t, 2); ps.Party.Leader != cat.id {
				t.Fatalf("%s sees leader %s, want Cat", l.id, ps.Party.Leader)
			}
		}
		if r := cat.command(t, "kick2", protocol.PartyKick{CharacterID: dan.id}); !r.OK {
			t.Fatalf("the new leader kicks: %+v", r)
		}
		dan.partyState(t, 0)
		cat.partyState(t, 1)
	})
}

// A social restart forgets every party (they live in memory, TD-19), but
// keeps every socket: the gateway sees the new boot ID, tells each player
// they have no party, and registers them again, so a new party can form.
func TestASocialRestartForgetsPartiesAndKeepsSockets(t *testing.T) {
	s := newStack(t)
	var ls []*looter
	for _, n := range [][2]string{{"eve", "Eve"}, {"fin", "Fin"}} {
		token, id := s.createCharacter(t, n[0], n[1], "warrior")
		p, snap := s.join(t, token, id)
		ls = append(ls, newLooter(p, snap))
	}
	eve, fin := ls[0], ls[1]
	party := func(req string) {
		t.Helper()
		if r := eve.command(t, "inv-"+req, protocol.PartyInvite{CharacterID: fin.id}); !r.OK {
			t.Fatalf("invite: %+v", r)
		}
		inv := take(t, fin, "PartyInvited", &fin.invites, 5*time.Second, nil)
		if r := fin.command(t, "acc-"+req, protocol.PartyRespond{InviteID: inv.InviteID, Accept: true}); !r.OK {
			t.Fatalf("accept: %+v", r)
		}
		for _, l := range ls {
			l.partyState(t, 2)
		}
	}
	party("1")

	s.social.restart()
	for _, l := range ls {
		l.partyState(t, 0) // the parties are gone, and the players are told
	}
	if err := eve.trySend(wsconn.Message{Data: protocol.Ping{ClientTime: 1}}); err != nil {
		t.Fatal(err)
	}
	if !eve.pump(5*time.Second, func(m wsconn.Message) bool { return m.Type == "Pong" }) {
		t.Fatal("no Pong: the socket did not survive the restart")
	}
	time.Sleep(time.Second) // party commands are limited to 2 per second (page 4)
	party("2")              // registered again on the new service: a party forms
}
