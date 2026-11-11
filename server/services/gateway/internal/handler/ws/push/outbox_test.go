package push_test

import (
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	clientv1 "finga/api/gen/client/v1"
	"finga/pkg/wsconn"
	"finga/services/gateway/internal/handler/ws/push"
	"finga/services/gateway/internal/model"
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
			push.NewOutbox(s, s, push.NewFrames()).End(tt.reason)
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
	o := push.NewOutbox(s, s, push.NewFrames())
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
	o := push.NewOutbox(s, s, push.NewFrames())
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
				o.Deliver(model.Delivery{Push: model.Delta{Frame: &model.Frame{}}})
				o.Reply("", &clientv1.Pong{})
			}
		})
	}
	o.End(model.EndServerRestart)
	close(stop)
	wg.Wait()
	o.Deliver(model.Delivery{Push: model.Delta{Frame: &model.Frame{}}})
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
			HPRegen: 0.25, MPRegen: 0.2, AttackSpeed: 0.03, CooldownSpeed: 0.01, MoveSpeed: 0.014, MaxHP: 135, MaxMP: 33,
		},
		DerivedWorn: model.DerivedStats{
			PhysicalAttack: 11.5, MagicAttack: 0.5, PhysicalDefense: 3, MagicDefense: 1, Accuracy: 2, Evasion: 0.5, CritRate: 0.01, CritDamage: 0.1, Penetration: 0.05,
			HPRegen: 0.1, MPRegen: 0.1, AttackSpeed: 0.02, CooldownSpeed: 0.005, MoveSpeed: 0.01, MaxHP: 30, MaxMP: 1,
		},
		Points:              model.Points{Unspent: 2, Spent: model.Stats{DEX: 1, AGI: 2}, FreeResetLeft: true},
		AttackCooldownTicks: 16,
	}
	wireSheet = &clientv1.StatSheet{
		Primary: &clientv1.Stats{Str: 5, Dex: 13, Int: 4, Agi: 10, Vit: 5}, PrimaryWorn: &clientv1.Stats{Dex: 1},
		Derived: &clientv1.DerivedStats{
			PhysicalAttack: 41.5, MagicAttack: 8, PhysicalDefense: 3, MagicDefense: 4.5, Accuracy: 30, Evasion: 12.5, CritRate: 0.1, CritDamage: 1.5, Penetration: 0.05,
			HpRegen: 0.25, MpRegen: 0.2, AttackSpeed: 0.03, CooldownSpeed: 0.01, MoveSpeed: 0.014, MaxHp: 135, MaxMp: 33,
		},
		DerivedWorn: &clientv1.DerivedStats{
			PhysicalAttack: 11.5, MagicAttack: 0.5, PhysicalDefense: 3, MagicDefense: 1, Accuracy: 2, Evasion: 0.5, CritRate: 0.01, CritDamage: 0.1, Penetration: 0.05,
			HpRegen: 0.1, MpRegen: 0.1, AttackSpeed: 0.02, CooldownSpeed: 0.005, MoveSpeed: 0.01, MaxHp: 30, MaxMp: 1,
		},
		Points:              &clientv1.Points{Unspent: 2, Spent: &clientv1.Stats{Dex: 1, Agi: 2}, FreeResetLeft: true},
		AttackCooldownTicks: 16,
	}
	// A bow with every stat rolled, as no real one is.
	bow = model.ItemView{ID: "i-2", Template: "short_bow", Quantity: 1, Stats: &model.ItemStats{
		STR: 1, DEX: 2, INT: 3, AGI: 4, VIT: 5, PhysicalAttack: 6, MagicAttack: 7, PhysicalDefense: 8, MagicDefense: 9, MaxHP: 10, MaxMP: 11, Accuracy: 12, Evasion: 13,
		CritRate: 14, CritDamage: 15, Penetration: 16, HPRegen: 17, MPRegen: 18, AttackSpeed: 19, CooldownSpeed: 20, MoveSpeed: 21,
	}}
	wireBow = &clientv1.ItemView{ItemId: "i-2", Template: "short_bow", Quantity: 1, Stats: &clientv1.ItemStats{
		Str: 1, Dex: 2, Int: 3, Agi: 4, Vit: 5, PhysicalAttack: 6, MagicAttack: 7, PhysicalDefense: 8, MagicDefense: 9, MaxHp: 10, MaxMp: 11, Accuracy: 12, Evasion: 13,
		CritRate: 14, CritDamage: 15, Penetration: 16, HpRegen: 17, MpRegen: 18, AttackSpeed: 19, CooldownSpeed: 20, MoveSpeed: 21,
	}}
)

