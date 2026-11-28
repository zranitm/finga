package e2etest

import (
	"fmt"
	"slices"
	"testing"

	"finga/internal/shared/gamedata"
	"finga/internal/shared/gamedata/gamedatatest"
)

// Layout is where the rows stand on Willowmere and its places, in metres, read from the game data: the stand-in the stacks run on, or the real region for a smoke test.
type Layout struct {
	Slimes        gamedata.Spawn // Sunmeadow's slimes' camp
	SlimeField    gamedata.Point // the camp's west edge, which its slimes see
	AmongSlimes   gamedata.Point // the camp's middle, where the slimes are on a player at once
	OutOfSight    gamedata.Point // west of the camp, farther than any mob sees
	ClearOfSlimes gamedata.Point // farther west, out of the slimes' sight, even of those that chase a player there till they let them go
	SlimesGate    gamedata.Point // where GateByTheSlimes stands the cave's gate
	FieldGuide    gamedata.Point // where guideByTheSlimes stands the guide
	InView        gamedata.Point // between the slimes' camp and the mushrooms', where one block of interest cells holds both
	ByTheTrainer  gamedata.Point // in talking reach of the Warrior Trainer
	ByTheGuide    gamedata.Point // in talking reach of the guide where the plan stands it
	Start         gamedata.Point // where a new character appears
	CaveGate      gamedata.Point // Willowmere's gate to Greyfang Cave
	CaveMouth     gamedata.Point // the cave's gate back, where a hop lands
	NorthPass     gamedata.Point // Willowmere's gate to Sunreach
	SouthGate     gamedata.Point // Sunreach's gate back, where a hop lands
	Boss          gamedata.Point // Sunmeadow's boss spot
	FarBats       gamedata.Point // 2 m west of the cave's bats' camp, out of the boars' sight
}

// Spots is the Layout of the stand-in data the stacks run on.
func Spots(t testing.TB) Layout {
	t.Helper()
	l, err := LayoutOf(gamedatatest.Shipped(t))
	if err != nil {
		t.Fatal(err)
	}
	return l
}

// LayoutOf is the Layout of data's Willowmere, Greyfang Cave and Sunreach.
func LayoutOf(data *gamedata.Data) (Layout, error) {
	var l Layout
	region, ok := data.Map("willowmere")
	cave, okCave := data.Map("greyfang_cave")
	sunreach, okSunreach := data.Map("sunreach")
	if !ok || !okCave || !okSunreach {
		return l, fmt.Errorf("no willowmere, greyfang_cave or sunreach map")
	}
	slimes := slices.IndexFunc(region.Spawns, func(s gamedata.Spawn) bool { return s.Mob == "green_slime" })
	caps := slices.IndexFunc(region.Spawns, func(s gamedata.Spawn) bool { return s.Mob == "orange_mushroom" })
	if slimes < 0 || caps < 0 {
		return l, fmt.Errorf("willowmere has no slimes' or mushrooms' camp")
	}
	l.Slimes = region.Spawns[slimes]
	c, edge := l.Slimes.At, l.Slimes.At.X-l.Slimes.Radius
	l.SlimeField = gamedata.Point{X: edge - 0.5, Z: c.Z}
	l.AmongSlimes = c
	l.OutOfSight = gamedata.Point{X: edge - 17, Z: c.Z}
	l.ClearOfSlimes = gamedata.Point{X: edge - 32, Z: c.Z}
	l.SlimesGate = gamedata.Point{X: edge - 16, Z: c.Z}
	l.FieldGuide = gamedata.Point{X: edge - 14, Z: c.Z}
	m := region.Spawns[caps].At
	l.InView = gamedata.Point{X: (c.X + m.X) / 2, Z: (c.Z + m.Z) / 2}
	trainer, ok := data.NPC("warrior_trainer")
	guide, okGuide := data.NPC("town_guide")
	if !ok || !okGuide {
		return l, fmt.Errorf("no warrior_trainer or town_guide")
	}
	l.ByTheTrainer = gamedata.Point{X: trainer.At.X + 0.5, Z: trainer.At.Z - 1}
	l.ByTheGuide = gamedata.Point{X: guide.At.X - 0.5, Z: guide.At.Z - 0.5}
	l.Start = region.PlayerSpawn
	var err error
	if l.CaveGate, l.CaveMouth, err = gatePair(region, cave); err != nil {
		return l, err
	}
	if l.NorthPass, l.SouthGate, err = gatePair(region, sunreach); err != nil {
		return l, err
	}
	for _, a := range region.Areas {
		if a.ID == "sunmeadow" && a.Boss != nil {
			l.Boss = a.Boss.At
		}
	}
	if l.Boss == (gamedata.Point{}) {
		return l, fmt.Errorf("sunmeadow has no boss")
	}
	bats := slices.IndexFunc(cave.Spawns, func(s gamedata.Spawn) bool { return s.Mob == "cave_bat" })
	if bats < 0 {
		return l, fmt.Errorf("greyfang_cave has no bats' camp")
	}
	b := cave.Spawns[bats]
	l.FarBats = gamedata.Point{X: b.At.X - b.Radius - 2, Z: b.At.Z}
	return l, nil
}

// gatePair is the middle of from's portal to to, and of to's portal back to from.
func gatePair(from, to *gamedata.Map) (there, back gamedata.Point, err error) {
	out := slices.IndexFunc(from.Portals, func(p gamedata.Portal) bool { return p.TargetMap == to.ID })
	in := slices.IndexFunc(to.Portals, func(p gamedata.Portal) bool { return p.TargetMap == from.ID })
	if out < 0 || in < 0 {
		return there, back, fmt.Errorf("%s and %s have no portals to each other", from.ID, to.ID)
	}
	return from.Portals[out].At, to.Portals[in].At, nil
}
