package instance_test

import (
	"fmt"
	"math"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"finga/internal/gamedata"
	"finga/internal/kernel"
	"finga/internal/progression"
	"finga/internal/sim"
	"finga/services/world/internal/application/instance"
	"finga/services/world/internal/model"
	"finga/services/world/worldtest"
)

// bossData is the shipped game data with King Slime standing still, its HP set and one leather cap its only drop, under the field boss rules given.
func bossData(t *testing.T, hp int, life string, share float64) *gamedata.Data {
	t.Helper()
	return worldtest.LoadDataWith(t, map[string]func(string) string{"mobs.yaml": func(s string) string {
		start := strings.Index(s, "  - id: king_slime\n")
		if start < 0 {
			t.Fatal("no king_slime in mobs.yaml")
		}
		end := len(s)
		if next := strings.Index(s[start+1:], "\n  - id: "); next >= 0 {
			end = start + 1 + next + 1
		}
		king := fmt.Sprintf("  - {id: king_slime, name: King Slime, boss: true, level: 6, hp: %d, attack: 24, damage: physical, physical_defense: 8, magic_defense: 5, accuracy: 32, evasion: 4, exp: 840, speed: 0, width: 96, height: 84, behavior: patrol, look: {sprite: slime}, drops: [{item: leather_cap, chance: 1, min: 1, max: 1}]}\n", hp)
		s = s[:start] + king + s[end:]
		s = regexp.MustCompile(`(?m)^  life: .*$`).ReplaceAllString(s, "  life: "+life)
		return regexp.MustCompile(`(?m)^  drop_share: .*$`).ReplaceAllString(s, fmt.Sprintf("  drop_share: %g", share))
	}})
}

// bossArena is the arena with King Slime's area on the floor east of the slime, which comes back two ticks after a kill: the king stands at x 500, its box from 452 to 548, where a warrior at 430 reaches it untouched.
func bossArena(kills int) *gamedata.Map {
	m := arena(2 * worldtest.TickInterval)
	m.Boss = &gamedata.BossSpot{Mob: "king_slime", Kills: kills, Area: sim.Rect{X: 400, Y: 480, W: 200, H: 128}}
	return m
}

const atKing = 430.0

// armed is a warrior at x swinging a sword of the given attack: 200 kills a slime or a weak king in one blow.
func armed(name string, x float64, attack int) model.Player {
	p := worldtest.NewPlayer(name, x, 608)
	p.Worn = model.Worn{Revision: 1, Items: []model.WornItem{{Template: "wooden_sword", Stats: progression.ItemStats{PhysicalAttack: attack}}}}
	return p
}

// runBossArena runs m at the fixed roll, its drops granted by the fake item service, with o's parties and notices.
func runBossArena(t *testing.T, data *gamedata.Data, m *gamedata.Map, roll float64, o worldtest.Options) (*instance.Instance, func()) {
	t.Helper()
	inst, stop := worldtest.RunMap(t, data, m, nil, worldtest.Options{NewRoll: worldtest.Rolls(roll), Items: newFakeItems(), Parties: o.Parties, Notices: o.Notices})
	settle(worldtest.TickInterval)
	return inst, stop
}

// notices records what a map announces.
type notices struct {
	mu  sync.Mutex
	got []model.Announcement
}

func (n *notices) Announce(a model.Announcement) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.got = append(n.got, a)
}

func (n *notices) heard() []model.Announcement {
	n.mu.Lock()
	defer n.mu.Unlock()
	return slices.Clone(n.got)
}

// swing attacks once and waits out the warrior's 900 ms attack.
func swing(inst *instance.Instance, id kernel.CharacterID) {
	_ = inst.Attack(id)
	settle(19 * worldtest.TickInterval)
}

func bossUps(s *worldtest.Sink) []model.FieldBoss       { return worldtest.Of[model.FieldBoss](s) }
func bossGones(s *worldtest.Sink) []model.FieldBossGone { return worldtest.Of[model.FieldBossGone](s) }

