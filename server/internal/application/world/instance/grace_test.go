package instance_test

import (
	"context"
	"errors"
	"math"
	"regexp"
	"slices"
	"testing"
	"testing/synctest"
	"time"

	"finga/internal/application/world/instance"
	"finga/internal/application/world/worldtest"
	model "finga/internal/model/world"
	"finga/internal/shared/gamedata"
	"finga/internal/shared/kernel"
	"finga/internal/shared/sim"
)

// fight is a match run to its first round, with what it reported.
type fight struct {
	inst    *instance.Instance
	sinks   map[kernel.CharacterID]*worldtest.Sink
	results *worldtest.Results
	moves   *worldtest.Moves
	cps     *worldtest.Checkpoints
	parties *fakeParties
	grace   time.Duration
}

// fightOf runs spec, with ps joined, to its first round.
func fightOf(t *testing.T, data *gamedata.Data, spec model.MatchSpec, ps ...model.Player) (fight, func()) {
	t.Helper()
	mode, _ := data.PvpMode(spec.Mode)
	f := fight{results: &worldtest.Results{}, moves: &worldtest.Moves{}, cps: &worldtest.Checkpoints{}, parties: &fakeParties{}, grace: mode.Reconnect}
	var stop func()
	f.inst, stop = worldtest.RunMatch(t, data, spec, f.cps, worldtest.Options{Results: f.results, Transfer: f.moves, Parties: f.parties})
	f.sinks = joinAll(t, f.inst, ps...)
	sleep(data.Pvp().Rounds.Prep + data.Pvp().Rounds.Countdown + worldtest.TickInterval)
	return f, stop
}

// holdOf holds p's place through their connection's drop.
func holdOf(t *testing.T, inst *instance.Instance, p model.Player) {
	t.Helper()
	if held, err := inst.Hold(context.Background(), p.ID, p.Connection); err != nil || !held {
		t.Fatalf("holding %s: %v, %v; want held", p.Name, held, err)
	}
}

// listed is id as the last MatchState s heard lists them.
func listed(t *testing.T, s *worldtest.Sink, id kernel.CharacterID) model.MatchFighter {
	t.Helper()
	st := worldtest.Of[model.MatchState](s)
	for _, side := range st[len(st)-1].Sides {
		for _, f := range side {
			if f.ID == id {
				return f
			}
		}
	}
	t.Fatalf("the last MatchState doesn't list %v", id)
	return model.MatchFighter{}
}

// gone reports whether the match took id off: dropped, a bot in their place.
func (f fight) gone(id kernel.CharacterID) bool { return slices.Contains(f.moves.Drops(), id) }

// lastSaved is id's last checkpoint.
func (f fight) lastSaved(t *testing.T, id kernel.CharacterID) model.Checkpoint {
	t.Helper()
	saved := slices.DeleteFunc(f.cps.Drain(), func(cp model.Checkpoint) bool { return cp.ID != id })
	if len(saved) == 0 {
		t.Fatalf("no checkpoint of %v", id)
	}
	return saved[len(saved)-1]
}

