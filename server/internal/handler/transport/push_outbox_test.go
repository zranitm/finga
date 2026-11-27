package transport_test

import (
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	clientv1 "finga/api/gen/client/v1"
	"finga/internal/application/pvp"
	"finga/internal/application/social"
	"finga/internal/application/world"
	"finga/internal/handler/transport"
	model "finga/internal/model/account"
	"finga/internal/shared/kernel"
	"finga/pkg/wsconn"
)

func TestEndTellsTheClientWhyWhenTheProtocolHasAReason(t *testing.T) {
	tests := []struct {
		reason model.EndReason
		kicked clientv1.KickReason // unspecified: no Kicked, the client reconnects
		code   int
		why    string
	}{
		{model.EndLoggedInElsewhere, clientv1.KickReason_KICK_REASON_LOGGED_IN_ELSEWHERE, wsconn.CloseKicked, "logged_in_elsewhere"},
		{model.EndSessionExpired, clientv1.KickReason_KICK_REASON_SESSION_EXPIRED, wsconn.CloseKicked, "session_expired"},
		{model.EndServerRestart, clientv1.KickReason_KICK_REASON_SERVER_RESTART, wsconn.CloseKicked, "server_restart"},
		{model.EndDeliveryBacklog, clientv1.KickReason_KICK_REASON_UNSPECIFIED, wsconn.CloseInternal, "delivery_backlog"},
		{model.EndRejoinFailed, clientv1.KickReason_KICK_REASON_UNSPECIFIED, wsconn.CloseInternal, "rejoin_failed"},
		{model.EndTickRateChanged, clientv1.KickReason_KICK_REASON_UNSPECIFIED, wsconn.CloseInternal, "tick_rate_changed"},
	}
	for _, tt := range tests {
		t.Run(string(tt.reason), func(t *testing.T) {
			s := &socket{}
			transport.NewOutbox(s, s, slog.New(slog.DiscardHandler), allEncoders()).End(tt.reason)
			s.mu.Lock()
			defer s.mu.Unlock()
			switch {
			case tt.kicked == clientv1.KickReason_KICK_REASON_UNSPECIFIED && len(s.sent) != 0:
				t.Fatalf("sent %+v before closing", s.sent)
			case tt.kicked != clientv1.KickReason_KICK_REASON_UNSPECIFIED && (len(s.sent) != 1 || s.sent[0].Data.(*clientv1.Kicked).GetReason() != tt.kicked):
				t.Fatalf("sent %+v, want Kicked %s", s.sent, tt.kicked)
			}
			if s.code != tt.code || s.why != tt.why {
				t.Fatalf("closed %d %q, want %d %q", s.code, s.why, tt.code, tt.why)
			}
		})
	}
}

// The first end wins: racing ends (a session end and a newer Hello) send one Kicked, whose reason the close repeats.
func TestOnlyTheFirstEndIsSent(t *testing.T) {
	s := &socket{}
	o := transport.NewOutbox(s, s, slog.New(slog.DiscardHandler), allEncoders())
	var wg sync.WaitGroup
	for _, r := range []model.EndReason{model.EndSessionExpired, model.EndLoggedInElsewhere, model.EndServerRestart, model.EndRejoinFailed} {
		wg.Go(func() { o.End(r) })
	}
	wg.Go(func() { o.Kick(clientv1.KickReason_KICK_REASON_UNAUTHENTICATED) })
	wg.Wait()
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.sent) > 1 {
		t.Fatalf("sent %d messages, want at most one Kicked: %+v", len(s.sent), s.sent)
	}
	if len(s.sent) == 1 {
		why := strings.ToLower(strings.TrimPrefix(s.sent[0].Data.(*clientv1.Kicked).GetReason().String(), "KICK_REASON_"))
		if s.why != why {
			t.Fatalf("Kicked %s, but closed as %q", why, s.why)
		}
	}
}

