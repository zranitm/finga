package local

import (
	"maps"
	"reflect"
	"slices"
	"testing"
	"time"

	"finga/internal/kernel"
	"finga/pkg/copytest"
	"finga/services/gateway/internal/model"
	"finga/services/world"
)

// worldEnums take a valid value of their own, as an unknown one reads as the zero one; the maps between them have their own tests.
var worldEnums = map[reflect.Type]any{
	reflect.TypeFor[world.LootKind]():        world.LootWaiting,
	reflect.TypeFor[world.QuestChange]():     world.QuestHandedIn,
	reflect.TypeFor[world.QuestStatus]():     world.QuestFinished,
	reflect.TypeFor[world.OfferState]():      world.OfferUnmet, // so the conditions it lacks go too
	reflect.TypeFor[world.ConditionKind]():   world.ConditionSkill,
	reflect.TypeFor[world.MobPose]():         world.Down,
	reflect.TypeFor[model.LootKind]():        model.LootWaiting,
	reflect.TypeFor[model.QuestChange]():     model.QuestHandedIn,
	reflect.TypeFor[model.QuestStatus]():     model.QuestFinished,
	reflect.TypeFor[model.QuestOfferState](): model.OfferUnmet,
	reflect.TypeFor[model.ConditionKind]():   model.ConditionSkill,
	reflect.TypeFor[model.MobPose]():         model.Down,
}

// clientMovement is the movement state the client keeps itself, below a player's state at path.
func clientMovement(path string) map[string]string {
	const why = "movement state the client keeps itself"
	return map[string]string{path + ".Ground": why, path + ".DropPlatform": why, path + ".DropTimer": why}
}

// sheet is how a world sheet at from reaches the gateway at to: the invested skill points renamed, the free reset turned to whether it is left.
func sheet(m copytest.Match, from, to string) copytest.Match {
	m.Renames[from] = to
	m.Renames[from+".Skills.Points"] = to + ".Skills.Invested"
	m.Skip[from+".Points.FreeResetUsed"] = "the sheet says whether the reset is left: TestTheSheetSaysWhetherTheFreeResetIsLeft"
	m.Computed[to+".Points.FreeResetLeft"] = "FreeResetUsed negated: TestTheSheetSaysWhetherTheFreeResetIsLeft"
	return m
}

// streamed is a world event as its gateway's stream brings it.
type streamed[E world.Event] struct {
	Character kernel.CharacterID
	Conn      string
	Tick      uint64
	Event     E
}

// eventDiff diffs one event's push, of push's type, against the event: the delivery's character, connection and tick, and the event's fields under m's renames.
func eventDiff[E world.Event](t *testing.T, push any, m copytest.Match) []string {
	t.Helper()
	m.Renames = maps.Clone(m.Renames)
	for from, to := range map[string]string{"Conn": "Connection", "Tick": "Delivery.Tick", "Event": "Delivery.Push"} {
		if _, ok := m.Renames[from]; !ok {
			m.Renames[from] = to
		}
	}
	m.Enums = worldEnums
	return copytest.Diff(m, func(s streamed[E]) model.WorldEvent {
		var got []model.WorldEvent
		delivered(world.Delivery{Character: s.Character, Conn: s.Conn, Tick: s.Tick, Event: s.Event}, func(ev model.WorldEvent) { got = append(got, ev) })
		if len(got) != 1 {
			t.Fatalf("%d events", len(got))
		}
		if p, ok := got[0].(model.WorldDelivery); ok && reflect.TypeOf(p.Delivery.Push) != reflect.TypeOf(push) {
			t.Fatalf("became %T, want %T", p.Delivery.Push, push)
		} else if !ok && reflect.TypeOf(got[0]) != reflect.TypeOf(push) {
			t.Fatalf("became %T, want %T", got[0], push)
		}
		return got[0]
	})
}

// match is a Match with its maps made, so a row adds to them.
func match() copytest.Match {
	return copytest.Match{Renames: map[string]string{}, Skip: map[string]string{}, Computed: map[string]string{}}
}

// with adds renames, skips or computed paths to a map of a Match.
func with(m map[string]string, add ...map[string]string) map[string]string {
	for _, a := range add {
		maps.Copy(m, a)
	}
	return m
}

