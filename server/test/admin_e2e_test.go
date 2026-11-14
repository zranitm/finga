package e2e_test

import (
	"context"
	"errors"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/coder/websocket"

	"finga/cmd/finga/app"
	"finga/config"
	"finga/internal/adminapi"
	"finga/internal/host"
	"finga/pkg/uuidv7"
)

// status is the admin API's answer to err: its status and reason, or 0 for none.
func status(err error) (int, string) {
	if e, ok := errors.AsType[adminapi.StatusError](err); ok {
		return e.Status, e.Reason
	}
	return 0, ""
}

// The admin API writes as a session's account, on that account's characters only, through the modules' rules and only while the world doesn't hold the character.
func TestTheAdminAPI(t *testing.T) {
	t.Parallel()
	s := newStack(t)
	ada, id := s.createCharacter(t, "ada", "Ada", "warrior")
	bob, _ := s.createCharacter(t, "bob", "Bob", "warrior")
	admin := adminapi.Client{URL: s.admin, HTTP: &http.Client{Timeout: 10 * time.Second}}
	ctx := t.Context()
	c, err := admin.Character(ctx, ada, id)
	if err != nil || c.Level != 1 {
		t.Fatalf("Ada at %+v (%v); want level 1", c, err)
	}
	progress := adminapi.Progress{Class: c.Class, MapID: "town", X: 416, Y: 608, HP: 1, Energy: 5, Level: 5, UnspentPoints: 12, UnspentSkillPoints: 4,
		Quests: []adminapi.Quest{{ID: "slime_cleanup", Status: "active", Progress: []int{3}}}}

	t.Run("progress", func(t *testing.T) {
		if err := admin.SetProgress(ctx, ada, id, progress); err != nil {
			t.Fatal(err)
		}
		if c, err = admin.Character(ctx, ada, id); err != nil || c.Level != 5 || c.UnspentPoints != 12 || c.UnspentSkillPoints != 4 {
			t.Fatalf("Ada at %+v (%v); want level 5 with 12 points and 4 skill points unspent", c, err)
		}
		var level, count int
		if err := s.db.QueryRow(context.Background(), `SELECT c.level, q.progress[1] FROM characters c JOIN character_quests q ON q.character_id = c.id WHERE c.id = $1 AND q.quest_id = 'slime_cleanup'`, id).Scan(&level, &count); err != nil || level != 5 || count != 3 {
			t.Fatalf("the database holds level %d, quest count %d (%v)", level, count, err)
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
			if code, reason := status(admin.SetProgress(ctx, ada, id, bad)); code != http.StatusUnprocessableEntity || reason != "bad_progress" {
				t.Errorf("%s: %d %s; want 422 bad_progress", name, code, reason)
			}
		}
		if c, err := admin.Character(ctx, ada, id); err != nil || c.Level != 5 {
			t.Fatalf("Ada at %+v (%v) after the refusals; want level 5 still", c, err)
		}
	})

	t.Run("items", func(t *testing.T) {
		before := s.inventoryTotals(t, id)["red_potion"]
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
		if got := s.inventoryTotals(t, id)["red_potion"]; got != before+7 || held != got {
			t.Fatalf("%d red potions, then %d, listed as %d; want 7 more, granted once", before, got, held)
		}
		err = admin.Grant(ctx, ada, id, adminapi.Grant{Key: uuidv7.NewString(), Template: "knight_sword", Quantity: 1}) // equipment needs its roll
		if code, reason := status(err); code != http.StatusUnprocessableEntity || reason == "" {
			t.Fatalf("a sword without its roll: %v; want 422 with the item rule's reason", err)
		}
	})

	t.Run("a character on a map", func(t *testing.T) {
		p, _ := s.join(t, ada, id)
		ada = p.token // Hello rotated it
		if code, reason := status(admin.SetProgress(ctx, ada, id, progress)); code != http.StatusConflict || reason != "character_online" {
			t.Fatalf("progress for Ada on a map: %d %s; want 409 character_online", code, reason)
		}
		_ = p.conn.Close(websocket.StatusNormalClosure, "bye")
		deadline := time.Now().Add(10 * time.Second)
		for err = admin.SetProgress(ctx, ada, id, progress); err != nil; err = admin.SetProgress(ctx, ada, id, progress) {
			if code, _ := status(err); code != http.StatusConflict || time.Now().After(deadline) {
				t.Fatalf("progress once Ada left: %v; want it saved within 10 s", err)
			}
			time.Sleep(50 * time.Millisecond)
		}
	})

	t.Run("refusals", func(t *testing.T) {
		gone := s.session(t, "cyd")
		s.mustStatus(s.call("DELETE", "/api/sessions/current", gone, nil), http.StatusNoContent)
		expired := s.session(t, "dee")
		if _, err := s.db.Exec(context.Background(), `UPDATE sessions SET expires_at = now() - interval '1 minute' WHERE account_id = (SELECT id FROM accounts WHERE username = 'dee')`); err != nil {
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
				if code, reason := status(do(c.token, c.id)); code != c.status || reason != c.reason {
					t.Errorf("%s, %s: %d %s; want %d %s", call, c.name, code, reason, c.status, c.reason)
				}
			}
		}
		err := admin.Grant(ctx, ada, id, adminapi.Grant{Key: "not a uuid", Template: "red_potion", Quantity: 1})
		if code, reason := status(err); code != http.StatusBadRequest || reason != "bad_body" {
			t.Errorf("a grant under a bad key: %v; want 400 bad_body", err)
		}
	})
}

// session signs username up and logs in: the session's token.
func (s *stack) session(t *testing.T, username string) string {
	t.Helper()
	s.mustStatus(s.call("POST", "/api/accounts", "", map[string]string{"username": username, "password": "correct horse"}), http.StatusCreated)
	return s.call("POST", "/api/sessions", "", map[string]string{"username": username, "password": "correct horse"}).body["token"].(string)
}

// The admin API listens only when the config enables it.
func TestTheAdminAPIIsOffUnlessEnabled(t *testing.T) {
	t.Parallel()
	s := newStackBuilt(t, "../data", func(ctx context.Context, h *host.Host, cfg config.Config) error {
		cfg.Admin.Enabled = false
		return app.Build(ctx, h, cfg)
	})
	if conn, err := net.DialTimeout("tcp", s.cfg.Admin.Addr, time.Second); err == nil {
		conn.Close()
		t.Fatalf("something listens on the admin address %s", s.cfg.Admin.Addr)
	}
}