// Once a connection ends nothing more is sent: its Kicked is the last message, whatever sends race it.
func TestNothingFollowsTheEnd(t *testing.T) {
	s := &socket{}
	o := transport.NewOutbox(s, s, slog.New(slog.DiscardHandler), allEncoders())
	stop := make(chan struct{})
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			for {
				select {
				case <-stop:
					return
				default:
				}
				o.Deliver(model.Delivery{Event: world.Framed{Frame: &world.Frame{}}})
				o.Reply("", &clientv1.Pong{})
			}
		})
	}
	o.End(model.EndServerRestart)
	close(stop)
	wg.Wait()
	o.Deliver(model.Delivery{Event: world.Framed{Frame: &world.Frame{}}})
	o.Reply("r-1", &clientv1.Response{Ok: true})
	o.Welcome(model.Welcome{})
	s.mu.Lock()
	defer s.mu.Unlock()
	if last := s.sent[len(s.sent)-1]; last.Type != "Kicked" {
		t.Fatalf("%s sent after Kicked", last.Type)
	}
}

// The wire forms of a sheet and a bow with every field set, which the world events' tests build their inputs for.
var (
	wireSheet = &clientv1.StatSheet{
		Primary: &clientv1.Stats{Str: 5, Dex: 13, Int: 4, Agi: 10, Vit: 5}, PrimaryWorn: &clientv1.Stats{Dex: 1},
		Derived: &clientv1.DerivedStats{
			PhysicalAttack: 41.5, MagicAttack: 8, PhysicalDefense: 3, MagicDefense: 4.5, Accuracy: 30, Evasion: 12.5, CritRate: 0.1, CritDamage: 1.5, Penetration: 0.05,
			HpRegen: 0.25, CooldownSpeed: 0.01, MoveSpeed: 0.014, MaxHp: 135,
		},
		DerivedWorn: &clientv1.DerivedStats{
			PhysicalAttack: 11.5, MagicAttack: 0.5, PhysicalDefense: 3, MagicDefense: 1, Accuracy: 2, Evasion: 0.5, CritRate: 0.01, CritDamage: 0.1, Penetration: 0.05,
			HpRegen: 0.1, CooldownSpeed: 0.005, MoveSpeed: 0.01, MaxHp: 30,
		},
		Points:            &clientv1.Points{Unspent: 2, Spent: &clientv1.Stats{Dex: 1, Agi: 2}, FreeResetLeft: true},
		AttackLengthTicks: 16,
		PrimarySkills:     &clientv1.Stats{Vit: 1},
		DerivedSkills:     &clientv1.DerivedStats{HpRegen: 0.6, MaxHp: 10},
		Skills:            &clientv1.SkillPoints{Unspent: 3, Invested: map[string]int32{"second_wind": 1, "strike": 4}},
		Buffs:             []*clientv1.BuffView{{Skill: "iron_body", LeftMs: 12400, Points: 3}, {Skill: "war_cry", LeftMs: 8250, Points: 4, From: "p-2"}},
	}
	wireBow = &clientv1.ItemView{ItemId: "i-2", Template: "short_bow", Quantity: 1, Stats: &clientv1.ItemStats{
		Str: 1, Dex: 2, Int: 3, Agi: 4, Vit: 5, PhysicalAttack: 6, MagicAttack: 7, PhysicalDefense: 8, MagicDefense: 9, MaxHp: 10, Accuracy: 11, Evasion: 12,
		CritRate: 13, CritDamage: 14, Penetration: 15, HpRegen: 16, CooldownSpeed: 17, MoveSpeed: 18,
	}}
)

