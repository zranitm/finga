package world

import (
	"go/token"
	"maps"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	clientv1 "finga/api/gen/client/v1"
	"finga/internal/application/item"
	"finga/internal/application/world"
	"finga/internal/handler/transport"
	"finga/internal/shared/kernel"
	"finga/internal/shared/sim"
	"finga/pkg/copytest"

	"google.golang.org/protobuf/proto"
)

// worldEnums take a valid value of their own, as an unknown one reads as the zero one; the maps between them have their own tests.
var worldEnums = map[reflect.Type]any{
	reflect.TypeFor[world.LootKind]():       world.LootLeft,
	reflect.TypeFor[world.PouchKind]():      world.PouchBox,
	reflect.TypeFor[world.QuestChange]():    world.QuestHandedIn,
	reflect.TypeFor[world.QuestStatus]():    world.QuestFinished,
	reflect.TypeFor[world.OfferState]():     world.OfferUnmet, // so the conditions it lacks go too
	reflect.TypeFor[world.ConditionKind]():  world.ConditionSkill,
	reflect.TypeFor[world.MobPose]():        world.Down,
	reflect.TypeFor[world.TeleportCancel](): world.TeleportHit,
	reflect.TypeFor[world.MatchPhase]():     world.PhaseResult,
	reflect.TypeFor[world.PieceCause]():     world.PieceFromFeat,
}

// enumMapped is why an enum's value is no copy: the wire's enum is numbered by its own.
const enumMapped = "the enum's own map: TestEachWorldEnumKeepsItsName"

// named adds to m the renames of every field of in the wire message out names by another case (HP, Hp), at any depth, so a row names only the real differences.
func named[In, Out any](m copytest.Match) copytest.Match {
	var walk func(in, out reflect.Type, inPath, outPath string)
	walk = func(in, out reflect.Type, inPath, outPath string) {
		for in.Kind() == reflect.Pointer || in.Kind() == reflect.Slice {
			in = in.Elem()
		}
		for out.Kind() == reflect.Pointer || out.Kind() == reflect.Slice {
			out = out.Elem()
		}
		if in.Kind() != reflect.Struct || out.Kind() != reflect.Struct {
			return
		}
		for f := range in.Fields() {
			if !f.IsExported() {
				continue
			}
			ip := join(inPath, f.Name)
			if _, skipped := m.Skip[ip]; skipped {
				continue
			}
			if f.Anonymous && f.Type.Kind() == reflect.Struct { // an embedded struct's fields lie in its parent
				walk(f.Type, out, inPath, outPath)
				continue
			}
			if f.Name == "State" && f.Type == reflect.TypeFor[sim.State]() { // the movement state lies flat in the wire's view
				m.Renames[ip] = outPath
				walk(f.Type, out, ip, outPath)
				continue
			}
			g, ok := out.FieldByNameFunc(func(n string) bool { return token.IsExported(n) && strings.EqualFold(n, f.Name) })
			if !ok {
				continue
			}
			op := join(outPath, g.Name)
			if r, set := m.Renames[ip]; set {
				op = r
			}
			ft := f.Type
			if ft.Kind() == reflect.Pointer {
				ft = ft.Elem()
			}
			if _, isEnum := worldEnums[ft]; isEnum {
				m.Skip[ip], m.Computed[op] = enumMapped, enumMapped
				continue
			}
			if _, set := m.Renames[ip]; !set && ip != op {
				m.Renames[ip] = op
			}
			walk(f.Type, g.Type, ip, op)
		}
	}
	walk(reflect.TypeFor[In](), reflect.TypeFor[Out](), "", "")
	return m
}

func join(prefix, name string) string {
	if prefix == "" {
		return name
	}
	return prefix + "." + name
}

// match is a Match with its maps made, so a row adds to them.
func match() copytest.Match {
	return copytest.Match{Renames: map[string]string{}, Skip: map[string]string{}, Computed: map[string]string{}, Enums: worldEnums}
}

// with adds renames, skips or computed paths to a map of a Match.
func with(m map[string]string, add map[string]string) map[string]string {
	maps.Copy(m, add)
	return m
}

