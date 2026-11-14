package social

import (
	"finga/internal/kernel"
	"finga/modules/social/internal/model"
)

// PartyID identifies a party.
type PartyID = model.PartyID

// InviteID identifies an invite.
type InviteID = model.InviteID

// ParseInviteID parses an invite's ID; bad text is kernel.ErrBadID.
func ParseInviteID(s string) (InviteID, error) { return model.ParseInviteID(s) }

// Member is a character as their party sees them.
type Member struct {
	ID    kernel.CharacterID
	Name  string
	Class kernel.Class
	Level int
	HP    int
	MaxHP int
	MapID string
}

// Presence is a character online through one connection of a gateway, which the world's join of sequence JoinSeq is behind.
type Presence struct {
	Member     Member
	Gateway    string
	Connection string
	JoinSeq    uint64
}

// MemberUpdate is what the world saw of a member.
type MemberUpdate struct {
	ID    kernel.CharacterID
	MapID string
	Class kernel.Class
	Level int
	HP    int
	MaxHP int
}

// Channel is where a chat line goes.
type Channel string

// The chat channels.
const (
	ChannelMap   Channel = "map"   // everyone on the sender's map; the world delivers it
	ChannelParty Channel = "party" // the sender's party, wherever its members are
)

// Line is a chat line as its sender typed it.
type Line struct {
	From     kernel.CharacterID
	FromName string
	Channel  Channel
	Text     string
}

// Message is a line as accepted and cleaned.
type Message struct {
	Channel    Channel
	Sender     kernel.CharacterID
	SenderName string
	Text       string
}

func (p Presence) model() model.Member {
	m := p.Member
	return model.Member{ID: m.ID, Name: m.Name, Class: m.Class, Level: m.Level, HP: m.HP, MaxHP: m.MaxHP, MapID: m.MapID, Connection: p.Connection, Gateway: p.Gateway, JoinSeq: p.JoinSeq}
}

func messageOf(m model.Message) Message {
	return Message{Channel: Channel(m.Channel), Sender: m.Sender, SenderName: m.SenderName, Text: m.Text}
}