// Each world event reaches the use cases as its push, with every value the world sent where the gateway's own name for it says, but those the gateway leaves out.
func TestEveryWorldEventKeepsItsValues(t *testing.T) {
	const ref = "a ref names a player or a mob, and this one a player: TestADamageRefNamesItsMob"
	levelUp := func() copytest.Match {
		m := sheet(match(), "Event.Sheet", "Delivery.Push.Character.Sheet")
		m.Renames["Event"] = "Delivery.Push.Character"
		m.Computed["Delivery.Push.Character.Energy"] = "a level-up's energy comes with the frames"
		return m
	}
	for name, diff := range map[string]func() []string{
		"Snapshot": func() []string {
			m := match()
			with(m.Renames, map[string]string{
				"Event.Players.State": "Delivery.Push.Players", "Event.FieldBoss.Mob": "Delivery.Push.FieldBoss.MobID", "Event.FieldBoss.LeavesIn": "Delivery.Push.FieldBoss.LeavesInMs",
			})
			with(m.Skip, map[string]string{"Event.MapID": "the join or the teleport names the map"}, clientMovement("Event.Players.State"))
			return eventDiff[world.Snapshot](t, model.Snapshot{}, m)
		},
		"Teleport": func() []string { return eventDiff[world.Teleport](t, model.MapChange{}, match()) },
		"Detached": func() []string {
			m := match()
			m.Skip["Tick"] = "a detached character is on no map"
			return eventDiff[world.Detached](t, model.Detached{}, m)
		},
		"Damage": func() []string {
			m := match()
			with(m.Renames, map[string]string{"Event.Source.Player": "Delivery.Push.Source.Character", "Event.Target.Player": "Delivery.Push.Target.Character"})
			with(m.Skip, map[string]string{"Event.Source.Mob": ref, "Event.Target.Mob": ref})
			with(m.Computed, map[string]string{"Delivery.Push.Source.Mob": ref, "Delivery.Push.Target.Mob": ref})
			return eventDiff[world.Damage](t, model.Damage{}, m)
		},
		"Died": func() []string {
			m := match()
			m.Renames["Event.ID"] = "Delivery.Push.CharacterID"
			return eventDiff[world.Died](t, model.Died{}, m)
		},
		"Respawned": func() []string {
			m := match()
			m.Renames["Event.ID"] = "Delivery.Push.CharacterID"
			return eventDiff[world.Respawned](t, model.Respawned{}, m)
		},
		"ExpGained": func() []string { return eventDiff[world.ExpGained](t, model.ExpGained{}, match()) },
		"LevelUp":   func() []string { return eventDiff[world.LevelUp](t, model.LevelUp{}, levelUp()) },
		"Promoted":  func() []string { return eventDiff[world.Promoted](t, model.Promoted{}, levelUp()) },
		"StatsChanged": func() []string {
			return eventDiff[world.StatsChanged](t, model.StatsChanged{}, sheet(match(), "Event.Sheet", "Delivery.Push.Sheet"))
		},
		"SkillUsed": func() []string { return eventDiff[world.SkillUsed](t, model.SkillUsed{}, match()) },
		"AttackUsed": func() []string {
			m := match()
			m.Renames["Event.Length"] = "Delivery.Push.LengthTicks"
			return eventDiff[world.AttackUsed](t, model.AttackUsed{}, m)
		},
		"MobAttacked": func() []string {
			m := match()
			m.Renames["Event.Mob"] = "Delivery.Push.MobID"
			return eventDiff[world.MobAttacked](t, model.MobAttacked{}, m)
		},
		"FieldBoss": func() []string {
			m := match()
			with(m.Renames, map[string]string{"Event.Mob": "Delivery.Push.MobID", "Event.LeavesIn": "Delivery.Push.LeavesInMs"})
			return eventDiff[world.FieldBoss](t, model.FieldBoss{}, m)
		},
		"FieldBossGone": func() []string {
			m := match()
			m.Renames["Event.Mob"] = "Delivery.Push.MobID"
			return eventDiff[world.FieldBossGone](t, model.FieldBossGone{}, m)
		},
		"Chat": func() []string {
			m := match()
			m.Computed["Delivery.Push.Channel"] = "the world's lines are map lines: TestAWorldChatLineIsAMapLine"
			return eventDiff[world.Chat](t, model.ChatMessage{}, m)
		},
		"Loot": func() []string { return eventDiff[world.Loot](t, model.Loot{}, match()) },
		"InventoryChanged": func() []string {
			m := match()
			with(m.Computed, map[string]string{
				"Delivery.Push.Slots.Item.Tab":      "the slot's own tab: TestAnItemNamesTheSlotItSitsIn",
				"Delivery.Push.Slots.Item.Slot":     "the slot's own index: TestAnItemNamesTheSlotItSitsIn",
				"Delivery.Push.Equipment.Item.Tab":  "a worn item is in no tab: TestAnItemNamesTheSlotItSitsIn",
				"Delivery.Push.Equipment.Item.Slot": "a worn item is in no tab: TestAnItemNamesTheSlotItSitsIn",
			})
			return eventDiff[world.InventoryChanged](t, model.InventoryChanged{}, m)
		},
		"NpcDialog": func() []string { return eventDiff[world.NpcDialog](t, model.NpcDialog{}, match()) },
		"QuestChanged": func() []string {
			m := match()
			m.Renames["Event.ID"] = "Delivery.Push.Quest.Quest"
			return eventDiff[world.QuestChanged](t, model.QuestChanged{}, m)
		},
	} {
		if got := diff(); len(got) > 0 {
			t.Errorf("%s: %v", name, got)
		}
	}
}