// sheetMatch is how a world sheet reaches the client: the invested skill points renamed, the free reset turned to whether it is left.
func sheetMatch(m copytest.Match, from, to string) copytest.Match {
	m.Renames[from+".Skills.Points"] = to + ".Skills.Invested"
	m.Renames[from+".Buffs.Left"] = to + ".Buffs.LeftMs"
	m.Skip[from+".Points.FreeResetUsed"] = "the sheet says whether the reset is left: TestTheSheetSaysWhetherTheFreeResetIsLeft"
	m.Computed[to+".Points.FreeResetLeft"] = "FreeResetUsed negated: TestTheSheetSaysWhetherTheFreeResetIsLeft"
	return m
}

// Each world event reaches the client as its message, with every value the world sent where the wire's own name for it says, but those the wire leaves out.
func TestEveryWorldEventKeepsItsValues(t *testing.T) {
	const ref = "a ref names a player or a mob, and the wire one string for either: TestADamageRefNamesItsMob"
	const lock = "a lock names a player or a mob, or none: TestASkillsLockNamesItsMobOrNothing"
	levelUpRows := func(from string) copytest.Match {
		m := sheetMatch(match(), "Sheet", "Sheet")
		m.Computed["Energy"] = "a level-up's energy comes with the frames"
		return m
	}
	for name, diff := range map[string]func() []string{
		"Snapshot": func() []string {
			m := match()
			with(m.Skip, map[string]string{"MapID": "the join or the teleport names the map", "Players.Look.Revision": "the revision stays on the server"})
			with(m.Renames, map[string]string{"FieldBoss.Mob": "FieldBoss.MobId", "FieldBossGone.Mob": "FieldBossGone.MobId", "FieldBossGone.BackIn": "FieldBossGone.BackInMs", "Pouches.GoneIn": "Pouches.GoneInMs", "Pouches.Items.ID": "Pouches.Items.Item"})
			return copytest.Diff(named[world.Snapshot, clientv1.Snapshot](m), snapshot)
		},
		"Teleport": func() []string { return copytest.Diff(named[world.Teleport, clientv1.MapChange](match()), teleport) },
		"TeleportCancelled": func() []string {
			return copytest.Diff(named[world.TeleportCancelled, clientv1.TeleportCancelled](match()), teleportCancelled)
		},
		"PartySpots": func() []string {
			m := match()
			m.Renames["Spots.ID"] = "Spots.CharacterId"
			return copytest.Diff(named[world.PartySpots, clientv1.PartySpots](m), partySpots)
		},
		"WaypointFound": func() []string {
			return copytest.Diff(named[world.WaypointFound, clientv1.WaypointFound](match()), waypointFound)
		},
		"PiecesAdded": func() []string {
			m := match()
			m.Renames["Pieces"] = "Pieces.Id"
			m.Skip["Cause"] = "each piece names it: TestPiecesAddedNamesEachPieceAndItsCause"
			m.Computed["Pieces.Cause"] = "the cause's name: TestPiecesAddedNamesEachPieceAndItsCause"
			return copytest.Diff(named[world.PiecesAdded, clientv1.PiecesAdded](m), piecesAdded)
		},
		"AreaEntered": func() []string {
			m := match()
			with(m.Renames, map[string]string{"FieldBoss.Mob": "FieldBoss.MobId", "FieldBossGone.Mob": "FieldBossGone.MobId", "FieldBossGone.BackIn": "FieldBossGone.BackInMs"})
			return copytest.Diff(named[world.AreaEntered, clientv1.AreaEntered](m), areaEntered)
		},
		"ArenaBar": func() []string {
			m := match()
			m.Renames["Bar"] = "SkillBar"
			return copytest.Diff(named[world.ArenaBar, clientv1.ArenaBar](m), arenaBar)
		},
		"Damage": func() []string {
			m := match()
			with(m.Skip, map[string]string{"Source.Player": ref, "Source.Mob": ref, "Target.Player": ref, "Target.Mob": ref})
			with(m.Computed, map[string]string{"Source": ref, "Target": ref})
			m.Renames["Status"], m.Renames["Shielded"] = "StatusId", "Shielded"
			return copytest.Diff(named[world.Damage, clientv1.Damage](m), damage)
		},
		"Died": func() []string {
			m := match()
			m.Renames["ID"] = "CharacterId"
			return copytest.Diff(named[world.Died, clientv1.Died](m), died)
		},
		"Respawned": func() []string {
			m := match()
			m.Renames["ID"] = "CharacterId"
			return copytest.Diff(named[world.Respawned, clientv1.Respawned](m), respawned)
		},
		"ExpGained": func() []string { return copytest.Diff(named[world.ExpGained, clientv1.ExpGained](match()), expGained) },
		"LevelUp": func() []string {
			return copytest.Diff(named[world.LevelUp, clientv1.CharacterView](levelUpRows("")), func(p world.LevelUp) *clientv1.CharacterView { return levelUp(p).Character })
		},
		"Promoted": func() []string {
			return copytest.Diff(named[world.LevelUp, clientv1.CharacterView](levelUpRows("")), func(p world.LevelUp) *clientv1.CharacterView { return promoted(world.Promoted(p)).Character })
		},
		"StatsChanged": func() []string {
			m := sheetMatch(match(), "Sheet", "Sheet")
			m.Skip["Timers"] = "time left at now, as the Welcome's: TestARetimeCarriesEachTimersTimeLeft"
			m.Computed["Timers"] = "time left at now: TestARetimeCarriesEachTimersTimeLeft"
			return copytest.Diff(named[world.StatsChanged, clientv1.StatsChanged](m), statsChanged)
		},
		"MatchState": func() []string {
			m := match()
			with(m.Renames, map[string]string{"EndsIn": "EndsInMs", "Sides.ID": "Sides.Fighters.Id", "Sides.Name": "Sides.Fighters.Name", "Sides.Class": "Sides.Fighters.Class", "Sides.Bot": "Sides.Fighters.Bot", "Sides.Away": "Sides.Fighters.Away"})
			const battle = "a battle's or a round match's own: TestAMatchStateNamesAScoreOrWinsByItsKind"
			with(m.Skip, map[string]string{"Battle": battle, "Score": battle, "Points": battle, "Wins": battle})
			with(m.Computed, map[string]string{"Score": battle, "Points.Id": battle, "Points.Holder": battle, "Points.Progress": battle, "Points.Contested": battle, "Wins": battle})
			return copytest.Diff(named[world.MatchState, clientv1.MatchState](m), matchState)
		},
		"SkillUsed": func() []string {
			m := match()
			m.Skip["Target.Player"], m.Skip["Target.Mob"], m.Computed["Target"] = lock, lock, lock
			return copytest.Diff(named[world.SkillUsed, clientv1.SkillUsed](m), skillUsed)
		},
		"AttackUsed": func() []string {
			m := match()
			m.Renames["Length"] = "LengthTicks"
			return copytest.Diff(named[world.AttackUsed, clientv1.AttackUsed](m), attackUsed)
		},
		"MobAttacked": func() []string {
			return copytest.Diff(named[world.MobAttacked, clientv1.MobAttacked](match()), mobAttacked)
		},
		"FieldBoss": func() []string {
			m := match()
			m.Renames["Mob"] = "MobId"
			return copytest.Diff(named[world.FieldBoss, clientv1.FieldBoss](m), fieldBoss)
		},
		"FieldBossGone": func() []string {
			m := match()
			with(m.Renames, map[string]string{"Mob": "MobId", "BackIn": "BackInMs"})
			return copytest.Diff(named[world.FieldBossGone, clientv1.FieldBossGone](m), bossGone)
		},
		"Chat": func() []string {
			m := match()
			m.Computed["Channel"] = "the world's lines are map lines: TestAWorldChatLineIsAMapLine"
			return copytest.Diff(named[world.Chat, clientv1.ChatMessage](m), chat)
		},
		"Loot": func() []string {
			m := match()
			with(m.Renames, map[string]string{"Pouch": "PouchId", "Owner": "OwnerId", "Items": "Items"})
			return copytest.Diff(named[world.Loot, clientv1.Loot](m), loot)
		},
		"InventoryChanged": func() []string {
			m := match()
			with(m.Renames, map[string]string{"Slots.Item.ID": "Slots.Item.ItemId", "Equipment.Slot": "Equipment.EquipSlot", "Equipment.Item.ID": "Equipment.Item.ItemId", "Slots.Item.Template": "Slots.Item.Template"})
			with(m.Computed, map[string]string{
				"Slots.Item.Tab": "the slot's own tab: TestAnItemNamesTheSlotItSitsIn", "Slots.Item.Slot": "the slot's own index: TestAnItemNamesTheSlotItSitsIn",
				"Equipment.Item.Tab": "a worn item is in no tab: TestAnItemNamesTheSlotItSitsIn", "Equipment.Item.Slot": "a worn item is in no tab: TestAnItemNamesTheSlotItSitsIn",
			})
			return copytest.Diff(named[world.InventoryChanged, clientv1.InventoryChanged](m), inventoryChanged)
		},
		"NpcDialog": func() []string {
			m := match()
			with(m.Renames, map[string]string{"NPC": "NpcId", "Gives.Quest": "Gives.QuestId", "Takes.Quest": "Takes.QuestId", "Promotion.Conditions.ID": "Promotion.Conditions.Id"})
			return copytest.Diff(named[world.NpcDialog, clientv1.NpcDialog](m), npcDialog)
		},
		"QuestChanged": func() []string {
			m := match()
			with(m.Renames, map[string]string{"ID": "Quest.QuestId", "Quest.Progress": "Quest.Progress", "Quest.TimesFinished": "Quest.TimesFinished"})
			m.Skip["Quest.Status"], m.Computed["Quest.Status"] = enumMapped, enumMapped
			return copytest.Diff(named[world.QuestChanged, clientv1.QuestChanged](m), questChanged)
		},
	} {
		if got := diff(); len(got) > 0 {
			t.Errorf("%s: %v", name, got)
		}
	}
	_ = []any{kernel.CharacterID{}, time.Second, proto.Equal}
}

