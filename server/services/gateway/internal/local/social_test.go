package local

import (
	"context"
	"log/slog"
	"reflect"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"finga/internal/host"
	"finga/internal/kernel"
	"finga/pkg/copytest"
	"finga/pkg/hub"
	"finga/pkg/panics"
	"finga/services/gateway/internal/model"
	"finga/services/social"
)

// leaving counts the social module's GatewayLeft calls.
type leaving struct {
	*social.Module
	mu   sync.Mutex
	left []string
}

func (l *leaving) GatewayLeft(gateway string) {
	l.mu.Lock()
	l.left = append(l.left, gateway)
	l.mu.Unlock()
	l.Module.GatewayLeft(gateway)
}

// newSocial is the social module on a host never run, over the game data.
func newSocial(t *testing.T) *social.Module {
	t.Helper()
	log := slog.New(slog.DiscardHandler)
	h, err := host.New(host.Options{Name: "finga", Settings: host.Settings{DataDir: "../../../../data"}, Logger: log, Registry: prometheus.NewRegistry()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(h.Close)
	data, err := h.GameData()
	if err != nil {
		t.Fatal(err)
	}
	m, err := h.Module("social")
	if err != nil {
		t.Fatal(err)
	}
	s, err := social.New(t.Context(), social.Deps{Module: m, Data: data}) // its retention never runs on a host never run
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// A stream that breaks is followed again at once, and the gateway registers its players again: the module's sweep of the gone gateway finds it back, and every party stays.
func TestABrokenSocialStreamFollowedAgainKeepsItsParties(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		mod := &leaving{Module: newSocial(t)}
		s := NewSocial(mod, slog.New(slog.DiscardHandler), panics.NewRecorder(slog.New(slog.DiscardHandler), nil))
		ann := model.Player{ID: kernel.NewCharacterID(), Name: "Ann", Class: "warrior", Level: 3, HP: 90, MaxHP: 100}
		bo := model.Player{ID: kernel.NewCharacterID(), Name: "Bo", Class: "warrior", Level: 3, HP: 90, MaxHP: 100}
		register := func() {
			for k, p := range []model.Player{ann, bo} {
				if err := s.Online(context.Background(), "gw", "conn-"+p.Name, uint64(k+1), p, "forest"); err != nil {
					t.Error(err)
				}
			}
		}
		var mu sync.Mutex
		subscribed, invites, parties := 0, []string{}, map[kernel.CharacterID]*model.PartyView{}
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		go s.FollowDeliveries(ctx, "gw", func(e model.SocialEvent) {
			mu.Lock()
			defer mu.Unlock()
			if _, ok := e.(model.SocialSubscribed); ok {
				subscribed++
				go register() // as the push flow does, off the stream's goroutine
			}
			d, _ := e.(model.SocialDelivery)
			if p, ok := d.Delivery.Push.(model.PartyInvited); ok {
				invites = append(invites, p.InviteID)
			}
			if p, ok := d.Delivery.Push.(model.PartyState); ok {
				parties[d.Character] = p.Party
			}
		})
		synctest.Wait()
		if err := s.Invite(ctx, ann.ID, bo.ID); err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		if err := s.Respond(ctx, bo.ID, invites[0], true); err != nil {
			t.Fatal(err)
		}
		synctest.Wait()

		mod.Subscribe("gw") // a newer subscription under the gateway's ID closes the stream, as a full queue does
		synctest.Wait()
		time.Sleep(time.Second) // past the follower's longest backoff
		synctest.Wait()
		time.Sleep(time.Minute) // past the module's grace, when its sweep of the gateway looks
		synctest.Wait()

		mu.Lock()
		defer mu.Unlock()
		if subscribed != 2 || len(mod.left) != 1 || mod.left[0] != "gw" {
			t.Fatalf("subscribed %d times, the gateway left %v; want a second subscription after one GatewayLeft", subscribed, mod.left)
		}
		for _, id := range []kernel.CharacterID{ann.ID, bo.ID} {
			p := parties[id]
			if p == nil || len(p.Members) != 2 || !p.Members[0].Online || !p.Members[1].Online {
				t.Fatalf("%s's party %+v; want both members, online", id, p)
			}
		}
		var members int
		if err := mod.Memberships(ctx, func(_ social.PartyID, in []kernel.CharacterID) { members += len(in) }); err != nil || members != 2 {
			t.Fatalf("the module holds %d members in parties (%v)", members, err)
		}
		cancel()
	})
}

// socialModule records Online's presence and Chat's line, and answers Chat with msg.
type socialModule struct {
	presence social.Presence
	line     social.Line
	msg      social.Message
}

func (m *socialModule) Online(_ context.Context, p social.Presence) error {
	m.presence = p
	return nil
}

func (m *socialModule) Offline(context.Context, kernel.CharacterID, string) error { return nil }

func (m *socialModule) Invite(context.Context, kernel.CharacterID, kernel.CharacterID) (social.InviteID, error) {
	return social.InviteID{}, nil
}

func (m *socialModule) Respond(context.Context, kernel.CharacterID, social.InviteID, bool) error {
	return nil
}

func (m *socialModule) LeaveParty(context.Context, kernel.CharacterID) error { return nil }

func (m *socialModule) Kick(context.Context, kernel.CharacterID, kernel.CharacterID) error {
	return nil
}

func (m *socialModule) Promote(context.Context, kernel.CharacterID, kernel.CharacterID) error {
	return nil
}

func (m *socialModule) Chat(_ context.Context, l social.Line) (social.Message, error) {
	m.line = l
	return m.msg, nil
}

func (m *socialModule) Subscribe(string) *hub.Sub[social.Delivery] { return nil }

func (m *socialModule) GatewayLeft(string) {}

// A player online reaches the social module with every field it registers.
func TestOnlineKeepsItsFields(t *testing.T) {
	m := &socialModule{}
	s := NewSocial(m, slog.New(slog.DiscardHandler), nil)
	type online struct {
		Gateway, Conn string
		JoinSeq       uint64
		Player        model.Player
		MapID         string
	}
	match := copytest.Match{Renames: map[string]string{"Conn": "Connection", "Player": "Member", "MapID": "Member.MapID"}}
	got := copytest.Diff(match, func(o online) social.Presence {
		if err := s.Online(t.Context(), o.Gateway, o.Conn, o.JoinSeq, o.Player, o.MapID); err != nil {
			t.Fatal(err)
		}
		return m.presence
	})
	if len(got) > 0 {
		t.Fatal(got)
	}
}

// A chat line reaches the social module with its sender and text, and the cleaned line comes back with its channel and text.
func TestAChatLineKeepsItsFields(t *testing.T) {
	m := &socialModule{}
	s := NewSocial(m, slog.New(slog.DiscardHandler), nil)
	type chat struct {
		Sender  model.Player
		Channel model.ChatChannel
		Text    string
	}
	channels := map[reflect.Type]any{reflect.TypeFor[model.ChatChannel](): model.ChatParty, reflect.TypeFor[social.Channel](): social.ChannelParty}
	sent := copytest.Match{
		Renames: map[string]string{"Sender.ID": "From", "Sender.Name": "FromName"},
		Skip: map[string]string{
			"Sender.Class": "a line names its sender by ID and name", "Sender.Level": "a line names its sender by ID and name",
			"Sender.HP": "a line names its sender by ID and name", "Sender.MaxHP": "a line names its sender by ID and name",
		},
		Enums: channels,
	}
	if got := copytest.Diff(sent, func(c chat) social.Line {
		_, _ = s.Chat(t.Context(), c.Sender, c.Channel, c.Text)
		return m.line
	}); len(got) > 0 {
		t.Errorf("sent: %v", got)
	}
	back := copytest.Match{
		Skip:  map[string]string{"Sender": "the line goes back to its own sender", "SenderName": "the line goes back to its own sender"},
		Enums: channels,
	}
	if got := copytest.Diff(back, func(msg social.Message) model.ChatLine {
		m.msg = msg
		line, _ := s.Chat(t.Context(), model.Player{}, model.ChatParty, "")
		return line
	}); len(got) > 0 {
		t.Errorf("answered: %v", got)
	}
	for ch, want := range map[model.ChatChannel]social.Channel{model.ChatMap: social.ChannelMap, model.ChatParty: social.ChannelParty, model.ChatChannel(9): ""} {
		if _, _ = s.Chat(t.Context(), model.Player{}, ch, ""); m.line.Channel != want {
			t.Errorf("channel %d sent as %q, want %q", ch, m.line.Channel, want)
		}
	}
	for ch, want := range map[social.Channel]model.ChatChannel{social.ChannelMap: model.ChatMap, social.ChannelParty: model.ChatParty} {
		m.msg = social.Message{Channel: ch}
		if line, _ := s.Chat(t.Context(), model.Player{}, model.ChatMap, ""); line.Channel != want {
			t.Errorf("a %s line answered as channel %d, want %d", ch, line.Channel, want)
		}
	}
}

// socialEventOf is the event a delivery becomes.
func socialEventOf[D social.Delivery](t *testing.T) func(D) model.SocialEvent {
	return func(d D) model.SocialEvent {
		e, ok := socialEvent(d)
		if !ok {
			t.Fatalf("%T: no event", d)
		}
		return e
	}
}

// Each social delivery reaches the gateway with every field, the player's to that player and an announcement to every player.
func TestEachSocialDeliveryKeepsItsFields(t *testing.T) {
	tick := map[string]string{"Delivery.Tick": "social pushes carry no map tick"}
	party := copytest.Match{
		Renames:  map[string]string{"To": "Character", "Party": "Delivery.Push.Party", "Party.Members.ID": "Delivery.Push.Party.Members.CharacterID"},
		Computed: tick,
	}
	if got := copytest.Diff(party, socialEventOf[social.PartyState](t)); len(got) > 0 {
		t.Errorf("PartyState: %v", got)
	}
	invited := copytest.Match{
		Renames:  map[string]string{"To": "Character", "Invite": "Delivery.Push.InviteID", "From": "Delivery.Push.From", "FromName": "Delivery.Push.FromName"},
		Computed: tick,
	}
	if got := copytest.Diff(invited, socialEventOf[social.Invited](t)); len(got) > 0 {
		t.Errorf("Invited: %v", got)
	}
	line := copytest.Match{
		Renames:  map[string]string{"To": "Character", "Sender": "Delivery.Push.Sender", "SenderName": "Delivery.Push.SenderName", "Text": "Delivery.Push.Text"},
		Computed: map[string]string{"Delivery.Tick": tick["Delivery.Tick"], "Delivery.Push.Channel": "a party line, checked below"},
	}
	if got := copytest.Diff(line, socialEventOf[social.ChatLine](t)); len(got) > 0 {
		t.Errorf("ChatLine: %v", got)
	}
	if e, _ := socialEvent(social.ChatLine{}); e.(model.SocialDelivery).Delivery.Push.(model.ChatMessage).Channel != model.ChatParty {
		t.Errorf("a party line became %+v", e)
	}
	boss := copytest.Match{Renames: map[string]string{"Boss": "Delivery.Push.Boss", "MapID": "Delivery.Push.MapID"}, Computed: tick}
	if got := copytest.Diff(boss, socialEventOf[social.BossAppeared](t)); len(got) > 0 {
		t.Errorf("BossAppeared: %v", got)
	}
	if e, _ := socialEvent(social.BossAppeared{}); reflect.TypeOf(e) != reflect.TypeFor[model.SocialBroadcast]() {
		t.Errorf("an announcement became %T", e)
	}
	if e, _ := socialEvent(social.PartyState{}); e.(model.SocialDelivery).Delivery.Push.(model.PartyState).Party != nil {
		t.Errorf("no party became %+v", e)
	}
}