// frameRecipient is one recipient's share of a frame: what differs between the events a frame becomes.
type frameRecipient struct {
	Connection string
	Character  kernel.CharacterID
	AckSeq     uint32
	Energy     *uint32
}

// A frame becomes one push per recipient, all sharing one copy of it, with every value the world sent but the movement state the client keeps itself.
func TestAFrameKeepsItsValues(t *testing.T) {
	type pushed struct {
		Tick       uint64
		Frame      *model.Frame
		Recipients []frameRecipient
	}
	const recipients = "a frame names its recipients instead"
	m := match()
	with(m.Renames, map[string]string{"Tick": "Tick", "Event": "Frame", "Event.Players.State": "Frame.Players", "Event.Removed": "Frame.RemovedPlayers", "Event.Recipients": "Recipients"})
	with(m.Skip, map[string]string{"Character": recipients, "Conn": recipients}, clientMovement("Event.Players.State"))
	m.Enums = worldEnums
	got := copytest.Diff(m, func(s streamed[world.Frame]) pushed {
		var out pushed
		delivered(world.Delivery{Character: s.Character, Conn: s.Conn, Tick: s.Tick, Event: s.Event}, func(ev model.WorldEvent) {
			d, ok := ev.(model.WorldDelivery)
			delta, is := d.Delivery.Push.(model.Delta)
			if !ok || !is {
				t.Fatalf("became %T", ev)
			}
			if out.Frame != nil && (delta.Frame != out.Frame || d.Delivery.Tick != out.Tick) {
				t.Fatal("the recipients' pushes share no frame")
			}
			out.Tick, out.Frame = d.Delivery.Tick, delta.Frame
			out.Recipients = append(out.Recipients, frameRecipient{Connection: d.Connection, Character: d.Character, AckSeq: delta.AckSeq, Energy: delta.Energy})
		})
		return out
	})
	if len(got) > 0 {
		t.Fatal(got)
	}
}

// An item in a changed slot names that slot's tab and index; a worn one names none.
func TestAnItemNamesTheSlotItSitsIn(t *testing.T) {
	push, _ := worldPush(world.InventoryChanged{
		Slots: []world.Slot{
			{Tab: "use", Slot: 4, Item: &world.SlotItem{Template: "red_potion", Quantity: 3}},
			{Tab: "equip", Slot: 7, Item: &world.SlotItem{Template: "knight_sword", Quantity: 1}},
		},
		Equipment: []world.EquipSlot{{Slot: "weapon", Item: &world.SlotItem{Template: "wooden_sword", Quantity: 1}}},
	})
	changed := push.(model.InventoryChanged)
	if len(changed.Slots) != 2 || len(changed.Equipment) != 1 {
		t.Fatalf("became %+v", changed)
	}
	for _, s := range changed.Slots {
		if s.Item.Tab != s.Tab || s.Item.Slot != s.Slot {
			t.Errorf("the item in %s %d names %s %d", s.Tab, s.Slot, s.Item.Tab, s.Item.Slot)
		}
	}
	if w := changed.Equipment[0].Item; w.Tab != "" || w.Slot != 0 {
		t.Errorf("a worn item names %s %d", w.Tab, w.Slot)
	}
}