// untilTick lets the map run until s has the frame of tick n.
func untilTick(s *worldtest.Sink, n uint64) {
	for s.Last().Tick < n {
		settle(worldtest.TickInterval)
	}
}

// expAfterBoss is the EXP s was given once the boss fell.
func expAfterBoss(s *worldtest.Sink) int64 {
	var total int64
	fell := false
	for _, e := range s.All() {
		switch ev := e.(type) {
		case model.FieldBossGone:
			fell = fell || ev.Beaten
		case model.ExpGained:
			if fell {
				total += ev.Amount
			}
		}
	}
	return total
}

// dealt is the HP each player took from mob, in the order s saw the blows, overkill left out.
func dealt(s *worldtest.Sink, mob uint64, hp int) map[kernel.CharacterID]int {
	out := map[kernel.CharacterID]int{}
	for _, d := range worldtest.Of[model.Damage](s) {
		if d.Target.Mob != mob || !d.Target.IsMob() || d.Miss {
			continue
		}
		took := min(d.Amount, hp)
		hp -= took
		out[d.Source.Player] += took
	}
	return out
}

// dropsOf is every drop s saw fall of template, by owner, collected or not.
func dropsOf(s *worldtest.Sink, template string) map[kernel.CharacterID]int {
	out := map[kernel.CharacterID]int{}
	for _, d := range s.Deltas() {
		for _, v := range d.Drops {
			if v.Template == template {
				out[v.Owner]++
			}
		}
	}
	return out
}

func bossExp(dealt, hp int) int64 { return int64(math.Round(840 * float64(dealt) / float64(hp))) }

// Kills on the map raise its boss once, at the end of the tick the count reaches its number; kills while it is up and its own don't count, and the count starts again from nothing.
func TestKillsRaiseTheMapsBossOnceAtItsNumber(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		news := &notices{}
		inst, stop := runBossArena(t, bossData(t, 100, "10m", 0.01), bossArena(2), 0.5, worldtest.Options{Notices: news})
		defer stop()
		hunter, slayer := armed("Hunter", 250, 200), armed("Slayer", atKing, 5000)
		s := join(t, inst, hunter)
		join(t, inst, slayer)
		swing(inst, hunter.ID)
		if len(bossUps(s)) != 0 {
			t.Fatal("the king came after one kill of two")
		}
		swing(inst, hunter.ID)
		ups := bossUps(s)
		if len(ups) != 1 || ups[0].Type != "king_slime" || ups[0].LeavesIn != 10*time.Minute {
			t.Fatalf("raised %+v; want the king, leaving in 10 minutes", ups)
		}
		kill := worldtest.Of[model.Damage](s)[1]
		king, ok := mobsIn(s)[ups[0].Mob]
		if ups[0].Tick != kill.Tick || !ok || king.X != 500 || king.Y != 608 || king.HP != 100 {
			t.Fatalf("the king %+v rose at tick %d, the second kill at %d; want it at its spot, in that tick's frame", king, ups[0].Tick, kill.Tick)
		}
		if got := news.heard(); len(got) != 1 || got[0] != (model.BossAppeared{Boss: "king_slime", Map: "arena"}) {
			t.Fatalf("announced %+v; want the king appearing in the arena, to everyone online", got)
		}
		swing(inst, hunter.ID)
		swing(inst, hunter.ID)
		if len(bossUps(s)) != 1 {
			t.Fatal("kills while the king was up raised another")
		}
		swing(inst, slayer.ID)
		if g := bossGones(s); len(g) != 1 || g[0].Mob != ups[0].Mob || !g[0].Beaten {
			t.Fatalf("gone %+v; want the king beaten", g)
		}
		swing(inst, hunter.ID)
		if len(bossUps(s)) != 1 {
			t.Fatal("the king came back after one kill: kills while it was up, or its own, counted")
		}
		swing(inst, hunter.ID)
		if ups := bossUps(s); len(ups) != 2 || ups[1].Mob == ups[0].Mob {
			t.Fatalf("raised %+v; want a new king after two more kills", ups)
		}
		if len(news.heard()) != 2 {
			t.Fatalf("announced %+v; want each king announced once", news.heard())
		}
	})
}

