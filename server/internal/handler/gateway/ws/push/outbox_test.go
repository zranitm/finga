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
	"finga/internal/handler/gateway/ws/push"
	model "finga/internal/model/gateway"
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
	players := []model.PlayerView{{ID: "p-1", Name: "Robin", Class: "bowman", Level: 3, X: 1, Y: 2, Z: 3, VX: 5, VY: -5, VZ: 4, HX: 0.6, HZ: 0.8, OnGround: true, HP: 50, MaxHP: 105, Dead: true, Invulnerable: true,
		Look: model.Look{Outfit: "knight_plate", Headwear: "knight_helm", Face: "round_glasses", Weapon: "greatsword", Wings: "bat_wings", HideHeadwear: true, Revision: 4}}}
	wirePlayers := []*clientv1.PlayerView{{Id: "p-1", Name: "Robin", Class: "bowman", Level: 3, X: 1, Y: 2, Z: 3, Vx: 5, Vy: -5, Vz: 4, Hx: 0.6, Hz: 0.8, OnGround: true, Hp: 50, MaxHp: 105, Dead: true, Invulnerable: true,
		Look: &clientv1.Look{Outfit: "knight_plate", Headwear: "knight_helm", Face: "round_glasses", Weapon: "greatsword", Wings: "bat_wings", HideHeadwear: true}}}
	mobs := []model.MobView{{ID: 7, Type: "green_slime", X: 3, Y: 4, Z: 5, Yaw: 1.5, HP: 9, MaxHP: 30}}
	wireMobs := []*clientv1.MobView{{Id: 7, Type: "green_slime", X: 3, Y: 4, Z: 5, Yaw: 1.5, Hp: 9, MaxHp: 30}}
	pouches := []model.PouchView{{ID: 3, Kind: model.PouchBox, X: 5, Y: 6, Z: 7, GoneInMs: 60000, Items: []model.PouchItem{
		{ID: 1, Template: "slime_gel", Quantity: 2}, {ID: 2, Template: "iron_sword", Quantity: 1, Stats: &model.ItemStats{PhysicalAttack: 17, CritRate: 5}},
	}}}
	wirePouches := []*clientv1.PouchView{{Id: 3, Kind: clientv1.PouchKind_POUCH_KIND_BOX, X: 5, Y: 6, Z: 7, GoneInMs: 60000, Items: []*clientv1.PouchItem{
		{Item: 1, Template: "slime_gel", Quantity: 2}, {Item: 2, Template: "iron_sword", Quantity: 1, Stats: &clientv1.ItemStats{PhysicalAttack: 17, CritRate: 5}},
	}}}
	party := &model.PartyView{Leader: "p-1", Members: []model.PartyMember{{CharacterID: "p-1", Name: "Robin", Class: "bowman", Level: 3, HP: 50, MaxHP: 105, MapID: "forest", Online: true}}}
	wireParty := &clientv1.PartyView{Leader: "p-1", Members: []*clientv1.PartyMember{{CharacterId: "p-1", Name: "Robin", Class: "bowman", Level: 3, Hp: 50, MaxHp: 105, MapId: "forest", Online: true}}}
	character := model.CharacterView{ID: "p-1", Name: "Robin", Class: "bowman", Level: 2, Exp: 15, Stats: model.Stats{STR: 5, DEX: 12, INT: 4, AGI: 10, VIT: 5}, HP: 50, MaxHP: 105, Energy: 6, Sheet: sheet}
	wireCharacter := &clientv1.CharacterView{Id: "p-1", Name: "Robin", Class: "bowman", Level: 2, Exp: 15, Stats: &clientv1.Stats{Str: 5, Dex: 12, Int: 4, Agi: 10, Vit: 5}, Hp: 50, MaxHp: 105, Energy: 6, Sheet: wireSheet}
	slots := []model.InventorySlot{{Tab: "etc", Slot: 1}, {Tab: "etc", Slot: 2, Item: &model.ItemView{ID: "i-1", Template: "slime_gel", Tab: "etc", Slot: 2, Quantity: 3}}}
	wireSlots := []*clientv1.InventorySlot{{Tab: "etc", Slot: 1}, {Tab: "etc", Slot: 2, Item: &clientv1.ItemView{ItemId: "i-1", Template: "slime_gel", Tab: "etc", Slot: 2, Quantity: 3}}}
	tests := []struct {
		push model.Push
		want proto.Message
	}{
		{model.Snapshot{Players: players, Mobs: mobs, Pouches: pouches}, &clientv1.Snapshot{Players: wirePlayers, Mobs: wireMobs, Pouches: wirePouches}},
		{model.Snapshot{Pouches: []model.PouchView{{ID: 4, Kind: model.PouchSmall}}}, &clientv1.Snapshot{Pouches: []*clientv1.PouchView{{Id: 4, Kind: clientv1.PouchKind_POUCH_KIND_POUCH, Items: []*clientv1.PouchItem{}}}}},
		{
			model.Snapshot{Mobs: mobs, FieldBoss: &model.FieldBoss{MobID: 9, Type: "king_slime"}},
			&clientv1.Snapshot{Mobs: wireMobs, FieldBoss: &clientv1.FieldBoss{MobId: 9, Type: "king_slime"}},
		},
		{
			model.Snapshot{Mobs: mobs, FieldBossGone: &model.FieldBossGone{MobID: 9, Type: "king_slime", BackInMs: 180000}},
			&clientv1.Snapshot{Mobs: wireMobs, FieldBossGone: &clientv1.FieldBossGone{MobId: 9, Type: "king_slime", BackInMs: 180000}},
		},
		{ // the boss anywhere on the map, for its bar
			model.Snapshot{FieldBoss: &model.FieldBoss{MobID: 9, Type: "king_slime"}, Boss: &model.MobView{ID: 9, Type: "king_slime", X: 400, HP: 900, MaxHP: 1000}},
			&clientv1.Snapshot{FieldBoss: &clientv1.FieldBoss{MobId: 9, Type: "king_slime"}, Boss: &clientv1.MobView{Id: 9, Type: "king_slime", X: 400, Hp: 900, MaxHp: 1000}},
		},
		{model.FieldBoss{MobID: 9, Type: "king_slime"}, &clientv1.FieldBoss{MobId: 9, Type: "king_slime"}},
		{model.FieldBossGone{MobID: 9, Type: "king_slime", BackInMs: 300000}, &clientv1.FieldBossGone{MobId: 9, Type: "king_slime", BackInMs: 300000}},
		{
			model.Delta{AckSeq: 3, Energy: proto.Uint32(6), Pouches: pouches, PouchesGone: []uint64{2}, Frame: &model.Frame{Parts: []model.Part{{Players: players, Mobs: mobs, RemovedPlayers: []string{"p-2"}, RemovedMobs: []uint64{6}}}}, Parts: []int{0}},
			&clientv1.Delta{AckSeq: 3, Energy: proto.Uint32(6), Pouches: wirePouches, PouchesGone: []uint32{2}, Players: wirePlayers, Mobs: wireMobs, RemovedPlayers: []string{"p-2"}, RemovedMobs: []uint32{6}},
		},
		{
			model.Delta{Frame: &model.Frame{Parts: []model.Part{{Mobs: []model.MobView{{ID: 1, Pose: model.Flinching}, {ID: 2, Pose: model.Down}, {ID: 3, Pose: model.Rising}}}}}, Parts: []int{0}},
			&clientv1.Delta{Mobs: []*clientv1.MobView{{Id: 1, Pose: clientv1.MobPose_MOB_POSE_FLINCH}, {Id: 2, Pose: clientv1.MobPose_MOB_POSE_DOWN}, {Id: 3, Pose: clientv1.MobPose_MOB_POSE_RISING}}},
		},
		{
			model.Delta{Frame: &model.Frame{Parts: []model.Part{{
				Players: []model.PlayerView{{ID: "p-1", Held: 30, Slow: model.PlayerSlow{Left: 12, Factor: 0.6}, Statuses: []model.StatusView{{ID: "slow", Ends: 81246}, {ID: "stun", Ends: 81250}}}},
				Mobs:    []model.MobView{{ID: 7, Statuses: []model.StatusView{{ID: "poison", Ends: 81290}}}},
			}}}, Parts: []int{0}},
			&clientv1.Delta{
				Players: []*clientv1.PlayerView{{Id: "p-1", Held: 30, Slow: &clientv1.SlowWalk{Left: 12, Factor: 0.6}, Statuses: []*clientv1.StatusView{{Id: "slow", Ends: 81246}, {Id: "stun", Ends: 81250}}, Look: &clientv1.Look{}}},
				Mobs:    []*clientv1.MobView{{Id: 7, Statuses: []*clientv1.StatusView{{Id: "poison", Ends: 81290}}}},
			},
		},
		{ // two cells' parts and the map's, in the player's order, the parts they're not sent left out, and what they stopped seeing
			model.Delta{
				Frame: &model.Frame{Parts: []model.Part{
					{Mobs: []model.MobView{{ID: 1}}, RemovedMobs: []uint64{4}},
					{Players: []model.PlayerView{{ID: "p-9"}}},
					{Mobs: []model.MobView{{ID: 2}}, RemovedMobs: []uint64{5}},
					{Boss: &model.MobView{ID: 9, HP: 800}},
				}},
				Parts:  []int{2, 0, 3},
				Unseen: &model.Unseen{Players: []string{"p-3"}, Mobs: []uint64{6, 7}},
			},
			&clientv1.Delta{
				Mobs: []*clientv1.MobView{{Id: 2}, {Id: 1}}, RemovedMobs: []uint32{5, 4}, Boss: &clientv1.MobView{Id: 9, Hp: 800},
				Unseen: &clientv1.Unseen{Players: []string{"p-3"}, Mobs: []uint32{6, 7}},
			},
		},
		{ // a sprint's count, which the player's own reconcile steps on from
			model.Delta{Frame: &model.Frame{Parts: []model.Part{{Players: []model.PlayerView{{ID: "p-1", Sprint: 120}}}}}, Parts: []int{0}},
			&clientv1.Delta{Players: []*clientv1.PlayerView{{Id: "p-1", Sprint: 120, Look: &clientv1.Look{}}}},
		},
		{ // walk mode, the mode the player's own reconcile steps on in
			model.Delta{Frame: &model.Frame{Parts: []model.Part{{Players: []model.PlayerView{{ID: "p-1", Walk: true}}}}}, Parts: []int{0}},
			&clientv1.Delta{Players: []*clientv1.PlayerView{{Id: "p-1", Walk: true, Look: &clientv1.Look{}}}},
		},
		{ // a break-free's leap through mobs, with the farthest point it reached clear of them
			model.Delta{Frame: &model.Frame{Parts: []model.Part{{Players: []model.PlayerView{{ID: "p-1", Move: model.PlayerMove{VX: -13, Left: 9, Through: true, ClearX: 6.5, ClearZ: 3}}}}}}, Parts: []int{0}},
			&clientv1.Delta{Players: []*clientv1.PlayerView{{Id: "p-1", Move: &clientv1.SkillMove{Vx: -13, Left: 9, Through: true, ClearX: 6.5, ClearZ: 3}, Look: &clientv1.Look{}}}},
		},
		{model.MapChange{MapID: "forest", X: 2, Z: 1, Yaw: -0.5}, &clientv1.MapChange{MapId: "forest", X: 2, Z: 1, Yaw: -0.5}},
		{model.TeleportCancelled{Reason: model.TeleportMoved}, &clientv1.TeleportCancelled{Reason: clientv1.TeleportCancelReason_TELEPORT_CANCEL_REASON_MOVED}},
		{model.PartySpots{Spots: []model.PartySpot{{CharacterID: "p-2", X: 140.5, Z: 812.25}}}, &clientv1.PartySpots{Spots: []*clientv1.PartySpot{{CharacterId: "p-2", X: 140.5, Z: 812.25}}}},
		{model.PartySpots{}, &clientv1.PartySpots{}},
		{model.WaypointFound{Waypoint: "north"}, &clientv1.WaypointFound{Waypoint: "north"}},
		{
			model.AreaEntered{Area: "oasis", FieldBoss: &model.FieldBoss{MobID: 9, Type: "king_slime"}},
			&clientv1.AreaEntered{Area: "oasis", FieldBoss: &clientv1.FieldBoss{MobId: 9, Type: "king_slime"}},
		},
		{
			model.AreaEntered{Area: "oasis", FieldBossGone: &model.FieldBossGone{MobID: 9, Type: "king_slime", BackInMs: 120000}},
			&clientv1.AreaEntered{Area: "oasis", FieldBossGone: &clientv1.FieldBossGone{MobId: 9, Type: "king_slime", BackInMs: 120000}},
		},
		{model.ArenaBar{Bar: []string{"slash", "", "guard"}}, &clientv1.ArenaBar{SkillBar: []string{"slash", "", "guard"}}},
		{
			model.MatchState{
				Match: "m-1", Mode: "team", Phase: model.PhaseFight, Round: 2, Wins: [2]int{1, 0}, EndsIn: 45 * time.Second,
				Sides: [2][]model.MatchFighter{{{ID: "p-1", Name: "Robin", Class: "bowman"}}, {{ID: "b-1", Name: "Bot", Class: "warrior", Bot: true}}},
			},
			&clientv1.MatchState{
				Match: "m-1", Mode: "team", Phase: clientv1.MatchPhase_MATCH_PHASE_FIGHT, Round: 2, Wins: []int32{1, 0}, EndsInMs: 45000,
				Sides: []*clientv1.MatchSide{
					{Fighters: []*clientv1.MatchFighter{{Id: "p-1", Name: "Robin", Class: "bowman"}}},
					{Fighters: []*clientv1.MatchFighter{{Id: "b-1", Name: "Bot", Class: "warrior", Bot: true}}},
				},
			},
		},
		{model.TeleportCancelled{Reason: model.TeleportActed}, &clientv1.TeleportCancelled{Reason: clientv1.TeleportCancelReason_TELEPORT_CANCEL_REASON_ACTED}},
		{model.TeleportCancelled{Reason: model.TeleportHit}, &clientv1.TeleportCancelled{Reason: clientv1.TeleportCancelReason_TELEPORT_CANCEL_REASON_HIT}},
		{model.TeleportCancelled{Reason: model.TeleportDied}, &clientv1.TeleportCancelled{Reason: clientv1.TeleportCancelReason_TELEPORT_CANCEL_REASON_DIED}},
		{
			model.Damage{Source: model.Entity{Character: "p-1"}, Target: model.Entity{Mob: 7}, Amount: 21, Crit: true, Miss: true},
			&clientv1.Damage{Source: "p:p-1", Target: "m:7", Amount: 21, Crit: true, Miss: true},
		},
		{
			model.Damage{Source: model.Entity{Mob: 7}, Target: model.Entity{Character: "p-1"}, Amount: 4, Status: "poison", Shielded: 3},
			&clientv1.Damage{Source: "m:7", Target: "p:p-1", Amount: 4, StatusId: "poison", Shielded: 3},
		},
		{model.Died{CharacterID: "p-1", X: 1, Y: 2, Z: 3}, &clientv1.Died{CharacterId: "p-1", X: 1, Y: 2, Z: 3}},
		{model.SkillUsed{Caster: "p-1", Skill: "strike", Stage: 2, Trait: "concussion", Aim: -1.5, Target: model.Entity{Mob: 7}}, &clientv1.SkillUsed{Caster: "p-1", Skill: "strike", Stage: 2, Trait: "concussion", Aim: -1.5, Target: "m:7"}},
		{model.SkillUsed{Caster: "p-1", Skill: "war_cry"}, &clientv1.SkillUsed{Caster: "p-1", Skill: "war_cry"}}, // no lock: no ref, not m:0
		{model.AttackUsed{Attacker: "p-1", LengthTicks: 18, Aim: 1.5}, &clientv1.AttackUsed{Attacker: "p-1", LengthTicks: 18, Aim: 1.5}},
		{model.MobAttacked{MobID: 7, Attack: 1, Yaw: -1.5, ToX: 13.625, ToY: 0.5, ToZ: 3.25}, &clientv1.MobAttacked{Mob: 7, Attack: 1, Yaw: -1.5, ToX: 13.625, ToY: 0.5, ToZ: 3.25}},
		{model.Respawned{CharacterID: "p-1", MapID: "town", X: 13, Z: 2, Yaw: 1.5, HP: 102}, &clientv1.Respawned{CharacterId: "p-1", MapId: "town", X: 13, Z: 2, Yaw: 1.5, Hp: 102}},
		{model.ExpGained{Amount: 5, Exp: 15}, &clientv1.ExpGained{Amount: 5, Exp: 15}},
		{model.LevelUp{Character: character}, &clientv1.LevelUp{Character: wireCharacter}},
		{model.Promoted{Character: character}, &clientv1.Promoted{Character: wireCharacter}},
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
		{model.StatsChanged{Sheet: &sheet, HP: 50, MaxHP: 135}, &clientv1.StatsChanged{Sheet: wireSheet, Hp: 50, MaxHp: 135}},
		{model.StatsChanged{HP: 50, MaxHP: 135}, &clientv1.StatsChanged{Hp: 50, MaxHp: 135}}, // regen: the sheet stays unset, not zeroed
		{
			model.Loot{Kind: model.LootTaken, MapID: "forest", Pouch: 7, Owner: "p-2", OwnerName: "Robin", Items: []model.LootItem{{Template: "slime_gel", Quantity: 2}, {Template: "red_potion", Quantity: 1}}},
			&clientv1.Loot{Kind: clientv1.LootKind_LOOT_KIND_TAKEN, MapId: "forest", PouchId: 7, OwnerId: "p-2", OwnerName: "Robin", Items: []*clientv1.LootItem{{Template: "slime_gel", Quantity: 2}, {Template: "red_potion", Quantity: 1}}},
		},
		{model.Loot{Kind: model.LootLeft, Pouch: 9}, &clientv1.Loot{Kind: clientv1.LootKind_LOOT_KIND_LEFT, PouchId: 9, Items: []*clientv1.LootItem{}}},
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
			model.NpcDialog{NPC: "warrior_trainer", Promotion: &model.PromotionOffer{Class: "warrior", Conditions: []model.PromotionCondition{
				{Kind: model.ConditionLevel, Need: 10, Have: 9}, {Kind: model.ConditionStat, ID: "str", Need: 30, Have: 30},
				{Kind: model.ConditionSkill, ID: "strike", Need: 10, Have: 4}, {Kind: model.ConditionQuest, ID: "warriors_trial", Need: 1},
			}}},
			&clientv1.NpcDialog{NpcId: "warrior_trainer", Promotion: &clientv1.PromotionOffer{Class: "warrior", Conditions: []*clientv1.PromotionCondition{
				{Kind: clientv1.PromotionConditionKind_PROMOTION_CONDITION_KIND_LEVEL, Need: 10, Have: 9},
				{Kind: clientv1.PromotionConditionKind_PROMOTION_CONDITION_KIND_STAT, Id: "str", Need: 30, Have: 30},
				{Kind: clientv1.PromotionConditionKind_PROMOTION_CONDITION_KIND_SKILL, Id: "strike", Need: 10, Have: 4},
				{Kind: clientv1.PromotionConditionKind_PROMOTION_CONDITION_KIND_QUEST, Id: "warriors_trial", Need: 1},
			}}},
		},
		{
			model.QuestChanged{Change: model.QuestProgressed, Quest: model.QuestState{Quest: "a", Status: model.QuestActive, Progress: []int{2, 0}, TimesFinished: 3}},
			&clientv1.QuestChanged{Change: clientv1.QuestChange_QUEST_CHANGE_PROGRESSED, Quest: &clientv1.QuestState{QuestId: "a", Status: clientv1.QuestStatus_QUEST_STATUS_ACTIVE, Progress: []int32{2, 0}, TimesFinished: 3}},
		},
		{
			model.QuestChanged{Change: model.QuestAbandoned, Quest: model.QuestState{Quest: "a"}},
			&clientv1.QuestChanged{Change: clientv1.QuestChange_QUEST_CHANGE_ABANDONED, Quest: &clientv1.QuestState{QuestId: "a"}},
		},
		{model.PvpQueued{Mode: "team", Since: 2 * time.Second, Members: []string{"p-1", "p-2"}}, &clientv1.PvpQueued{Mode: "team", SinceMs: 2000, Members: []string{"p-1", "p-2"}}},
		{model.PvpUnqueued{Reason: model.PvpUnqueueLapsed, By: []string{"p-2"}}, &clientv1.PvpUnqueued{Reason: clientv1.PvpUnqueueReason_PVP_UNQUEUE_REASON_LAPSED, By: []string{"p-2"}}},
		{model.PvpOffer{Offer: "of-1", Mode: "team", Bots: 2, Until: 10 * time.Second}, &clientv1.PvpOffer{Offer: "of-1", Mode: "team", Bots: 2, UntilMs: 10000}},
		{model.PvpOfferCancelled{Offer: "of-1", Reason: model.PvpGoneLapsed, Requeued: true}, &clientv1.PvpOfferGone{Offer: "of-1", Reason: clientv1.PvpOfferGoneReason_PVP_OFFER_GONE_REASON_LAPSED, Requeued: true}},
		{
			model.MatchEnded{Mode: "team", Result: model.MatchLoss, Wins: [2]int{2, 1}, Rating: &model.PvpRatingChange{Before: 1000, After: 968, RankBefore: "bronze", RankAfter: "iron", PlacementLeft: 4}, Left: true},
			&clientv1.MatchEnded{Mode: "team", Result: clientv1.MatchOutcome_MATCH_OUTCOME_LOSS, Wins: []int32{2, 1}, Rating: &clientv1.PvpRatingChange{Before: 1000, After: 968, RankBefore: "bronze", RankAfter: "iron", PlacementLeft: 4}, Left: true},
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

// The players sent a frame's part share one encoding of it; each message adds only its player's own fields.
func TestAFramesPlayersShareEachPartsEncoding(t *testing.T) {
	frames := push.NewFrames()
	frame := &model.Frame{Parts: []model.Part{{Players: []model.PlayerView{{ID: "p-1"}}}, {Mobs: []model.MobView{{ID: 2}}}}}
	next := &model.Frame{Parts: []model.Part{{}}}
	shared := func(f *model.Frame, parts []int, ack uint32) []*wsconn.Shared {
		t.Helper()
		s := &socket{}
		push.NewOutbox(s, s, frames).Deliver(model.Delivery{Tick: 9, Push: model.Delta{Frame: f, Parts: parts, AckSeq: ack}})
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
	push.NewOutbox(s, s, push.NewFrames()).Welcome(model.Welcome{
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
