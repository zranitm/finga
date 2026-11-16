package world

import (
	"context"
	"errors"
	"log/slog"
	"reflect"
	"testing"

	"finga/internal/host"
	"finga/internal/kernel"
	"finga/modules/world/internal/application/instance"
	"finga/modules/world/internal/model"
	"finga/pkg/copytest"
	"finga/pkg/hub"
	"finga/pkg/observability/metrics"
)

// eventKeepsItsFields checks that each fill of E reaches the gateway's stream as the facade's own event of that name, with every field, and the tick it happened at as the delivery's.
func eventKeepsItsFields[E model.Event](t *testing.T) {
	name := reflect.TypeFor[E]().Name()
	t.Run(name, func(t *testing.T) {
		m := copytest.Match{Skip: map[string]string{}}
		if _, ok := reflect.TypeFor[E]().FieldByName("Tick"); ok {
			m.Skip["Tick"] = "the delivery carries it"
		}
		if name == "Snapshot" {
			m.Skip["FieldBoss.Tick"] = "a snapshot's boss is as of the snapshot"
		}
		got := copytest.Diff(m, func(e E) Event {
			d := deliveryOf(e)
			if d.Event == nil || reflect.TypeOf(d.Event).Name() != name {
				t.Fatalf("became %T", d.Event)
			}
			var tick uint64
			if field := reflect.ValueOf(e).FieldByName("Tick"); field.IsValid() {
				tick = field.Uint()
			}
			if d.Tick != tick {
				t.Errorf("tick %d became %d", tick, d.Tick)
			}
			return d.Event
		})
		for _, problem := range got {
			t.Error(problem)
		}
	})
}

// Each event a map sends reaches the gateway's stream as the facade's own event of that name, with every field and the tick it happened at.
func TestEveryModelEventKeepsItsFields(t *testing.T) {
	for _, check := range []func(*testing.T){
		eventKeepsItsFields[model.Snapshot], eventKeepsItsFields[model.FieldBoss], eventKeepsItsFields[model.FieldBossGone],
		eventKeepsItsFields[model.Teleport], eventKeepsItsFields[model.Damage], eventKeepsItsFields[model.SkillUsed],
		eventKeepsItsFields[model.AttackUsed], eventKeepsItsFields[model.MobAttacked], eventKeepsItsFields[model.Died],
		eventKeepsItsFields[model.Respawned], eventKeepsItsFields[model.ExpGained], eventKeepsItsFields[model.LevelUp],
		eventKeepsItsFields[model.Promoted], eventKeepsItsFields[model.StatsChanged], eventKeepsItsFields[model.Chat],
		eventKeepsItsFields[model.InventoryChanged], eventKeepsItsFields[model.Loot], eventKeepsItsFields[model.Detached],
		eventKeepsItsFields[model.NpcDialog], eventKeepsItsFields[model.QuestChanged],
	} {
		check(t)
	}
	t.Run("Frame", func(t *testing.T) {
		type sent struct {
			Frame model.Frame
			To    []model.Recipient
		}
		m := copytest.Match{Renames: map[string]string{"Frame": "", "To": "Recipients"}, Skip: map[string]string{"Frame.Tick": "SendFrame puts it on the delivery"}}
		for _, problem := range copytest.Diff(m, func(s sent) Frame { return frameOf(&s.Frame, s.To) }) {
			t.Error(problem)
		}
	})
}

// A join answers with the character's every field as the map joined it, and the maps' tick rate.
func TestAJoinsViewKeepsItsFields(t *testing.T) {
	m := copytest.Match{Computed: map[string]string{"TickRateHz": "the module's tick rate, which every join in module_test checks"}}
	for _, problem := range copytest.Diff(m, viewOf) {
		t.Error(problem)
	}
}

