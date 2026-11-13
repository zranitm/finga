package world

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"testing"
	"time"

	"finga/internal/host"
	"finga/internal/kernel"
	"finga/pkg/hub"
	"finga/pkg/observability/metrics"
	"finga/services/world/internal/application/instance"
	"finga/services/world/internal/model"
)

// filler sets every exported field it reaches to a value no other field has, so a field the conversion drops or swaps shows.
type filler struct{ n int64 }

func (f *filler) fill(v reflect.Value) {
	if v.Type() == reflect.TypeFor[time.Time]() {
		f.n++
		v.Set(reflect.ValueOf(time.Unix(f.n, 0)))
		return
	}
	switch v.Kind() {
	case reflect.Bool:
		v.SetBool(true)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		f.n++
		v.SetInt(f.n)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		f.n++
		v.SetUint(uint64(f.n))
	case reflect.Float32, reflect.Float64:
		f.n++
		v.SetFloat(float64(f.n) + 0.5)
	case reflect.String:
		f.n++
		v.SetString(fmt.Sprintf("s%d", f.n))
	case reflect.Array:
		for i := range v.Len() {
			f.fill(v.Index(i))
		}
	case reflect.Slice:
		v.Set(reflect.MakeSlice(v.Type(), 2, 2))
		for i := range 2 {
			f.fill(v.Index(i))
		}
	case reflect.Map:
		v.Set(reflect.MakeMap(v.Type()))
		for range 2 {
			k, e := reflect.New(v.Type().Key()).Elem(), reflect.New(v.Type().Elem()).Elem()
			f.fill(k)
			f.fill(e)
			v.SetMapIndex(k, e)
		}
	case reflect.Pointer:
		p := reflect.New(v.Type().Elem())
		f.fill(p.Elem())
		v.Set(p)
	case reflect.Struct:
		for i := range v.NumField() {
			if v.Type().Field(i).IsExported() {
				f.fill(v.Field(i))
			}
		}
	default:
		panic(fmt.Sprintf("filler: no value for a %s", v.Type()))
	}
}

// fromElsewhere are the facade's fields the model's events have no source for: a frame's recipients come with it, not in it.
var fromElsewhere = map[string]bool{"Frame.Recipients": true}

// differences lists each exported field of m, but a tick, that f lacks or holds another value in, and each of f's that m has no source for; fields match by name, values by kind.
func differences(path string, m, f reflect.Value) []string {
	if m.Type() == f.Type() {
		if !reflect.DeepEqual(m.Interface(), f.Interface()) {
			return []string{fmt.Sprintf("%s: %v became %v", path, m, f)}
		}
		return nil
	}
	if m.Kind() != f.Kind() {
		return []string{fmt.Sprintf("%s: a %s became a %s", path, m.Type(), f.Type())}
	}
	var out []string
	switch m.Kind() {
	case reflect.Pointer:
		if m.IsNil() != f.IsNil() {
			return []string{path + ": set and unset"}
		}
		if !m.IsNil() {
			out = differences(path, m.Elem(), f.Elem())
		}
	case reflect.Struct:
		for i := range m.NumField() {
			field := m.Type().Field(i)
			if !field.IsExported() || field.Name == "Tick" {
				continue
			}
			to := f.FieldByName(field.Name)
			if !to.IsValid() {
				out = append(out, path+"."+field.Name+": the facade has no such field")
				continue
			}
			out = append(out, differences(path+"."+field.Name, m.Field(i), to)...)
		}
		for i := range f.NumField() {
			field := f.Type().Field(i)
			if _, ok := m.Type().FieldByName(field.Name); field.IsExported() && !ok && !fromElsewhere[path+"."+field.Name] {
				out = append(out, path+"."+field.Name+": the model has no such field, so nothing sets it")
			}
		}
	case reflect.Slice:
		if m.Len() != f.Len() {
			return []string{fmt.Sprintf("%s: %d elements became %d", path, m.Len(), f.Len())}
		}
		for i := range m.Len() {
			out = append(out, differences(fmt.Sprintf("%s[%d]", path, i), m.Index(i), f.Index(i))...)
		}
	case reflect.Map:
		if m.Len() != f.Len() {
			return []string{fmt.Sprintf("%s: %d entries became %d", path, m.Len(), f.Len())}
		}
		for _, k := range m.MapKeys() {
			to := f.MapIndex(k.Convert(f.Type().Key()))
			if !to.IsValid() {
				out = append(out, fmt.Sprintf("%s[%v]: dropped", path, k))
				continue
			}
			out = append(out, differences(fmt.Sprintf("%s[%v]", path, k), m.MapIndex(k), to)...)
		}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if m.Int() != f.Int() {
			out = append(out, fmt.Sprintf("%s: %d became %d", path, m.Int(), f.Int()))
		}
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		if m.Uint() != f.Uint() {
			out = append(out, fmt.Sprintf("%s: %d became %d", path, m.Uint(), f.Uint()))
		}
	case reflect.String:
		if m.String() != f.String() {
			out = append(out, fmt.Sprintf("%s: %q became %q", path, m.String(), f.String()))
		}
	default:
		out = append(out, fmt.Sprintf("%s: can't compare a %s", path, m.Type()))
	}
	return out
}

