package transport_test

import (
	"fmt"
	"log/slog"
	"slices"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	clientv1 "finga/api/gen/client/v1"
	"finga/internal/application/item"
	"finga/internal/application/world"
	"finga/internal/handler/transport"
	model "finga/internal/model/account"
	"finga/internal/shared/gamedata"
	"finga/internal/shared/kernel"
	"finga/internal/shared/progression"
	"finga/internal/shared/sim"
	"finga/pkg/wsconn"
)

// id is the character whose ID ends in n, as it reads on the wire too.
func id(n int) kernel.CharacterID {
	c, err := kernel.ParseCharacterID(fmt.Sprintf("00000000-0000-7000-8000-%012d", n))
	if err != nil {
		panic(err)
	}
	return c
}

// A sheet with every field set, as the world sends it; wireSheet is its wire form, the free reset used turned to whether it is left.
var worldSheet = world.Sheet{
	Primary: gamedata.Stats{STR: 5, DEX: 13, INT: 4, AGI: 10, VIT: 5}, PrimaryWorn: gamedata.Stats{DEX: 1},
	Derived: progression.Derived{
		PhysicalAttack: 41.5, MagicAttack: 8, PhysicalDefense: 3, MagicDefense: 4.5, Accuracy: 30, Evasion: 12.5, CritRate: 0.1, CritDamage: 1.5, Penetration: 0.05,
		HPRegen: 0.25, CooldownSpeed: 0.01, MoveSpeed: 0.014, MaxHP: 135,
	},
	DerivedWorn: progression.Derived{
		PhysicalAttack: 11.5, MagicAttack: 0.5, PhysicalDefense: 3, MagicDefense: 1, Accuracy: 2, Evasion: 0.5, CritRate: 0.01, CritDamage: 0.1, Penetration: 0.05,
		HPRegen: 0.1, CooldownSpeed: 0.005, MoveSpeed: 0.01, MaxHP: 30,
	},
	PrimarySkills:     gamedata.Stats{VIT: 1},
	DerivedSkills:     progression.Derived{HPRegen: 0.6, MaxHP: 10},
	Points:            world.Points{Unspent: 2, Spent: gamedata.Stats{DEX: 1, AGI: 2}},
	Skills:            world.Skills{Unspent: 3, Points: map[string]int{"second_wind": 1, "strike": 4}},
	AttackLengthTicks: 16,
	Buffs:             []world.BuffView{{Skill: "iron_body", Left: 12400 * time.Millisecond, Points: 3}, {Skill: "war_cry", Left: 8250 * time.Millisecond, Points: 4, From: id(2)}},
}

// wireWorldSheet is worldSheet on the wire.
func wireWorldSheet() *clientv1.StatSheet {
	w := proto.Clone(wireSheet).(*clientv1.StatSheet)
	w.Buffs = []*clientv1.BuffView{{Skill: "iron_body", LeftMs: 12400, Points: 3}, {Skill: "war_cry", LeftMs: 8250, Points: 4, From: id(2).String()}}
	return w
}

