package transport_test

import (
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	clientv1 "finga/api/gen/client/v1"
	"finga/internal/application/world"
	"finga/internal/handler/transport"
	model "finga/internal/model/account"
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

// A sheet with every field set, and its wire form.
var (
	sheet = model.StatSheet{
		Primary: model.Stats{STR: 5, DEX: 13, INT: 4, AGI: 10, VIT: 5}, PrimaryWorn: model.Stats{DEX: 1},
		Derived: model.DerivedStats{
			PhysicalAttack: 41.5, MagicAttack: 8, PhysicalDefense: 3, MagicDefense: 4.5, Accuracy: 30, Evasion: 12.5, CritRate: 0.1, CritDamage: 1.5, Penetration: 0.05,
			HPRegen: 0.25, CooldownSpeed: 0.01, MoveSpeed: 0.014, MaxHP: 135,
		},
		DerivedWorn: model.DerivedStats{
			PhysicalAttack: 11.5, MagicAttack: 0.5, PhysicalDefense: 3, MagicDefense: 1, Accuracy: 2, Evasion: 0.5, CritRate: 0.01, CritDamage: 0.1, Penetration: 0.05,
			HPRegen: 0.1, CooldownSpeed: 0.005, MoveSpeed: 0.01, MaxHP: 30,
		},
		Points:            model.Points{Unspent: 2, Spent: model.Stats{DEX: 1, AGI: 2}, FreeResetLeft: true},
		AttackLengthTicks: 16,
		PrimarySkills:     model.Stats{VIT: 1},
		DerivedSkills:     model.DerivedStats{HPRegen: 0.6, MaxHP: 10},
		Skills:            model.SkillPoints{Unspent: 3, Invested: map[string]int{"second_wind": 1, "strike": 4}},
		Buffs:             []model.BuffView{{Skill: "iron_body", Left: 12400 * time.Millisecond, Points: 3}, {Skill: "war_cry", Left: 8250 * time.Millisecond, Points: 4, From: "p-2"}},
	}
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
	// A bow with every stat rolled, as no real one is.
	bow = model.ItemView{ID: "i-2", Template: "short_bow", Quantity: 1, Stats: &model.ItemStats{
		STR: 1, DEX: 2, INT: 3, AGI: 4, VIT: 5, PhysicalAttack: 6, MagicAttack: 7, PhysicalDefense: 8, MagicDefense: 9, MaxHP: 10, Accuracy: 11, Evasion: 12,
		CritRate: 13, CritDamage: 14, Penetration: 15, HPRegen: 16, CooldownSpeed: 17, MoveSpeed: 18,
	}}
	wireBow = &clientv1.ItemView{ItemId: "i-2", Template: "short_bow", Quantity: 1, Stats: &clientv1.ItemStats{
		Str: 1, Dex: 2, Int: 3, Agi: 4, Vit: 5, PhysicalAttack: 6, MagicAttack: 7, PhysicalDefense: 8, MagicDefense: 9, MaxHp: 10, Accuracy: 11, Evasion: 12,
		CritRate: 13, CritDamage: 14, Penetration: 15, HpRegen: 16, CooldownSpeed: 17, MoveSpeed: 18,
	}}
)

func TestEachPushReachesTheClientAsItsMessage(t *testing.T) {
	party := &model.PartyView{Leader: "p-1", Members: []model.PartyMember{{CharacterID: "p-1", Name: "Robin", Class: "bowman", Level: 3, HP: 50, MaxHP: 105, MapID: "forest", Online: true}}}
	wireParty := &clientv1.PartyView{Leader: "p-1", Members: []*clientv1.PartyMember{{CharacterId: "p-1", Name: "Robin", Class: "bowman", Level: 3, Hp: 50, MaxHp: 105, MapId: "forest", Online: true}}}
	tests := []struct {
		push any
		want proto.Message
	}{
		{
			model.PiecesAdded{Pieces: []model.AddedPiece{{ID: "knight_plate", Cause: "item"}, {ID: "helm_gold", Cause: "rank"}}},
			&clientv1.PiecesAdded{Pieces: []*clientv1.AddedPiece{{Id: "knight_plate", Cause: "item"}, {Id: "helm_gold", Cause: "rank"}}},
		},
		{
			model.ChatMessage{Channel: model.ChatParty, Sender: "p-1", SenderName: "Robin", Text: "psst"},
			&clientv1.ChatMessage{Channel: clientv1.ChatChannel_CHAT_CHANNEL_PARTY, Sender: "p-1", SenderName: "Robin", Text: "psst"},
		},
		{model.PartyState{Party: party}, &clientv1.PartyState{Party: wireParty}},
		{model.PartyState{}, &clientv1.PartyState{}},
		{model.PartyInvited{InviteID: "inv-2", From: "p-1", FromName: "Robin"}, &clientv1.PartyInvited{InviteId: "inv-2", From: "p-1", FromName: "Robin"}},
		{model.PvpQueued{Mode: "team", Since: 2 * time.Second, Members: []string{"p-1", "p-2"}}, &clientv1.PvpQueued{Mode: "team", SinceMs: 2000, Members: []string{"p-1", "p-2"}}},
		{model.PvpQueued{Mode: "duel", Since: 20 * time.Second, Members: []string{"p-1"}, WaitingRoom: true}, &clientv1.PvpQueued{Mode: "duel", SinceMs: 20000, Members: []string{"p-1"}, WaitingRoom: true}},
		{model.PvpUnqueued{Reason: model.PvpUnqueueLapsed, By: []string{"p-2"}}, &clientv1.PvpUnqueued{Reason: clientv1.PvpUnqueueReason_PVP_UNQUEUE_REASON_LAPSED, By: []string{"p-2"}}},
		{model.PvpOffer{Offer: "of-1", Mode: "team", Bots: 2, Until: 10 * time.Second}, &clientv1.PvpOffer{Offer: "of-1", Mode: "team", Bots: 2, UntilMs: 10000}},
		{model.PvpOfferCancelled{Offer: "of-1", Reason: model.PvpGoneLapsed, Requeued: true}, &clientv1.PvpOfferGone{Offer: "of-1", Reason: clientv1.PvpOfferGoneReason_PVP_OFFER_GONE_REASON_LAPSED, Requeued: true}},
		{
			model.MatchEnded{Mode: "team", Result: model.MatchLoss, Wins: [2]int{2, 1}, Rating: &model.PvpRatingChange{Before: 1000, After: 968, RankBefore: "bronze", RankAfter: "iron", PlacementLeft: 4}, Left: true},
			&clientv1.MatchEnded{Mode: "team", Result: clientv1.MatchOutcome_MATCH_OUTCOME_LOSS, Wins: []int32{2, 1}, Rating: &clientv1.PvpRatingChange{Before: 1000, After: 968, RankBefore: "bronze", RankAfter: "iron", PlacementLeft: 4}, Left: true},
		},
		{ // a battle's score, and no rounds
			model.MatchEnded{Mode: "battle", Result: model.MatchWin, Battle: true, Wins: [2]int{1, 0}, Score: [2]int{1000, 640}},
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

func TestWelcomeCarriesTheCharacterItsItemsAtTheirRevisionItsQuestsAndItsSkillTimers(t *testing.T) {
	s := &socket{}
	at := time.UnixMilli(1_700_000_000_000)
	transport.NewOutbox(s, s, slog.New(slog.DiscardHandler), allEncoders()).Welcome(model.Welcome{
		Token:             "t-2",
		Character:         model.CharacterView{ID: "p-1", Name: "Robin", Class: "bowman", Level: 2, Exp: 15, Stats: model.Stats{STR: 5, DEX: 12, INT: 4, AGI: 10, VIT: 5}, HP: 50, MaxHP: 105, Energy: 6, Sheet: sheet},
		Inventory:         []model.ItemView{{ID: "i-1", Template: "slime_gel", Tab: "etc", Slot: 2, Quantity: 3}},
		Equipment:         []model.EquipSlot{{Slot: "weapon", Item: &bow}},
		InventoryRevision: 8,
		MapID:             "town",
		ServerTime:        at,
		Quests:            []model.QuestState{{Quest: "a", Status: model.QuestFinished, TimesFinished: 2}},
		Timers:            []model.SkillTimer{{Skill: "focus", Kind: model.BuffTimer, Left: 1500 * time.Millisecond, Points: 4, Trait: "expose"}},
		Waypoints:         []string{"town", "west"},
	})
	want := &clientv1.Welcome{
		Token:             "t-2",
		Character:         &clientv1.CharacterView{Id: "p-1", Name: "Robin", Class: "bowman", Level: 2, Exp: 15, Stats: &clientv1.Stats{Str: 5, Dex: 12, Int: 4, Agi: 10, Vit: 5}, Hp: 50, MaxHp: 105, Energy: 6, Sheet: wireSheet},
		Inventory:         []*clientv1.ItemView{{ItemId: "i-1", Template: "slime_gel", Tab: "etc", Slot: 2, Quantity: 3}},
		Equipment:         []*clientv1.EquipSlot{{EquipSlot: "weapon", Item: wireBow}},
		InventoryRevision: 8,
		MapId:             "town",
		ServerTime:        at.UnixMilli(),
		Quests:            []*clientv1.QuestState{{QuestId: "a", Status: clientv1.QuestStatus_QUEST_STATUS_FINISHED, TimesFinished: 2}},
		SkillTimers:       []*clientv1.SkillTimer{{Skill: "focus", Kind: clientv1.SkillTimerKind_SKILL_TIMER_KIND_BUFF, LeftMs: 1500, Points: 4, Trait: "expose"}},
		Waypoints:         []string{"town", "west"},
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.sent) != 1 || s.sent[0].Type != "Welcome" || !proto.Equal(s.sent[0].Data.(proto.Message), want) {
		t.Fatalf("sent %+v, want %v", s.sent, want)
	}
	if !slices.Equal(s.welcomes, []string{fmt.Sprintf("login %d", proto.Size(want))}) {
		t.Fatalf("welcomes measured %v", s.welcomes)
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