// A battle counts its score and its capture points, and every other match its rounds' wins.
func TestAMatchStateNamesAScoreOrWinsByItsKind(t *testing.T) {
	st := world.MatchState{Wins: [2]int{2, 1}, Score: [2]int{30, 28}, Points: []world.CapturePoint{{ID: "a", Holder: 1, Progress: 0.5, Contested: true}}}
	rounds := matchState(st)
	if !slices.Equal(rounds.Wins, []int32{2, 1}) || rounds.Score != nil || rounds.Points != nil {
		t.Fatalf("a round match went out as %+v", rounds)
	}
	st.Battle = true
	battle := matchState(st)
	if !slices.Equal(battle.Score, []int32{30, 28}) || battle.Wins != nil || len(battle.Points) != 1 || battle.Points[0].Id != "a" || battle.Points[0].Holder != 1 || battle.Points[0].Progress != 0.5 || !battle.Points[0].Contested {
		t.Fatalf("a battle went out as %+v", battle)
	}
}

// A frame's part reaches every player sent it with every value the world sent, the removed players by their IDs.
func TestAFramesPartKeepsItsValues(t *testing.T) {
	m := match()
	with(m.Renames, map[string]string{"Removed": "RemovedPlayers", "Boss.ID": "Boss.Id", "Mobs.ID": "Mobs.Id"})
	m.Renames["Players.Look.Revision"] = ""
	m.Skip["Players.Look.Revision"] = "the revision stays on the server"
	m.Computed["AckSeq"] = "a part is no player's: TestAFramesRecipientKeepsItsValues"
	if got := copytest.Diff(named[world.Part, clientv1.Delta](m), partDelta); len(got) > 0 {
		t.Error(got)
	}
}