// An event waits on the stream while the map's tick goes on, so it shares none of the counts, points or lists the map may go on changing.
func TestAnEventSharesNothingTheMapChanges(t *testing.T) {
	progress, skills := []int{1, 0}, map[string]int{"strike": 2}
	quest := deliveryOf(model.QuestChanged{ID: "a", Quest: model.Quest{Status: model.QuestActive, Progress: progress}}).Event.(QuestChanged)
	sheet := model.Sheet{Skills: model.Skills{Points: skills}}
	stats := deliveryOf(model.StatsChanged{Sheet: &sheet}).Event.(StatsChanged)
	level := deliveryOf(model.LevelUp{Sheet: sheet}).Event.(LevelUp)
	removed := &model.Frame{Removed: []kernel.CharacterID{kernel.NewCharacterID()}, RemovedMobs: []uint64{1}, RemovedDrops: []uint64{2}, CollectedDrops: []uint64{3}}
	frame := frameOf(removed, nil)
	progress[0], skills["strike"] = 9, 9
	removed.Removed[0], removed.RemovedMobs[0], removed.RemovedDrops[0], removed.CollectedDrops[0] = kernel.CharacterID{}, 9, 9, 9
	if quest.Quest.Progress[0] != 1 || stats.Sheet.Skills.Points["strike"] != 2 || level.Sheet.Skills.Points["strike"] != 2 {
		t.Fatalf("the events changed with the map: progress %v, skills %v and %v", quest.Quest.Progress, stats.Sheet.Skills.Points, level.Sheet.Skills.Points)
	}
	if frame.Removed[0].IsZero() || frame.RemovedMobs[0] != 1 || frame.RemovedDrops[0] != 2 || frame.CollectedDrops[0] != 3 {
		t.Fatalf("the frame changed with the map: %+v", frame)
	}
}

// Every player joined through a gateway shares its stream, and a frame goes on it once, with each recipient's own ack and no player of its own.
func TestAFrameGoesToItsGatewayOnce(t *testing.T) {
	sub := hub.New[Delivery](8).Subscribe("gw")
	a, b := sink{sub: sub, character: kernel.NewCharacterID(), conn: "c-a"}, sink{sub: sub, character: kernel.NewCharacterID(), conn: "c-b"}
	if a.Stream() != b.Stream() {
		t.Fatal("two players on one gateway have two streams")
	}
	if other := (sink{sub: hub.New[Delivery](8).Subscribe("gw-2")}); other.Stream() == a.Stream() {
		t.Fatal("two gateways share a stream")
	}
	energy := 6
	to := []model.Recipient{{Character: a.character, Connection: "c-a", AckSeq: 8, Energy: &energy}, {Character: b.character, Connection: "c-b"}}
	if !a.Stream().SendFrame(&model.Frame{Tick: 3, RemovedMobs: []uint64{6}}, to) {
		t.Fatal("the stream refused the frame")
	}
	if n := len(sub.C()); n != 1 {
		t.Fatalf("%d deliveries on the stream, want 1", n)
	}
	d := <-sub.C()
	want := Delivery{Tick: 3, Event: Frame{RemovedMobs: []uint64{6}, Recipients: []Recipient{{Character: a.character, Connection: "c-a", AckSeq: 8, Energy: &energy}, {Character: b.character, Connection: "c-b"}}}}
	if !reflect.DeepEqual(d, want) {
		t.Fatalf("got %+v, want %+v", d, want)
	}
}