func TestEachPushReachesTheClientAsItsMessage(t *testing.T) {
	p1, p2, to := id(1), id(2), id(3)
	party := &social.Party{Leader: p1, Members: []social.PartyMember{{ID: p1, Name: "Robin", Class: "bowman", Level: 3, HP: 50, MaxHP: 105, MapID: "forest", Online: true}}}
	wireParty := &clientv1.PartyView{Leader: p1.String(), Members: []*clientv1.PartyMember{{CharacterId: p1.String(), Name: "Robin", Class: "bowman", Level: 3, Hp: 50, MaxHp: 105, MapId: "forest", Online: true}}}
	invite, _ := social.ParseInviteID("00000000-0000-7000-8000-0000000000b1")
	tests := []struct {
		push any
		want proto.Message
	}{
		{
			social.ChatLine{To: to, Sender: p1, SenderName: "Robin", Text: "psst"},
			&clientv1.ChatMessage{Channel: clientv1.ChatChannel_CHAT_CHANNEL_PARTY, Sender: p1.String(), SenderName: "Robin", Text: "psst"},
		},
		{social.PartyState{To: to, Party: party}, &clientv1.PartyState{Party: wireParty}},
		{social.PartyState{To: to}, &clientv1.PartyState{}},
		{social.Invited{To: to, Invite: invite, From: p1, FromName: "Robin"}, &clientv1.PartyInvited{InviteId: invite.String(), From: p1.String(), FromName: "Robin"}},
		{
			pvp.PiecesAdded{Pieces: []pvp.RankPiece{{Piece: "helm_silver"}, {Piece: "helm_gold"}}, Feats: []pvp.FeatPiece{{Piece: "victors_horns"}}},
			&clientv1.PiecesAdded{Pieces: []*clientv1.AddedPiece{{Id: "helm_silver", Cause: "rank"}, {Id: "helm_gold", Cause: "rank"}, {Id: "victors_horns", Cause: "feat"}}},
		},
		{pvp.Queued{Mode: "team", Since: 2 * time.Second, Members: []kernel.CharacterID{p1, p2}}, &clientv1.PvpQueued{Mode: "team", SinceMs: 2000, Members: []string{p1.String(), p2.String()}}},
		{pvp.Queued{Mode: "duel", Since: 20 * time.Second, Members: []kernel.CharacterID{p1}, WaitingRoom: true}, &clientv1.PvpQueued{Mode: "duel", SinceMs: 20000, Members: []string{p1.String()}, WaitingRoom: true}},
		{pvp.Unqueued{Reason: pvp.UnqueueLapsed, By: []kernel.CharacterID{p2}}, &clientv1.PvpUnqueued{Reason: clientv1.PvpUnqueueReason_PVP_UNQUEUE_REASON_LAPSED, By: []string{p2.String()}}},
		{pvp.Offered{Offer: "of-1", Mode: "team", Bots: 2, Until: 10 * time.Second}, &clientv1.PvpOffer{Offer: "of-1", Mode: "team", Bots: 2, UntilMs: 10000}},
		{pvp.OfferGone{Offer: "of-1", Reason: pvp.GoneLapsed, Requeued: true}, &clientv1.PvpOfferGone{Offer: "of-1", Reason: clientv1.PvpOfferGoneReason_PVP_OFFER_GONE_REASON_LAPSED, Requeued: true}},
		{
			pvp.Ended{Mode: "team", Outcome: pvp.Loss, Wins: [2]int{2, 1}, Rating: &pvp.RatingChange{Before: 1000, After: 968, RankBefore: "bronze", RankAfter: "iron", PlacementLeft: 4}, Left: true},
			&clientv1.MatchEnded{Mode: "team", Result: clientv1.MatchOutcome_MATCH_OUTCOME_LOSS, Wins: []int32{2, 1}, Rating: &clientv1.PvpRatingChange{Before: 1000, After: 968, RankBefore: "bronze", RankAfter: "iron", PlacementLeft: 4}, Left: true},
		},
		{ // a battle's score, and no rounds
			pvp.Ended{Mode: "battle", Outcome: pvp.Win, Battle: true, Wins: [2]int{1, 0}, Score: [2]int{1000, 640}},
			&clientv1.MatchEnded{Mode: "battle", Result: clientv1.MatchOutcome_MATCH_OUTCOME_WIN, Score: []int32{1000, 640}},
		},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%T", tt.push), func(t *testing.T) {
			s := &socket{}
			transport.NewOutbox(s, s, slog.New(slog.DiscardHandler), allEncoders()).Deliver(model.Delivery{Tick: 9, Event: tt.push})
			s.mu.Lock()
			defer s.mu.Unlock()
			if len(s.sent) != 1 {
				t.Fatalf("sent %d messages", len(s.sent))
			}
			m := s.sent[0]
			data := m.Data
			if sp, ok := data.(wsconn.Spliced); ok {
				data = sp.Message()
			}
			if name := string(tt.want.ProtoReflect().Descriptor().Name()); m.Type != name || m.Tick != 9 || !proto.Equal(data.(proto.Message), tt.want) || s.counted[0] != name {
				t.Fatalf("sent %s at tick %d: %v, want %s: %v", m.Type, m.Tick, m.Data, name, tt.want)
			}
		})
	}
}