// A frame's share for one player alone carries their input acknowledged, their energy, what they stopped seeing, their own view and pouches.
func TestAFramesRecipientKeepsItsValues(t *testing.T) {
	const shared = "the frame's parts they're sent are the shared ones: TestAFramesPartKeepsItsValues"
	const elsewhere = "the delivery names who gets it, not the message"
	m := match()
	with(m.Skip, map[string]string{"Character": elsewhere, "Connection": elsewhere, "Parts": shared, "Own": "a player's own view: TestAPlayersOwnViewKeepsItsValues", "Pouches.Changed": "a pouch: the Snapshot row of TestEveryWorldEventKeepsItsValues"})
	with(m.Renames, map[string]string{"Pouches.Gone": "PouchesGone", "Unseen.Players": "Unseen.Players", "Unseen.Mobs": "Unseen.Mobs"})
	got := copytest.Diff(named[world.Recipient, clientv1.Delta](m), func(r world.Recipient) *clientv1.Delta {
		d := recipientDelta(r)
		d.Players, d.Pouches = nil, nil
		return d
	})
	if len(got) > 0 {
		t.Error(got)
	}
}

// A player's own view reaches them whole with every value the world sent, and as nothing when it didn't change.
func TestAPlayersOwnViewKeepsItsValues(t *testing.T) {
	m := match()
	m.Skip["Look.Revision"] = "the revision stays on the server"
	if got := copytest.Diff(named[world.PlayerView, clientv1.PlayerView](m), func(v world.PlayerView) *clientv1.PlayerView { return ownView(&v)[0] }); len(got) > 0 {
		t.Error(got)
	}
	if ownView(nil) != nil {
		t.Error("an unchanged view went out")
	}
}

