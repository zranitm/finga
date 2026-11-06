package repository

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"finga/internal/gamedata"
	"finga/internal/kernel"
	"finga/pkg/outbox"
	"finga/services/item/internal/model"
)

type capture struct{ events []outbox.Event }

func (c *capture) Add(_ context.Context, events ...outbox.Event) error {
	c.events = append(c.events, events...)
	return nil
}

// A notice goes out on the item topic, keyed by its item, in the shape other services read.
func TestANoticeGoesOutAsAnItemTopicEvent(t *testing.T) {
	id, err := kernel.ParseItemID("0190a0f4-1c2d-7e3f-8a4b-5c6d7e8f9a0b")
	if err != nil {
		t.Fatal(err)
	}
	owner := kernel.CharacterID(uuid.MustParse("0190a0f4-1c2d-7e3f-8a4b-5c6d7e8f9012"))
	drop := uuid.MustParse("0190a0f4-1c2d-7e3f-8a4b-5c6d7e8f9abc")
	it := model.Item{ID: id, TemplateID: "red_potion", Owner: owner, Tab: gamedata.Tab("use"), Slot: 3, Quantity: 9}
	at := time.Date(2026, 11, 6, 9, 0, 0, 0, time.UTC)
	const fields = `"item_id":"0190a0f4-1c2d-7e3f-8a4b-5c6d7e8f9a0b","template_id":"red_potion","owner_id":"0190a0f4-1c2d-7e3f-8a4b-5c6d7e8f9012"`
	tests := []struct {
		name    string
		notice  model.Notice
		typ     string
		payload string
	}{
		{"a loot names its drop", model.Notice{Kind: model.NoticeLooted, Item: it, Quantity: 5, Cause: model.Cause{Type: model.CauseDrop, ID: drop}, OccurredAt: at},
			"ItemLooted", `{` + fields + `,"quantity":5,"tab":"use","slot":3,"cause":"drop","cause_id":"0190a0f4-1c2d-7e3f-8a4b-5c6d7e8f9abc"}`},
		{"a grant without a cause ID leaves it out", model.Notice{Kind: model.NoticeGranted, Item: it, Quantity: 2, Cause: model.Cause{Type: model.CauseStarter}, OccurredAt: at},
			"ItemGranted", `{` + fields + `,"quantity":2,"tab":"use","slot":3,"cause":"starter"}`},
		{"a discard carries its negative change", model.Notice{Kind: model.NoticeDiscarded, Item: it, Quantity: -4, Cause: model.Cause{Type: model.CausePlayer}, OccurredAt: at},
			"ItemDiscarded", `{` + fields + `,"quantity":-4,"tab":"use","slot":3,"cause":"player"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := &capture{}
			if err := (notices{w: w}).Send(context.Background(), tt.notice); err != nil {
				t.Fatal(err)
			}
			if len(w.events) != 1 {
				t.Fatalf("%d events, want 1", len(w.events))
			}
			got := w.events[0]
			if got.Topic != "item" || got.Key != id.String() || got.Type != tt.typ || !got.OccurredAt.Equal(at) || string(got.Payload) != tt.payload {
				t.Fatalf("got %+v with payload %s", got, got.Payload)
			}
		})
	}
	t.Run("a kind without an event type writes nothing", func(t *testing.T) {
		w := &capture{}
		if err := (notices{w: w}).Send(context.Background(), model.Notice{Kind: "lent", Item: it}); err == nil || len(w.events) != 0 {
			t.Fatalf("err %v, %d events", err, len(w.events))
		}
	})
}
