package chat_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"finga/internal/model"
	"finga/services/social/internal/chat"
)

type fakeParties struct {
	members map[model.CharacterID][]model.CharacterID
}

func (f fakeParties) Members(id model.CharacterID) []model.CharacterID { return f.members[id] }

func TestClean(t *testing.T) {
	for _, tt := range []struct{ in, want string }{
		{"hello there", "hello there"},
		{"  padded  ", "padded"},
		{"line\nbreak\ttab\x1b[31mred", "linebreaktab[31mred"}, // the escape goes; what follows is plain text
		{"<b>not html</b>", "<b>not html</b>"},                 // the client renders plain text; nothing to strip
		{"ünïcödé ✓", "ünïcödé ✓"},
		{"\x00\x7f", ""},
	} {
		if got := chat.Clean(tt.in); got != tt.want {
			t.Errorf("Clean(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestSend(t *testing.T) {
	ayla, robin, cass := model.NewCharacterID(), model.NewCharacterID(), model.NewCharacterID()
	parties := fakeParties{members: map[model.CharacterID][]model.CharacterID{ayla: {ayla, robin}, robin: {ayla, robin}}}
	now := time.Unix(1_700_000_000, 0)
	svc := chat.NewService(parties, chat.Options{Clock: func() time.Time { return now }})
	from := chat.Sender{ID: ayla, Name: "Ayla"}

	t.Run("a map line is cleaned and has no fixed recipients", func(t *testing.T) {
		msg, to, err := svc.Send(from, chat.Map, "  hi\nall ")
		if err != nil || to != nil || msg.Text != "hiall" || msg.Channel != chat.Map || msg.Sender != ayla || msg.SenderName != "Ayla" {
			t.Fatalf("%+v %v %v", msg, to, err)
		}
	})
	t.Run("a party line goes to every member, the sender included", func(t *testing.T) {
		_, to, err := svc.Send(from, chat.Party, "on me")
		if err != nil || len(to) != 2 || to[0] != ayla || to[1] != robin {
			t.Fatalf("%v %v", to, err)
		}
	})
	t.Run("a party line without a party", func(t *testing.T) {
		_, _, err := svc.Send(chat.Sender{ID: cass, Name: "Cass"}, chat.Party, "anyone?")
		if !errors.Is(err, chat.ErrNoParty) {
			t.Fatalf("err %v", err)
		}
	})
	t.Run("the fourth line inside a second is rate limited, and a second later one more fits", func(t *testing.T) {
		// Ayla sent two lines above; the burst is three.
		if _, _, err := svc.Send(from, chat.Map, "three"); err != nil {
			t.Fatalf("third line: %v", err)
		}
		_, _, err := svc.Send(from, chat.Map, "four")
		if !errors.Is(err, chat.ErrRate) {
			t.Fatalf("fourth line: %v", err)
		}
		now = now.Add(time.Second)
		if _, _, err := svc.Send(from, chat.Map, "five"); err != nil {
			t.Fatalf("after a second: %v", err)
		}
		if _, _, err := svc.Send(from, chat.Map, "six"); !errors.Is(err, chat.ErrRate) {
			t.Fatalf("sixth line: %v", err)
		}
		// Another sender has their own bucket.
		if _, _, err := svc.Send(chat.Sender{ID: robin, Name: "Robin"}, chat.Map, "me too"); err != nil {
			t.Fatalf("another sender: %v", err)
		}
	})
	now = now.Add(time.Minute)
	t.Run("length and emptiness are checked after cleaning, and never charge the limit", func(t *testing.T) {
		if _, _, err := svc.Send(from, chat.Map, strings.Repeat("é", 120)); err != nil {
			t.Fatalf("120 characters: %v", err)
		}
		if _, _, err := svc.Send(from, chat.Map, strings.Repeat("a", 121)); !errors.Is(err, chat.ErrTooLong) {
			t.Fatalf("121 characters: %v", err)
		}
		if _, _, err := svc.Send(from, chat.Map, " \n\t "); !errors.Is(err, chat.ErrEmpty) {
			t.Fatalf("blank: %v", err)
		}
		if _, _, err := svc.Send(from, chat.Channel("global"), "hi"); !errors.Is(err, chat.ErrChannel) {
			t.Fatalf("unknown channel: %v", err)
		}
		for range 2 { // the two refused lines cost nothing: two more fit the burst
			if _, _, err := svc.Send(from, chat.Map, "ok"); err != nil {
				t.Fatalf("after refused lines: %v", err)
			}
		}
	})
	t.Run("without a party service the party channel is unavailable", func(t *testing.T) {
		solo := chat.NewService(nil, chat.Options{})
		if _, _, err := solo.Send(from, chat.Party, "hi"); !errors.Is(err, chat.ErrNoParties) {
			t.Fatalf("err %v", err)
		}
	})
}