// An item in a changed slot names that slot's tab and index; a worn one names none.
func TestAnItemNamesTheSlotItSitsIn(t *testing.T) {
	changed := inventoryChanged(world.InventoryChanged{
		Slots: []world.Slot{
			{Tab: "use", Slot: 4, Item: &world.SlotItem{Template: "red_potion", Quantity: 3}},
			{Tab: "equip", Slot: 7, Item: &world.SlotItem{Template: "knight_sword", Quantity: 1}},
		},
		Equipment: []world.EquipSlot{{Slot: "weapon", Item: &world.SlotItem{Template: "wooden_sword", Quantity: 1}}},
	})
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
	if got := chat(world.Chat{Text: "hi"}); got.Channel != clientv1.ChatChannel_CHAT_CHANNEL_MAP {
		t.Fatalf("became %+v", got)
	}
}

// A skill's lock names its player, else its mob, and no lock stays none rather than mob 0.
func TestASkillsLockNamesItsMobOrNothing(t *testing.T) {
	player := kernel.NewCharacterID()
	for _, c := range []struct {
		lock world.EntityRef
		want string
	}{
		{world.EntityRef{}, ""},
		{world.EntityRef{Mob: 7}, "m:7"},
		{world.EntityRef{Player: player}, "p:" + player.String()},
	} {
		if got := skillUsed(world.SkillUsed{Caster: player, Skill: "strike", Target: c.lock}).Target; got != c.want {
			t.Errorf("lock %+v went out as %q, want %q", c.lock, got, c.want)
		}
	}
}

// A ref names a mob when it names no player.
func TestADamageRefNamesItsMob(t *testing.T) {
	player := kernel.NewCharacterID()
	if d := damage(world.Damage{Source: world.EntityRef{Player: player}, Target: world.EntityRef{Mob: 7}, Amount: 3}); d.Source != "p:"+player.String() || d.Target != "m:7" {
		t.Fatalf("damage %+v", d)
	}
	if d := damage(world.Damage{Source: world.EntityRef{Mob: 9}, Target: world.EntityRef{Player: player}, Amount: 5}); d.Source != "m:9" || d.Target != "p:"+player.String() {
		t.Fatalf("a mob's hit %+v", d)
	}
}

// An offer the player may take carries no conditions, as world.v1 left them unset.
func TestOnlyAnUnmetOfferNamesItsConditions(t *testing.T) {
	d := npcDialog(world.NpcDialog{NPC: "elder", Gives: []world.QuestOffer{
		{Quest: "rats", State: world.OfferAvailable, Unmet: world.Unmet{Level: 9}},
		{Quest: "wolves", State: world.OfferUnmet, Unmet: world.Unmet{Level: 9, Class: "warrior"}},
	}})
	if u := d.Gives[1].Unmet; d.Gives[0].Unmet != nil || u.Level != 9 || u.Class != "warrior" {
		t.Fatalf("offers %+v", d.Gives)
	}
}

// An HP-only change leaves the sheet unset, so the client keeps its stats; a changed sheet comes through.
func TestAStatsChangedKeepsAnUnsetSheetUnset(t *testing.T) {
	if sc := statsChanged(world.StatsChanged{HP: 50, MaxHP: 135}); sc.Sheet != nil || sc.Hp != 50 || sc.MaxHp != 135 {
		t.Fatalf("got %+v, want HP with no sheet", sc)
	}
	sh := world.Sheet{Points: world.Points{Unspent: 4}}
	sh.Primary.STR = 11
	if sc := statsChanged(world.StatsChanged{Sheet: &sh, HP: 50, MaxHP: 208}); sc.Sheet == nil || sc.Sheet.Primary.Str != 11 || sc.Sheet.Points.Unspent != 4 {
		t.Fatalf("got %+v, want the sheet carried", sc)
	}
}