// filled is a zero value's type with every field filled.
func filled[T any](f *filler, zero T) T {
	v := reflect.New(reflect.TypeOf(zero)).Elem()
	f.fill(v)
	return v.Interface().(T)
}

// Each event a map sends reaches the gateway's stream as the facade's own event of that name, with every field and the tick it happened at.
func TestEveryModelEventKeepsItsFields(t *testing.T) {
	f := &filler{}
	for _, zero := range []model.Event{
		model.Snapshot{}, model.FieldBoss{}, model.FieldBossGone{}, model.Teleport{}, model.Damage{}, model.SkillUsed{}, model.AttackUsed{},
		model.MobAttacked{}, model.Died{}, model.Respawned{}, model.ExpGained{}, model.LevelUp{}, model.Promoted{}, model.StatsChanged{},
		model.Chat{}, model.InventoryChanged{}, model.Loot{}, model.Detached{}, model.NpcDialog{}, model.QuestChanged{},
	} {
		name := reflect.TypeOf(zero).Name()
		t.Run(name, func(t *testing.T) {
			e := filled(f, zero)
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
			for _, problem := range differences(name, reflect.ValueOf(e), reflect.ValueOf(d.Event)) {
				t.Error(problem)
			}
		})
	}
	t.Run("Frame", func(t *testing.T) {
		frame, to := filled(f, model.Frame{}), filled(f, []model.Recipient{})
		got := frameOf(&frame, to)
		for _, problem := range append(differences("Frame", reflect.ValueOf(frame), reflect.ValueOf(got)), differences("Frame.Recipients", reflect.ValueOf(to), reflect.ValueOf(got.Recipients))...) {
			t.Error(problem)
		}
	})
}

// An event waits on the stream while the map's tick goes on, so it shares none of the counts or points the map goes on changing.
func TestAnEventSharesNothingTheMapChanges(t *testing.T) {
	progress, skills := []int{1, 0}, map[string]int{"strike": 2}
	quest := deliveryOf(model.QuestChanged{ID: "a", Quest: model.Quest{Status: model.QuestActive, Progress: progress}}).Event.(QuestChanged)
	sheet := model.Sheet{Skills: model.Skills{Points: skills}}
	stats := deliveryOf(model.StatsChanged{Sheet: &sheet}).Event.(StatsChanged)
	level := deliveryOf(model.LevelUp{Sheet: sheet}).Event.(LevelUp)
	progress[0], skills["strike"] = 9, 9
	if quest.Quest.Progress[0] != 1 || stats.Sheet.Skills.Points["strike"] != 2 || level.Sheet.Skills.Points["strike"] != 2 {
		t.Fatalf("the events changed with the map: progress %v, skills %v and %v", quest.Quest.Progress, stats.Sheet.Skills.Points, level.Sheet.Skills.Points)
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
	to := []model.Recipient{{Character: a.character, Connection: "c-a", AckSeq: 8}, {Character: b.character, Connection: "c-b"}}
	if !a.Stream().SendFrame(&model.Frame{Tick: 3, RemovedMobs: []uint64{6}}, to) {
		t.Fatal("the stream refused the frame")
	}
	if n := len(sub.C()); n != 1 {
		t.Fatalf("%d deliveries on the stream, want 1", n)
	}
	d := <-sub.C()
	want := Delivery{Tick: 3, Event: Frame{RemovedMobs: []uint64{6}, Recipients: []Recipient{{Character: a.character, Connection: "c-a", AckSeq: 8}, {Character: b.character, Connection: "c-b"}}}}
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
	m := Over(t, mod, j)
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
