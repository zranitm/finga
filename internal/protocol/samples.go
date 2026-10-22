package protocol

import "finga/pkg/wsconn"

// Samples is one representative message per type, with every field set.
// They produce the golden files in testdata/contracts, which the client's
// tests read too, so the two sides agree on the wire format.
func Samples() []wsconn.Message {
	const (
		charA = "0192f6a4-6f2b-7c3e-8a1b-2c3d4e5f6a7b"
		charB = "0192f6a4-6f2b-7c3e-8a1b-2c3d4e5f6a7c"
		charC = "0192f6a4-6f2b-7c3e-8a1b-2c3d4e5f6a7d"
		item1 = "0192f6a4-6f2b-7c3e-8a1b-2c3d4e5f6b01"
	)
	view := CharacterView{ID: charA, Name: "Ayla", Class: "warrior", Level: 2, Exp: 15, Stats: Stats{STR: 15, DEX: 5, INT: 4, AGI: 5, VIT: 12}, HP: 250, MaxHP: 250, MP: 32, MaxMP: 32}
	player := PlayerView{ID: charA, Name: "Ayla", Class: "warrior", Level: 2, X: 416, Y: 608, VX: 200, VY: 0, Facing: 1, OnGround: true, HP: 250, MaxHP: 250}
	// A second player a second after contact damage: invulnerable (step 19).
	hurt := PlayerView{ID: charB, Name: "Robin", Class: "bowman", Level: 1, X: 540, Y: 596, VX: 0, VY: -300, Facing: -1, OnGround: false, HP: 90, MaxHP: 105, Invulnerable: true}
	mob := MobView{ID: 7, Type: "green_slime", X: 500, Y: 608, Facing: -1, HP: 30, MaxHP: 30}
	drop := DropView{ID: 12, Template: "slime_gel", Quantity: 2, X: 520, Y: 608}
	party := &PartyView{Leader: charA, Members: []PartyMember{
		{CharacterID: charA, Name: "Ayla", Level: 2, HP: 250, MaxHP: 250, MapID: "forest", Online: true},
		{CharacterID: charB, Name: "Robin", Level: 1, HP: 105, MaxHP: 105, MapID: "town", Online: false},
	}}
	return []wsconn.Message{
		{Type: "Hello", Data: Hello{Token: "opaque-session-token", ProtocolVersion: Version, CharacterID: charA}},
		{Type: "Input", Seq: 117, Data: Input{Right: true, Jump: true}},
		{Type: "Attack", Seq: 118, Data: Attack{}},
		{Type: "PickUp", Seq: 119, Data: PickUp{}},
		{Type: "EnterPortal", Seq: 120, Data: EnterPortal{}},
		{Type: "ChatSend", Data: ChatSend{Channel: ChatMap, Text: "hello there"}},
		{Type: "PartyInvite", ReqID: "r-1", Data: PartyInvite{CharacterID: charB}},
		{Type: "PartyRespond", ReqID: "r-2", Data: PartyRespond{InviteID: "inv-1", Accept: true}},
		{Type: "PartyLeave", ReqID: "r-3", Data: PartyLeave{}},
		{Type: "PartyKick", ReqID: "r-4", Data: PartyKick{CharacterID: charB}},
		{Type: "PartyPromote", ReqID: "r-5", Data: PartyPromote{CharacterID: charB}},
		{Type: "InventoryMove", ReqID: "r-6", Data: InventoryMove{IdempotencyKey: "k-1", ItemID: item1, Tab: "use", Slot: 3}},
		{Type: "InventoryDiscard", ReqID: "r-7", Data: InventoryDiscard{IdempotencyKey: "k-2", ItemID: item1, Quantity: 1}},
		{Type: "Ping", Data: Ping{ClientTime: 1761000000000}},

		{Type: "Welcome", Data: Welcome{Token: "s3ss10n-t0k3n-r0t4t3d", Character: view, Inventory: []ItemView{{ItemID: item1, Template: "red_potion", Tab: "use", Slot: 0, Quantity: 3}}, Party: party, MapID: "town", Tick: 81234, ServerTime: 1761000000050}},
		{Type: "Snapshot", Tick: 81234, Data: Snapshot{Players: []PlayerView{player}, Mobs: []MobView{mob}, Drops: []DropView{drop}}},
		{Type: "Delta", Tick: 81235, Data: Delta{AckSeq: 117, Players: []PlayerView{player, hurt}, Mobs: []MobView{mob}, Drops: []DropView{drop}, RemovedPlayers: []string{charC}, RemovedMobs: []uint64{6}, RemovedDrops: []uint64{11}}},
		{Type: "Correction", Tick: 81235, Data: Correction{Seq: 117, X: 416, Y: 608, VX: 0, VY: 0, OnGround: true}},
		{Type: "Damage", Tick: 81235, Data: Damage{Source: "p:" + charA, Target: "m:7", Amount: 23, Crit: true}},
		{Type: "Died", Tick: 81236, Data: Died{CharacterID: charA, X: 500, Y: 608}},
		{Type: "Respawned", Tick: 81296, Data: Respawned{CharacterID: charA, MapID: "town", X: 416, Y: 608, HP: 125}},
		{Type: "ExpGained", Data: ExpGained{Amount: 5, Exp: 15}},
		{Type: "LevelUp", Data: LevelUp{Character: view}},
		{Type: "InventoryChanged", Data: InventoryChanged{Slots: []InventorySlot{{Tab: "use", Slot: 0, Item: &ItemView{ItemID: item1, Template: "red_potion", Tab: "use", Slot: 0, Quantity: 4}}, {Tab: "etc", Slot: 2}}}},
		{Type: "MapChange", Data: MapChange{MapID: "forest", X: 64, Y: 608}},
		{Type: "ChatMessage", Data: ChatMessage{Channel: ChatMap, Sender: charA, SenderName: "Ayla", Text: "hello there"}},
		{Type: "PartyState", Data: PartyState{Party: party}},
		{Type: "PartyInvited", Data: PartyInvited{InviteID: "inv-1", From: charA, FromName: "Ayla"}},
		{Type: "Response", ReqID: "r-6", Data: Response{OK: false, Code: "inventory_full", Message: "your inventory is full"}},
		{Type: "Kicked", Data: Kicked{Reason: KickLoggedInElsewhere}},
		{Type: "Pong", Data: Pong{ClientTime: 1761000000000, ServerTime: 1761000000050}},
	}
}