// A fighter whose connection drops keeps their place: their body stands, sent nothing and steering by no keys, and takes hits as anyone's; their side hears them away, with no bot in their place. Back within the grace on a new connection, they're on the body as it stands, steering it from that connection's first input, their party told them again, and the match runs on past where the grace would have ended.
func TestADroppedFightersBodyIsHeldTillTheyReturn(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		ayla, robin := fighterFrom(t, data, "Ayla"), fighterFrom(t, data, "Robin")
		f, stop := fightOf(t, data, duel(model.Fighter{Character: ayla.ID}, model.Fighter{Character: robin.ID}), ayla, robin)
		defer stop()
		as, rs := f.sinks[ayla.ID], f.sinks[robin.ID]
		robinAt, _ := rs.Own(robin.ID)
		seq := walkTo(f.inst, as, ayla.ID, 0, robinAt.State.Z-1.2, 0.2)
		_ = f.inst.Input(ayla.ID, seq+1, sim.Input{Up: true}, 0) // a key held as the connection drops
		holdOf(t, f.inst, ayla)
		heard := len(as.All())
		stood, _ := rs.Own(ayla.ID)
		sleep(time.Second)
		if body, _ := rs.Own(ayla.ID); math.Abs(body.State.Z-stood.State.Z) > 0.01 {
			t.Fatalf("the held body went from z %.2f to %.2f, want it standing", stood.State.Z, body.State.Z)
		}
		if l := listed(t, rs, ayla.ID); !l.Away || l.Bot {
			t.Fatalf("Robin's match lists Ayla %+v, want her away and no bot", l)
		}
		for range 200 {
			if body, _ := rs.Own(ayla.ID); body.HP < body.MaxHP {
				break
			}
			_ = f.inst.Attack(robin.ID, math.Pi, model.EntityRef{Player: ayla.ID})
			sleep(worldtest.TickInterval)
		}
		sleep(time.Second) // Robin's blow over
		body, _ := rs.Own(ayla.ID)
		if body.HP >= body.MaxHP {
			t.Fatalf("Ayla %d/%d, want her held body hurt", body.HP, body.MaxHP)
		}
		if n := len(as.All()); n != heard {
			t.Fatalf("the dropped connection was sent %d more, want none", n-heard)
		}
		told := len(f.parties.updated())
		back := &worldtest.Sink{}
		v, err := f.inst.Rejoin(context.Background(), ayla.ID, "c-Ayla-again", back)
		if err != nil || v.MapID != "ring" || v.HP != body.HP || v.MaxHP != body.MaxHP {
			t.Fatalf("the return %+v, %v; want her on the ring at %d/%d", v, err, body.HP, body.MaxHP)
		}
		sleep(worldtest.TickInterval)
		if l := listed(t, rs, ayla.ID); l.Away {
			t.Fatalf("Robin's match lists Ayla %+v, want her back", l)
		}
		if !slices.ContainsFunc(f.parties.updated()[told:], func(u model.MemberUpdate) bool {
			return u.ID == ayla.ID && u.MapID == "ring" && u.HP == body.HP
		}) {
			t.Fatal("her party wasn't told her again on her return")
		}
		if len(worldtest.Of[model.Snapshot](back)) != 1 || !slices.ContainsFunc(worldtest.Of[model.StatsChanged](back), func(c model.StatsChanged) bool { return c.Timers != nil }) {
			t.Fatal("the new connection wasn't sent the map and her timers")
		}
		if d := back.Deltas(); len(d) == 0 || d[0].Energy == nil || *d[0].Energy != v.Energy {
			t.Fatal("the new connection's first frame didn't tell her energy")
		}
		own, ok := back.Own(ayla.ID)
		if !ok || own.HP != body.HP || math.Abs(own.State.Z-body.State.Z) > 0.01 {
			t.Fatalf("the new connection sees Ayla %+v, want her body as it stood, %+v", own, body)
		}
		worldtest.Walk(f.inst, ayla.ID, 0, sim.Input{Down: true}, 10)
		if moved, _ := back.Own(ayla.ID); moved.State.Z >= own.State.Z-0.5 {
			t.Fatalf("Ayla at z %.2f from %.2f, want her new connection's inputs walking her south", moved.State.Z, own.State.Z)
		}
		sleep(f.grace)
		if f.gone(ayla.ID) || len(f.results.All()) != 0 {
			t.Fatalf("drops %v, outcomes %+v; want the match running on with Ayla", f.moves.Drops(), f.results.All())
		}
	})
}

