package social

import (
	"testing"

	clientv1 "finga/api/gen/client/v1"
	"finga/internal/application/social"
	"finga/pkg/copytest"
)

// A player's party reaches the client with every field of every member, and none when they have none.
func TestAPartyKeepsEveryField(t *testing.T) {
	m := copytest.Match{Renames: map[string]string{"Party.Members.ID": "Party.Members.CharacterId", "Party.Members.HP": "Party.Members.Hp", "Party.Members.MaxHP": "Party.Members.MaxHp", "Party.Members.MapID": "Party.Members.MapId"}, Skip: map[string]string{"To": "the delivery names who gets it, not the message"}}
	if got := copytest.Diff(m, partyState); len(got) > 0 {
		t.Error(got)
	}
	if p := partyState(social.PartyState{}); p.Party != nil {
		t.Errorf("no party went out as %+v", p)
	}
}

// An invite and a party's line reach the client with every field; a line from the social module is a party's.
func TestAnInviteAndAPartyLineKeepEveryField(t *testing.T) {
	invite := copytest.Match{Renames: map[string]string{"Invite": "InviteId"}, Skip: map[string]string{"To": "the delivery names who gets it, not the message"}}
	if got := copytest.Diff[social.Invited, *clientv1.PartyInvited](invite, invited); len(got) > 0 {
		t.Errorf("invite: %v", got)
	}
	line := copytest.Match{Skip: map[string]string{"To": "the delivery names who gets it, not the message"}, Computed: map[string]string{"Channel": "the social module's lines are party lines, checked below"}}
	if got := copytest.Diff(line, chatLine); len(got) > 0 {
		t.Errorf("line: %v", got)
	}
	if got := chatLine(social.ChatLine{}); got.Channel != clientv1.ChatChannel_CHAT_CHANNEL_PARTY {
		t.Errorf("a party line went out as %v", got.Channel)
	}
}