// A world's chat line is a map line.
func TestAWorldChatLineIsAMapLine(t *testing.T) {
	if p, ok := worldPush(world.Chat{Text: "hi"}); !ok || p.(model.ChatMessage).Channel != model.ChatMap {
		t.Fatalf("became %+v", p)
	}
}

// A ref names a mob when it names no player.
func TestADamageRefNamesItsMob(t *testing.T) {
	player := kernel.NewCharacterID()
	p, ok := worldPush(world.Damage{Source: world.EntityRef{Player: player}, Target: world.EntityRef{Mob: 7}, Amount: 3})
	if !ok {
		t.Fatal("no push")
	}
	if d := p.(model.Damage); d.Source != (model.Entity{Character: player.String()}) || d.Target != (model.Entity{Mob: 7}) {
		t.Fatalf("damage %+v", d)
	}
	p, _ = worldPush(world.Damage{Source: world.EntityRef{Mob: 9}, Target: world.EntityRef{Player: player}, Amount: 5})
	if d := p.(model.Damage); d.Source != (model.Entity{Mob: 9}) || d.Target != (model.Entity{Character: player.String()}) {
		t.Fatalf("a mob's hit %+v", d)
	}
}

// An offer the player may take carries no conditions, as world.v1 left them unset.
func TestOnlyAnUnmetOfferNamesItsConditions(t *testing.T) {
	d := npcDialog(world.NpcDialog{NPC: "elder", Gives: []world.QuestOffer{
		{Quest: "rats", State: world.OfferAvailable, Unmet: world.Unmet{Level: 9}},
		{Quest: "wolves", State: world.OfferUnmet, Unmet: world.Unmet{Level: 9, Class: "warrior"}},
	}})
	if u := d.Gives[1].Unmet; d.Gives[0].Unmet.Level != 0 || u.Level != 9 || u.Class != "warrior" {
		t.Fatalf("offers %+v", d.Gives)
	}
}

// An HP-only change leaves the sheet unset, so the client keeps its stats; a changed sheet comes through.
func TestAStatsChangedKeepsAnUnsetSheetUnset(t *testing.T) {
	push, ok := worldPush(world.StatsChanged{HP: 50, MaxHP: 135})
	if sc, is := push.(model.StatsChanged); !ok || !is || sc.Sheet != nil || sc.HP != 50 || sc.MaxHP != 135 {
		t.Fatalf("got %+v, want HP with no sheet", push)
	}
	sheet := world.Sheet{Points: world.Points{Unspent: 4}}
	sheet.Primary.STR = 11
	push, ok = worldPush(world.StatsChanged{Sheet: &sheet, HP: 50, MaxHP: 208})
	if sc, is := push.(model.StatsChanged); !ok || !is || sc.Sheet == nil || sc.Sheet.Primary.STR != 11 || sc.Sheet.Points.Unspent != 4 {
		t.Fatalf("got %+v, want the sheet carried", push)
	}
}

// A mob's pose comes through, so a player who joins sees a downed mob; a pose the gateway doesn't know is standing.
func TestAMobCarriesItsPose(t *testing.T) {
	for pose, want := range map[world.MobPose]model.MobPose{
		world.Standing:   model.Standing,
		world.Flinching:  model.Flinching,
		world.Down:       model.Down,
		world.Rising:     model.Rising,
		world.MobPose(9): model.Standing,
	} {
		push, ok := worldPush(world.Snapshot{Mobs: []world.MobView{{ID: 4, Pose: pose}}})
		if s, is := push.(model.Snapshot); !ok || !is || len(s.Mobs) != 1 || s.Mobs[0].Pose != want {
			t.Errorf("pose %d: got %+v, want a mob in pose %d", pose, push, want)
		}
	}
}