// The players sent a frame's part share one encoding of it; each message adds only its player's own fields.
func TestAFramesPlayersShareEachPartsEncoding(t *testing.T) {
	enc := allEncoders()
	frame := &world.Frame{Parts: []world.Part{{Players: []world.PlayerView{{ID: id(1)}}}, {Mobs: []world.MobView{{ID: 2}}}}}
	next := &world.Frame{Parts: []world.Part{{}}}
	shared := func(f *world.Frame, parts []int, ack uint32) []*wsconn.Shared {
		t.Helper()
		s := &socket{}
		transport.NewOutbox(s, s, slog.New(slog.DiscardHandler), enc).Deliver(model.Delivery{Tick: 9, Event: world.Framed{Frame: f, Recipient: world.Recipient{Parts: parts, AckSeq: ack}}})
		sp, ok := s.sent[0].Data.(wsconn.Spliced)
		if !ok || !proto.Equal(sp.Own, &clientv1.Delta{AckSeq: ack}) || len(sp.Shared) != len(parts) {
			t.Fatalf("sent %+v", s.sent[0].Data)
		}
		return sp.Shared
	}
	a, c, b := shared(frame, []int{0}, 1), shared(frame, []int{1}, 2), shared(frame, []int{1, 0}, 3)
	if a[0] != b[1] || b[0] == b[1] || c[0] != b[0] {
		t.Fatal("two players sent one part got two encodings of it, or two parts shared one")
	}
	if shared(next, []int{0}, 1)[0] == a[0] {
		t.Fatal("another frame reused this one's encoding")
	}
	if shared(frame, []int{0}, 4)[0] != a[0] {
		t.Fatal("a frame's encoding was lost to another map's frame sent between its players")
	}
}

func TestAWelcomeWithNoTokenIsMeasuredAsARejoins(t *testing.T) {
	s := &socket{}
	o := transport.NewOutbox(s, s, slog.New(slog.DiscardHandler), allEncoders())
	o.Welcome(model.Welcome{Token: "t-1", MapID: "town"})
	o.Welcome(model.Welcome{MapID: "town"})
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.welcomes) != 2 || !strings.HasPrefix(s.welcomes[0], "login ") || !strings.HasPrefix(s.welcomes[1], "rejoin ") {
		t.Fatalf("welcomes measured %v", s.welcomes)
	}
}

// socket records what a connection sends, how it first closes, and the types the outbox counted.
type socket struct {
	mu       sync.Mutex
	sent     []wsconn.Message
	counted  []string
	welcomes []string // "<kind> <bytes>", one a Welcome
	code     int
	why      string
}

func (s *socket) MessageOut(msgType string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.counted = append(s.counted, msgType)
}

func (s *socket) WelcomeSent(kind string, bytes int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.welcomes = append(s.welcomes, fmt.Sprintf("%s %d", kind, bytes))
}

func (s *socket) MessageIn(string) {}
func (s *socket) SlowConsumer()    {}
func (s *socket) Unencoded(string) {}
func (s *socket) ID() string       { return "c-1" }

func (s *socket) Send(m wsconn.Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sent = append(s.sent, m)
	return nil
}

func (s *socket) Close(code int, why string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.code == 0 {
		s.code, s.why = code, why
	}
}
