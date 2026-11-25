package protocol_test

import (
	"google.golang.org/protobuf/proto"

	clientv1 "finga/api/gen/client/v1"
	"finga/internal/shared/protocol"
	"finga/pkg/wsconn"
)

// samples is one full message per type: the source of the golden files the client's tests also read.
func samples() []wsconn.Message {
	const (
		charA = "0192f6a4-6f2b-7c3e-8a1b-2c3d4e5f6a7b"
		charB = "0192f6a4-6f2b-7c3e-8a1b-2c3d4e5f6a7c"
		charC = "0192f6a4-6f2b-7c3e-8a1b-2c3d4e5f6a7d"
		charD = "0192f6a4-6f2b-7c3e-8a1b-2c3d4e5f6a7e"
		item1 = "0192f6a4-6f2b-7c3e-8a1b-2c3d4e5f6b01"
		item2 = "0192f6a4-6f2b-7c3e-8a1b-2c3d4e5f6b02"
		item3 = "0192f6a4-6f2b-7c3e-8a1b-2c3d4e5f6b03"
		item4 = "0192f6a4-6f2b-7c3e-8a1b-2c3d4e5f6b04"
		ring  = "0192f6a4-6f2b-7c3e-8a1b-2c3d4e5f6b05"
	)
	// A level 2 warrior who spent 3 STR and 2 VIT, wearing a sword and a cap, with 3 points in Second Wind: the totals, the equipment's and the skills' share of each, the points, the skill points, and the attack's cooldown; under their own Iron Body and a party member's War Cry.
	sheet := func() *clientv1.StatSheet {
		return &clientv1.StatSheet{
			Primary: &clientv1.Stats{Str: 16, Dex: 5, Int: 4, Agi: 5, Vit: 12}, PrimaryWorn: &clientv1.Stats{Str: 1},
			Derived: &clientv1.DerivedStats{
				PhysicalAttack: 49.5, MagicAttack: 8.5, PhysicalDefense: 17, MagicDefense: 11, Accuracy: 15, Evasion: 10, CritRate: 0.065, CritDamage: 0.525,
				Penetration: 0.02, HpRegen: 0.6, CooldownSpeed: 0.008, MoveSpeed: 0.007, MaxHp: 280,
			},
			DerivedWorn:       &clientv1.DerivedStats{PhysicalAttack: 12.5, PhysicalDefense: 3, CritRate: 0.01, Penetration: 0.02, MaxHp: 30},
			Points:            &clientv1.Points{Unspent: 0, Spent: &clientv1.Stats{Str: 3, Vit: 2}, FreeResetLeft: true},
			AttackLengthTicks: 12,
			PrimarySkills:     &clientv1.Stats{},
			DerivedSkills:     &clientv1.DerivedStats{HpRegen: 0.8},
			Skills:            &clientv1.SkillPoints{Unspent: 2, Invested: map[string]int32{"second_wind": 3, "iron_body": 3}, Traits: map[string]string{"iron_body": "iron_wall"}},
			Buffs: []*clientv1.BuffView{
				{Skill: "iron_body", LeftMs: 12400, Points: 3},
				{Skill: "war_cry", LeftMs: 8250, Points: 4, From: charB},
			},
		}
	}
	// view is Ayla; only a Welcome tells the energy, which a LevelUp or Promoted leaves to the Delta.
	view := func(energy uint32) *clientv1.CharacterView {
		return &clientv1.CharacterView{Id: charA, Name: "Ayla", Class: "warrior", Level: 2, Exp: 15, Stats: &clientv1.Stats{Str: 15, Dex: 5, Int: 4, Agi: 5, Vit: 12}, Hp: 250, MaxHp: 280, Energy: energy, Sheet: sheet()}
	}
	sword := func() *clientv1.ItemView {
		return &clientv1.ItemView{ItemId: item2, Template: "wooden_sword", Quantity: 1, Stats: &clientv1.ItemStats{PhysicalAttack: 10, Str: 1, CritRate: 10, Penetration: 20}}
	}
	leatherCap := func() *clientv1.ItemView {
		return &clientv1.ItemView{ItemId: item3, Template: "leather_cap", Quantity: 1, Stats: &clientv1.ItemStats{PhysicalDefense: 3, MaxHp: 30}}
	}
	player := func() *clientv1.PlayerView { // walking slowed
		return &clientv1.PlayerView{Id: charA, Name: "Ayla", Class: "warrior", Level: 2, X: 13, Y: 0.5, Vx: 3, OnGround: true, Hp: 250, MaxHp: 250, Z: 2.5, Vz: 4, Hx: 0.6, Hz: 0.8, Aim: 0.75,
			Slow: &clientv1.SlowWalk{Left: 33, Factor: 0.6}, Statuses: []*clientv1.StatusView{{Id: "slow", Ends: 81246}},
			Look: &clientv1.Look{Outfit: "knight_plate", Weapon: "greatsword", HideHeadwear: true}} // the class's own headwear, hidden, and face
	}
	// A player just hit by a mob: in their hit guard, stunned and poisoned.
	hurt := &clientv1.PlayerView{Id: charB, Name: "Robin", Class: "bowman", Level: 1, X: 16.875, Y: 0.375, Vy: -9.375, Hp: 90, MaxHp: 105, Invulnerable: true, Hx: -1,
		Held: 57, Statuses: []*clientv1.StatusView{{Id: "poison", Ends: 81316}, {Id: "stun", Ends: 81255}}}
	// A player breaking free with Vanish: leaping back through a mob, nine sub-steps of its move left, the farthest point it reached clear of mobs behind it, dodging, behind a shield.
	leaping := &clientv1.PlayerView{Id: charD, Name: "Mira", Class: "thief", Level: 12, X: 20, Y: 0.25, Vy: 3.5, Hp: 300, MaxHp: 300, Z: 8, Hz: 1,
		Move: &clientv1.SkillMove{Vx: 0, Vz: -13.333333333333334, Left: 9, Through: true, ClearX: 20, ClearZ: 8.5}, Dodging: true, Shield: 64}
	// A bot on side 2 of a match.
	bot := &clientv1.PlayerView{Id: "0192f6a4-7002-7c3e-8a1b-2c3d4e5f6a7b", Name: "Cedar", Class: "magician", Level: 30, X: 22, Hp: 410, MaxHp: 410, Z: 18, Hx: -1, Team: 2, Bot: true,
		Look: &clientv1.Look{Headwear: "pointed_hat", Weapon: "oak_wand"}}
	mob := func() *clientv1.MobView { // just hit: flinching, poisoned
		return &clientv1.MobView{Id: 7, Type: "green_slime", X: 15.625, Y: 0.5, Hp: 30, MaxHp: 30, Pose: clientv1.MobPose_MOB_POSE_FLINCH, Z: 1.25, Yaw: -1.5,
			Statuses: []*clientv1.StatusView{{Id: "poison", Ends: 81290}}}
	}
	boss := func() *clientv1.MobView {
		return &clientv1.MobView{Id: 9, Type: "king_slime", X: 43.75, Y: 1, Hp: 3100, MaxHp: 4260, Z: 30, Yaw: 3}
	}
	// A box of the player's from a field boss: a gel, and a sword with its roll.
	pouch := func() *clientv1.PouchView {
		return &clientv1.PouchView{Id: 12, Kind: clientv1.PouchKind_POUCH_KIND_BOX, X: 43.75, Y: 1, Z: 30, GoneInMs: 118000, Items: []*clientv1.PouchItem{
			{Item: 1, Template: "slime_gel", Quantity: 2}, {Item: 2, Template: "iron_sword", Quantity: 1, Stats: &clientv1.ItemStats{PhysicalAttack: 18, CritRate: 15}},
		}}
	}
	party := func() *clientv1.PartyView {
		return &clientv1.PartyView{Leader: charA, Members: []*clientv1.PartyMember{
			{CharacterId: charA, Name: "Ayla", Level: 2, Hp: 250, MaxHp: 250, MapId: "forest", Online: true},
			{CharacterId: charB, Name: "Robin", Level: 1, Hp: 105, MaxHp: 105, MapId: "town"},
		}}
	}
	return []wsconn.Message{
		{Type: "Hello", Data: &clientv1.Hello{Token: "opaque-session-token", ProtocolVersion: protocol.Version, CharacterId: charA}},
		{Type: "Input", Seq: 117, Data: &clientv1.Input{Right: true, Jump: true, View: 4810}},
		{Type: "Attack", Seq: 118, Data: &clientv1.Attack{Aim: 1.5}},
		{Type: "EnterPortal", Seq: 120, Data: &clientv1.EnterPortal{}},
		{Type: "TakeLoot", ReqID: "r-22", Data: &clientv1.TakeLoot{Picks: []*clientv1.LootPick{{Pouch: 12, Item: 1}, {Pouch: 12, Item: 2}}}},
		{Type: "Teleport", Seq: 121, ReqID: "r-21", Data: &clientv1.Teleport{MapId: "forest"}},
		{Type: "SwitchChannel", Seq: 122, ReqID: "r-23", Data: &clientv1.SwitchChannel{Channel: 2}},
		{Type: "Channels", ReqID: "r-24", Data: &clientv1.Channels{}},
		{Type: "ChatSend", Data: &clientv1.ChatSend{Channel: clientv1.ChatChannel_CHAT_CHANNEL_MAP, Text: "hello there"}},
		{Type: "PartyInvite", ReqID: "r-1", Data: &clientv1.PartyInvite{CharacterId: charB}},
		{Type: "PartyRespond", ReqID: "r-2", Data: &clientv1.PartyRespond{InviteId: "inv-1", Accept: true}},
		{Type: "PartyLeave", ReqID: "r-3", Data: &clientv1.PartyLeave{}},
		{Type: "PartyKick", ReqID: "r-4", Data: &clientv1.PartyKick{CharacterId: charB}},
		{Type: "PartyPromote", ReqID: "r-5", Data: &clientv1.PartyPromote{CharacterId: charB}},
		{Type: "InventoryMove", ReqID: "r-6", Data: &clientv1.InventoryMove{IdempotencyKey: "k-1", ItemId: item1, Tab: "use", Slot: 3}},
		{Type: "InventoryDiscard", ReqID: "r-7", Data: &clientv1.InventoryDiscard{IdempotencyKey: "k-2", ItemId: item1, Quantity: 1}},
		{Type: "InventoryDiscardMany", ReqID: "r-20", Data: &clientv1.InventoryDiscardMany{IdempotencyKey: "k-6", ItemIds: []string{item1, item2}}},
		{Type: "Equip", ReqID: "r-8", Data: &clientv1.Equip{IdempotencyKey: "k-3", ItemId: ring, EquipSlot: "ring_2"}}, // a ring names its slot
		{Type: "Unequip", ReqID: "r-9", Data: &clientv1.Unequip{IdempotencyKey: "k-4", EquipSlot: "hat", ToSlot: proto.Int32(7)}},
		{Type: "SpendPoints", ReqID: "r-10", Data: &clientv1.SpendPoints{ExpectedUnspent: 5, Points: &clientv1.Stats{Str: 3, Vit: 2}}},
		{Type: "ResetPoints", ReqID: "r-11", Data: &clientv1.ResetPoints{ExpectedUnspent: 0}},
		{Type: "SpendSkillPoints", ReqID: "r-17", Data: &clientv1.SpendSkillPoints{ExpectedUnspent: 5, Points: map[string]int32{"strike": 3, "second_wind": 2}}},
		{Type: "ChooseTrait", ReqID: "r-22", Data: &clientv1.ChooseTrait{Skill: "strike", Trait: "concussion"}},
		{Type: "UseSkill", ReqID: "r-18", Data: &clientv1.UseSkill{Skill: "lunge", ChainRoot: "strike", Aim: -1.5, Target: "m:7", Way: &clientv1.SkillWay{X: 0.6, Z: -0.8, Length: 3.25}}},
		{Type: "Promote", ReqID: "r-19", Data: &clientv1.Promote{NpcId: "warrior_trainer", Class: "warrior"}},
		{Type: "Talk", ReqID: "r-12", Data: &clientv1.Talk{NpcId: "town_guide"}},
		{Type: "AcceptQuest", ReqID: "r-13", Data: &clientv1.AcceptQuest{NpcId: "town_guide", QuestId: "slime_cleanup"}},
		{Type: "AbandonQuest", ReqID: "r-14", Data: &clientv1.AbandonQuest{QuestId: "slime_cleanup"}},
		{Type: "HandInQuest", ReqID: "r-15", Data: &clientv1.HandInQuest{NpcId: "town_guide", QuestId: "sticky_samples"}},
		{Type: "UseItem", ReqID: "r-16", Data: &clientv1.UseItem{IdempotencyKey: "k-5", ItemId: item1, Template: "red_potion"}},
		{Type: "Ping", Data: &clientv1.Ping{ClientTime: 1761000000000}},

		{Type: "Welcome", Data: &clientv1.Welcome{
			Token: "s3ss10n-t0k3n-r0t4t3d", Character: view(6), Party: party(), MapId: "town", Tick: 81234, ServerTime: 1761000000050,
			Inventory:         []*clientv1.ItemView{{ItemId: item1, Template: "red_potion", Tab: "use", Slot: 0, Quantity: 3}, {ItemId: item3, Template: "leather_cap", Tab: "equip", Slot: 4, Quantity: 1, Stats: leatherCap().GetStats()}},
			Equipment:         []*clientv1.EquipSlot{{EquipSlot: "weapon", Item: sword()}},
			InventoryRevision: 7,
			Quests: []*clientv1.QuestState{
				{QuestId: "slime_cleanup", Status: clientv1.QuestStatus_QUEST_STATUS_FINISHED, TimesFinished: 1},
				{QuestId: "sticky_samples", Status: clientv1.QuestStatus_QUEST_STATUS_ACTIVE, Progress: []int32{0}},
			},
			SkillTimers: []*clientv1.SkillTimer{
				{Skill: "iron_body", Kind: clientv1.SkillTimerKind_SKILL_TIMER_KIND_COOLDOWN, LeftMs: 41200},
				{Skill: "iron_body", Kind: clientv1.SkillTimerKind_SKILL_TIMER_KIND_BUFF, LeftMs: 11200, Points: 3, Trait: "iron_wall"},
			},
			Waypoints: []string{"town", "west"},
		}},
		// A map whose field boss is up.
		{Type: "Snapshot", Tick: 81234, Data: &clientv1.Snapshot{Players: []*clientv1.PlayerView{player()}, Mobs: []*clientv1.MobView{mob(), boss()}, Pouches: []*clientv1.PouchView{pouch()}, FieldBoss: &clientv1.FieldBoss{MobId: 9, Type: "king_slime"}, Channel: 2, Area: "west"}},
		{Type: "Delta", Tick: 81235, Data: &clientv1.Delta{AckSeq: 117, Energy: proto.Uint32(8), Players: []*clientv1.PlayerView{player(), hurt, leaping, bot}, Mobs: []*clientv1.MobView{mob()}, Pouches: []*clientv1.PouchView{pouch()}, PouchesGone: []uint32{11}, RemovedPlayers: []string{charC}, RemovedMobs: []uint32{6}}},
		{Type: "Damage", Tick: 81235, Data: &clientv1.Damage{Source: "p:" + charA, Target: "m:7", Amount: 23, Crit: true}},
		{Type: "SkillUsed", Tick: 81235, Data: &clientv1.SkillUsed{Caster: charA, Skill: "strike", Stage: 2, Trait: "concussion", Aim: -1.5, Target: "m:7"}},
		{Type: "AttackUsed", Tick: 81236, Data: &clientv1.AttackUsed{Attacker: charA, LengthTicks: 18, Aim: 1.5}},
		{Type: "MobAttacked", Tick: 81236, Data: &clientv1.MobAttacked{Mob: 7, Attack: 1, ToX: 13.625, Yaw: -1.5, ToY: 0.5, ToZ: 3.25}},
		{Type: "FieldBoss", Tick: 81235, Data: &clientv1.FieldBoss{MobId: 9, Type: "king_slime"}},
		{Type: "FieldBossGone", Tick: 81236, Data: &clientv1.FieldBossGone{MobId: 9, Type: "king_slime", BackInMs: 300000}},
		{Type: "Died", Tick: 81236, Data: &clientv1.Died{CharacterId: charA, X: 15.625, Y: 0.5, Z: 2}},
		{Type: "Respawned", Tick: 81296, Data: &clientv1.Respawned{CharacterId: charA, MapId: "town", X: 13, Y: 0, Hp: 125, Z: 0.5, Yaw: 1.5}},
		{Type: "ExpGained", Data: &clientv1.ExpGained{Amount: 5, Exp: 15}},
		{Type: "LevelUp", Data: &clientv1.LevelUp{Character: view(0)}},
		{Type: "Promoted", Data: &clientv1.Promoted{Character: view(0)}},
		// An equip that swapped the cap in for another: the old one took the tab slot the new one left.
		{Type: "InventoryChanged", Data: &clientv1.InventoryChanged{
			Slots:     []*clientv1.InventorySlot{{Tab: "equip", Slot: 4, Item: &clientv1.ItemView{ItemId: item4, Template: "leather_cap", Tab: "equip", Slot: 4, Quantity: 1, Stats: &clientv1.ItemStats{PhysicalDefense: 2}}}},
			Equipment: []*clientv1.EquipSlot{{EquipSlot: "hat", Item: leatherCap()}},
			Revision:  8,
		}},
		{Type: "StatsChanged", Data: &clientv1.StatsChanged{Sheet: sheet(), Hp: 250, MaxHp: 280, Timers: &clientv1.SkillTimers{Timers: []*clientv1.SkillTimer{
			{Skill: "war_cry", Kind: clientv1.SkillTimerKind_SKILL_TIMER_KIND_COOLDOWN, LeftMs: 41250},
		}}}},
		{Type: "Loot", Tick: 81236, Data: &clientv1.Loot{Kind: clientv1.LootKind_LOOT_KIND_TAKEN, MapId: "forest", PouchId: 10, OwnerId: charB, OwnerName: "Robin", Items: []*clientv1.LootItem{{Template: "slime_gel", Quantity: 2}, {Template: "red_potion", Quantity: 1}}}},
		{Type: "MapChange", Data: &clientv1.MapChange{MapId: "forest", X: 2, Y: 0, Z: 1, Yaw: -0.5}},
		{Type: "TeleportCancelled", Data: &clientv1.TeleportCancelled{Reason: clientv1.TeleportCancelReason_TELEPORT_CANCEL_REASON_HIT}},
		{Type: "PvpQueue", ReqID: "r-30", Data: &clientv1.PvpQueue{Mode: "duel"}},
		{Type: "PvpLeaveQueue", ReqID: "r-31", Data: &clientv1.PvpLeaveQueue{}},
		{Type: "PvpAnswer", ReqID: "r-32", Data: &clientv1.PvpAnswer{Offer: "0192f6a4-7000-7c3e-8a1b-2c3d4e5f6a7b", Accept: true}},
		{Type: "PvpForfeit", ReqID: "r-33", Data: &clientv1.PvpForfeit{}},
		{Type: "AddToWardrobe", ReqID: "r-34", Data: &clientv1.AddToWardrobe{IdempotencyKey: "k-9", ItemId: item1}},
		{Type: "SetLook", ReqID: "r-35", Data: &clientv1.SetLook{IdempotencyKey: "k-10", Look: &clientv1.Look{Outfit: "mage_robe", Headwear: "pointed_hat", Weapon: "oak_wand", Wings: "crystal_wings"}}},
		{Type: "PvpQueued", Data: &clientv1.PvpQueued{Mode: "team", SinceMs: 4250, Members: []string{charA, charB}}},
		{Type: "PvpUnqueued", Data: &clientv1.PvpUnqueued{Reason: clientv1.PvpUnqueueReason_PVP_UNQUEUE_REASON_DECLINED, By: []string{charB}}},
		{Type: "PvpOffer", Data: &clientv1.PvpOffer{Offer: "0192f6a4-7000-7c3e-8a1b-2c3d4e5f6a7b", Mode: "duel", Bots: 1, UntilMs: 10000}},
		{Type: "PvpOfferGone", Data: &clientv1.PvpOfferGone{Offer: "0192f6a4-7000-7c3e-8a1b-2c3d4e5f6a7b", Reason: clientv1.PvpOfferGoneReason_PVP_OFFER_GONE_REASON_DECLINED, Requeued: true}},
		// A 1v1 against a bot in its second round's countdown, side 1 a round up.
		{Type: "MatchState", Tick: 412, Data: &clientv1.MatchState{
			Match: "0192f6a4-7001-7c3e-8a1b-2c3d4e5f6a7b", Mode: "duel", Phase: clientv1.MatchPhase_MATCH_PHASE_COUNTDOWN, Round: 2, Wins: []int32{1, 0}, EndsInMs: 3000,
			Sides: []*clientv1.MatchSide{
				{Fighters: []*clientv1.MatchFighter{{Id: charA, Name: "Ayla", Class: "warrior"}}},
				{Fighters: []*clientv1.MatchFighter{{Id: "0192f6a4-7002-7c3e-8a1b-2c3d4e5f6a7b", Name: "Cedar", Class: "magician", Bot: true}}},
			},
		}},
		// A 3v3 leaver's: their side won, and they lost.
		{Type: "MatchEnded", Tick: 2810, Data: &clientv1.MatchEnded{
			Result: clientv1.MatchOutcome_MATCH_OUTCOME_LOSS, Wins: []int32{2, 1},
			Rating: &clientv1.PvpRatingChange{Before: 1000, After: 968, RankBefore: "bronze", RankAfter: "bronze", PlacementLeft: 4},
			Mode:   "team", Left: true,
		}},
		{Type: "ChatMessage", Data: &clientv1.ChatMessage{Channel: clientv1.ChatChannel_CHAT_CHANNEL_MAP, Sender: charA, SenderName: "Ayla", Text: "hello there"}},
		{Type: "PartyState", Data: &clientv1.PartyState{Party: party()}},
		{Type: "ArenaBar", Tick: 2810, Data: &clientv1.ArenaBar{SkillBar: []string{"strike", "war_cry", "", "", "", "", "", "taunt"}}},
		{Type: "AreaEntered", Tick: 81241, Data: &clientv1.AreaEntered{Area: "west", FieldBossGone: &clientv1.FieldBossGone{MobId: 12, Type: "king_slime", BackInMs: 180000}}},
		{Type: "WaypointFound", Tick: 81242, Data: &clientv1.WaypointFound{Waypoint: "north"}},
		{Type: "PiecesAdded", Tick: 81243, Data: &clientv1.PiecesAdded{Pieces: []*clientv1.AddedPiece{{Id: "knight_plate", Cause: "item"}, {Id: "helm_gold", Cause: "rank"}}}},
		{Type: "PartySpots", Tick: 81240, Data: &clientv1.PartySpots{Spots: []*clientv1.PartySpot{{CharacterId: charB, X: 140.5, Z: 812.25}}}},
		{Type: "PartyInvited", Data: &clientv1.PartyInvited{InviteId: "inv-1", From: charA, FromName: "Ayla"}},
		// The guide's dialog for a level 1 character with the cleanup under way.
		{Type: "NpcDialog", Data: &clientv1.NpcDialog{
			NpcId: "town_guide",
			Gives: []*clientv1.QuestOffer{
				{QuestId: "slime_cleanup", State: clientv1.QuestOfferState_QUEST_OFFER_STATE_ACTIVE},
				{QuestId: "sticky_samples", State: clientv1.QuestOfferState_QUEST_OFFER_STATE_CONDITIONS_UNMET, Unmet: &clientv1.QuestConditions{Level: 2, Quests: []string{"slime_cleanup"}}},
			},
			Takes: []*clientv1.QuestTurnIn{{QuestId: "slime_cleanup", Ready: false}},
		}},
		{Type: "QuestChanged", Tick: 81237, Data: &clientv1.QuestChanged{
			Change: clientv1.QuestChange_QUEST_CHANGE_PROGRESSED,
			Quest:  &clientv1.QuestState{QuestId: "slime_cleanup", Status: clientv1.QuestStatus_QUEST_STATUS_ACTIVE, Progress: []int32{5}},
		}},
		{Type: "Response", ReqID: "r-6", Data: &clientv1.Response{Code: 220005}},
		{Type: "Kicked", Data: &clientv1.Kicked{Reason: clientv1.KickReason_KICK_REASON_LOGGED_IN_ELSEWHERE}},
		{Type: "Pong", Data: &clientv1.Pong{ClientTime: 1761000000000, ServerTime: 1761000000050}},
	}
}