// A player's event names the player and the connection it joined by.
func TestAPlayersEventNamesItsConnection(t *testing.T) {
	sub := hub.New[Delivery](1).Subscribe("gw")
	s := sink{sub: sub, character: kernel.NewCharacterID(), conn: "c-a"}
	if !s.Send(model.Chat{Tick: 4, Sender: s.character, SenderName: "Ayla", Text: "hi"}) {
		t.Fatal("the stream refused the line")
	}
	want := Delivery{Character: s.character, Conn: "c-a", Tick: 4, Event: Chat{Sender: s.character, SenderName: "Ayla", Text: "hi"}}
	if got := <-sub.C(); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

// An event with nothing to say, which no map sends, puts nothing on the stream rather than an empty delivery.
func TestNoEventSendsNothing(t *testing.T) {
	sub := hub.New[Delivery](1).Subscribe("gw")
	if s := (sink{sub: sub, character: kernel.NewCharacterID(), conn: "c-a"}); !s.Send(nil) || len(sub.C()) != 0 {
		t.Fatalf("%d deliveries on the stream, want none, and the player kept", len(sub.C()))
	}
}

// A gateway whose stream is full refuses the frame, which drops its players.
func TestAFullStreamRefusesTheFrame(t *testing.T) {
	s := sink{sub: hub.New[Delivery](1).Subscribe("gw")}
	if !s.Send(model.Detached{}) {
		t.Fatal("the first delivery was refused")
	}
	if s.Stream().SendFrame(&model.Frame{}, []model.Recipient{{Character: kernel.NewCharacterID()}}) {
		t.Fatal("a full stream took the frame")
	}
}

// joins records the sink each join hands the maps.
type joins struct {
	service
	sink instance.Sink
}

func (j *joins) Join(_ context.Context, _ kernel.AccountID, id kernel.CharacterID, _ string, s instance.Sink) (model.View, error) {
	j.sink = s
	return model.View{ID: id, Quests: model.Quests{"a": {Status: model.QuestActive, Progress: []int{1}}}}, nil
}

// A join goes through its gateway's stream, which must be open: its events then go on that stream, for its connection.
func TestAJoinNeedsItsGatewaysStream(t *testing.T) {
	h, err := host.New(host.Options{Name: "world", Logger: slog.New(slog.DiscardHandler), Registry: metrics.NewRegistry()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(h.Close)
	mod, err := h.Module("world")
	if err != nil {
		t.Fatal(err)
	}
	j := &joins{}
	m := Over(t, mod, j, 20)
	p := Presence{Character: kernel.NewCharacterID(), Conn: "c-a"}
	if _, err := m.Join(t.Context(), "gw", kernel.NewAccountID(), p); !errors.Is(err, kernel.ErrNotSubscribed) || j.sink != nil {
		t.Fatalf("a join with no stream: %v, the maps reached: %v", err, j.sink != nil)
	}
	sub := m.Subscribe("gw")
	v, err := m.Join(t.Context(), "gw", kernel.NewAccountID(), p)
	if err != nil {
		t.Fatal(err)
	}
	if got := j.sink.(sink); got.sub != sub || got.character != p.Character || got.conn != p.Conn {
		t.Fatalf("the maps got sink %+v", got)
	}
	if v.ID != p.Character || v.Quests["a"].Status != QuestActive || v.Quests["a"].Progress[0] != 1 {
		t.Fatalf("view %+v", v)
	}
}

// A mob's pose goes out with it, on a Snapshot and on each frame, so a player sees a downed mob.
func TestAMobViewCarriesItsPose(t *testing.T) {
	for pose, want := range map[model.MobPose]MobPose{model.Standing: Standing, model.Flinching: Flinching, model.Down: Down, model.Rising: Rising} {
		mobs := []model.MobView{{ID: 4, Pose: pose}}
		snap, _ := deliveryOf(model.Snapshot{Mobs: mobs}).Event.(Snapshot)
		if len(snap.Mobs) != 1 || snap.Mobs[0].Pose != want {
			t.Errorf("pose %d went out on a Snapshot as %+v, want %d", pose, snap.Mobs, want)
		}
		if got := frameOf(&model.Frame{Mobs: mobs}, nil).Mobs; len(got) != 1 || got[0].Pose != want {
			t.Errorf("pose %d went out on a frame as %+v, want %d", pose, got, want)
		}
	}
}

// An HP-only change leaves the sheet unset, not zeroed, so the client keeps its stats.
func TestStatsChangedWithoutASheetLeavesItUnset(t *testing.T) {
	d := deliveryOf(model.StatsChanged{Tick: 6, HP: 200, MaxHP: 234})
	if d.Tick != 6 || d.Event != (StatsChanged{HP: 200, MaxHP: 234}) {
		t.Fatalf("got %+v, want HP 200 of 234 with no sheet", d)
	}
}

// A Snapshot carries no field boss while none is up.
func TestASnapshotWithoutABossCarriesNone(t *testing.T) {
	if snap, _ := deliveryOf(model.Snapshot{MapID: "forest"}).Event.(Snapshot); snap.MapID != "forest" || snap.FieldBoss != nil {
		t.Fatalf("got %+v, want forest with no boss", snap)
	}
}

// A trainer's dialog carries the promotion with each condition by its kind.
func TestATrainersDialogCarriesItsPromotion(t *testing.T) {
	d := deliveryOf(model.NpcDialog{Tick: 4, NPC: "warrior_trainer", Promotion: &model.PromotionOffer{Class: "warrior", Conditions: []model.Condition{
		{Kind: model.ConditionLevel, Need: 10, Have: 9},
		{Kind: model.ConditionStat, ID: "str", Need: 30, Have: 30},
		{Kind: model.ConditionSkill, ID: "strike", Need: 10, Have: 4},
		{Kind: model.ConditionQuest, ID: "warriors_trial", Need: 1, Have: 1},
	}}})
	want := NpcDialog{NPC: "warrior_trainer", Promotion: &PromotionOffer{Class: "warrior", Conditions: []Condition{
		{Kind: ConditionLevel, Need: 10, Have: 9},
		{Kind: ConditionStat, ID: "str", Need: 30, Have: 30},
		{Kind: ConditionSkill, ID: "strike", Need: 10, Have: 4},
		{Kind: ConditionQuest, ID: "warriors_trial", Need: 1, Have: 1},
	}}}
	if d.Tick != 4 || !reflect.DeepEqual(d.Event, want) {
		t.Fatalf("got %+v, want %+v", d.Event, want)
	}
}

// Each loot kind, quest change, quest status and offer state reaches the gateway as the facade's value of the same name, which a cast keeps only while both lists stay in step.
func TestEachEnumKeepsItsName(t *testing.T) {
	for kind, want := range map[model.LootKind]LootKind{model.LootCollected: LootCollected, model.LootWaiting: LootWaiting, model.LootLost: LootLost} {
		if got := deliveryOf(model.Loot{Kind: kind}).Event.(Loot).Kind; got != want {
			t.Errorf("loot kind %d went out as %d, want %d", kind, got, want)
		}
	}
	for change, want := range map[model.QuestChange]QuestChange{
		model.QuestProgressed: QuestProgressed, model.QuestAccepted: QuestAccepted, model.QuestHandedIn: QuestHandedIn, model.QuestAbandoned: QuestAbandoned,
	} {
		if got := deliveryOf(model.QuestChanged{Change: change}).Event.(QuestChanged).Change; got != want {
			t.Errorf("quest change %d went out as %d, want %d", change, got, want)
		}
	}
	for status, want := range map[model.QuestStatus]QuestStatus{model.QuestActive: QuestActive, model.QuestFinished: QuestFinished} {
		if got := deliveryOf(model.QuestChanged{Quest: model.Quest{Status: status}}).Event.(QuestChanged).Quest.Status; got != want {
			t.Errorf("quest status %d went out as %d, want %d", status, got, want)
		}
	}
	for state, want := range map[model.OfferState]OfferState{
		model.OfferAvailable: OfferAvailable, model.OfferActive: OfferActive, model.OfferFinished: OfferFinished, model.OfferUnmet: OfferUnmet,
	} {
		if gives := deliveryOf(model.NpcDialog{Gives: []model.QuestOffer{{State: state}}}).Event.(NpcDialog).Gives; len(gives) != 1 || gives[0].State != want {
			t.Errorf("offer state %d went out as %+v, want %d", state, gives, want)
		}
	}
}
