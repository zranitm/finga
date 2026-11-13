package protocol_test

import (
	"google.golang.org/protobuf/proto"

	clientv1 "finga/api/gen/client/v1"
	"finga/internal/protocol"
	"finga/pkg/wsconn"
)

// samples is one full message per type: the source of the golden files the client's tests also read.
func samples() []wsconn.Message {
	const (
		charA = "0192f6a4-6f2b-7c3e-8a1b-2c3d4e5f6a7b"
		charB = "0192f6a4-6f2b-7c3e-8a1b-2c3d4e5f6a7c"
		charC = "0192f6a4-6f2b-7c3e-8a1b-2c3d4e5f6a7d"
		item1 = "0192f6a4-6f2b-7c3e-8a1b-2c3d4e5f6b01"
		item2 = "0192f6a4-6f2b-7c3e-8a1b-2c3d4e5f6b02"
		item3 = "0192f6a4-6f2b-7c3e-8a1b-2c3d4e5f6b03"
		item4 = "0192f6a4-6f2b-7c3e-8a1b-2c3d4e5f6b04"
		ring  = "0192f6a4-6f2b-7c3e-8a1b-2c3d4e5f6b05"
	)
	// A level 2 warrior who spent 3 STR and 2 VIT, wearing a sword and a cap, with 3 points in Second Wind: the totals, the equipment's and the skills' share of each, the points, the skill points, and the attack's cooldown.
	sheet := func() *clientv1.StatSheet {
		return &clientv1.StatSheet{
			Primary: &clientv1.Stats{Str: 16, Dex: 5, Int: 4, Agi: 5, Vit: 12}, PrimaryWorn: &clientv1.Stats{Str: 1},
			Derived: &clientv1.DerivedStats{
				PhysicalAttack: 49.5, MagicAttack: 8.5, PhysicalDefense: 17, MagicDefense: 11, Accuracy: 15, Evasion: 10, CritRate: 0.065, CritDamage: 0.525,
				Penetration: 0.02, HpRegen: 0.6, MpRegen: 0.2, AttackSpeed: 0.016, CooldownSpeed: 0.008, MoveSpeed: 0.007, MaxHp: 280, MaxMp: 36,
			},
			DerivedWorn:       &clientv1.DerivedStats{PhysicalAttack: 12.5, PhysicalDefense: 3, CritRate: 0.01, Penetration: 0.02, MaxHp: 30},
			Points:            &clientv1.Points{Unspent: 0, Spent: &clientv1.Stats{Str: 3, Vit: 2}, FreeResetLeft: true},
			AttackLengthTicks: 12,
			PrimarySkills:     &clientv1.Stats{},
			DerivedSkills:     &clientv1.DerivedStats{HpRegen: 0.8, MpRegen: 0.8},
			Skills:            &clientv1.SkillPoints{Unspent: 2, Invested: map[string]int32{"second_wind": 3}},
		}
	}
	view := func() *clientv1.CharacterView {
		return &clientv1.CharacterView{Id: charA, Name: "Ayla", Class: "warrior", Level: 2, Exp: 15, Stats: &clientv1.Stats{Str: 15, Dex: 5, Int: 4, Agi: 5, Vit: 12}, Hp: 250, MaxHp: 280, Mp: 32, MaxMp: 32, Sheet: sheet()}
	}
	sword := func() *clientv1.ItemView {
		return &clientv1.ItemView{ItemId: item2, Template: "wooden_sword", Quantity: 1, Stats: &clientv1.ItemStats{PhysicalAttack: 10, Str: 1, CritRate: 10, Penetration: 20}}
	}
	leatherCap := func() *clientv1.ItemView {
		return &clientv1.ItemView{ItemId: item3, Template: "leather_cap", Quantity: 1, Stats: &clientv1.ItemStats{PhysicalDefense: 3, MaxHp: 30}}
	}
	player := func() *clientv1.PlayerView {
		return &clientv1.PlayerView{Id: charA, Name: "Ayla", Class: "warrior", Level: 2, X: 416, Y: 608, Vx: 200, Facing: 1, OnGround: true, Hp: 250, MaxHp: 250}
	}
	// A player just hit by contact damage: invulnerable.
	hurt := &clientv1.PlayerView{Id: charB, Name: "Robin", Class: "bowman", Level: 1, X: 540, Y: 596, Vy: -300, Facing: -1, Hp: 90, MaxHp: 105, Invulnerable: true}
	mob := func() *clientv1.MobView {
		return &clientv1.MobView{Id: 7, Type: "green_slime", X: 500, Y: 608, Facing: -1, Hp: 30, MaxHp: 30}
	}
	boss := func() *clientv1.MobView {
		return &clientv1.MobView{Id: 9, Type: "king_slime", X: 1400, Y: 608, Facing: 1, Hp: 3100, MaxHp: 4260}
	}
	drop := func() *clientv1.DropView {
		return &clientv1.DropView{Id: 12, Template: "slime_gel", Quantity: 2, X: 520, Y: 608, OwnerId: charA}
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
		{Type: "Attack", Seq: 118, Data: &clientv1.Attack{}},
		{Type: "PickUp", Seq: 119, Data: &clientv1.PickUp{}},
		{Type: "EnterPortal", Seq: 120, Data: &clientv1.EnterPortal{}},
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
		{Type: "UseSkill", ReqID: "r-18", Data: &clientv1.UseSkill{Skill: "strike"}},
		{Type: "Promote", ReqID: "r-19", Data: &clientv1.Promote{NpcId: "warrior_trainer", Class: "warrior"}},
		{Type: "Talk", ReqID: "r-12", Data: &clientv1.Talk{NpcId: "town_guide"}},
		{Type: "AcceptQuest", ReqID: "r-13", Data: &clientv1.AcceptQuest{NpcId: "town_guide", QuestId: "slime_cleanup"}},
		{Type: "AbandonQuest", ReqID: "r-14", Data: &clientv1.AbandonQuest{QuestId: "slime_cleanup"}},
		{Type: "HandInQuest", ReqID: "r-15", Data: &clientv1.HandInQuest{NpcId: "town_guide", QuestId: "sticky_samples"}},
		{Type: "UseItem", ReqID: "r-16", Data: &clientv1.UseItem{IdempotencyKey: "k-5", ItemId: item1, Template: "red_potion"}},
		{Type: "Ping", Data: &clientv1.Ping{ClientTime: 1761000000000}},

		{Type: "Welcome", Data: &clientv1.Welcome{
			Token: "s3ss10n-t0k3n-r0t4t3d", Character: view(), Party: party(), MapId: "town", Tick: 81234, ServerTime: 1761000000050,
			Inventory:         []*clientv1.ItemView{{ItemId: item1, Template: "red_potion", Tab: "use", Slot: 0, Quantity: 3}, {ItemId: item3, Template: "leather_cap", Tab: "equip", Slot: 4, Quantity: 1, Stats: leatherCap().GetStats()}},
			Equipment:         []*clientv1.EquipSlot{{EquipSlot: "weapon", Item: sword()}},
			InventoryRevision: 7,
			Quests: []*clientv1.QuestState{
				{QuestId: "slime_cleanup", Status: clientv1.QuestStatus_QUEST_STATUS_FINISHED, TimesFinished: 1},
				{QuestId: "sticky_samples", Status: clientv1.QuestStatus_QUEST_STATUS_ACTIVE, Progress: []int32{0}},
			},
			SkillTimers: []*clientv1.SkillTimer{
				{Skill: "iron_body", Kind: clientv1.SkillTimerKind_SKILL_TIMER_KIND_COOLDOWN, LeftMs: 41200},
				{Skill: "iron_body", Kind: clientv1.SkillTimerKind_SKILL_TIMER_KIND_BUFF, LeftMs: 11200, Points: 3},
			},
		}},
		// A map whose field boss is up, leaving in 7 minutes.
		{Type: "Snapshot", Tick: 81234, Data: &clientv1.Snapshot{Players: []*clientv1.PlayerView{player()}, Mobs: []*clientv1.MobView{mob(), boss()}, Drops: []*clientv1.DropView{drop()}, FieldBoss: &clientv1.FieldBoss{MobId: 9, Type: "king_slime", LeavesInMs: 420000}}},
		{Type: "Delta", Tick: 81235, Data: &clientv1.Delta{AckSeq: 117, Players: []*clientv1.PlayerView{player(), hurt}, Mobs: []*clientv1.MobView{mob()}, Drops: []*clientv1.DropView{drop()}, RemovedPlayers: []string{charC}, RemovedMobs: []uint32{6}, RemovedDrops: []uint32{11}, CollectedDrops: []uint32{10}}},
		{Type: "Damage", Tick: 81235, Data: &clientv1.Damage{Source: "p:" + charA, Target: "m:7", Amount: 23, Crit: true}},
		{Type: "SkillUsed", Tick: 81235, Data: &clientv1.SkillUsed{Caster: charA, Skill: "strike", Stage: 1, Facing: -1}},
		{Type: "AttackUsed", Tick: 81236, Data: &clientv1.AttackUsed{Attacker: charA, Facing: 1, LengthTicks: 18}},
		{Type: "FieldBoss", Tick: 81235, Data: &clientv1.FieldBoss{MobId: 9, Type: "king_slime", LeavesInMs: 600000}},
		{Type: "FieldBossGone", Tick: 81236, Data: &clientv1.FieldBossGone{MobId: 9, Beaten: true}},
		{Type: "Announcement", Data: &clientv1.Announcement{Kind: &clientv1.Announcement_BossAppeared{BossAppeared: &clientv1.BossAppeared{Boss: "king_slime", MapId: "forest"}}}},
		{Type: "Died", Tick: 81236, Data: &clientv1.Died{CharacterId: charA, X: 500, Y: 608}},
		{Type: "Respawned", Tick: 81296, Data: &clientv1.Respawned{CharacterId: charA, MapId: "town", X: 416, Y: 608, Hp: 125}},
		{Type: "ExpGained", Data: &clientv1.ExpGained{Amount: 5, Exp: 15}},
		{Type: "LevelUp", Data: &clientv1.LevelUp{Character: view()}},
		{Type: "Promoted", Data: &clientv1.Promoted{Character: view()}},
		// An equip that swapped the cap in for another: the old one took the tab slot the new one left.
		{Type: "InventoryChanged", Data: &clientv1.InventoryChanged{
			Slots:     []*clientv1.InventorySlot{{Tab: "equip", Slot: 4, Item: &clientv1.ItemView{ItemId: item4, Template: "leather_cap", Tab: "equip", Slot: 4, Quantity: 1, Stats: &clientv1.ItemStats{PhysicalDefense: 2}}}},
			Equipment: []*clientv1.EquipSlot{{EquipSlot: "hat", Item: leatherCap()}},
			Revision:  8,
		}},
		{Type: "StatsChanged", Data: &clientv1.StatsChanged{Sheet: sheet(), Hp: 250, MaxHp: 280, Mp: 32, MaxMp: 32}},
		{Type: "Loot", Tick: 81236, Data: &clientv1.Loot{Kind: clientv1.LootKind_LOOT_KIND_COLLECTED, MapId: "forest", DropId: 10, OwnerId: charB, OwnerName: "Robin", Template: "slime_gel", Quantity: 2}},
		{Type: "MapChange", Data: &clientv1.MapChange{MapId: "forest", X: 64, Y: 608}},
		{Type: "ChatMessage", Data: &clientv1.ChatMessage{Channel: clientv1.ChatChannel_CHAT_CHANNEL_MAP, Sender: charA, SenderName: "Ayla", Text: "hello there"}},
		{Type: "PartyState", Data: &clientv1.PartyState{Party: party()}},
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
