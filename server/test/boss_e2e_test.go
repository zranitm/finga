package e2e_test

import (
	"maps"
	"math"
	"slices"
	"testing"
	"time"

	clientv1 "finga/api/gen/client/v1"
	"finga/internal/shared/gamedata"
	"finga/internal/shared/gamedata/load"
	"finga/test/e2etest"
)

// quickBoss: one-hit mushrooms that bite 0.03 m out, and after two kills a still, weak boss paying 1001 EXP and a boar tusk.
func quickBoss() []e2etest.DataEdit {
	return []e2etest.DataEdit{
		{File: "mobs.yaml", Old: "    name: Orange Mushroom\n    level: 4\n    hp: 72\n", New: "    name: Orange Mushroom\n    level: 4\n    hp: 1\n"},
		{File: "mobs.yaml", Old: "    attacks: [{use: charge}]\n    look: {model: mushnub, tint: 0xe08a3c}\n", New: "    attacks: [{use: bite, reach: 0.03}]\n    look: {model: mushnub, tint: 0xe08a3c}\n"},
		{File: "maps/forest.yaml", Old: "mob: orange_mushroom, max: 4, respawn: 12s}", New: "mob: orange_mushroom, max: 4, respawn: 1s}"},
		{File: "maps/forest.yaml", Old: "mob: king_slime, kills: 300}", New: "mob: king_slime, kills: 2}"},
		{File: "mobs.yaml", Old: "    name: King Slime\n    boss: true\n    level: 6\n    hp: 4530\n    attack: 24\n", New: "    name: King Slime\n    boss: true\n    level: 6\n    hp: 40\n    attack: 1\n"},
		{File: "mobs.yaml", Old: "    exp: 840\n    speed: 0.75\n", New: "    exp: 1001\n    speed: 0\n"},
		{File: "mobs.yaml", Old: kingSlimeDrops, New: "      - {item: boar_tusk, chance: 1, min: 1, max: 1}\n"},
	}
}

const kingSlimeDrops = `      - {item: red_potion, chance: 1, min: 2, max: 4}
      - {item: leather_cap, chance: 0.75, min: 1, max: 1}
      - {item: cloth_trousers, chance: 0.6, min: 1, max: 1}
      - {item: leather_shoes, chance: 0.6, min: 1, max: 1}
      - {item: work_gloves, chance: 0.45, min: 1, max: 1}
      - {item: wooden_pendant, chance: 0.3, min: 1, max: 1}
      - {item: copper_ring, chance: 0.3, min: 1, max: 1}
`

// fightBoss has the fighters walk up to the boss's side, within reach metres of its middle across the ground, and attack it until each hears it fall.
func fightBoss(t *testing.T, boss uint32, reach float64, d time.Duration, fighters, everyone []*e2etest.Looter) {
	t.Helper()
	deadline, next := time.Now().Add(d), time.Now()
	walking := map[*e2etest.Looter]bool{}
	last, stuck := map[*e2etest.Looter]gamedata.Point{}, map[*e2etest.Looter]int{}
	for slices.ContainsFunc(fighters, func(l *e2etest.Looter) bool { return !l.Beaten(boss) }) {
		if time.Now().After(deadline) {
			t.Fatalf("the boss still stands after %s", d)
		}
		strike := !time.Now().Before(next)
		if strike {
			next = time.Now().Add(850 * time.Millisecond)
		}
		for _, l := range fighters {
			mv, ok := l.Mobs[boss]
			if !ok {
				continue
			}
			var err error
			switch dx, dz := mv.GetX()-l.X, mv.GetZ()-l.Z; {
			case math.Hypot(dx, dz) > reach:
				if math.Hypot(l.X-last[l].X, l.Z-last[l].Z) < 0.03 {
					stuck[l]++
				} else {
					stuck[l] = 0
				}
				last[l], walking[l] = gamedata.Point{X: l.X, Z: l.Z}, true
				in := e2etest.Toward(dx, dz)
				in.Jump = stuck[l] >= 3
				err = l.Input(in)
			case walking[l]:
				walking[l] = false
				err = l.Input(&clientv1.Input{})
			case strike:
				err = l.Attack(l.AimAt(mv))
			}
			if err != nil {
				t.Fatal(err)
			}
		}
		for _, l := range everyone {
			l.Pump(25*time.Millisecond, nil)
		}
	}
}

