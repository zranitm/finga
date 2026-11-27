package world

import (
	"reflect"
	"testing"
	"time"

	clientv1 "finga/api/gen/client/v1"
	"finga/internal/application/world"
	"finga/internal/shared/kernel"
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

// match is a Match with its maps made, so a row adds to them.
func match() copytest.Match {
	return copytest.Match{Renames: map[string]string{}, Skip: map[string]string{}, Computed: map[string]string{}, Enums: worldEnums}
}

// with adds renames, skips or computed paths to a map of a Match.
func with(m map[string]string, add map[string]string) map[string]string {
	for k, v := range add {
		m[k] = v
	}
	return m
}

// sheetMatch is how a world sheet reaches the client: the invested skill points renamed, the free reset turned to whether it is left.
func sheetMatch(m copytest.Match, from, to string) copytest.Match {
	m.Renames[from+".Skills.Points"] = to + ".Skills.Invested"
	m.Skip[from+".Points.FreeResetUsed"] = "the sheet says whether the reset is left: TestTheSheetSaysWhetherTheFreeResetIsLeft"
	m.Computed[to+".Points.FreeResetLeft"] = "FreeResetUsed negated: TestTheSheetSaysWhetherTheFreeResetIsLeft"
	return m
}

// Each world event reaches the client as its message, with every value the world sent where the wire's own name for it says, but those the wire leaves out.
func TestEveryWorldEventKeepsItsValues(t *testing.T) {
	const ref = "a ref names a player or a mob, and the wire one string for either: TestADamageRefNamesItsMob"
	const lock = "a lock names a player or a mob, or none: TestASkillsLockNamesItsMobOrNothing"
	const enum = "the enum's own map: TestEachWorldEnumKeepsItsName"
	levelUp := func() copytest.Match {
		m := sheetMatch(match(), "Sheet", "Character.Sheet")
		for _, f := range []string{"ID", "Name", "Class", "Level", "Exp", "Stats", "HP", "MaxHP"} {
			m.Renames[f] = "Character." + f
		}
		m.Renames["ID"] = "Character.Id"
		m.Renames["MaxHP"] = "Character.MaxHp"
		m.Renames["Stats"] = "Character.Stats"
		m.Computed["Character.Energy"] = "a level-up's energy comes with the frames"
		return m
	}
	for name, diff := range map[string]func() []string{
		"Snapshot": func() []string {
			m := match()
			with(m.Skip, map[string]string{"MapID": "the join or the teleport names the map", "Players.Look.Revision": "the revision stays on the server", "Players.State.Move": "a player's move: TestAPlayerKeepsItsMove"})
			with(m.Renames, map[string]string{"Players.State": "Players", "FieldBoss.Mob": "FieldBoss.MobId", "FieldBossGone.Mob": "FieldBossGone.MobId", "FieldBossGone.BackIn": "FieldBossGone.BackInMs", "Pouches.GoneIn": "Pouches.GoneInMs"})
			return copytest.Diff(m, snapshot)
		},
		"Teleport":          func() []string { return copytest.Diff(match(), teleport) },
		"TeleportCancelled": func() []string { m := match(); m.Skip["Reason"] = enum; m.Computed["Reason"] = enum; return copytest.Diff(m, teleportCancelled) },
	} {
		if got := diff(); len(got) > 0 {
			t.Errorf("%s: %v", name, got)
		}
	}
	_ = []any{ref, lock, levelUp, kernel.CharacterID{}, time.Second, proto.Equal}
	_ = clientv1.Damage{}
}
