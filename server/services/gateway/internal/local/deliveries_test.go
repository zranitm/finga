package local

import (
	"fmt"
	"math/rand/v2"
	"reflect"
	"regexp"
	"strconv"
	"testing"
	"time"

	"finga/internal/kernel"
	"finga/services/gateway/internal/model"
	"finga/services/world"
)

// enums take a valid value of their own, as an unknown one reads as the zero one.
var enums = map[reflect.Type]reflect.Value{
	reflect.TypeFor[world.LootKind]():      reflect.ValueOf(world.LootWaiting),
	reflect.TypeFor[world.QuestChange]():   reflect.ValueOf(world.QuestHandedIn),
	reflect.TypeFor[world.QuestStatus]():   reflect.ValueOf(world.QuestFinished),
	reflect.TypeFor[world.OfferState]():    reflect.ValueOf(world.OfferUnmet), // so the conditions it lacks go too
	reflect.TypeFor[world.ConditionKind](): reflect.ValueOf(world.ConditionSkill),
	reflect.TypeFor[world.MobPose]():       reflect.ValueOf(world.Down),
}

// filler sets every exported field it reaches to a value no other field has, so a field the conversion drops shows.
type filler struct{ n int64 }

func (f *filler) fill(v reflect.Value) {
	if e, ok := enums[v.Type()]; ok {
		v.Set(e)
		return
	}
	if v.Type() == reflect.TypeFor[time.Duration]() {
		f.n++
		v.SetInt(int64(time.Duration(f.n) * time.Millisecond))
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
	case reflect.Array: // an ID's bytes
		for i := range v.Len() {
			v.Index(i).SetUint(uint64(rand.IntN(256))) //nolint:gosec // distinct enough, not a secret
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

// leaves adds each value v holds to out by where it is, as text: an ID as its text and a duration in milliseconds; booleans and enums are left out.
func leaves(path string, v reflect.Value, out map[string]string) {
	switch {
	case (v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface) && v.IsNil():
		return
	case v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface:
		leaves(path, v.Elem(), out)
		return
	case enums[v.Type()].IsValid():
		return
	case v.Type() == reflect.TypeFor[time.Duration]():
		out[path] = strconv.FormatInt(time.Duration(v.Int()).Milliseconds(), 10)
		return
	case v.Kind() == reflect.Array:
		out[path] = v.Interface().(fmt.Stringer).String()
		return
	}
	switch v.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		out[path] = strconv.FormatInt(v.Int(), 10)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		out[path] = strconv.FormatUint(v.Uint(), 10)
	case reflect.Float32, reflect.Float64:
		out[path] = strconv.FormatFloat(v.Float(), 'g', -1, 64)
	case reflect.String:
		out[path] = v.String()
	case reflect.Slice:
		for i := range v.Len() {
			leaves(fmt.Sprintf("%s[%d]", path, i), v.Index(i), out)
		}
	case reflect.Map:
		for _, k := range v.MapKeys() {
			leaves(path+"{"+fmt.Sprint(k)+"}", k, out)
			leaves(path+"["+fmt.Sprint(k)+"]", v.MapIndex(k), out)
		}
	case reflect.Struct:
		for i := range v.NumField() {
			if v.Type().Field(i).IsExported() {
				leaves(path+"."+v.Type().Field(i).Name, v.Field(i), out)
			}
		}
	}
}

// dropped are the world's fields the gateway never took over gRPC either, by path without indices.
var dropped = map[string]string{
	"Snapshot.MapID":                      "the join or the teleport names the map",
	"Damage.Source.Mob":                   "a ref names a player or a mob, and this one a player",
	"Damage.Target.Mob":                   "a ref names a player or a mob, and this one a player",
	"Detached.Tick":                       "a detached character is on no map",
	"Snapshot.Players.State.Ground":       "movement state the client keeps itself",
	"Snapshot.Players.State.DropPlatform": "movement state the client keeps itself",
	"Snapshot.Players.State.DropTimer":    "movement state the client keeps itself",
	"Frame.Players.State.Ground":          "movement state the client keeps itself",
	"Frame.Players.State.DropPlatform":    "movement state the client keeps itself",
	"Frame.Players.State.DropTimer":       "movement state the client keeps itself",
}

var index = regexp.MustCompile(`\[[^]]*\]|\{[^}]*\}`)

// Each world event reaches the use cases as its push, with every value the gRPC path carried.
func TestEveryWorldEventKeepsItsValues(t *testing.T) {
	f := &filler{}
	for _, tt := range []struct {
		zero world.Event
		push any // the event's push, or the WorldEvent with none
	}{
		{world.Snapshot{}, model.Snapshot{}}, {world.Frame{}, model.Delta{}}, {world.Teleport{}, model.MapChange{}}, {world.Detached{}, model.Detached{}},
		{world.Damage{}, model.Damage{}}, {world.Died{}, model.Died{}}, {world.Respawned{}, model.Respawned{}}, {world.ExpGained{}, model.ExpGained{}},
		{world.LevelUp{}, model.LevelUp{}}, {world.Promoted{}, model.Promoted{}}, {world.StatsChanged{}, model.StatsChanged{}},
		{world.SkillUsed{}, model.SkillUsed{}}, {world.AttackUsed{}, model.AttackUsed{}}, {world.MobAttacked{}, model.MobAttacked{}},
		{world.FieldBoss{}, model.FieldBoss{}}, {world.FieldBossGone{}, model.FieldBossGone{}}, {world.Chat{}, model.ChatMessage{}},
		{world.Loot{}, model.Loot{}}, {world.InventoryChanged{}, model.InventoryChanged{}}, {world.NpcDialog{}, model.NpcDialog{}},
		{world.QuestChanged{}, model.QuestChanged{}},
	} {
		name := reflect.TypeOf(tt.zero).Name()
		t.Run(name, func(t *testing.T) {
			e := reflect.New(reflect.TypeOf(tt.zero)).Elem()
			f.fill(e)
			d := world.Delivery{Event: e.Interface().(world.Event)}
			if name != "Frame" { // a frame names its recipients instead
				f.fill(reflect.ValueOf(&d.Character).Elem())
				f.fill(reflect.ValueOf(&d.Conn).Elem())
				f.fill(reflect.ValueOf(&d.Tick).Elem())
			}
			var got []model.WorldEvent
			delivered(d, func(ev model.WorldEvent) { got = append(got, ev) })
			if len(got) == 0 {
				t.Fatal("no event")
			}
			for _, ev := range got {
				if p, ok := ev.(model.WorldDelivery); ok && reflect.TypeOf(p.Delivery.Push) != reflect.TypeOf(tt.push) {
					t.Fatalf("became %T, want %T", p.Delivery.Push, tt.push)
				} else if !ok && reflect.TypeOf(ev) != reflect.TypeOf(tt.push) {
					t.Fatalf("became %T, want %T", ev, tt.push)
				}
			}
			in, out := map[string]string{}, map[string]string{}
			leaves(name, e, in)
			if name != "Frame" {
				leaves(name+".Character", reflect.ValueOf(d.Character), in)
				leaves(name+".Conn", reflect.ValueOf(d.Conn), in)
				leaves(name+".Tick", reflect.ValueOf(d.Tick), in)
			}
			for k, ev := range got {
				leaves(fmt.Sprint(k), reflect.ValueOf(ev), out)
			}
			held := map[string]bool{}
			for _, v := range out {
				held[v] = true
			}
			for path, v := range in {
				where := index.ReplaceAllString(path, "")
				if _, ok := dropped[where]; ok || v == "" || v == "0" {
					continue
				}
				if !held[v] {
					t.Errorf("%s (%s) is lost", path, v)
				}
			}
		})
	}
}

// A ref names a mob when it names no player.
func TestADamageRefNamesItsMob(t *testing.T) {
	player := kernel.NewCharacterID()
	p, ok := worldPush(world.Damage{Source: world.EntityRef{Player: player}, Target: world.EntityRef{Mob: 7}, Amount: 3})
	if !ok {
		t.Fatal("no push")
	}
	if d := p.(model.Damage); d.Source != (model.Entity{Character: player.String()}) || d.Target != (model.Entity{Mob: 7}) {
		t.Fatalf("damage %+v", d)
	}
}

// An offer the player may take carries no conditions, as world.v1 left them unset.
func TestOnlyAnUnmetOfferNamesItsConditions(t *testing.T) {
	d := npcDialog(world.NpcDialog{NPC: "elder", Gives: []world.QuestOffer{
		{Quest: "rats", State: world.OfferAvailable, Unmet: world.Unmet{Level: 9}},
		{Quest: "wolves", State: world.OfferUnmet, Unmet: world.Unmet{Level: 9, Class: "warrior"}},
	}})
	if u := d.Gives[1].Unmet; d.Gives[0].Unmet.Level != 0 || u.Level != 9 || u.Class != "warrior" {
		t.Fatalf("offers %+v", d.Gives)
	}
}