func TestEachPushReachesTheClientAsItsMessage(t *testing.T) {
	players := []model.PlayerView{{ID: "p-1", Name: "Robin", Class: "bowman", Level: 3, X: 1, Y: 2, VX: 200, VY: -5, Facing: -1, OnGround: true, Climbing: true, HP: 50, MaxHP: 105, Dead: true, Invulnerable: true}}
	wirePlayers := []*clientv1.PlayerView{{Id: "p-1", Name: "Robin", Class: "bowman", Level: 3, X: 1, Y: 2, Vx: 200, Vy: -5, Facing: -1, OnGround: true, Climbing: true, Hp: 50, MaxHp: 105, Dead: true, Invulnerable: true}}
	mobs := []model.MobView{{ID: 7, Type: "green_slime", X: 3, Y: 4, Facing: 1, HP: 9, MaxHP: 30}}
	wireMobs := []*clientv1.MobView{{Id: 7, Type: "green_slime", X: 3, Y: 4, Facing: 1, Hp: 9, MaxHp: 30}}
	drops := []model.DropView{{ID: 3, Template: "slime_gel", Quantity: 2, X: 5, Y: 6}}
	wireDrops := []*clientv1.DropView{{Id: 3, Template: "slime_gel", Quantity: 2, X: 5, Y: 6}}
	party := &model.PartyView{Leader: "p-1", Members: []model.PartyMember{{CharacterID: "p-1", Name: "Robin", Level: 3, HP: 50, MaxHP: 105, MapID: "forest", Online: true}}}
	wireParty := &clientv1.PartyView{Leader: "p-1", Members: []*clientv1.PartyMember{{CharacterId: "p-1", Name: "Robin", Level: 3, Hp: 50, MaxHp: 105, MapId: "forest", Online: true}}}
	character := model.CharacterView{ID: "p-1", Name: "Robin", Class: "bowman", Level: 2, Exp: 15, Stats: model.Stats{STR: 5, DEX: 12, INT: 4, AGI: 10, VIT: 5}, HP: 50, MaxHP: 105, MP: 10, MaxMP: 33, Sheet: sheet}
	wireCharacter := &clientv1.CharacterView{Id: "p-1", Name: "Robin", Class: "bowman", Level: 2, Exp: 15, Stats: &clientv1.Stats{Str: 5, Dex: 12, Int: 4, Agi: 10, Vit: 5}, Hp: 50, MaxHp: 105, Mp: 10, MaxMp: 33, Sheet: wireSheet}
	slots := []model.InventorySlot{{Tab: "etc", Slot: 1}, {Tab: "etc", Slot: 2, Item: &model.ItemView{ID: "i-1", Template: "slime_gel", Tab: "etc", Slot: 2, Quantity: 3}}}
	wireSlots := []*clientv1.InventorySlot{{Tab: "etc", Slot: 1}, {Tab: "etc", Slot: 2, Item: &clientv1.ItemView{ItemId: "i-1", Template: "slime_gel", Tab: "etc", Slot: 2, Quantity: 3}}}
	tests := []struct {
		push model.Push
		want proto.Message
	}{
		{model.Snapshot{Players: players, Mobs: mobs, Drops: drops}, &clientv1.Snapshot{Players: wirePlayers, Mobs: wireMobs, Drops: wireDrops}},
		{
			model.Delta{AckSeq: 3, Frame: &model.Frame{Players: players, Mobs: mobs, Drops: drops, RemovedPlayers: []string{"p-2"}, RemovedMobs: []uint64{6}, RemovedDrops: []uint64{2}}},
			&clientv1.Delta{AckSeq: 3, Players: wirePlayers, Mobs: wireMobs, Drops: wireDrops, RemovedPlayers: []string{"p-2"}, RemovedMobs: []uint32{6}, RemovedDrops: []uint32{2}},
		},
		{model.MapChange{MapID: "forest", X: 64, Y: 608}, &clientv1.MapChange{MapId: "forest", X: 64, Y: 608}},
		{
			model.Damage{Source: model.Entity{Character: "p-1"}, Target: model.Entity{Mob: 7}, Amount: 21, Crit: true, Miss: true},
			&clientv1.Damage{Source: "p:p-1", Target: "m:7", Amount: 21, Crit: true, Miss: true},
		},
		{model.Died{CharacterID: "p-1", X: 1, Y: 2}, &clientv1.Died{CharacterId: "p-1", X: 1, Y: 2}},
		{model.Respawned{CharacterID: "p-1", MapID: "town", X: 416, Y: 608, HP: 102}, &clientv1.Respawned{CharacterId: "p-1", MapId: "town", X: 416, Y: 608, Hp: 102}},
		{model.ExpGained{Amount: 5, Exp: 15}, &clientv1.ExpGained{Amount: 5, Exp: 15}},
		{model.LevelUp{Character: character}, &clientv1.LevelUp{Character: wireCharacter}},
		{
			model.ChatMessage{Channel: model.ChatParty, Sender: "p-1", SenderName: "Robin", Text: "psst"},
			&clientv1.ChatMessage{Channel: clientv1.ChatChannel_CHAT_CHANNEL_PARTY, Sender: "p-1", SenderName: "Robin", Text: "psst"},
		},
		{
			model.ChatMessage{Channel: model.ChatMap, Sender: "p-1", SenderName: "Robin", Text: "hi"},
			&clientv1.ChatMessage{Channel: clientv1.ChatChannel_CHAT_CHANNEL_MAP, Sender: "p-1", SenderName: "Robin", Text: "hi"},
		},
		{model.InventoryChanged{Revision: 5, Slots: slots}, &clientv1.InventoryChanged{Revision: 5, Slots: wireSlots}},
		{
			model.InventoryChanged{Revision: 6, Slots: []model.InventorySlot{{Tab: "equip", Slot: 0}}, Equipment: []model.EquipSlot{{Slot: "weapon", Item: &bow}, {Slot: "hat"}}},
			&clientv1.InventoryChanged{Revision: 6, Slots: []*clientv1.InventorySlot{{Tab: "equip", Slot: 0}}, Equipment: []*clientv1.EquipSlot{{EquipSlot: "weapon", Item: wireBow}, {EquipSlot: "hat"}}},
		},
		{model.StatsChanged{Sheet: &sheet, HP: 50, MaxHP: 135, MP: 10, MaxMP: 33}, &clientv1.StatsChanged{Sheet: wireSheet, Hp: 50, MaxHp: 135, Mp: 10, MaxMp: 33}},
		{model.StatsChanged{HP: 50, MaxHP: 135, MP: 11, MaxMP: 33}, &clientv1.StatsChanged{Hp: 50, MaxHp: 135, Mp: 11, MaxMp: 33}}, // regen: the sheet stays unset, not zeroed
		{
			model.Loot{Kind: model.LootCollected, MapID: "forest", DropID: 7, Owner: "p-2", OwnerName: "Robin", Template: "slime_gel", Quantity: 2},
			&clientv1.Loot{Kind: clientv1.LootKind_LOOT_KIND_COLLECTED, MapId: "forest", DropId: 7, OwnerId: "p-2", OwnerName: "Robin", Template: "slime_gel", Quantity: 2},
		},
		{model.Loot{Kind: model.LootWaiting, DropID: 8}, &clientv1.Loot{Kind: clientv1.LootKind_LOOT_KIND_WAITING, DropId: 8}},
		{model.Loot{Kind: model.LootLost, DropID: 9}, &clientv1.Loot{Kind: clientv1.LootKind_LOOT_KIND_LOST, DropId: 9}},
		{model.PartyState{Party: party}, &clientv1.PartyState{Party: wireParty}},
		{model.PartyState{}, &clientv1.PartyState{}},
		{model.PartyInvited{InviteID: "inv-2", From: "p-1", FromName: "Robin"}, &clientv1.PartyInvited{InviteId: "inv-2", From: "p-1", FromName: "Robin"}},
		{
			model.NpcDialog{NPC: "town_guide", Gives: []model.QuestOffer{
				{Quest: "a", State: model.OfferAvailable}, {Quest: "b", State: model.OfferActive}, {Quest: "c", State: model.OfferFinished},
				{Quest: "d", State: model.OfferUnmet, Unmet: model.QuestConditions{Level: 5, Class: "thief", Quests: []string{"a"}}},
			}, Takes: []model.QuestTurnIn{{Quest: "b", Ready: true}, {Quest: "e"}}},
			&clientv1.NpcDialog{NpcId: "town_guide", Gives: []*clientv1.QuestOffer{
				{QuestId: "a", State: clientv1.QuestOfferState_QUEST_OFFER_STATE_AVAILABLE}, {QuestId: "b", State: clientv1.QuestOfferState_QUEST_OFFER_STATE_ACTIVE},
				{QuestId: "c", State: clientv1.QuestOfferState_QUEST_OFFER_STATE_FINISHED},
				{QuestId: "d", State: clientv1.QuestOfferState_QUEST_OFFER_STATE_CONDITIONS_UNMET, Unmet: &clientv1.QuestConditions{Level: 5, Class: "thief", Quests: []string{"a"}}},
			}, Takes: []*clientv1.QuestTurnIn{{QuestId: "b", Ready: true}, {QuestId: "e"}}},
		},
		{
			model.QuestChanged{Change: model.QuestProgressed, Quest: model.QuestState{Quest: "a", Status: model.QuestActive, Progress: []int{2, 0}, TimesFinished: 3}},
			&clientv1.QuestChanged{Change: clientv1.QuestChange_QUEST_CHANGE_PROGRESSED, Quest: &clientv1.QuestState{QuestId: "a", Status: clientv1.QuestStatus_QUEST_STATUS_ACTIVE, Progress: []int32{2, 0}, TimesFinished: 3}},
		},
		{
			model.QuestChanged{Change: model.QuestAbandoned, Quest: model.QuestState{Quest: "a"}},
			&clientv1.QuestChanged{Change: clientv1.QuestChange_QUEST_CHANGE_ABANDONED, Quest: &clientv1.QuestState{QuestId: "a"}},
		},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%T", tt.push), func(t *testing.T) {
			s := &socket{}
			push.NewOutbox(s, s, push.NewFrames()).Deliver(model.Delivery{Tick: 9, Push: tt.push})
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

// A frame's players share one encoding of it; each message adds only its player's ack.
func TestAFramesPlayersShareOneEncoding(t *testing.T) {
	frames := push.NewFrames()
	frame, next := &model.Frame{Players: []model.PlayerView{{ID: "p-1"}}}, &model.Frame{}
	shared := func(f *model.Frame, ack uint32) *wsconn.Shared {
		t.Helper()
		s := &socket{}
		push.NewOutbox(s, s, frames).Deliver(model.Delivery{Tick: 9, Push: model.Delta{Frame: f, AckSeq: ack}})
		sp, ok := s.sent[0].Data.(wsconn.Spliced)
		if !ok || !proto.Equal(sp.Own, &clientv1.Delta{AckSeq: ack}) {
			t.Fatalf("sent %+v", s.sent[0].Data)
		}
		return sp.Shared
	}
	a, b := shared(frame, 1), shared(frame, 2)
	if a != b {
		t.Fatal("two players of one frame got two encodings")
	}
	if shared(next, 1) == a {
		t.Fatal("the next frame reused the last one's encoding")
	}
}

func TestWelcomeCarriesTheCharacterItsItemsAtTheirRevisionAndItsQuests(t *testing.T) {
	s := &socket{}
	at := time.UnixMilli(1_700_000_000_000)
	push.NewOutbox(s, s, push.NewFrames()).Welcome(model.Welcome{
		Token:             "t-2",
		Character:         model.CharacterView{ID: "p-1", Name: "Robin", Class: "bowman", Level: 2, Exp: 15, Stats: model.Stats{STR: 5, DEX: 12, INT: 4, AGI: 10, VIT: 5}, HP: 50, MaxHP: 105, MP: 10, MaxMP: 33, Sheet: sheet},
		Inventory:         []model.ItemView{{ID: "i-1", Template: "slime_gel", Tab: "etc", Slot: 2, Quantity: 3}},
		Equipment:         []model.EquipSlot{{Slot: "weapon", Item: &bow}},
		InventoryRevision: 8,
		MapID:             "town",
		ServerTime:        at,
		Quests:            []model.QuestState{{Quest: "a", Status: model.QuestFinished, TimesFinished: 2}},
	})
	want := &clientv1.Welcome{
		Token:             "t-2",
		Character:         &clientv1.CharacterView{Id: "p-1", Name: "Robin", Class: "bowman", Level: 2, Exp: 15, Stats: &clientv1.Stats{Str: 5, Dex: 12, Int: 4, Agi: 10, Vit: 5}, Hp: 50, MaxHp: 105, Mp: 10, MaxMp: 33, Sheet: wireSheet},
		Inventory:         []*clientv1.ItemView{{ItemId: "i-1", Template: "slime_gel", Tab: "etc", Slot: 2, Quantity: 3}},
		Equipment:         []*clientv1.EquipSlot{{EquipSlot: "weapon", Item: wireBow}},
		InventoryRevision: 8,
		MapId:             "town",
		ServerTime:        at.UnixMilli(),
		Quests:            []*clientv1.QuestState{{QuestId: "a", Status: clientv1.QuestStatus_QUEST_STATUS_FINISHED, TimesFinished: 2}},
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
	o := push.NewOutbox(s, s, push.NewFrames())
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

func (s *socket) ID() string { return "c-1" }

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
