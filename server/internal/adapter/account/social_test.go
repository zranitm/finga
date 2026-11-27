package account

import (
	"context"
	"log/slog"
	"reflect"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"finga/internal/application/social"
	model "finga/internal/model/account"
	repository "finga/internal/repository/social"
	"finga/internal/shared/gamedata/gamedatatest"
	"finga/internal/shared/host"
	"finga/internal/shared/kernel"
	"finga/pkg/copytest"
	"finga/pkg/hub"
	"finga/pkg/panics"
)

// leaving counts the social module's NodeLeft calls.
type leaving struct {
	*social.Module
	mu   sync.Mutex
	left []string
}

func (l *leaving) NodeLeft(node string) {
	l.mu.Lock()
	l.left = append(l.left, node)
	l.mu.Unlock()
	l.Module.NodeLeft(node)
}

// newSocial is the social module on a host never run, over the game data.
func newSocial(t *testing.T) *social.Module {
	t.Helper()
	log := slog.New(slog.DiscardHandler)
	h, err := host.New(host.Options{Name: "finga", Settings: host.Settings{DataDir: gamedatatest.ShippedDir(t)}, Logger: log, Registry: prometheus.NewRegistry()})
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
	s, err := social.New(social.Deps{Module: m, Data: data, Store: repository.NewMemory()}) // its retention never runs on a host never run
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// A stream that breaks is followed again at once, and the server registers its players again: the module's sweep of the gone node finds it back, and every party stays.
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
		subscribed, invites, parties := 0, []string{}, map[kernel.CharacterID]*social.Party{}
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
			if p, ok := d.Delivery.Event.(social.Invited); ok {
				invites = append(invites, p.Invite.String())
			}
			if p, ok := d.Delivery.Event.(social.PartyState); ok {
				parties[d.Character] = p.Party
			}
		})
		synctest.Wait()
		if _, err := mod.Invite(ctx, ann.ID, bo.ID); err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		invite, err := social.ParseInviteID(invites[0])
		if err != nil {
			t.Fatal(err)
		}
		if err := mod.Respond(ctx, bo.ID, invite, true); err != nil {
			t.Fatal(err)
		}
		synctest.Wait()

		mod.Subscribe("gw") // a newer subscription under the server's ID closes the stream, as a full queue does
		synctest.Wait()
		time.Sleep(time.Second) // past the follower's longest backoff
		synctest.Wait()
		time.Sleep(time.Minute) // past the module's grace, when its sweep of the server looks
		synctest.Wait()

		mu.Lock()
		defer mu.Unlock()
		if subscribed != 2 || len(mod.left) != 1 || mod.left[0] != "gw" {
			t.Fatalf("subscribed %d times, the server left %v; want a second subscription after one NodeLeft", subscribed, mod.left)
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

func (m *socialModule) Chat(_ context.Context, l social.Line) (social.Message, error) {
	m.line = l
	return m.msg, nil
}

func (m *socialModule) Subscribe(string) *hub.Sub[social.Delivery] { return nil }

func (m *socialModule) NodeLeft(string) {}

// A player online reaches the social module with every field it registers.
func TestOnlineKeepsItsFields(t *testing.T) {
	m := &socialModule{}
	s := NewSocial(m, slog.New(slog.DiscardHandler), nil)
	type online struct {
		Node, Conn string
		JoinSeq    uint64
		Player     model.Player
		MapID      string
	}
	match := copytest.Match{
		Renames:  map[string]string{"Conn": "Connection", "Player": "Member", "MapID": "Member.MapID"},
		Computed: map[string]string{"Member.Channel": "the world's report of the member names it, and social keeps a report over an older login's values"},
	}
	got := copytest.Diff(match, func(o online) social.Presence {
		if err := s.Online(t.Context(), o.Node, o.Conn, o.JoinSeq, o.Player, o.MapID); err != nil {
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

// Each social delivery reaches the player it names as the module sent it; one the flows have no name for is no event.
func TestEachSocialDeliveryIsHandedOnAsTheModuleSentIt(t *testing.T) {
	to := kernel.NewCharacterID()
	for _, in := range []social.Delivery{
		social.PartyState{To: to, Party: &social.Party{Leader: to}},
		social.PartyState{To: to},
		social.Invited{To: to, From: kernel.NewCharacterID(), FromName: "Robin"},
		social.ChatLine{To: to, Sender: kernel.NewCharacterID(), SenderName: "Robin", Text: "psst"},
	} {
		ev, ok := socialEvent(in)
		d, is := ev.(model.SocialDelivery)
		if !ok || !is || d.Character != to || d.Delivery.Tick != 0 || d.Delivery.Starts || d.Delivery.MapID != "" || !reflect.DeepEqual(d.Delivery.Event, in) {
			t.Errorf("%T became %+v", in, ev)
		}
	}
}
