package e2e_test

import (
	"maps"
	"math"
	"slices"
	"testing"
	"time"

	clientv1 "finga/api/gen/client/v1"
)

// quickBoss: one-hit mushrooms that bite 1 px out, and after two kills a still, weak boss paying 1001 EXP and a boar tusk.
func quickBoss() []dataEdit {
	return []dataEdit{
		{"mobs.yaml", "    name: Orange Mushroom\n    level: 4\n    hp: 72\n", "    name: Orange Mushroom\n    level: 4\n    hp: 1\n"},
		{"mobs.yaml", "    attacks: [{use: charge}]\n    look: {sprite: snail, tint: 0xe08a3c}\n", "    attacks: [{use: bite, reach: 1}]\n    look: {sprite: snail, tint: 0xe08a3c}\n"},
		{"maps/forest.tmj", `{"name": "respawn_seconds", "type": "int", "value": 12}`, `{"name": "respawn_seconds", "type": "int", "value": 1}`},
		{"maps/forest.tmj", `{"name": "kills", "type": "int", "value": 300}`, `{"name": "kills", "type": "int", "value": 2}`},
		{"mobs.yaml", "    name: King Slime\n    boss: true\n    level: 6\n    hp: 4530\n    attack: 24\n", "    name: King Slime\n    boss: true\n    level: 6\n    hp: 40\n    attack: 1\n"},
		{"mobs.yaml", "    exp: 840\n    speed: 24\n", "    exp: 1001\n    speed: 0\n"},
		{"mobs.yaml", kingSlimeDrops, "      - {item: boar_tusk, chance: 1, min: 1, max: 1}\n"},
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

// beaten reports whether the looter has heard the boss fall.
func (l *looter) beaten(boss uint32) bool {
	return slices.ContainsFunc(l.bossesGone, func(g *clientv1.FieldBossGone) bool { return g.GetMobId() == boss && g.GetBeaten() })
}

// fightBoss has the fighters walk to the boss, jumping when stuck, and attack it every 850 ms until each hears it fall; everyone is read alongside, so no one's messages back up.
func fightBoss(t *testing.T, boss uint32, d time.Duration, fighters, everyone []*looter) {
	t.Helper()
	deadline, next := time.Now().Add(d), time.Now()
	walking := map[*looter]bool{}
	lastX, stuck := map[*looter]float64{}, map[*looter]int{}
	for slices.ContainsFunc(fighters, func(l *looter) bool { return !l.beaten(boss) }) {
		if time.Now().After(deadline) {
			t.Fatalf("the boss still stands after %s", d)
		}
		strike := !time.Now().Before(next)
		if strike {
			next = time.Now().Add(850 * time.Millisecond)
		}
		for _, l := range fighters {
			mv, ok := l.mobs[boss]
			if !ok {
				continue
			}
			var err error
			switch dx := mv.GetX() - l.x; {
			case math.Abs(dx) > 24:
				if math.Abs(l.x-lastX[l]) < 1 {
					stuck[l]++
				} else {
					stuck[l] = 0
				}
				lastX[l], walking[l] = l.x, true
				err = l.input(&clientv1.Input{Right: dx > 0, Left: dx < 0, Jump: stuck[l] >= 3})
			case walking[l]:
				walking[l] = false
				err = l.input(&clientv1.Input{})
			case strike:
				err = l.attack()
			}
			if err != nil {
				t.Fatal(err)
			}
		}
		for _, l := range everyone {
			l.pump(25*time.Millisecond, nil)
		}
	}
}

func TestFieldBoss(t *testing.T) {
	t.Parallel()
	s := newStackOver(t, gameData(t, quickBoss()...))
	// Ann and Ben hunt the mushrooms either side of the boss's spot, at x 1400, as a party, Ann on the map first; Cal waits in the town.
	var ls []*looter
	for _, c := range []struct {
		user, name, mapID string
		x                 float64
	}{{"ann", "Ann", "forest", 1200}, {"ben", "Ben", "forest", 1600}, {"cal", "Cal", "town", 500}} {
		token, id := s.createCharacter(t, c.user, c.name, "warrior")
		s.place(t, id, c.mapID, c.x, 608, 144, 0)
		ls = append(ls, newLooter(s.join(t, token, id)))
	}
	ann, ben, cal := ls[0], ls[1], ls[2]
	if r := ann.command(t, "inv-1", &clientv1.PartyInvite{CharacterId: ben.id}); !r.GetOk() {
		t.Fatalf("invite: %+v", r)
	}
	inv := take(t, ben, "PartyInvited", &ben.invites, 5*time.Second, nil)
	if r := ben.command(t, "acc-1", &clientv1.PartyRespond{InviteId: inv.GetInviteId(), Accept: true}); !r.GetOk() {
		t.Fatalf("accept the invite: %+v", r)
	}
	ann.partyState(t, 2)
	ben.partyState(t, 2)
	var boss uint32

	if !t.Run("C10/two-kills-raise-the-boss-and-every-player-hears-of-it", func(t *testing.T) {
		for deadline, next := time.Now().Add(30*time.Second), time.Now(); len(ann.fieldBosses) == 0; {
			if time.Now().After(deadline) {
				t.Fatal("no boss after 30 s of hunting")
			}
			if !time.Now().Before(next) {
				for _, l := range []*looter{ann, ben} {
					if err := l.swing(); err != nil {
						t.Fatal(err)
					}
				}
				next = time.Now().Add(850 * time.Millisecond)
			}
			for _, l := range ls {
				l.pump(25*time.Millisecond, nil)
			}
		}
		for _, l := range []*looter{ann, ben} {
			fb := take(t, l, "FieldBoss", &l.fieldBosses, 5*time.Second, nil)
			if fb.GetType() != "king_slime" || fb.GetLeavesInMs() != 600_000 {
				t.Fatalf("%s sees the boss %+v", l.id, fb)
			}
			boss = fb.GetMobId()
		}
		for _, l := range ls {
			a := take(t, l, "Announcement", &l.announcements, 5*time.Second, nil)
			if b := a.GetBossAppeared(); b.GetBoss() != "king_slime" || b.GetMapId() != "forest" {
				t.Fatalf("%s heard %+v", l.id, a)
			}
		}
	}) {
		return
	}

	t.Run("C10/a-party-beats-it-and-each-member-gets-their-share-and-their-own-drop", func(t *testing.T) {
		fightBoss(t, boss, 60*time.Second, []*looter{ann, ben}, ls)
		// 1001 EXP for all its HP, 10% more for the second sharer, 1101 in two: Ann, first on the map, takes the odd one.
		for l, want := range map[*looter]int64{ann: 551, ben: 550} {
			take(t, l, "the boss's ExpGained", &l.exp, 5*time.Second, func(e *clientv1.ExpGained) bool { return e.GetAmount() == want })
		}
		tuskOwners := func() map[string]bool {
			owners := map[string]bool{}
			for _, d := range ann.seen {
				if d.GetTemplate() == "boar_tusk" {
					owners[d.GetOwnerId()] = true
				}
			}
			return owners
		}
		want := map[string]bool{ann.id: true, ben.id: true}
		if !ann.until(5*time.Second, func() bool { return maps.Equal(tuskOwners(), want) }) {
			t.Fatalf("the tusks are %v's, want one each", tuskOwners())
		}
		for _, l := range []*looter{ann, ben} {
			take(t, l, "Loot of its own tusk", &l.loots, 10*time.Second, func(lt *clientv1.Loot) bool {
				return lt.GetTemplate() == "boar_tusk" && lt.GetKind() == clientv1.LootKind_LOOT_KIND_COLLECTED && lt.GetOwnerId() == l.id
			})
		}
		if len(cal.fieldBosses) != 0 || len(cal.bossesGone) != 0 {
			t.Fatalf("Cal, in the town, saw the forest's boss: %v, %v", cal.fieldBosses, cal.bossesGone)
		}
	})
}