// A Snapshot carries the map's field boss while one is up, and none otherwise.
func TestASnapshotCarriesTheFieldBossWhileOneIsUp(t *testing.T) {
	push, ok := worldPush(world.Snapshot{FieldBoss: &world.FieldBoss{Mob: 9, Type: "king_slime", LeavesIn: 7 * time.Minute}})
	if s, is := push.(model.Snapshot); !ok || !is || s.FieldBoss == nil || *s.FieldBoss != (model.FieldBoss{MobID: 9, Type: "king_slime", LeavesInMs: 420000}) {
		t.Fatalf("got %+v, want the king leaving in 7 minutes", push)
	}
	push, ok = worldPush(world.Snapshot{MapID: "forest"})
	if s, is := push.(model.Snapshot); !ok || !is || s.FieldBoss != nil {
		t.Fatalf("got %+v, want no boss", push)
	}
}

// A trainer's promotion names each condition by its kind, as the client words it.
func TestEachPromotionConditionKeepsItsKind(t *testing.T) {
	push, ok := worldPush(world.NpcDialog{NPC: "trainer", Promotion: &world.PromotionOffer{Class: "knight", Conditions: []world.Condition{
		{Kind: world.ConditionLevel, Need: 10, Have: 3},
		{Kind: world.ConditionStat, ID: "str", Need: 20, Have: 12},
		{Kind: world.ConditionSkill, ID: "strike", Need: 5, Have: 5},
		{Kind: world.ConditionQuest, ID: "slime_cleanup", Need: 1, Have: 0},
	}}})
	d, is := push.(model.NpcDialog)
	if !ok || !is || d.Promotion == nil {
		t.Fatalf("got %+v, want a promotion", push)
	}
	want := []model.PromotionCondition{
		{Kind: model.ConditionLevel, Need: 10, Have: 3},
		{Kind: model.ConditionStat, ID: "str", Need: 20, Have: 12},
		{Kind: model.ConditionSkill, ID: "strike", Need: 5, Have: 5},
		{Kind: model.ConditionQuest, ID: "slime_cleanup", Need: 1, Have: 0},
	}
	if d.Promotion.Class != "knight" || !slices.Equal(d.Promotion.Conditions, want) {
		t.Fatalf("promotion %+v, want knight with %+v", d.Promotion, want)
	}
}

// Each loot kind, quest change, quest status and offer state reaches the use cases as its own; one the gateway doesn't know reads as none.
func TestEachWorldEnumKeepsItsValue(t *testing.T) {
	for kind, want := range map[world.LootKind]model.LootKind{
		world.LootCollected: model.LootCollected, world.LootWaiting: model.LootWaiting, world.LootLost: model.LootLost, world.LootKind(9): 0,
	} {
		if push, _ := worldPush(world.Loot{Kind: kind}); push.(model.Loot).Kind != want {
			t.Errorf("loot kind %d: %+v, want %d", kind, push, want)
		}
	}
	for change, want := range map[world.QuestChange]model.QuestChange{
		world.QuestProgressed: model.QuestProgressed, world.QuestAccepted: model.QuestAccepted, world.QuestHandedIn: model.QuestHandedIn,
		world.QuestAbandoned: model.QuestAbandoned, world.QuestChange(9): 0,
	} {
		if push, _ := worldPush(world.QuestChanged{Change: change}); push.(model.QuestChanged).Change != want {
			t.Errorf("quest change %d: %+v, want %d", change, push, want)
		}
	}
	for status, want := range map[world.QuestStatus]model.QuestStatus{
		world.QuestActive: model.QuestActive, world.QuestFinished: model.QuestFinished, world.QuestStatus(9): 0,
	} {
		if push, _ := worldPush(world.QuestChanged{Quest: world.Quest{Status: status}}); push.(model.QuestChanged).Quest.Status != want {
			t.Errorf("quest status %d: %+v, want %d", status, push, want)
		}
	}
	for state, want := range map[world.OfferState]model.QuestOfferState{
		world.OfferAvailable: model.OfferAvailable, world.OfferActive: model.OfferActive, world.OfferFinished: model.OfferFinished,
		world.OfferUnmet: model.OfferUnmet, world.OfferState(9): 0,
	} {
		push, _ := worldPush(world.NpcDialog{Gives: []world.QuestOffer{{Quest: "q", State: state}}})
		if gives := push.(model.NpcDialog).Gives; len(gives) != 1 || gives[0].State != want {
			t.Errorf("offer state %d: %+v, want %d", state, push, want)
		}
	}
}