// An unbeaten boss leaves at its time, with nothing paid; one killed in its last tick is beaten.
func TestAnUnbeatenBossLeavesAtItsTime(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		inst, stop := runBossArena(t, bossData(t, 100, "2s", 0.01), bossArena(1), 0.5, worldtest.Options{})
		defer stop()
		hunter, slayer, poker := armed("Hunter", 250, 200), armed("Slayer", atKing, 5000), armed("Poker", atKing, 10)
		s := join(t, inst, hunter)
		join(t, inst, slayer)
		ps := join(t, inst, poker)
		swing(inst, hunter.ID)
		up := bossUps(s)[0]
		swing(inst, poker.ID)
		untilTick(s, up.Tick+40)
		g := bossGones(s)
		if len(g) != 1 || g[0].Beaten || g[0].Tick != up.Tick+40 || !slices.Contains(s.Last().RemovedMobs, up.Mob) {
			t.Fatalf("gone %+v, removed %v; want the king to leave unbeaten 2 s after it rose, at tick %d", g, s.Last().RemovedMobs, up.Tick+40)
		}
		if expAfterBoss(ps) != 0 || len(dropsOf(ps, "leather_cap")) != 0 {
			t.Fatal("a king that left paid its attacker")
		}
		swing(inst, hunter.ID)
		up = bossUps(s)[1]
		untilTick(s, up.Tick+39)
		_ = inst.Attack(slayer.ID) // lands in the king's last tick
		settle(2 * worldtest.TickInterval)
		if g := bossGones(s); len(g) != 2 || !g[1].Beaten || g[1].Tick != up.Tick+40 {
			t.Fatalf("gone %+v; want the king killed in its last tick beaten", g)
		}
	})
}

// A beaten boss pays each player alone by the HP they took from it: EXP by their share, and a drop roll of their own once their share reaches the drop share; a bystander gets nothing.
func TestABeatenBossPaysEachPlayerByTheHPTheyTook(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		const hp = 400
		inst, stop := runBossArena(t, bossData(t, hp, "10m", 0.3), bossArena(1), 0.5, worldtest.Options{})
		defer stop()
		hunter, ayla, robin := armed("Hunter", 250, 200), armed("Ayla", atKing, 40), armed("Robin", atKing, 80)
		s := join(t, inst, hunter)
		sa, sr := join(t, inst, ayla), join(t, inst, robin)
		swing(inst, hunter.ID)
		king := bossUps(s)[0].Mob
		swing(inst, ayla.ID)
		for range 40 {
			if len(bossGones(s)) > 0 {
				break
			}
			swing(inst, robin.ID)
		}
		if g := bossGones(s); len(g) != 1 || !g[0].Beaten {
			t.Fatalf("gone %+v; want the king beaten", g)
		}
		took := dealt(s, king, hp)
		if took[ayla.ID]+took[robin.ID] != hp || took[ayla.ID] == 0 || 10*took[ayla.ID] >= 3*hp {
			t.Fatalf("took %v; want Ayla under 30%% of the %d HP and Robin the rest", took, hp)
		}
		if got, want := expAfterBoss(sa), bossExp(took[ayla.ID], hp); got != want {
			t.Errorf("Ayla got %d EXP, want %d", got, want)
		}
		if got, want := expAfterBoss(sr), bossExp(took[robin.ID], hp); got != want {
			t.Errorf("Robin got %d EXP, want %d", got, want)
		}
		if got := expAfterBoss(s); got != 0 {
			t.Errorf("the hunter, who never hit the king, got %d EXP", got)
		}
		if caps := dropsOf(s, "leather_cap"); len(caps) != 1 || caps[robin.ID] != 1 {
			t.Fatalf("caps by owner %v; want Robin's roll alone, Ayla being under the drop share", caps)
		}
	})
}