func TestFieldBoss(t *testing.T) {
	t.Parallel()
	s := newStackOver(t, e2etest.GameData(t, quickBoss()...))
	data, err := load.Dir(s.Data)
	if err != nil {
		t.Fatal(err)
	}
	king, _ := data.Mob("king_slime")
	reach := king.Radius + data.Movement().Radius + 0.125 // a fighter blocked at its side is within reach
	// Ann and Ben hunt the mushrooms either side of their circle, south of the boss's spot at 58, 20, as a party, Ann on the map first; Cal waits in the town.
	var ls []*e2etest.Looter
	for _, c := range []struct {
		user, name, mapID string
		at                gamedata.Point
	}{{"ann", "Ann", "forest", gamedata.Point{X: 52, Z: 14}}, {"ben", "Ben", "forest", gamedata.Point{X: 60, Z: 14}}, {"cal", "Cal", "town", gamedata.Point{X: 20, Z: 10}}} {
		token, id := s.CreateCharacter(t, c.user, c.name, "warrior")
		s.Place(t, id, c.mapID, c.at, 144, 0)
		ls = append(ls, e2etest.NewLooter(s.Join(t, token, id)))
	}
	ann, ben, cal := ls[0], ls[1], ls[2]
	if r := ann.Command(t, "inv-1", &clientv1.PartyInvite{CharacterId: ben.ID}); !r.GetOk() {
		t.Fatalf("invite: %+v", r)
	}
	inv := e2etest.Take(t, ben, "PartyInvited", &ben.Invites, 5*time.Second, nil)
	if r := ben.Command(t, "acc-1", &clientv1.PartyRespond{InviteId: inv.GetInviteId(), Accept: true}); !r.GetOk() {
		t.Fatalf("accept the invite: %+v", r)
	}
	ann.PartyState(t, 2)
	ben.PartyState(t, 2)
	var boss uint32

	if !t.Run("C10/two-kills-raise-the-boss-and-every-player-hears-of-it", func(t *testing.T) {
		for deadline, next := time.Now().Add(30*time.Second), time.Now(); len(ann.FieldBosses) == 0; {
			if time.Now().After(deadline) {
				t.Fatal("no boss after 30 s of hunting")
			}
			if !time.Now().Before(next) {
				for _, l := range []*e2etest.Looter{ann, ben} {
					if err := l.Swing(); err != nil {
						t.Fatal(err)
					}
				}
				next = time.Now().Add(850 * time.Millisecond)
			}
			for _, l := range ls {
				l.Pump(25*time.Millisecond, nil)
			}
		}
		for _, l := range []*e2etest.Looter{ann, ben} {
			fb := e2etest.Take(t, l, "FieldBoss", &l.FieldBosses, 5*time.Second, nil)
			if fb.GetType() != "king_slime" || fb.GetLeavesInMs() != 600_000 {
				t.Fatalf("%s sees the boss %+v", l.ID, fb)
			}
			boss = fb.GetMobId()
		}
		for _, l := range ls {
			a := e2etest.Take(t, l, "Announcement", &l.Announcements, 5*time.Second, nil)
			if b := a.GetBossAppeared(); b.GetBoss() != "king_slime" || b.GetMapId() != "forest" {
				t.Fatalf("%s heard %+v", l.ID, a)
			}
		}
	}) {
		return
	}

	t.Run("C10/a-party-beats-it-and-each-member-gets-their-share-and-their-own-drop", func(t *testing.T) {
		fightBoss(t, boss, reach, 60*time.Second, []*e2etest.Looter{ann, ben}, ls)
		// 1001 EXP for all its HP, 10% more for the second sharer, 1101 in two: Ann, first on the map, takes the odd one.
		for l, want := range map[*e2etest.Looter]int64{ann: 551, ben: 550} {
			e2etest.Take(t, l, "the boss's ExpGained", &l.Exp, 5*time.Second, func(e *clientv1.ExpGained) bool { return e.GetAmount() == want })
		}
		tuskOwners := func() map[string]bool {
			owners := map[string]bool{}
			for _, d := range ann.Seen {
				if d.GetTemplate() == "boar_tusk" {
					owners[d.GetOwnerId()] = true
				}
			}
			return owners
		}
		want := map[string]bool{ann.ID: true, ben.ID: true}
		if !ann.Until(5*time.Second, func() bool { return maps.Equal(tuskOwners(), want) }) {
			t.Fatalf("the tusks are %v's, want one each", tuskOwners())
		}
		for _, l := range []*e2etest.Looter{ann, ben} {
			e2etest.Take(t, l, "Loot of its own tusk", &l.Loots, 10*time.Second, func(lt *clientv1.Loot) bool {
				return lt.GetTemplate() == "boar_tusk" && lt.GetKind() == clientv1.LootKind_LOOT_KIND_COLLECTED && lt.GetOwnerId() == l.ID
			})
		}
		if len(cal.FieldBosses) != 0 || len(cal.BossesGone) != 0 {
			t.Fatalf("Cal, in the town, saw the forest's boss: %v, %v", cal.FieldBosses, cal.BossesGone)
		}
	})
}