// The sheet says whether the free reset is left, not whether it is used.
func TestTheSheetSaysWhetherTheFreeResetIsLeft(t *testing.T) {
	for _, used := range []bool{false, true} {
		if got := sheet(world.Sheet{Points: world.Points{FreeResetUsed: used}}).Points.FreeResetLeft; got == used {
			t.Errorf("the reset used = %v went out as left = %v", used, got)
		}
	}
}

// A mob's pose comes through, so a player who joins sees a downed mob; a pose the wire doesn't know is standing.
func TestAMobCarriesItsPose(t *testing.T) {
	for pose, want := range map[world.MobPose]clientv1.MobPose{
		world.Standing:   clientv1.MobPose_MOB_POSE_UNSPECIFIED,
		world.Flinching:  clientv1.MobPose_MOB_POSE_FLINCH,
		world.Down:       clientv1.MobPose_MOB_POSE_DOWN,
		world.Rising:     clientv1.MobPose_MOB_POSE_RISING,
		world.MobPose(9): clientv1.MobPose_MOB_POSE_UNSPECIFIED,
	} {
		if s := snapshot(world.Snapshot{Mobs: []world.MobView{{ID: 4, Pose: pose}}}); len(s.Mobs) != 1 || s.Mobs[0].Pose != want {
			t.Errorf("pose %d: got %+v, want a mob in pose %v", pose, s.Mobs, want)
		}
	}
}

// Each cause of a teleport's cancel comes through as the wire's own; one it doesn't know is unspecified.
func TestATeleportCancelCarriesItsReason(t *testing.T) {
	for reason, want := range map[world.TeleportCancel]clientv1.TeleportCancelReason{
		world.TeleportMoved:     clientv1.TeleportCancelReason_TELEPORT_CANCEL_REASON_MOVED,
		world.TeleportActed:     clientv1.TeleportCancelReason_TELEPORT_CANCEL_REASON_ACTED,
		world.TeleportHit:       clientv1.TeleportCancelReason_TELEPORT_CANCEL_REASON_HIT,
		world.TeleportDied:      clientv1.TeleportCancelReason_TELEPORT_CANCEL_REASON_DIED,
		world.TeleportCancel(9): clientv1.TeleportCancelReason_TELEPORT_CANCEL_REASON_UNSPECIFIED,
	} {
		if got := teleportCancelled(world.TeleportCancelled{Reason: reason}); got.Reason != want {
			t.Errorf("reason %d: got %v, want %v", reason, got.Reason, want)
		}
	}
}

// A Snapshot carries the map's field boss while one is up, the beaten one with its time back while it's down, and neither on a map without one.
func TestASnapshotCarriesTheFieldBossUpOrDown(t *testing.T) {
	up := snapshot(world.Snapshot{FieldBoss: &world.FieldBoss{Mob: 9, Type: "king_slime"}})
	if up.FieldBoss == nil || up.FieldBoss.MobId != 9 || up.FieldBoss.Type != "king_slime" || up.FieldBossGone != nil {
		t.Fatalf("got %+v, want the king up", up)
	}
	down := snapshot(world.Snapshot{FieldBossGone: &world.FieldBossGone{Mob: 9, Type: "king_slime", BackIn: 3 * time.Minute}})
	if down.FieldBoss != nil || down.FieldBossGone == nil || down.FieldBossGone.MobId != 9 || down.FieldBossGone.BackInMs != 180000 {
		t.Fatalf("got %+v, want the king back in 3 minutes", down)
	}
	none := snapshot(world.Snapshot{MapID: "town"})
	if none.FieldBoss != nil || none.FieldBossGone != nil {
		t.Fatalf("got %+v, want no boss", none)
	}
}