// Past the grace a held fighter has left, as one who logs out: in 1v1 the other side wins, and they're saved where they came from, which their next login lands on; in 3v3 a bot takes their body as it stands then.
func TestAFighterHeldPastTheGraceHasLeft(t *testing.T) {
	t.Run("1v1", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			data := worldtest.LoadData(t)
			ayla, robin := fighterFrom(t, data, "Ayla"), fighterFrom(t, data, "Robin")
			f, stop := fightOf(t, data, duel(model.Fighter{Character: ayla.ID}, model.Fighter{Character: robin.ID}), ayla, robin)
			defer stop()
			holdOf(t, f.inst, ayla)
			sleep(f.grace - 2*worldtest.TickInterval)
			if f.gone(ayla.ID) || len(f.results.All()) != 0 {
				t.Fatalf("drops %v, outcomes %+v before the grace ran out; want none", f.moves.Drops(), f.results.All())
			}
			sleep(4 * worldtest.TickInterval)
			if got := f.results.All(); !f.gone(ayla.ID) || len(got) != 1 || got[0].Winner != 2 || !got[0].Sides[0][0].Left {
				t.Fatalf("drops %v, outcomes %+v; want Ayla dropped and Robin's win", f.moves.Drops(), got)
			}
			if cp := f.lastSaved(t, ayla.ID); cp.MapID != data.StartTown().ID {
				t.Fatalf("Ayla saved on %s, want where she came from", cp.MapID)
			}
			if _, err := f.inst.Rejoin(context.Background(), ayla.ID, "c-Ayla-again", &worldtest.Sink{}); !errors.Is(err, model.ErrNotInMatch) {
				t.Fatalf("Ayla back past the grace: %v, want ErrNotInMatch", err)
			}
		})
	})
	t.Run("3v3", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			data := worldtest.LoadData(t)
			ayla, bea, robin := fighterFrom(t, data, "Ayla"), fighterFrom(t, data, "Bea"), fighterFrom(t, data, "Robin")
			f, stop := fightOf(t, data, team([2][]model.Fighter{{{Character: ayla.ID}, {Character: bea.ID}}, {{Character: robin.ID}}}, "novice"), ayla, bea, robin)
			defer stop()
			holdOf(t, f.inst, ayla)
			sleep(f.grace - 2*worldtest.TickInterval)
			if l := listed(t, f.sinks[robin.ID], ayla.ID); !l.Away {
				t.Fatalf("Robin's match lists Ayla %+v near the grace's end, want her held", l)
			}
			sleep(4 * worldtest.TickInterval)
			st := worldtest.Of[model.MatchState](f.sinks[robin.ID])
			if last := st[len(st)-1]; last.Sides[0][0].ID == ayla.ID || !last.Sides[0][0].Bot || last.Sides[0][0].Name != "Ayla" {
				t.Fatalf("side 1 %+v past the grace; want a bot in Ayla's place", last.Sides[0])
			}
		})
	})
}

// A second drop in the same match has a grace of its own, from the drop.
func TestASecondDropHasAFreshGrace(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		ayla, robin := fighterFrom(t, data, "Ayla"), fighterFrom(t, data, "Robin")
		f, stop := fightOf(t, data, duel(model.Fighter{Character: ayla.ID}, model.Fighter{Character: robin.ID}), ayla, robin)
		defer stop()
		holdOf(t, f.inst, ayla)
		sleep(f.grace * 3 / 4)
		if _, err := f.inst.Rejoin(context.Background(), ayla.ID, "c-Ayla-again", &worldtest.Sink{}); err != nil {
			t.Fatal(err)
		}
		ayla.Connection = "c-Ayla-again"
		holdOf(t, f.inst, ayla)
		sleep(f.grace * 3 / 4)
		if f.gone(ayla.ID) {
			t.Fatal("Ayla dropped by her first grace's end, want her second running")
		}
		sleep(f.grace / 4)
		if !f.gone(ayla.ID) {
			t.Fatal("Ayla still held past her second grace")
		}
	})
}

// A return whose connection goes too before it's on the body holds the place again, to the grace's first end: failed returns buy no time.
func TestAFailedReturnKeepsTheGracesEnd(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		ayla, robin := fighterFrom(t, data, "Ayla"), fighterFrom(t, data, "Robin")
		f, stop := fightOf(t, data, duel(model.Fighter{Character: ayla.ID}, model.Fighter{Character: robin.ID}), ayla, robin)
		defer stop()
		holdOf(t, f.inst, ayla)
		sleep(f.grace * 3 / 4)
		closed := &worldtest.Sink{}
		closed.Refuse()
		if _, err := f.inst.Rejoin(context.Background(), ayla.ID, "c-Ayla-again", closed); !errors.Is(err, instance.ErrSinkClosed) {
			t.Fatalf("a return on a closed connection: %v, want ErrSinkClosed", err)
		}
		sleep(worldtest.TickInterval)
		if l := listed(t, f.sinks[robin.ID], ayla.ID); !l.Away || f.gone(ayla.ID) {
			t.Fatalf("Robin's match lists Ayla %+v, dropped %v; want her still held", l, f.gone(ayla.ID))
		}
		sleep(f.grace / 4)
		if !f.gone(ayla.ID) {
			t.Fatal("Ayla still held past her grace's first end")
		}
	})
}

