package e2e_test

import (
	"context"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/coder/websocket"

	"finga/config"
	"finga/internal/dependencies"
	"finga/internal/shared/adminapi"
	"finga/internal/shared/host"
	"finga/pkg/uuidv7"
	"finga/test/e2etest"
)

// The admin API writes as a session's account, on that account's characters only, through the modules' rules and only while the world doesn't hold the character.
func TestTheAdminAPI(t *testing.T) {
	t.Parallel()
	s := newStack(t)
	ada, id := s.CreateCharacter(t, "ada", "Ada", "warrior")
	bob, _ := s.CreateCharacter(t, "bob", "Bob", "warrior")
	admin := adminapi.Client{URL: s.Admin, HTTP: &http.Client{Timeout: 10 * time.Second}}
	ctx := t.Context()
	c, err := admin.Character(ctx, ada, id)
	if err != nil || c.Level != 1 {
		t.Fatalf("Ada at %+v (%v); want level 1", c, err)
	}
	progress := adminapi.Progress{Class: c.Class, MapID: "willowmere", X: 513, Y: 0, Z: 162.5, Yaw: -1.25, HP: 1, Energy: 5, Level: 5, UnspentPoints: 12, UnspentSkillPoints: 4,
		Quests: []adminapi.Quest{{ID: "slime_cleanup", Status: "active", Progress: []int{3}}}}

	t.Run("progress", func(t *testing.T) {
		if err := admin.SetProgress(ctx, ada, id, progress); err != nil {
			t.Fatal(err)
		}
		if c, err = admin.Character(ctx, ada, id); err != nil || c.Level != 5 || c.UnspentPoints != 12 || c.UnspentSkillPoints != 4 {
			t.Fatalf("Ada at %+v (%v); want level 5 with 12 points and 4 skill points unspent", c, err)
		}
		var level, count int
		var z, yaw float32
		if err := s.DB.QueryRow(context.Background(), `SELECT c.level, c.pos_z, c.yaw, q.progress[1] FROM characters c JOIN character_quests q ON q.character_id = c.id WHERE c.id = $1 AND q.quest_id = 'slime_cleanup'`, id).Scan(&level, &z, &yaw, &count); err != nil || level != 5 || z != 162.5 || yaw != -1.25 || count != 3 {
			t.Fatalf("the database holds level %d, z %v, yaw %v, quest count %d (%v)", level, z, yaw, count, err)
		}
	})

	t.Run("progress the game data doesn't allow", func(t *testing.T) {
		for name, edit := range map[string]func(*adminapi.Progress){
			"an unknown class":     func(p *adminapi.Progress) { p.Class = "paladin" },
			"an unknown map":       func(p *adminapi.Progress) { p.MapID = "nowhere" },
			"a level past the cap": func(p *adminapi.Progress) { p.Level = 31 },
			"level 0":              func(p *adminapi.Progress) { p.Level = 0 },
			"a level past int32":   func(p *adminapi.Progress) { p.Level = 1 << 32 },
			"HP past the max":      func(p *adminapi.Progress) { p.HP = 100_000 },
			"energy past the max":  func(p *adminapi.Progress) { p.Energy = 11 },
			"an unknown quest": func(p *adminapi.Progress) {
				p.Quests = []adminapi.Quest{{ID: "no_such_quest", Status: "active", Progress: []int{1}}}
			},
			"a count past its objective": func(p *adminapi.Progress) {
				p.Quests = []adminapi.Quest{{ID: "slime_cleanup", Status: "active", Progress: []int{11}}}
			},
			"two counts for one objective": func(p *adminapi.Progress) {
				p.Quests = []adminapi.Quest{{ID: "slime_cleanup", Status: "active", Progress: []int{1, 1}}}
			},
		} {
			bad := progress
			edit(&bad)
			if code, reason := e2etest.Status(admin.SetProgress(ctx, ada, id, bad)); code != http.StatusUnprocessableEntity || reason != "bad_progress" {
				t.Errorf("%s: %d %s; want 422 bad_progress", name, code, reason)
			}
		}
		if c, err := admin.Character(ctx, ada, id); err != nil || c.Level != 5 {
			t.Fatalf("Ada at %+v (%v) after the refusals; want level 5 still", c, err)
		}
	})

	t.Run("items", func(t *testing.T) {
		before := s.InventoryTotals(t, id)["red_potion"]
		key := uuidv7.NewString()
		for range 2 { // granted once under its key
			if err := admin.Grant(ctx, ada, id, adminapi.Grant{Key: key, Template: "red_potion", Quantity: 7}); err != nil {
				t.Fatal(err)
			}
		}
		items, err := admin.Items(ctx, ada, id)
		if err != nil {
			t.Fatal(err)
		}
		held := 0
		for _, it := range items {
			if it.Template == "red_potion" {
				held += it.Quantity
			}
		}
		if got := s.InventoryTotals(t, id)["red_potion"]; got != before+7 || held != got {
			t.Fatalf("%d red potions, then %d, listed as %d; want 7 more, granted once", before, got, held)
		}
		err = admin.Grant(ctx, ada, id, adminapi.Grant{Key: uuidv7.NewString(), Template: "knight_sword", Quantity: 1}) // equipment needs its roll
		if code, reason := e2etest.Status(err); code != http.StatusUnprocessableEntity || reason == "" {
			t.Fatalf("a sword without its roll: %v; want 422 with the item rule's reason", err)
		}
	})

	t.Run("a character on a map", func(t *testing.T) {
		p, _ := s.Join(t, ada, id)
		ada = p.Token // Hello rotated it
		if code, reason := e2etest.Status(admin.SetProgress(ctx, ada, id, progress)); code != http.StatusConflict || reason != "character_online" {
			t.Fatalf("progress for Ada on a map: %d %s; want 409 character_online", code, reason)
		}
		_ = p.Conn.Close(websocket.StatusNormalClosure, "bye")
		deadline := time.Now().Add(10 * time.Second)
		for err = admin.SetProgress(ctx, ada, id, progress); err != nil; err = admin.SetProgress(ctx, ada, id, progress) {
			if code, _ := e2etest.Status(err); code != http.StatusConflict || time.Now().After(deadline) {
				t.Fatalf("progress once Ada left: %v; want it saved within 10 s", err)
			}
			time.Sleep(50 * time.Millisecond)
		}
	})

	t.Run("refusals", func(t *testing.T) {
		gone := s.Session(t, "cyd")
		s.MustStatus(s.Call("DELETE", "/api/sessions/current", gone, nil), http.StatusNoContent)
		expired := s.Session(t, "dee")
		if _, err := s.DB.Exec(context.Background(), `UPDATE sessions SET expires_at = now() - interval '1 minute' WHERE account_id = (SELECT id FROM accounts WHERE username = 'dee')`); err != nil {
			t.Fatal(err)
		}
		calls := map[string]func(token, id string) error{
			"GET character": func(token, id string) error { _, err := admin.Character(ctx, token, id); return err },
			"PUT progress":  func(token, id string) error { return admin.SetProgress(ctx, token, id, progress) },
			"GET items":     func(token, id string) error { _, err := admin.Items(ctx, token, id); return err },
			"POST a grant": func(token, id string) error {
				return admin.Grant(ctx, token, id, adminapi.Grant{Key: uuidv7.NewString(), Template: "red_potion", Quantity: 1})
			},
		}
		for call, do := range calls {
			for _, c := range []struct {
				name, token, id string
				status          int
				reason          string
			}{
				{"another account's character", bob, id, http.StatusNotFound, "character_not_found"},
				{"no session", "", id, http.StatusUnauthorized, "unauthenticated"},
				{"a forged token", "forged", id, http.StatusUnauthorized, "unauthenticated"},
				{"a session logged out", gone, id, http.StatusUnauthorized, "unauthenticated"},
				{"a session expired", expired, id, http.StatusUnauthorized, "unauthenticated"},
				{"a bad id", ada, "not-an-id", http.StatusBadRequest, "bad_id"},
			} {
				if code, reason := e2etest.Status(do(c.token, c.id)); code != c.status || reason != c.reason {
					t.Errorf("%s, %s: %d %s; want %d %s", call, c.name, code, reason, c.status, c.reason)
				}
			}
		}
		err := admin.Grant(ctx, ada, id, adminapi.Grant{Key: "not a uuid", Template: "red_potion", Quantity: 1})
		if code, reason := e2etest.Status(err); code != http.StatusBadRequest || reason != "bad_body" {
			t.Errorf("a grant under a bad key: %v; want 400 bad_body", err)
		}
	})
}

// The admin API listens only when the config enables it.
func TestTheAdminAPIIsOffUnlessEnabled(t *testing.T) {
	t.Parallel()
	s := e2etest.NewStackBuilt(t, "../data", func(ctx context.Context, h *host.Host, cfg config.Config) error {
		cfg.Admin.Enabled = false
		return dependencies.Build(ctx, h, cfg)
	})
	if conn, err := net.DialTimeout("tcp", s.Cfg.Admin.Addr, time.Second); err == nil {
		conn.Close()
		t.Fatalf("something listens on the admin address %s", s.Cfg.Admin.Addr)
	}
}