// A party's damage pools, its members' gone or dead included, and its members alive on the map split the group's EXP with the party bonus, each rolling drops of their own. A member dead when it falls, one on another map and one who left collect nothing.
func TestAPartyPoolsItsDamageAndOnlyThePresentCollect(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		const hp = 400
		ayla, bram, cora, eli, lone := armed("Ayla", atKing, 40), armed("Bram", 250, 200), armed("Cora", 300, 10), armed("Eli", atKing, 40), armed("Lone", atKing, 5000)
		cora.HP = 1 // standing in the slime: dies on her first tick
		away := kernel.NewCharacterID()
		parties := &fakeParties{parties: [][]kernel.CharacterID{{bram.ID, ayla.ID, cora.ID, away, eli.ID}}}
		inst, stop := runBossArena(t, bossData(t, hp, "10m", 0.01), bossArena(1), 0.5, worldtest.Options{Parties: parties})
		defer stop()
		sa, sb, se, sl := join(t, inst, ayla), join(t, inst, bram), join(t, inst, eli), join(t, inst, lone)
		swing(inst, bram.ID)
		king := bossUps(sb)[0].Mob
		swing(inst, ayla.ID)
		swing(inst, eli.ID)
		_ = inst.Leave(eli.ID, "", nil)
		sc := join(t, inst, cora)
		swing(inst, lone.ID) // the last blow, within Cora's 3 s dead
		took := dealt(sb, king, hp)
		if g := bossGones(sb); len(g) != 1 || !g[0].Beaten || took[ayla.ID] == 0 || took[eli.ID] == 0 || took[lone.ID] == 0 || len(worldtest.Of[model.Died](sb)) != 1 {
			t.Fatalf("gone %+v, took %v; want Ayla, Eli and Lone to have beaten the king, Cora dead", g, took)
		}
		group := bossExp(took[ayla.ID]+took[eli.ID], hp) * 11 / 10
		if got, want := expAfterBoss(sa), group-group/2; got != want {
			t.Errorf("Ayla got %d EXP, want %d: half the party's share with its bonus, and what doesn't divide", got, want)
		}
		if got, want := expAfterBoss(sb), group/2; got != want {
			t.Errorf("Bram got %d EXP, want %d: half the party's share though he never hit it", got, want)
		}
		if got, want := expAfterBoss(sl), bossExp(took[lone.ID], hp); got != want {
			t.Errorf("Lone got %d EXP, want %d", got, want)
		}
		if expAfterBoss(sc) != 0 || expAfterBoss(se) != 0 {
			t.Error("the dead member or the one who left got EXP")
		}
		if caps := dropsOf(sb, "leather_cap"); len(caps) != 3 || caps[ayla.ID] != 1 || caps[bram.ID] != 1 || caps[lone.ID] != 1 {
			t.Fatalf("caps by owner %v; want one each for Ayla, Bram and Lone", caps)
		}
	})
}

// A player joining while the boss is up finds it in their Snapshot, with the time it has left.
func TestAPlayerJoiningMidFightSeesTheBoss(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		inst, stop := runBossArena(t, bossData(t, 100, "10m", 0.01), bossArena(1), 0.5, worldtest.Options{})
		defer stop()
		hunter := armed("Hunter", 250, 200)
		s := join(t, inst, hunter)
		swing(inst, hunter.ID)
		up := bossUps(s)[0]
		settle(3 * time.Second)
		late := join(t, inst, armed("Late", 100, 10))
		snap := late.All()[0].(model.Snapshot)
		want := model.FieldBoss{Tick: snap.Tick, Mob: up.Mob, Type: "king_slime", LeavesIn: 10*time.Minute - time.Duration(snap.Tick-up.Tick)*worldtest.TickInterval}
		if snap.FieldBoss == nil || *snap.FieldBoss != want {
			t.Fatalf("snapshot boss %+v; want %+v", snap.FieldBoss, want)
		}
		if first := s.All()[0].(model.Snapshot); first.FieldBoss != nil {
			t.Fatalf("a snapshot before the king rose has %+v", first.FieldBoss)
		}
	})
}
