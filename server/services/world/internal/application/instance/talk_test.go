package instance_test

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"testing/synctest"
	"time"

	"finga/internal/sim"
	"finga/pkg/transient"
	"finga/services/world/internal/model"
	"finga/services/world/worldtest"
)

// The town guide stands at x 496 on the town's floor, at y 608; reach is 60 px from the character's centre, 28 px above its feet.
func TestTalk(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		inst, stop := worldtest.Run(t, data, nil, worldtest.Options{CheckpointInterval: time.Hour})
		defer stop()
		p := worldtest.NewPlayer("Ayla", 450, 608)
		p.Level, p.Connection = 2, "c1"
		p.Quests = model.Quests{"slime_cleanup": {Status: model.QuestActive, Progress: []int{4}}}
		s := join(t, inst, p)
		talk := func(conn, npc string) error {
			err := inst.Talk(context.Background(), p.ID, conn, npc)
			tick(1)
			return err
		}

		if err := talk("c1", "town_guide"); err != nil {
			t.Fatal(err)
		}
		got := eventsOf[model.NpcDialog](s)
		want := model.NpcDialog{NPC: "town_guide",
			Gives: []model.QuestOffer{{Quest: "slime_cleanup", State: model.OfferActive}, {Quest: "sticky_samples", State: model.OfferUnmet, Unmet: model.Unmet{Quests: []string{"slime_cleanup"}}}},
			Takes: []model.QuestTurnIn{{Quest: "slime_cleanup", Ready: false}},
		}
		if len(got) != 1 {
			t.Fatalf("dialogs %+v", got)
		}
		got[0].Tick = 0
		if !reflect.DeepEqual(got[0], want) {
			t.Fatalf("dialog %+v, want %+v", got[0], want)
		}

		for _, c := range []struct {
			name, conn, npc string
			want            error
		}{
			{"an NPC on another map", "c1", "nobody", model.ErrNpcOutOfReach},
			{"a connection that doesn't hold the character", "c2", "town_guide", transient.ErrUnavailable},
		} {
			if err := talk(c.conn, c.npc); !errors.Is(err, c.want) {
				t.Fatalf("%s: %v, want %v", c.name, err, c.want)
			}
		}
		worldtest.Walk(inst, p.ID, 0, sim.Input{Left: true}, 5) // 50 px away: out of reach
		if err := talk("c1", "town_guide"); !errors.Is(err, model.ErrNpcOutOfReach) {
			t.Fatalf("out of reach: %v", err)
		}
		if n := len(eventsOf[model.NpcDialog](s)); n != 1 {
			t.Fatalf("%d dialogs; a refused talk sends none", n)
		}
	})
}

func TestTalkWhileDeadIsRefused(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		inst, stop := worldtest.RunMap(t, data, arena(time.Hour), nil, worldtest.Options{Transfer: &transferRecorder{}})
		defer stop()
		tick(1)
		p := warriorAt(260)
		p.HP, p.Connection = 1, "c1"
		join(t, inst, p)
		worldtest.Walk(inst, p.ID, 0, sim.Input{Right: true}, 2) // onto the slime, which kills
		tick(1)
		if err := inst.Talk(context.Background(), p.ID, "c1", "town_guide"); !errors.Is(err, model.ErrCharacterDead) {
			t.Fatalf("err = %v, want ErrCharacterDead", err)
		}
	})
}

// A checkpoint carries a copy of the whole quest log, which the tick may change while the saver reads it.
func TestACheckpointCarriesACopyOfTheQuestLog(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		cps := &worldtest.Checkpoints{}
		inst, stop := worldtest.Run(t, data, cps, worldtest.Options{CheckpointInterval: time.Hour})
		defer stop()
		p := worldtest.NewPlayer("Ayla", 450, 608)
		p.Connection = "c1"
		p.Quests = model.Quests{"slime_cleanup": {Status: model.QuestActive, Progress: []int{4}}}
		join(t, inst, p)
		if err := inst.Leave(p.ID, "c1", nil); err != nil {
			t.Fatal(err)
		}
		tick(1)
		saved := cps.Drain()
		if len(saved) != 1 || !reflect.DeepEqual(saved[0].Quests, p.Quests) {
			t.Fatalf("checkpoints %+v", saved)
		}
		p.Quests["slime_cleanup"].Progress[0] = 9
		if saved[0].Quests["slime_cleanup"].Progress[0] != 4 {
			t.Fatal("the checkpoint shares the log's counts")
		}
	})
}
