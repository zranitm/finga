package social

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/otel/trace/noop"

	"finga/internal/facade"
	"finga/internal/kernel"
	"finga/pkg/panics"
	"finga/services/social/internal/model"
)

// fakeParties records what the facade passed in and answers err when set.
type fakeParties struct {
	got []any
	err error
}

var (
	ann, bo = kernel.NewCharacterID(), kernel.NewCharacterID()
	invite  = model.NewInviteID()
	party   = model.NewPartyID()
)

func (f *fakeParties) record(args ...any) error { f.got = args; return f.err }

func (f *fakeParties) Online(_ context.Context, m model.Member) error { return f.record(m) }
func (f *fakeParties) Offline(_ context.Context, id kernel.CharacterID, conn string) error {
	return f.record(id, conn)
}

func (f *fakeParties) Invite(_ context.Context, from, to kernel.CharacterID) (model.Invite, error) {
	return model.Invite{ID: invite, From: from, To: to}, f.record(from, to)
}

func (f *fakeParties) Respond(_ context.Context, who kernel.CharacterID, id model.InviteID, accept bool) error {
	return f.record(who, id, accept)
}
func (f *fakeParties) Leave(_ context.Context, who kernel.CharacterID) error { return f.record(who) }
func (f *fakeParties) Kick(_ context.Context, leader, target kernel.CharacterID) error {
	return f.record(leader, target)
}

func (f *fakeParties) Promote(_ context.Context, leader, target kernel.CharacterID) error {
	return f.record(leader, target)
}

func (f *fakeParties) Update(_ context.Context, id kernel.CharacterID, mapID string, class kernel.Class, level, hp, maxHP int) error {
	return f.record(id, mapID, class, level, hp, maxHP)
}

func (f *fakeParties) Memberships(_ context.Context, visit func(model.PartyID, []kernel.CharacterID)) error {
	visit(party, []kernel.CharacterID{ann, bo})
	return f.record()
}

// fakeChat records the line it was sent and answers it back, cleaned, to Ann and Bo.
type fakeChat struct {
	got []any
	err error
}

func (f *fakeChat) Send(_ context.Context, from model.Sender, channel model.Channel, text string) (model.Message, []kernel.CharacterID, error) {
	f.got = []any{from, channel, text}
	return model.Message{Channel: channel, Sender: from.ID, SenderName: from.Name, Text: "hi"}, []kernel.CharacterID{ann, bo}, f.err
}

func newModule(t *testing.T, p parties, c chat) *Module {
	t.Helper()
	calls, err := facade.New(facade.Options{Module: "social", Traces: noop.NewTracerProvider(), Registry: prometheus.NewRegistry(), Panics: panics.NewRecorder(slog.New(slog.DiscardHandler), nil), Errors: errorTable})
	if err != nil {
		t.Fatal(err)
	}
	return &Module{calls: calls, parties: p, chat: c}
}

func TestEachCallConvertsToAndFromTheModel(t *testing.T) {
	ctx := t.Context()
	p, c := &fakeParties{}, &fakeChat{}
	m := newModule(t, p, c)
	same := func(t *testing.T, err error, got, want []any) {
		t.Helper()
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("got %v, want %v, err %v", got, want, err)
		}
	}
	t.Run("Online", func(t *testing.T) {
		err := m.Online(ctx, Presence{Member: Member{ID: ann, Name: "Ann", Class: "mage", Level: 3, HP: 90, MaxHP: 100, MapID: "forest"}, Gateway: "gw", Connection: "conn-1", JoinSeq: 7})
		want := model.Member{ID: ann, Name: "Ann", Class: "mage", Level: 3, HP: 90, MaxHP: 100, MapID: "forest", Gateway: "gw", Connection: "conn-1", JoinSeq: 7}
		same(t, err, p.got, []any{want})
	})
	t.Run("Offline", func(t *testing.T) { same(t, m.Offline(ctx, ann, "conn-1"), p.got, []any{ann, "conn-1"}) })
	t.Run("Invite", func(t *testing.T) {
		id, err := m.Invite(ctx, ann, bo)
		same(t, err, append(p.got, id), []any{ann, bo, invite})
	})
	t.Run("Respond", func(t *testing.T) { same(t, m.Respond(ctx, bo, invite, true), p.got, []any{bo, invite, true}) })
	t.Run("LeaveParty", func(t *testing.T) { same(t, m.LeaveParty(ctx, bo), p.got, []any{bo}) })
	t.Run("Kick", func(t *testing.T) { same(t, m.Kick(ctx, ann, bo), p.got, []any{ann, bo}) })
	t.Run("Promote", func(t *testing.T) { same(t, m.Promote(ctx, ann, bo), p.got, []any{ann, bo}) })
	t.Run("UpdateMember", func(t *testing.T) {
		err := m.UpdateMember(ctx, MemberUpdate{ID: ann, MapID: "town", Class: "mage", Level: 4, HP: 80, MaxHP: 110})
		same(t, err, p.got, []any{ann, "town", kernel.Class("mage"), 4, 80, 110})
	})
	t.Run("Memberships", func(t *testing.T) {
		var got []any
		err := m.Memberships(ctx, func(id PartyID, members []kernel.CharacterID) { got = append(got, id, members) })
		same(t, err, got, []any{party, []kernel.CharacterID{ann, bo}})
	})
	t.Run("Chat", func(t *testing.T) {
		msg, err := m.Chat(ctx, Line{From: ann, FromName: "Ann", Channel: ChannelParty, Text: " hi "})
		want := Message{Channel: ChannelParty, Sender: ann, SenderName: "Ann", Text: "hi", To: []kernel.CharacterID{ann, bo}}
		same(t, err, append(c.got, msg), []any{model.Sender{ID: ann, Name: "Ann"}, model.ChannelParty, " hi ", want})
	})
}

// Callers find the kernel error with errors.Is, and the logs keep the model's cause.
func TestEveryModelErrorReachesCallersAsItsKernelError(t *testing.T) {
	for _, row := range errorTable {
		t.Run(row.Model.Error(), func(t *testing.T) {
			m := newModule(t, &fakeParties{err: fmt.Errorf("kicking: %w", row.Model)}, &fakeChat{})
			if err := m.Kick(t.Context(), ann, bo); !errors.Is(err, row.Kernel) || !errors.Is(err, row.Model) {
				t.Fatalf("got %v, want %v wrapping %v", err, row.Kernel, row.Model)
			}
		})
	}
	m := newModule(t, &fakeParties{}, &fakeChat{err: model.ErrRate})
	if _, err := m.Chat(t.Context(), Line{From: ann, Channel: ChannelMap, Text: "hi"}); !errors.Is(err, kernel.ErrChatRateLimited) {
		t.Fatalf("a line past the rate: got %v", err)
	}
}
