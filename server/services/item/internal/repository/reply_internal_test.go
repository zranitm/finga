package repository

import (
	"testing"

	"finga/internal/gamedata"
	"finga/internal/kernel"
	"finga/services/item/internal/application"
	"finga/services/item/internal/model"
)

// A reply as the first item service stored it; a retry replays rows like it for as long as keys live.
const storedReply = `{"changed":[{"tab":"use","slot":3,"item":{"ID":"0190a0f4-1c2d-7e3f-8a4b-5c6d7e8f9a0b","TemplateID":"red_potion","Owner":"0190a0f4-1c2d-7e3f-8a4b-5c6d7e8f9012","Tab":"use","Slot":3,"Quantity":5}},{"tab":"etc","slot":0}]}`

func storedResult(t *testing.T) application.Result {
	t.Helper()
	id, err := kernel.ParseItemID("0190a0f4-1c2d-7e3f-8a4b-5c6d7e8f9a0b")
	if err != nil {
		t.Fatal(err)
	}
	owner, err := kernel.ParseCharacterID("0190a0f4-1c2d-7e3f-8a4b-5c6d7e8f9012")
	if err != nil {
		t.Fatal(err)
	}
	return application.Result{Changed: []model.Slot{
		{Tab: gamedata.Use, Slot: 3, Item: &model.Item{ID: id, TemplateID: "red_potion", Owner: owner, Tab: gamedata.Use, Slot: 3, Quantity: 5}},
		{Tab: gamedata.Etc, Slot: 0},
	}}
}

func TestAStoredReplyDecodes(t *testing.T) {
	got, err := decodeReply([]byte(storedReply))
	if err != nil {
		t.Fatal(err)
	}
	want := storedResult(t)
	if len(got.Changed) != 2 || got.Changed[1] != want.Changed[1] || got.Changed[0].Tab != want.Changed[0].Tab || got.Changed[0].Slot != want.Changed[0].Slot {
		t.Fatalf("slots %+v", got.Changed)
	}
	if it := got.Changed[0].Item; it == nil || *it != *want.Changed[0].Item {
		t.Fatalf("item %+v, want %+v", it, want.Changed[0].Item)
	}
}

func TestAReplyIsStoredInTheSameFormat(t *testing.T) {
	got, err := encodeReply(storedResult(t))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != storedReply {
		t.Fatalf("stored reply changed: rows already stored would no longer read the same\ngot  %s\nwant %s", got, storedReply)
	}
}

func TestAnEmptyReply(t *testing.T) {
	got, err := encodeReply(application.Result{})
	if err != nil || string(got) != `{"changed":null}` {
		t.Fatalf("encoded %s, %v", got, err)
	}
	for _, raw := range []string{`{"changed":null}`, ``} {
		if res, err := decodeReply([]byte(raw)); err != nil || res.Changed != nil {
			t.Fatalf("decoded %q as %+v, %v", raw, res, err)
		}
	}
}

func TestAReplyThatDoesNotDecodeFails(t *testing.T) {
	for _, raw := range []string{`{"changed":[{"tab":"use","slot":1,"item":{"ID":"nope"}}]}`, `{"changed":[{"tab":"use","slot":1,"item":{"ID":"0190a0f4-1c2d-7e3f-8a4b-5c6d7e8f9a0b","Owner":"nope"}}]}`, `[`} {
		if _, err := decodeReply([]byte(raw)); err == nil {
			t.Fatalf("decoded %s", raw)
		}
	}
}

// A reply keeps the revision its change took; one stored before revisions has none, and replays as revision 0.
func TestAReplyKeepsItsRevision(t *testing.T) {
	res := storedResult(t)
	res.Revision = 42
	raw, err := encodeReply(res)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := decodeReply(raw); err != nil || got.Revision != 42 || len(got.Changed) != 2 {
		t.Fatalf("decoded %s as %+v, %v; want revision 42 and both slots", raw, got, err)
	}
	if old, err := decodeReply([]byte(storedReply)); err != nil || old.Revision != 0 {
		t.Fatalf("a reply stored before revisions decoded as %+v, %v; want revision 0", old, err)
	}
}