// A trainer's promotion names each condition by its kind, as the client words it.
func TestEachPromotionConditionKeepsItsKind(t *testing.T) {
	d := npcDialog(world.NpcDialog{NPC: "trainer", Promotion: &world.PromotionOffer{Class: "knight", Conditions: []world.Condition{
		{Kind: world.ConditionLevel, Need: 10, Have: 3},
		{Kind: world.ConditionStat, ID: "str", Need: 20, Have: 12},
		{Kind: world.ConditionSkill, ID: "strike", Need: 5, Have: 5},
		{Kind: world.ConditionQuest, ID: "slime_cleanup", Need: 1, Have: 0},
	}}})
	if d.Promotion == nil || d.Promotion.Class != "knight" || len(d.Promotion.Conditions) != 4 {
		t.Fatalf("got %+v, want knight with four conditions", d.Promotion)
	}
	for i, want := range []clientv1.PromotionConditionKind{
		clientv1.PromotionConditionKind_PROMOTION_CONDITION_KIND_LEVEL, clientv1.PromotionConditionKind_PROMOTION_CONDITION_KIND_STAT,
		clientv1.PromotionConditionKind_PROMOTION_CONDITION_KIND_SKILL, clientv1.PromotionConditionKind_PROMOTION_CONDITION_KIND_QUEST,
	} {
		if got := d.Promotion.Conditions[i]; got.Kind != want {
			t.Errorf("condition %d is %v, want %v", i, got.Kind, want)
		}
	}
}

// Each loot kind, pouch kind, quest change, quest status and offer state reaches the client as its own; one the wire doesn't know reads as unspecified.
func TestEachWorldEnumKeepsItsName(t *testing.T) {
	for kind, want := range map[world.LootKind]clientv1.LootKind{world.LootTaken: clientv1.LootKind_LOOT_KIND_TAKEN, world.LootLeft: clientv1.LootKind_LOOT_KIND_LEFT, world.LootKind(9): 0} {
		if got := loot(world.Loot{Kind: kind}).Kind; got != want {
			t.Errorf("loot kind %d: %v, want %v", kind, got, want)
		}
	}
	for kind, want := range map[world.PouchKind]clientv1.PouchKind{world.PouchSmall: clientv1.PouchKind_POUCH_KIND_POUCH, world.PouchBox: clientv1.PouchKind_POUCH_KIND_BOX, world.PouchKind(9): 0} {
		if got := snapshot(world.Snapshot{Pouches: []world.PouchView{{Kind: kind}}}).Pouches[0].Kind; got != want {
			t.Errorf("pouch kind %d: %v, want %v", kind, got, want)
		}
	}
	for change, want := range map[world.QuestChange]clientv1.QuestChange{
		world.QuestProgressed: clientv1.QuestChange_QUEST_CHANGE_PROGRESSED, world.QuestAccepted: clientv1.QuestChange_QUEST_CHANGE_ACCEPTED,
		world.QuestHandedIn: clientv1.QuestChange_QUEST_CHANGE_HANDED_IN, world.QuestAbandoned: clientv1.QuestChange_QUEST_CHANGE_ABANDONED, world.QuestChange(9): 0,
	} {
		if got := questChanged(world.QuestChanged{Change: change}).Change; got != want {
			t.Errorf("quest change %d: %v, want %v", change, got, want)
		}
	}
	for status, want := range map[world.QuestStatus]clientv1.QuestStatus{
		world.QuestActive: clientv1.QuestStatus_QUEST_STATUS_ACTIVE, world.QuestFinished: clientv1.QuestStatus_QUEST_STATUS_FINISHED, world.QuestStatus(9): 0,
	} {
		if got := questChanged(world.QuestChanged{Quest: world.Quest{Status: status}}).Quest.Status; got != want {
			t.Errorf("quest status %d: %v, want %v", status, got, want)
		}
	}
	for state, want := range map[world.OfferState]clientv1.QuestOfferState{
		world.OfferAvailable: clientv1.QuestOfferState_QUEST_OFFER_STATE_AVAILABLE, world.OfferActive: clientv1.QuestOfferState_QUEST_OFFER_STATE_ACTIVE,
		world.OfferFinished: clientv1.QuestOfferState_QUEST_OFFER_STATE_FINISHED, world.OfferUnmet: clientv1.QuestOfferState_QUEST_OFFER_STATE_CONDITIONS_UNMET, world.OfferState(9): 0,
	} {
		d := npcDialog(world.NpcDialog{Gives: []world.QuestOffer{{Quest: "q", State: state}}})
		if len(d.Gives) != 1 || d.Gives[0].State != want {
			t.Errorf("offer state %d: %+v, want %v", state, d.Gives, want)
		}
	}
	for phase, want := range map[world.MatchPhase]clientv1.MatchPhase{
		world.PhasePrep: clientv1.MatchPhase_MATCH_PHASE_PREP, world.PhaseCountdown: clientv1.MatchPhase_MATCH_PHASE_COUNTDOWN, world.PhaseFight: clientv1.MatchPhase_MATCH_PHASE_FIGHT,
		world.PhaseBetween: clientv1.MatchPhase_MATCH_PHASE_BETWEEN, world.PhaseResult: clientv1.MatchPhase_MATCH_PHASE_RESULT,
	} {
		if got := matchState(world.MatchState{Phase: phase}).Phase; got != want {
			t.Errorf("match phase %d: %v, want %v", phase, got, want)
		}
	}
}