// A match that ends while a fighter's place is held rates them by its result, never as a leaver; they're saved where they came from, which their next login lands on, and handed nowhere.
func TestAMatchEndingInTheGraceRatesTheHeldFighterByItsResult(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		ayla, robin := fighterFrom(t, data, "Ayla"), fighterFrom(t, data, "Robin")
		f, stop := fightOf(t, data, duel(model.Fighter{Character: ayla.ID}, model.Fighter{Character: robin.ID}), ayla, robin)
		defer stop()
		holdOf(t, f.inst, ayla)
		if err := f.inst.Forfeit(context.Background(), robin.ID, robin.Connection); err != nil {
			t.Fatal(err)
		}
		sleep(data.Pvp().Result + 2*worldtest.TickInterval)
		if got := f.results.All(); len(got) != 1 || got[0].Winner != 1 || got[0].Sides[0][0].Left {
			t.Fatalf("outcomes %+v, want Ayla's win, her not a leaver", got)
		}
		if !f.gone(ayla.ID) || slices.ContainsFunc(f.moves.All(), func(m worldtest.Move) bool { return m.Player.ID == ayla.ID }) {
			t.Fatalf("drops %v, hand-offs %+v; want Ayla dropped as the match closed, handed nowhere", f.moves.Drops(), f.moves.All())
		}
		if cp := f.lastSaved(t, ayla.ID); cp.MapID != data.StartTown().ID {
			t.Fatalf("Ayla saved on %s, want where she came from", cp.MapID)
		}
	})
}

// reconnectLine is a mode's reconnect in pvp.yaml.
var reconnectLine = regexp.MustCompile(`reconnect: \w+`)

// The match holds no place off a match, for another connection, in a mode with no grace, or once its outcome shows; there a drop is a leave. Off a match, or with the outcome showing, a return finds no place either: it's a plain join.
func TestAPlaceIsHeldOnlyInAMatchUnderWay(t *testing.T) {
	for _, tc := range []struct {
		name     string
		data     func(t testing.TB) *gamedata.Data
		run      func(t *testing.T, data *gamedata.Data, ayla, robin model.Player) (*instance.Instance, model.Player, func())
		noReturn bool
	}{
		{"off a match", worldtest.LoadData, func(t *testing.T, data *gamedata.Data, ayla, _ model.Player) (*instance.Instance, model.Player, func()) {
			inst, stop := worldtest.Run(t, data, nil, worldtest.Options{})
			if err := inst.Join(context.Background(), ayla, &worldtest.Sink{}); err != nil {
				t.Fatal(err)
			}
			return inst, ayla, stop
		}, true},
		{"another connection", worldtest.LoadData, func(t *testing.T, data *gamedata.Data, ayla, robin model.Player) (*instance.Instance, model.Player, func()) {
			f, stop := fightOf(t, data, duel(model.Fighter{Character: ayla.ID}, model.Fighter{Character: robin.ID}), ayla, robin)
			ayla.Connection = "c-someone-else"
			return f.inst, ayla, stop
		}, false},
		{"a mode with no grace", func(t testing.TB) *gamedata.Data {
			return worldtest.LoadDataWith(t, map[string]func(string) string{"pvp.yaml": func(s string) string {
				if !reconnectLine.MatchString(s) {
					t.Fatal("no reconnect in pvp.yaml")
				}
				return reconnectLine.ReplaceAllString(s, "reconnect: 0s")
			}})
		}, func(t *testing.T, data *gamedata.Data, ayla, robin model.Player) (*instance.Instance, model.Player, func()) {
			f, stop := fightOf(t, data, duel(model.Fighter{Character: ayla.ID}, model.Fighter{Character: robin.ID}), ayla, robin)
			return f.inst, ayla, stop
		}, false},
		{"the outcome showing", worldtest.LoadData, func(t *testing.T, data *gamedata.Data, ayla, robin model.Player) (*instance.Instance, model.Player, func()) {
			f, stop := fightOf(t, data, duel(model.Fighter{Character: ayla.ID}, model.Fighter{Character: robin.ID}), ayla, robin)
			if err := f.inst.Forfeit(context.Background(), robin.ID, robin.Connection); err != nil {
				t.Fatal(err)
			}
			return f.inst, ayla, stop
		}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				data := tc.data(t)
				ayla, robin := fighterFrom(t, data, "Ayla"), fighterFrom(t, data, "Robin")
				inst, ayla, stop := tc.run(t, data, ayla, robin)
				defer stop()
				if held, err := inst.Hold(context.Background(), ayla.ID, ayla.Connection); err != nil || held {
					t.Fatalf("holding Ayla: %v, %v; want no place held", held, err)
				}
				if _, err := inst.Rejoin(context.Background(), ayla.ID, "c-Ayla-again", &worldtest.Sink{}); tc.noReturn && !errors.Is(err, model.ErrNotInMatch) {
					t.Fatalf("Ayla's return: %v, want ErrNotInMatch", err)
				}
			})
		})
	}
}
