package application_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"finga/internal/kernel"
	"finga/services/social/internal/application"
	"finga/services/social/internal/model"
)

type fakeParties struct {
	members map[kernel.CharacterID][]kernel.CharacterID
}

func (f fakeParties) Members(id kernel.CharacterID) []kernel.CharacterID { return f.members[id] }

func TestClean(t *testing.T) {
	for _, tt := range []struct{ in, want string }{
		{"hello there", "hello there"},
		{"  padded  ", "padded"},
		{"line\nbreak\ttab\x1b[31mred", "linebreaktab[31mred"}, // the escape goes; what follows is plain text
		{"<b>not html</b>", "<b>not html</b>"},                 // the client renders plain text; nothing to strip
		{"ünïcödé ✓", "ünïcödé ✓"},
		{"\x00\x7f", ""},
	} {
		if got := model.CleanLine(tt.in); got != tt.want {
			t.Errorf("Clean(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestSend(t *testing.T) {
	ayla, robin, cass := kernel.NewCharacterID(), kernel.NewCharacterID(), kernel.NewCharacterID()
	parties := fakeParties{members: map[kernel.CharacterID][]kernel.CharacterID{ayla: {ayla, robin}, robin: {ayla, robin}}}
	now := time.Unix(1_700_000_000, 0)
	svc := application.NewChatService(parties, application.ChatOptions{Clock: func() time.Time { return now }})
	from := model.Sender{ID: ayla, Name: "Ayla"}

	t.Run("a map line is cleaned and has no fixed recipients", func(t *testing.T) {
		msg, to, err := svc.Send(from, model.ChannelMap, "  hi\nall ")
		if err != nil || to != nil || msg.Text != "hiall" || msg.Channel != model.ChannelMap || msg.Sender != ayla || msg.SenderName != "Ayla" {
			t.Fatalf("%+v %v %v", msg, to, err)
		}
	})
	t.Run("a party line goes to every member, the sender included", func(t *testing.T) {
		_, to, err := svc.Send(from, model.ChannelParty, "on me")
		if err != nil || len(to) != 2 || to[0] != ayla || to[1] != robin {
			t.Fatalf("%v %v", to, err)
		}
	})
	t.Run("a party line without a party", func(t *testing.T) {
		_, _, err := svc.Send(model.Sender{ID: cass, Name: "Cass"}, model.ChannelParty, "anyone?")
		if !errors.Is(err, model.ErrChatNoParty) {
			t.Fatalf("err %v", err)
		}
	})
	t.Run("the fourth line inside a second is rate limited, and a second later one more fits", func(t *testing.T) {
		// Ayla sent two lines above; the burst is three.
		if _, _, err := svc.Send(from, model.ChannelMap, "three"); err != nil {
			t.Fatalf("third line: %v", err)
		}
		_, _, err := svc.Send(from, model.ChannelMap, "four")
		if !errors.Is(err, model.ErrRate) {
			t.Fatalf("fourth line: %v", err)
		}
		now = now.Add(time.Second)
		if _, _, err := svc.Send(from, model.ChannelMap, "five"); err != nil {
			t.Fatalf("after a second: %v", err)
		}
		if _, _, err := svc.Send(from, model.ChannelMap, "six"); !errors.Is(err, model.ErrRate) {
			t.Fatalf("sixth line: %v", err)
		}
		// Another sender has their own bucket.
		if _, _, err := svc.Send(model.Sender{ID: robin, Name: "Robin"}, model.ChannelMap, "me too"); err != nil {
			t.Fatalf("another sender: %v", err)
		}
	})
	now = now.Add(time.Minute)
	t.Run("length and emptiness are checked after cleaning, and never charge the limit", func(t *testing.T) {
		if _, _, err := svc.Send(from, model.ChannelMap, strings.Repeat("é", 120)); err != nil {
			t.Fatalf("120 characters: %v", err)
		}
		if _, _, err := svc.Send(from, model.ChannelMap, strings.Repeat("a", 121)); !errors.Is(err, model.ErrTooLong) {
			t.Fatalf("121 characters: %v", err)
		}
		if _, _, err := svc.Send(from, model.ChannelMap, " \n\t "); !errors.Is(err, model.ErrEmpty) {
			t.Fatalf("blank: %v", err)
		}
		if _, _, err := svc.Send(from, model.Channel("global"), "hi"); !errors.Is(err, model.ErrChannel) {
			t.Fatalf("unknown channel: %v", err)
		}
		for range 2 { // the two refused lines cost nothing: two more fit the burst
			if _, _, err := svc.Send(from, model.ChannelMap, "ok"); err != nil {
				t.Fatalf("after refused lines: %v", err)
			}
		}
	})
	t.Run("without a party service the party channel is unavailable", func(t *testing.T) {
		solo := application.NewChatService(nil, application.ChatOptions{})
		if _, _, err := solo.Send(from, model.ChannelParty, "hi"); !errors.Is(err, model.ErrNoParties) {
			t.Fatalf("err %v", err)
		}
	})
}
