package protocol

import (
	clientv1 "finga/api/gen/client/v1"
	"finga/pkg/wsconn"
)

// Samples is one full message per type: the source of the golden files the client's tests also read.
func Samples() []wsconn.Message {
	const (
		charA = "0192f6a4-6f2b-7c3e-8a1b-2c3d4e5f6a7b"
		charB = "0192f6a4-6f2b-7c3e-8a1b-2c3d4e5f6a7c"
		charC = "0192f6a4-6f2b-7c3e-8a1b-2c3d4e5f6a7d"
		item1 = "0192f6a4-6f2b-7c3e-8a1b-2c3d4e5f6b01"
	)
	view := func() *clientv1.CharacterView {
		return &clientv1.CharacterView{Id: charA, Name: "Ayla", Class: "warrior", Level: 2, Exp: 15, Stats: &clientv1.Stats{Str: 15, Dex: 5, Int: 4, Agi: 5, Vit: 12}, Hp: 250, MaxHp: 250, Mp: 32, MaxMp: 32}
	}
	player := func() *clientv1.PlayerView {
		return &clientv1.PlayerView{Id: charA, Name: "Ayla", Class: "warrior", Level: 2, X: 416, Y: 608, Vx: 200, Facing: 1, OnGround: true, Hp: 250, MaxHp: 250}
	}
	// A player just hit by contact damage: invulnerable.
	hurt := &clientv1.PlayerView{Id: charB, Name: "Robin", Class: "bowman", Level: 1, X: 540, Y: 596, Vy: -300, Facing: -1, Hp: 90, MaxHp: 105, Invulnerable: true}
	mob := func() *clientv1.MobView {
		return &clientv1.MobView{Id: 7, Type: "green_slime", X: 500, Y: 608, Facing: -1, Hp: 30, MaxHp: 30}
	}
	drop := func() *clientv1.DropView {
		return &clientv1.DropView{Id: 12, Template: "slime_gel", Quantity: 2, X: 520, Y: 608}
	}
	party := func() *clientv1.PartyView {
		return &clientv1.PartyView{Leader: charA, Members: []*clientv1.PartyMember{
			{CharacterId: charA, Name: "Ayla", Level: 2, Hp: 250, MaxHp: 250, MapId: "forest", Online: true},
			{CharacterId: charB, Name: "Robin", Level: 1, Hp: 105, MaxHp: 105, MapId: "town"},
		}}
	}
	return []wsconn.Message{
		{Type: "Hello", Data: &clientv1.Hello{Token: "opaque-session-token", ProtocolVersion: Version, CharacterId: charA}},
		{Type: "Input", Seq: 117, Data: &clientv1.Input{Right: true, Jump: true}},
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
		{Type: "Ping", Data: &clientv1.Ping{ClientTime: 1761000000000}},

		{Type: "Welcome", Data: &clientv1.Welcome{Token: "s3ss10n-t0k3n-r0t4t3d", Character: view(), Inventory: []*clientv1.ItemView{{ItemId: item1, Template: "red_potion", Tab: "use", Slot: 0, Quantity: 3}}, Party: party(), MapId: "town", Tick: 81234, ServerTime: 1761000000050}},
		{Type: "Snapshot", Tick: 81234, Data: &clientv1.Snapshot{Players: []*clientv1.PlayerView{player()}, Mobs: []*clientv1.MobView{mob()}, Drops: []*clientv1.DropView{drop()}}},
		{Type: "Delta", Tick: 81235, Data: &clientv1.Delta{AckSeq: 117, Players: []*clientv1.PlayerView{player(), hurt}, Mobs: []*clientv1.MobView{mob()}, Drops: []*clientv1.DropView{drop()}, RemovedPlayers: []string{charC}, RemovedMobs: []uint32{6}, RemovedDrops: []uint32{11}}},
		{Type: "Correction", Tick: 81235, Data: &clientv1.Correction{Seq: 117, X: 416, Y: 608, OnGround: true}},
		{Type: "Damage", Tick: 81235, Data: &clientv1.Damage{Source: "p:" + charA, Target: "m:7", Amount: 23, Crit: true}},
		{Type: "Died", Tick: 81236, Data: &clientv1.Died{CharacterId: charA, X: 500, Y: 608}},
		{Type: "Respawned", Tick: 81296, Data: &clientv1.Respawned{CharacterId: charA, MapId: "town", X: 416, Y: 608, Hp: 125}},
		{Type: "ExpGained", Data: &clientv1.ExpGained{Amount: 5, Exp: 15}},
		{Type: "LevelUp", Data: &clientv1.LevelUp{Character: view()}},
		{Type: "InventoryChanged", Data: &clientv1.InventoryChanged{Slots: []*clientv1.InventorySlot{{Tab: "use", Slot: 0, Item: &clientv1.ItemView{ItemId: item1, Template: "red_potion", Tab: "use", Slot: 0, Quantity: 4}}, {Tab: "etc", Slot: 2}}}},
		{Type: "MapChange", Data: &clientv1.MapChange{MapId: "forest", X: 64, Y: 608}},
		{Type: "ChatMessage", Data: &clientv1.ChatMessage{Channel: clientv1.ChatChannel_CHAT_CHANNEL_MAP, Sender: charA, SenderName: "Ayla", Text: "hello there"}},
		{Type: "PartyState", Data: &clientv1.PartyState{Party: party()}},
		{Type: "PartyInvited", Data: &clientv1.PartyInvited{InviteId: "inv-1", From: charA, FromName: "Ayla"}},
		{Type: "Response", ReqID: "r-6", Data: &clientv1.Response{Code: "inventory_full", Message: "your inventory is full"}},
		{Type: "Kicked", Data: &clientv1.Kicked{Reason: clientv1.KickReason_KICK_REASON_LOGGED_IN_ELSEWHERE}},
		{Type: "Pong", Data: &clientv1.Pong{ClientTime: 1761000000000, ServerTime: 1761000000050}},
	}
}