// A PiecesAdded tells of each piece with what gave it, by the item module's name for the cause; an unknown cause names none.
func TestPiecesAddedNamesEachPieceAndItsCause(t *testing.T) {
	for cause, want := range map[world.PieceCause]string{world.PieceFromQuest: "quest", world.PieceFromFeat: "feat", world.PieceCause(9): ""} {
		got := piecesAdded(world.PiecesAdded{Pieces: []string{"bronze_sword", "bronze_knife"}, Cause: cause})
		if len(got.Pieces) != 2 || got.Pieces[0].Id != "bronze_sword" || got.Pieces[1].Id != "bronze_knife" || got.Pieces[0].Cause != want || got.Pieces[1].Cause != want {
			t.Errorf("cause %d: %+v", cause, got)
		}
	}
}

// A retime carries each running cooldown and buff with its time left as it goes out, and an empty one says none run.
func TestARetimeCarriesEachTimersTimeLeft(t *testing.T) {
	soon := time.Now().Add(time.Hour)
	sc := statsChanged(world.StatsChanged{HP: 5, MaxHP: 9, Timers: &world.SkillTimers{
		Cooldowns: map[string]time.Time{"strike": soon, "gone": time.Now().Add(-time.Second)},
		Buffs:     map[string]world.BuffTimer{"war_cry": {Until: soon, Points: 3, Trait: "rally"}},
	}})
	ts := sc.GetTimers().GetTimers()
	if len(ts) != 2 || ts[0].Skill != "strike" || ts[0].Kind != clientv1.SkillTimerKind_SKILL_TIMER_KIND_COOLDOWN || ts[1].Skill != "war_cry" || ts[1].Kind != clientv1.SkillTimerKind_SKILL_TIMER_KIND_BUFF || ts[1].Points != 3 || ts[1].Trait != "rally" {
		t.Fatalf("pushed %+v", sc)
	}
	for _, tm := range ts {
		if left := time.Duration(tm.LeftMs) * time.Millisecond; left <= 59*time.Minute || left > time.Hour {
			t.Errorf("%s has %v left, want about an hour", tm.Skill, left)
		}
	}
	if none := statsChanged(world.StatsChanged{Timers: &world.SkillTimers{}}); none.Timers == nil || len(none.Timers.Timers) != 0 {
		t.Fatalf("an empty retime pushed %+v", none)
	}
	if plain := statsChanged(world.StatsChanged{HP: 5}); plain.Timers != nil {
		t.Fatal("a change of HP alone replaced the timers")
	}
}

// A frame and a detach never reach an outbox as themselves: the adapter turns the first into one Framed per recipient and the second into a join again, so neither has an encoder, and the Framed has the one.
func TestAFrameAndADetachHaveNoEncoderOfTheirOwn(t *testing.T) {
	e := transport.NewEncoders()
	if err := Encoders(e); err != nil {
		t.Fatal(err)
	}
	if err := transport.Encode(e, func(world.Frame) any { return nil }); err != nil {
		t.Errorf("a frame already has an encoder: %v", err)
	}
	if err := transport.Encode(e, func(world.Detached) any { return nil }); err != nil {
		t.Errorf("a detach already has an encoder: %v", err)
	}
	if err := transport.Encode(e, func(world.Framed) any { return nil }); err == nil {
		t.Error("a Framed has no encoder")
	}
}

// The causes a world event names a piece by are the item module's names for them: the handler may not import the item module, so the test holds them to it.
func TestThePieceCausesAreTheItemModulesNames(t *testing.T) {
	if pieceCauses[world.PieceFromQuest] != item.PieceFromQuest || pieceCauses[world.PieceFromFeat] != item.PieceFromFeat {
		t.Fatalf("the causes are %v, the item module's %q and %q", pieceCauses, item.PieceFromQuest, item.PieceFromFeat)
	}
}
