package local

import (
	"context"
	"log/slog"

	"finga/internal/kernel"
	"finga/pkg/hub"
	"finga/pkg/panics"
	"finga/services/gateway/internal/model"
	"finga/services/social"
)

// SocialModule is the social module as the gateway calls it.
type SocialModule interface {
	Online(ctx context.Context, p social.Presence) error
	Offline(ctx context.Context, id kernel.CharacterID, conn string) error
	Invite(ctx context.Context, from, to kernel.CharacterID) (social.InviteID, error)
	Respond(ctx context.Context, who kernel.CharacterID, invite social.InviteID, accept bool) error
	LeaveParty(ctx context.Context, who kernel.CharacterID) error
	Kick(ctx context.Context, leader, target kernel.CharacterID) error
	Promote(ctx context.Context, leader, target kernel.CharacterID) error
	Chat(ctx context.Context, l social.Line) (social.Message, error)
	Subscribe(gateway string) *hub.Sub[social.Delivery]
	GatewayLeft(gateway string)
}

// Social is the application's Social over the social module.
type Social struct {
	s      SocialModule
	log    *slog.Logger
	panics *panics.Recorder
}

// NewSocial adapts the social module; log hears the stream's breaks, and rec a panic handling it.
func NewSocial(s SocialModule, log *slog.Logger, rec *panics.Recorder) Social {
	return Social{s: s, log: log, panics: rec}
}

// Online registers a player connected to this gateway through conn, behind the world's join of sequence joinSeq.
func (s Social) Online(ctx context.Context, gateway, conn string, joinSeq uint64, p model.Player, mapID string) error {
	member := social.Member{ID: p.ID, Name: p.Name, Class: kernel.Class(p.Class), Level: p.Level, HP: p.HP, MaxHP: p.MaxHP, MapID: mapID}
	return wrap(s.s.Online(ctx, social.Presence{Member: member, Gateway: gateway, Connection: conn, JoinSeq: joinSeq}))
}

// Offline tells the module the player's connection conn closed.
func (s Social) Offline(ctx context.Context, character kernel.CharacterID, conn string) error {
	return wrap(s.s.Offline(ctx, character, conn))
}

// chatChannels maps the gateway's channels to the module's; it refuses anything else, as no channel.
var chatChannels = map[model.ChatChannel]social.Channel{
	model.ChatMap:   social.ChannelMap,
	model.ChatParty: social.ChannelParty,
}

// Chat cleans and limits a line.
func (s Social) Chat(ctx context.Context, sender model.Player, channel model.ChatChannel, text string) (model.ChatLine, error) {
	msg, err := s.s.Chat(ctx, social.Line{From: sender.ID, FromName: sender.Name, Channel: chatChannels[channel], Text: text})
	if err != nil {
		return model.ChatLine{}, wrap(err)
	}
	line := model.ChatLine{Channel: model.ChatParty, Text: msg.Text}
	if msg.Channel == social.ChannelMap {
		line.Channel = model.ChatMap
	}
	return line, nil
}

// Invite invites a character to the inviter's party.
func (s Social) Invite(ctx context.Context, from, to kernel.CharacterID) error {
	_, err := s.s.Invite(ctx, from, to)
	return wrap(err)
}

// Respond accepts or declines an invite.
func (s Social) Respond(ctx context.Context, who kernel.CharacterID, inviteID string, accept bool) error {
	invite, err := social.ParseInviteID(inviteID)
	if err != nil {
		return wrap(err)
	}
	return wrap(s.s.Respond(ctx, who, invite, accept))
}

// Leave leaves the party.
func (s Social) Leave(ctx context.Context, who kernel.CharacterID) error {
	return wrap(s.s.LeaveParty(ctx, who))
}

// Kick removes a member; the leader's command.
func (s Social) Kick(ctx context.Context, leader, target kernel.CharacterID) error {
	return wrap(s.s.Kick(ctx, leader, target))
}

// Promote hands the lead to a member; the leader's command.
func (s Social) Promote(ctx context.Context, leader, target kernel.CharacterID) error {
	return wrap(s.s.Promote(ctx, leader, target))
}

// FollowDeliveries follows this gateway's party, chat and announcement stream until ctx ends; after a break the module sweeps the gateway's players unless it subscribes again within its grace, which re-registers them.
func (s Social) FollowDeliveries(ctx context.Context, gateway string, deliver func(model.SocialEvent)) {
	hub.Follow(ctx, func() *hub.Sub[social.Delivery] { return s.s.Subscribe(gateway) }, func(context.Context) error {
		deliver(model.SocialSubscribed{}) // one process, one boot: parties outlive a break
		return nil
	}, func(d social.Delivery) {
		if e, ok := socialEvent(d); ok {
			deliver(e)
		}
	}, hub.FollowOptions{Panics: s.panics, OnBreak: func(err error) {
		s.s.GatewayLeft(gateway)
		s.log.WarnContext(ctx, "social stream broke; subscribing again", "err", err)
	}})
}

// socialEvent is one delivery as the gateway routes it: a player's, or an announcement for all of them.
func socialEvent(d social.Delivery) (model.SocialEvent, bool) {
	var to kernel.CharacterID
	var p model.Push
	switch d := d.(type) {
	case social.BossAppeared:
		return model.SocialBroadcast{Delivery: model.Delivery{Push: model.BossAppeared{Boss: d.Boss, MapID: d.MapID}}}, true
	case social.PartyState:
		to, p = d.To, model.PartyState{Party: partyView(d.Party)}
	case social.Invited:
		to, p = d.To, model.PartyInvited{InviteID: d.Invite.String(), From: d.From.String(), FromName: d.FromName}
	case social.ChatLine:
		to, p = d.To, model.ChatMessage{Channel: model.ChatParty, Sender: d.Sender.String(), SenderName: d.SenderName, Text: d.Text}
	default:
		return nil, false
	}
	return model.SocialDelivery{Character: to, Delivery: model.Delivery{Push: p}}, true
}

func partyView(p *social.Party) *model.PartyView {
	if p == nil {
		return nil
	}
	v := &model.PartyView{Leader: p.Leader.String(), Members: make([]model.PartyMember, 0, len(p.Members))}
	for _, m := range p.Members {
		v.Members = append(v.Members, model.PartyMember{
			CharacterID: m.ID.String(), Name: m.Name, Class: m.Class.String(), Level: m.Level, HP: m.HP, MaxHP: m.MaxHP, MapID: m.MapID, Online: m.Online,
		})
	}
	return v
}
