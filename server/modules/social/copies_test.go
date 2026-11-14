package social

import (
	"context"
	"testing"

	"finga/internal/kernel"
	"finga/modules/social/internal/model"
	"finga/pkg/copytest"
	"finga/pkg/hub"
)

// echoChat records the line it was sent and answers msg, for every member of the sender's party.
type echoChat struct {
	sent chatSent
	msg  model.Message
	to   []kernel.CharacterID
}

type chatSent struct {
	From    model.Sender
	Channel model.Channel
	Text    string
}

func (c *echoChat) Send(_ context.Context, from model.Sender, channel model.Channel, text string) (model.Message, []kernel.CharacterID, error) {
	c.sent = chatSent{from, channel, text}
	return c.msg, c.to, nil
}

// update is UpdateMember's arguments to the party module, by name.
type update struct {
	ID    kernel.CharacterID
	MapID string
	Class kernel.Class
	Level int
	HP    int
	MaxHP int
}

// Each conversion between the facade's types and the model's keeps every field, the deliveries the notifier builds too.
func TestEveryConversionKeepsEveryField(t *testing.T) {
	ctx := t.Context()
	p, c := &fakeParties{}, &echoChat{}
	m := newModule(t, p, c)
	routed := func(id kernel.CharacterID) *hub.Sub[Delivery] { // a fresh subscription, through which id's deliveries go
		m.notes.Reachable(id, "gw-a", "conn")
		return m.Subscribe("gw-a")
	}
	for name, got := range map[string][]string{
		"Online": copytest.Diff(copytest.Match{
			Renames: map[string]string{"Member": ""},
			Computed: map[string]string{
				"Online":     "the party module marks a member online",
				"JoinOrder":  "the party module numbers its members' joins",
				"OfflineAt":  "zero while online",
				"ReportedAt": "set when a world reports the member",
			},
		}, func(in Presence) model.Member {
			_ = m.Online(ctx, in)
			return p.got[0].(model.Member)
		}),
		"UpdateMember": copytest.Diff(copytest.Match{}, func(u MemberUpdate) update {
			_ = m.UpdateMember(ctx, u)
			return update{p.got[0].(kernel.CharacterID), p.got[1].(string), p.got[2].(kernel.Class), p.got[3].(int), p.got[4].(int), p.got[5].(int)}
		}),
		"Chat's line": copytest.Diff(copytest.Match{Renames: map[string]string{"From": "From.ID", "FromName": "From.Name"}}, func(l Line) chatSent {
			_, _ = m.Chat(ctx, l)
			return c.sent
		}),
		"Chat's message": copytest.Diff(copytest.Match{}, func(msg model.Message) Message {
			c.msg = msg
			out, _ := m.Chat(ctx, Line{})
			return out
		}),
		"a party line": copytest.Diff(copytest.Match{
			Renames:  map[string]string{"Message.Sender": "Sender", "Message.SenderName": "SenderName", "Message.Text": "Text"},
			Skip:     map[string]string{"Message.Channel": "only a party line reaches the members"},
			Computed: map[string]string{"To": "the member it goes to"},
		}, func(in struct{ Message model.Message }) ChatLine {
			to := kernel.NewCharacterID()
			sub := routed(to)
			c.msg, c.to = in.Message, []kernel.CharacterID{to}
			_, _ = m.Chat(ctx, Line{})
			c.to = nil
			return drain(sub)[0].(ChatLine)
		}),
		"partyOf": copytest.Diff(copytest.Match{Skip: map[string]string{
			"ID":                 "a member sees the party, not its ID",
			"Members.Connection": "routing, which the notifier keeps",
			"Members.Gateway":    "routing, which the notifier keeps",
			"Members.JoinSeq":    "the party module's check for a stale Online",
			"Members.JoinOrder":  "the party module's choice of the next leader",
			"Members.OfflineAt":  "the party module's grace for a member gone",
			"Members.ReportedAt": "the party module's record of the world's last report",
		}}, partyOf),
		"Invited": copytest.Diff(copytest.Match{
			Renames: map[string]string{"Invite.ID": "Invite", "Invite.From": "From", "Invite.FromName": "FromName", "Invite.To": "To"},
			Skip:    map[string]string{"Invite.Expires": "the party module expires invites itself"},
		}, func(in struct{ Invite model.Invite }) Invited {
			sub := routed(in.Invite.To)
			m.notes.Invited(in.Invite.To, in.Invite)
			return drain(sub)[0].(Invited)
		}),
		"Membership": copytest.Diff(copytest.Match{
			Renames: map[string]string{"Party.ID": "Party", "Party.Members.ID": "Members", "Told": "Characters"},
			Skip: map[string]string{
				"Party.Leader":  "the worlds keep who is in which party, not who leads",
				"Party.Members": "the worlds keep who is in which party, not what each member shows",
			},
		}, func(in struct {
			Told  []kernel.CharacterID
			Party model.Party
		}) Membership {
			sub := m.SubscribeMemberships("world-1")
			m.notes.PartyChanged(in.Told, &in.Party)
			return drain(sub)[0].(Membership)
		}),
	} {
		if len(got) > 0 {
			t.Errorf("%s: %v", name, got)
		}
	}
}