func TestEachWorldEventReachesTheClientAsItsMessage(t *testing.T) {
	p1, p2 := id(1), id(2)
	players := []world.PlayerView{{ID: p1, Name: "Robin", Class: "bowman", Level: 3, HP: 50, MaxHP: 105, Dead: true, Invulnerable: true,
		State: sim.State{X: 1, Y: 2, Z: 3, VX: 5, VY: -5, VZ: 4, HX: 0.6, HZ: 0.8, OnGround: true},
		Look:  world.Look{Outfit: "knight_plate", Headwear: "knight_helm", Face: "round_glasses", Weapon: "greatsword", Wings: "bat_wings", HideHeadwear: true, Revision: 4}}}
	wirePlayers := []*clientv1.PlayerView{{Id: p1.String(), Name: "Robin", Class: "bowman", Level: 3, X: 1, Y: 2, Z: 3, Vx: 5, Vy: -5, Vz: 4, Hx: 0.6, Hz: 0.8, OnGround: true, Hp: 50, MaxHp: 105, Dead: true, Invulnerable: true,
		Look: &clientv1.Look{Outfit: "knight_plate", Headwear: "knight_helm", Face: "round_glasses", Weapon: "greatsword", Wings: "bat_wings", HideHeadwear: true}}}
	mobs := []world.MobView{{ID: 7, Type: "green_slime", X: 3, Y: 4, Z: 5, Yaw: 1.5, HP: 9, MaxHP: 30}}
	wireMobs := []*clientv1.MobView{{Id: 7, Type: "green_slime", X: 3, Y: 4, Z: 5, Yaw: 1.5, Hp: 9, MaxHp: 30}}
	pouches := []world.PouchView{{ID: 3, Kind: world.PouchBox, X: 5, Y: 6, Z: 7, GoneIn: time.Minute, Items: []world.PouchItem{
		{ID: 1, Template: "slime_gel", Quantity: 2}, {ID: 2, Template: "iron_sword", Quantity: 1, Stats: &progression.ItemStats{PhysicalAttack: 17, CritRate: 5}},
	}}}
	wirePouches := []*clientv1.PouchView{{Id: 3, Kind: clientv1.PouchKind_POUCH_KIND_BOX, X: 5, Y: 6, Z: 7, GoneInMs: 60000, Items: []*clientv1.PouchItem{
		{Item: 1, Template: "slime_gel", Quantity: 2}, {Item: 2, Template: "iron_sword", Quantity: 1, Stats: &clientv1.ItemStats{PhysicalAttack: 17, CritRate: 5}},
	}}}
	levelUp := world.LevelUp{ID: p1, Name: "Robin", Class: "bowman", Level: 2, Exp: 15, Stats: gamedata.Stats{STR: 5, DEX: 12, INT: 4, AGI: 10, VIT: 5}, HP: 50, MaxHP: 105, Sheet: worldSheet}
	wireCharacter := &clientv1.CharacterView{Id: p1.String(), Name: "Robin", Class: "bowman", Level: 2, Exp: 15, Stats: &clientv1.Stats{Str: 5, Dex: 12, Int: 4, Agi: 10, Vit: 5}, Hp: 50, MaxHp: 105, Sheet: wireWorldSheet()}
	itemID1, _ := kernel.ParseItemID("00000000-0000-7000-8000-0000000000a1")
	itemID2, _ := kernel.ParseItemID("00000000-0000-7000-8000-0000000000a2")
	bowRoll := &progression.ItemStats{
		STR: 1, DEX: 2, INT: 3, AGI: 4, VIT: 5, PhysicalAttack: 6, MagicAttack: 7, PhysicalDefense: 8, MagicDefense: 9, MaxHP: 10, Accuracy: 11, Evasion: 12,
		CritRate: 13, CritDamage: 14, Penetration: 15, HPRegen: 16, CooldownSpeed: 17, MoveSpeed: 18,
	}
	wireBowRoll := &clientv1.ItemView{ItemId: itemID2.String(), Template: "short_bow", Quantity: 1, Stats: wireBow.Stats}
	framed := func(frame *world.Frame, r world.Recipient) world.Framed {
		return world.Framed{Frame: frame, Recipient: r}
	}
	tests := []struct {
		event any
		want  proto.Message
	}{
		{world.Snapshot{Players: players, Mobs: mobs, Pouches: pouches}, &clientv1.Snapshot{Players: wirePlayers, Mobs: wireMobs, Pouches: wirePouches}},
		{world.Snapshot{Pouches: []world.PouchView{{ID: 4, Kind: world.PouchSmall}}}, &clientv1.Snapshot{Pouches: []*clientv1.PouchView{{Id: 4, Kind: clientv1.PouchKind_POUCH_KIND_POUCH, Items: []*clientv1.PouchItem{}}}}},
		{
			world.Snapshot{Mobs: mobs, FieldBoss: &world.FieldBoss{Mob: 9, Type: "king_slime"}},
			&clientv1.Snapshot{Mobs: wireMobs, FieldBoss: &clientv1.FieldBoss{MobId: 9, Type: "king_slime"}},
		},
		{
			world.Snapshot{Mobs: mobs, FieldBossGone: &world.FieldBossGone{Mob: 9, Type: "king_slime", BackIn: 180 * time.Second}},
			&clientv1.Snapshot{Mobs: wireMobs, FieldBossGone: &clientv1.FieldBossGone{MobId: 9, Type: "king_slime", BackInMs: 180000}},
		},
		{ // the boss anywhere on the map, for its bar
			world.Snapshot{FieldBoss: &world.FieldBoss{Mob: 9, Type: "king_slime"}, Boss: &world.MobView{ID: 9, Type: "king_slime", X: 400, HP: 900, MaxHP: 1000}},
			&clientv1.Snapshot{FieldBoss: &clientv1.FieldBoss{MobId: 9, Type: "king_slime"}, Boss: &clientv1.MobView{Id: 9, Type: "king_slime", X: 400, Hp: 900, MaxHp: 1000}},
		},
		{world.FieldBoss{Mob: 9, Type: "king_slime"}, &clientv1.FieldBoss{MobId: 9, Type: "king_slime"}},
		{world.FieldBossGone{Mob: 9, Type: "king_slime", BackIn: 300 * time.Second}, &clientv1.FieldBossGone{MobId: 9, Type: "king_slime", BackInMs: 300000}},
		{
			framed(&world.Frame{Parts: []world.Part{{Players: players, Mobs: mobs, Removed: []kernel.CharacterID{p2}, RemovedMobs: []uint64{6}}}}, world.Recipient{Parts: []int{0}, AckSeq: 3, Energy: new(6), Own: nil, Pouches: world.Pouches{Changed: pouches, Gone: []uint64{2}}}),
			&clientv1.Delta{AckSeq: 3, Energy: proto.Uint32(6), Pouches: wirePouches, PouchesGone: []uint32{2}, Players: wirePlayers, Mobs: wireMobs, RemovedPlayers: []string{p2.String()}, RemovedMobs: []uint32{6}},
		},
		{
			framed(&world.Frame{Parts: []world.Part{{Mobs: []world.MobView{{ID: 1, Pose: world.Flinching}, {ID: 2, Pose: world.Down}, {ID: 3, Pose: world.Rising}}}}}, world.Recipient{Parts: []int{0}}),
			&clientv1.Delta{Mobs: []*clientv1.MobView{{Id: 1, Pose: clientv1.MobPose_MOB_POSE_FLINCH}, {Id: 2, Pose: clientv1.MobPose_MOB_POSE_DOWN}, {Id: 3, Pose: clientv1.MobPose_MOB_POSE_RISING}}},
		},
		{
			framed(&world.Frame{Parts: []world.Part{{
				Players: []world.PlayerView{{ID: p1, State: sim.State{Held: 30, Slow: sim.Slow{Left: 12, Factor: 0.6}}, Statuses: []world.StatusView{{ID: "slow", Ends: 81246}, {ID: "stun", Ends: 81250}}}},
				Mobs:    []world.MobView{{ID: 7, Statuses: []world.StatusView{{ID: "poison", Ends: 81290}}}},
			}}}, world.Recipient{Parts: []int{0}}),
			&clientv1.Delta{
				Players: []*clientv1.PlayerView{{Id: p1.String(), Held: 30, Slow: &clientv1.SlowWalk{Left: 12, Factor: 0.6}, Statuses: []*clientv1.StatusView{{Id: "slow", Ends: 81246}, {Id: "stun", Ends: 81250}}, Look: &clientv1.Look{}}},
				Mobs:    []*clientv1.MobView{{Id: 7, Statuses: []*clientv1.StatusView{{Id: "poison", Ends: 81290}}}},
			},
		},
		{ // two cells' parts and the map's, in the player's order, the parts they're not sent left out, and what they stopped seeing
			framed(&world.Frame{Parts: []world.Part{
				{Mobs: []world.MobView{{ID: 1}}, RemovedMobs: []uint64{4}},
				{Players: []world.PlayerView{{ID: id(9)}}},
				{Mobs: []world.MobView{{ID: 2}}, RemovedMobs: []uint64{5}},
				{Boss: &world.MobView{ID: 9, HP: 800}},
			}}, world.Recipient{Parts: []int{2, 0, 3}, Unseen: &world.Unseen{Players: []kernel.CharacterID{id(3)}, Mobs: []uint64{6, 7}}}),
			&clientv1.Delta{
				Mobs: []*clientv1.MobView{{Id: 2}, {Id: 1}}, RemovedMobs: []uint32{5, 4}, Boss: &clientv1.MobView{Id: 9, Hp: 800},
				Unseen: &clientv1.Unseen{Players: []string{id(3).String()}, Mobs: []uint32{6, 7}},
			},
		},
		{ // a sprint's count, which the player's own reconcile steps on from
			framed(&world.Frame{Parts: []world.Part{{Players: []world.PlayerView{{ID: p1, State: sim.State{Sprint: 120}}}}}}, world.Recipient{Parts: []int{0}}),
			&clientv1.Delta{Players: []*clientv1.PlayerView{{Id: p1.String(), Sprint: 120, Look: &clientv1.Look{}}}},
		},
		{ // walk mode, the mode the player's own reconcile steps on in
			framed(&world.Frame{Parts: []world.Part{{Players: []world.PlayerView{{ID: p1, State: sim.State{Walk: true}}}}}}, world.Recipient{Parts: []int{0}}),
			&clientv1.Delta{Players: []*clientv1.PlayerView{{Id: p1.String(), Walk: true, Look: &clientv1.Look{}}}},
		},
		{ // a break-free's leap through mobs, with the farthest point it reached clear of them
			framed(&world.Frame{Parts: []world.Part{{Players: []world.PlayerView{{ID: p1, State: sim.State{Move: sim.Move{VX: -13, Left: 9, Through: true, ClearX: 6.5, ClearZ: 3}}}}}}}, world.Recipient{Parts: []int{0}}),
			&clientv1.Delta{Players: []*clientv1.PlayerView{{Id: p1.String(), Move: &clientv1.SkillMove{Vx: -13, Left: 9, Through: true, ClearX: 6.5, ClearZ: 3}, Look: &clientv1.Look{}}}},
		},
		{world.Teleport{MapID: "forest", X: 2, Z: 1, Yaw: -0.5}, &clientv1.MapChange{MapId: "forest", X: 2, Z: 1, Yaw: -0.5}},
		{world.TeleportCancelled{Reason: world.TeleportMoved}, &clientv1.TeleportCancelled{Reason: clientv1.TeleportCancelReason_TELEPORT_CANCEL_REASON_MOVED}},
		{world.TeleportCancelled{Reason: world.TeleportActed}, &clientv1.TeleportCancelled{Reason: clientv1.TeleportCancelReason_TELEPORT_CANCEL_REASON_ACTED}},
		{world.TeleportCancelled{Reason: world.TeleportHit}, &clientv1.TeleportCancelled{Reason: clientv1.TeleportCancelReason_TELEPORT_CANCEL_REASON_HIT}},
		{world.TeleportCancelled{Reason: world.TeleportDied}, &clientv1.TeleportCancelled{Reason: clientv1.TeleportCancelReason_TELEPORT_CANCEL_REASON_DIED}},
		{world.PartySpots{Spots: []world.PartySpot{{ID: p2, X: 140.5, Z: 812.25}}}, &clientv1.PartySpots{Spots: []*clientv1.PartySpot{{CharacterId: p2.String(), X: 140.5, Z: 812.25}}}},
		{world.PartySpots{}, &clientv1.PartySpots{}},
		{world.WaypointFound{Waypoint: "north"}, &clientv1.WaypointFound{Waypoint: "north"}},
		{
			world.PiecesAdded{Pieces: []string{"bronze_sword", "bronze_knife"}, Cause: world.PieceFromQuest},
			&clientv1.PiecesAdded{Pieces: []*clientv1.AddedPiece{{Id: "bronze_sword", Cause: "quest"}, {Id: "bronze_knife", Cause: "quest"}}},
		},
		{
			world.AreaEntered{Area: "oasis", FieldBoss: &world.FieldBoss{Mob: 9, Type: "king_slime"}},
			&clientv1.AreaEntered{Area: "oasis", FieldBoss: &clientv1.FieldBoss{MobId: 9, Type: "king_slime"}},
		},
		{
			world.AreaEntered{Area: "oasis", FieldBossGone: &world.FieldBossGone{Mob: 9, Type: "king_slime", BackIn: 120 * time.Second}},
			&clientv1.AreaEntered{Area: "oasis", FieldBossGone: &clientv1.FieldBossGone{MobId: 9, Type: "king_slime", BackInMs: 120000}},
		},
		{world.ArenaBar{Bar: []string{"slash", "", "guard"}}, &clientv1.ArenaBar{SkillBar: []string{"slash", "", "guard"}}},
		{
			world.MatchState{
				Match: kernel.MatchID{}, Mode: "team", Phase: world.PhaseFight, Round: 2, Wins: [2]int{1, 0}, EndsIn: 45 * time.Second,
				Sides: [2][]world.MatchFighter{{{ID: p1, Name: "Robin", Class: "bowman"}}, {{ID: id(5), Name: "Bot", Class: "warrior", Bot: true}}},
			},
			&clientv1.MatchState{
				Match: kernel.MatchID{}.String(), Mode: "team", Phase: clientv1.MatchPhase_MATCH_PHASE_FIGHT, Round: 2, Wins: []int32{1, 0}, EndsInMs: 45000,
				Sides: []*clientv1.MatchSide{
					{Fighters: []*clientv1.MatchFighter{{Id: p1.String(), Name: "Robin", Class: "bowman"}}},
					{Fighters: []*clientv1.MatchFighter{{Id: id(5).String(), Name: "Bot", Class: "warrior", Bot: true}}},
				},
			},
		},
		{
			world.MatchState{Mode: "duel", Phase: world.PhaseCountdown, Round: 2, Wins: [2]int{1, 0}, EndsIn: 3 * time.Second, Score: [2]int{4, 5}, Sides: [2][]world.MatchFighter{{{ID: p1, Name: "Ayla", Class: "warrior"}}, {{ID: id(5), Name: "Cedar", Class: "magician", Bot: true}}}},
			&clientv1.MatchState{Match: kernel.MatchID{}.String(), Mode: "duel", Phase: clientv1.MatchPhase_MATCH_PHASE_COUNTDOWN, Round: 2, Wins: []int32{1, 0}, EndsInMs: 3000, Sides: []*clientv1.MatchSide{
				{Fighters: []*clientv1.MatchFighter{{Id: p1.String(), Name: "Ayla", Class: "warrior"}}},
				{Fighters: []*clientv1.MatchFighter{{Id: id(5).String(), Name: "Cedar", Class: "magician", Bot: true}}},
			}},
		},
		{ // a battle's score and points, and no rounds
			world.MatchState{Mode: "battle", Phase: world.PhaseFight, Wins: [2]int{1, 0}, EndsIn: 9 * time.Minute, Battle: true, Score: [2]int{120, 95}, Points: []world.CapturePoint{{ID: "west", Holder: 1, Progress: -1}, {ID: "middle", Progress: 0.25, Contested: true}}},
			&clientv1.MatchState{Match: kernel.MatchID{}.String(), Mode: "battle", Phase: clientv1.MatchPhase_MATCH_PHASE_FIGHT, EndsInMs: 540000, Score: []int32{120, 95}, Points: []*clientv1.CapturePoint{
				{Id: "west", Holder: 1, Progress: -1}, {Id: "middle", Progress: 0.25, Contested: true},
			}, Sides: []*clientv1.MatchSide{{}, {}}},
		},
		{
			world.Damage{Source: world.EntityRef{Player: p1}, Target: world.EntityRef{Mob: 7}, Amount: 21, Crit: true, Miss: true},
			&clientv1.Damage{Source: "p:" + p1.String(), Target: "m:7", Amount: 21, Crit: true, Miss: true},
		},
		{
			world.Damage{Source: world.EntityRef{Mob: 7}, Target: world.EntityRef{Player: p1}, Amount: 4, Status: "poison", Shielded: 3},
			&clientv1.Damage{Source: "m:7", Target: "p:" + p1.String(), Amount: 4, StatusId: "poison", Shielded: 3},
		},
		{world.Died{ID: p1, X: 1, Y: 2, Z: 3}, &clientv1.Died{CharacterId: p1.String(), X: 1, Y: 2, Z: 3}},
		{
			world.SkillUsed{Caster: p1, Skill: "strike", Stage: 2, Trait: "concussion", Aim: -1.5, Target: world.EntityRef{Mob: 7}},
			&clientv1.SkillUsed{Caster: p1.String(), Skill: "strike", Stage: 2, Trait: "concussion", Aim: -1.5, Target: "m:7"},
		},
		{world.SkillUsed{Caster: p1, Skill: "war_cry"}, &clientv1.SkillUsed{Caster: p1.String(), Skill: "war_cry"}}, // no lock: no ref, not m:0
		{world.AttackUsed{Attacker: p1, Length: 18, Aim: 1.5}, &clientv1.AttackUsed{Attacker: p1.String(), LengthTicks: 18, Aim: 1.5}},
		{world.MobAttacked{Mob: 7, Attack: 1, Yaw: -1.5, ToX: 13.625, ToY: 0.5, ToZ: 3.25}, &clientv1.MobAttacked{Mob: 7, Attack: 1, Yaw: -1.5, ToX: 13.625, ToY: 0.5, ToZ: 3.25}},
		{world.Respawned{ID: p1, MapID: "town", X: 13, Z: 2, Yaw: 1.5, HP: 102}, &clientv1.Respawned{CharacterId: p1.String(), MapId: "town", X: 13, Z: 2, Yaw: 1.5, Hp: 102}},
		{world.ExpGained{Amount: 5, Exp: 15}, &clientv1.ExpGained{Amount: 5, Exp: 15}},
		{levelUp, &clientv1.LevelUp{Character: wireCharacter}},
		{world.Promoted(levelUp), &clientv1.Promoted{Character: wireCharacter}},
		{
			world.Chat{Sender: p1, SenderName: "Robin", Text: "hi"},
			&clientv1.ChatMessage{Channel: clientv1.ChatChannel_CHAT_CHANNEL_MAP, Sender: p1.String(), SenderName: "Robin", Text: "hi"},
		},
		{
			world.InventoryChanged{Revision: 5, Slots: []world.Slot{{Tab: "etc", Slot: 1}, {Tab: "etc", Slot: 2, Item: &world.SlotItem{ID: itemID1, Template: "slime_gel", Quantity: 3}}}},
			&clientv1.InventoryChanged{Revision: 5, Slots: []*clientv1.InventorySlot{{Tab: "etc", Slot: 1}, {Tab: "etc", Slot: 2, Item: &clientv1.ItemView{ItemId: itemID1.String(), Template: "slime_gel", Tab: "etc", Slot: 2, Quantity: 3}}}},
		},
		{
			world.InventoryChanged{Revision: 6, Slots: []world.Slot{{Tab: "equip", Slot: 0}}, Equipment: []world.EquipSlot{{Slot: "weapon", Item: &world.SlotItem{ID: itemID2, Template: "short_bow", Quantity: 1, Stats: bowRoll}}, {Slot: "hat"}}},
			&clientv1.InventoryChanged{Revision: 6, Slots: []*clientv1.InventorySlot{{Tab: "equip", Slot: 0}}, Equipment: []*clientv1.EquipSlot{{EquipSlot: "weapon", Item: wireBowRoll}, {EquipSlot: "hat"}}},
		},
		{world.StatsChanged{Sheet: &worldSheet, HP: 50, MaxHP: 135}, &clientv1.StatsChanged{Sheet: wireWorldSheet(), Hp: 50, MaxHp: 135}},
		{world.StatsChanged{HP: 50, MaxHP: 135}, &clientv1.StatsChanged{Hp: 50, MaxHp: 135}}, // regen: the sheet stays unset, not zeroed
		{
			world.Loot{Kind: world.LootTaken, MapID: "forest", Pouch: 7, Owner: p2, OwnerName: "Robin", Items: []world.LootItem{{Template: "slime_gel", Quantity: 2}, {Template: "red_potion", Quantity: 1}}},
			&clientv1.Loot{Kind: clientv1.LootKind_LOOT_KIND_TAKEN, MapId: "forest", PouchId: 7, OwnerId: p2.String(), OwnerName: "Robin", Items: []*clientv1.LootItem{{Template: "slime_gel", Quantity: 2}, {Template: "red_potion", Quantity: 1}}},
		},
		{world.Loot{Kind: world.LootLeft, Pouch: 9, Owner: p2}, &clientv1.Loot{Kind: clientv1.LootKind_LOOT_KIND_LEFT, PouchId: 9, OwnerId: p2.String(), Items: []*clientv1.LootItem{}}},
		{
			world.NpcDialog{NPC: "town_guide", Gives: []world.QuestOffer{
				{Quest: "a", State: world.OfferAvailable}, {Quest: "b", State: world.OfferActive}, {Quest: "c", State: world.OfferFinished},
				{Quest: "d", State: world.OfferUnmet, Unmet: world.Unmet{Level: 5, Class: "thief", Quests: []string{"a"}}},
			}, Takes: []world.QuestTurnIn{{Quest: "b", Ready: true}, {Quest: "e"}}},
			&clientv1.NpcDialog{NpcId: "town_guide", Gives: []*clientv1.QuestOffer{
				{QuestId: "a", State: clientv1.QuestOfferState_QUEST_OFFER_STATE_AVAILABLE}, {QuestId: "b", State: clientv1.QuestOfferState_QUEST_OFFER_STATE_ACTIVE},
				{QuestId: "c", State: clientv1.QuestOfferState_QUEST_OFFER_STATE_FINISHED},
				{QuestId: "d", State: clientv1.QuestOfferState_QUEST_OFFER_STATE_CONDITIONS_UNMET, Unmet: &clientv1.QuestConditions{Level: 5, Class: "thief", Quests: []string{"a"}}},
			}, Takes: []*clientv1.QuestTurnIn{{QuestId: "b", Ready: true}, {QuestId: "e"}}},
		},
		{
			world.NpcDialog{NPC: "warrior_trainer", Promotion: &world.PromotionOffer{Class: "warrior", Conditions: []world.Condition{
				{Kind: world.ConditionLevel, Need: 10, Have: 9}, {Kind: world.ConditionStat, ID: "str", Need: 30, Have: 30},
				{Kind: world.ConditionSkill, ID: "strike", Need: 10, Have: 4}, {Kind: world.ConditionQuest, ID: "warriors_trial", Need: 1},
			}}},
			&clientv1.NpcDialog{NpcId: "warrior_trainer", Promotion: &clientv1.PromotionOffer{Class: "warrior", Conditions: []*clientv1.PromotionCondition{
				{Kind: clientv1.PromotionConditionKind_PROMOTION_CONDITION_KIND_LEVEL, Need: 10, Have: 9},
				{Kind: clientv1.PromotionConditionKind_PROMOTION_CONDITION_KIND_STAT, Id: "str", Need: 30, Have: 30},
				{Kind: clientv1.PromotionConditionKind_PROMOTION_CONDITION_KIND_SKILL, Id: "strike", Need: 10, Have: 4},
				{Kind: clientv1.PromotionConditionKind_PROMOTION_CONDITION_KIND_QUEST, Id: "warriors_trial", Need: 1},
			}}},
		},
		{
			world.QuestChanged{ID: "a", Change: world.QuestProgressed, Quest: world.Quest{Status: world.QuestActive, Progress: []int{2, 0}, TimesFinished: 3}},
			&clientv1.QuestChanged{Change: clientv1.QuestChange_QUEST_CHANGE_PROGRESSED, Quest: &clientv1.QuestState{QuestId: "a", Status: clientv1.QuestStatus_QUEST_STATUS_ACTIVE, Progress: []int32{2, 0}, TimesFinished: 3}},
		},
		{
			world.QuestChanged{ID: "a", Change: world.QuestAbandoned},
			&clientv1.QuestChanged{Change: clientv1.QuestChange_QUEST_CHANGE_ABANDONED, Quest: &clientv1.QuestState{QuestId: "a"}},
		},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%T", tt.event), func(t *testing.T) {
			s := &socket{}
			transport.NewOutbox(s, s, slog.New(slog.DiscardHandler), allEncoders()).Deliver(model.Delivery{Tick: 9, Event: tt.event})
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

func TestWelcomeCarriesTheCharacterItsItemsAtTheirRevisionItsQuestsAndItsSkillTimers(t *testing.T) {
	s := &socket{}
	at := time.UnixMilli(1_700_000_000_000)
	p1 := id(1)
	itemID1, _ := kernel.ParseItemID("00000000-0000-7000-8000-0000000000a1")
	itemID2, _ := kernel.ParseItemID("00000000-0000-7000-8000-0000000000a2")
	bowRoll := &progression.ItemStats{
		STR: 1, DEX: 2, INT: 3, AGI: 4, VIT: 5, PhysicalAttack: 6, MagicAttack: 7, PhysicalDefense: 8, MagicDefense: 9, MaxHP: 10, Accuracy: 11, Evasion: 12,
		CritRate: 13, CritDamage: 14, Penetration: 15, HPRegen: 16, CooldownSpeed: 17, MoveSpeed: 18,
	}
	view := world.View{
		ID: p1, Name: "Robin", Class: "bowman", Level: 2, Exp: 15, Stats: gamedata.Stats{STR: 5, DEX: 12, INT: 4, AGI: 10, VIT: 5}, HP: 50, MaxHP: 105, Energy: 6, Sheet: worldSheet,
		Quests: map[string]world.Quest{"a": {Status: world.QuestFinished, TimesFinished: 2}},
		Timers: world.SkillTimers{Buffs: map[string]world.BuffTimer{"focus": {Until: at.Add(1500 * time.Millisecond), Points: 4, Trait: "expose"}}},
	}
	inv := item.Inventory{Revision: 8, Items: []item.Item{
		{ID: itemID1, TemplateID: "slime_gel", Tab: "etc", Slot: 2, Quantity: 3},
		{ID: itemID2, TemplateID: "short_bow", Quantity: 1, EquipSlot: "weapon", Stats: bowRoll},
	}}
	transport.NewOutbox(s, s, slog.New(slog.DiscardHandler), allEncoders()).Welcome(model.Welcome{
		Token: "t-2", MapID: "town", ServerTime: at, Waypoints: []string{"town", "west"}, Joined: view, Inventory: inv,
	})
	want := &clientv1.Welcome{
		Token:             "t-2",
		Character:         &clientv1.CharacterView{Id: p1.String(), Name: "Robin", Class: "bowman", Level: 2, Exp: 15, Stats: &clientv1.Stats{Str: 5, Dex: 12, Int: 4, Agi: 10, Vit: 5}, Hp: 50, MaxHp: 105, Energy: 6, Sheet: wireWorldSheet()},
		Inventory:         []*clientv1.ItemView{{ItemId: itemID1.String(), Template: "slime_gel", Tab: "etc", Slot: 2, Quantity: 3}},
		Equipment:         []*clientv1.EquipSlot{{EquipSlot: "weapon", Item: &clientv1.ItemView{ItemId: itemID2.String(), Template: "short_bow", Quantity: 1, Stats: wireBow.Stats}}},
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
