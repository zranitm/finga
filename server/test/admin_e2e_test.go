package e2e_test

import (
	"context"
	"errors"
	"net"
	"net/http"
	"testing"
	"time"

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

// The admin API writes as a session's account, on that account's characters only, through the modules' rules.
func TestTheAdminAPI(t *testing.T) {
	t.Parallel()
	s := newStack(t)
	ada, id := s.createCharacter(t, "ada", "Ada", "warrior")
	bob, _ := s.createCharacter(t, "bob", "Bob", "warrior")
	admin := adminapi.Client{URL: s.admin, HTTP: &http.Client{Timeout: 10 * time.Second}}
	ctx := t.Context()

	t.Run("progress", func(t *testing.T) {
		c, err := admin.Character(ctx, ada, id)
		if err != nil || c.Level != 1 {
			t.Fatalf("Ada at %+v (%v); want level 1", c, err)
		}
		p := adminapi.Progress{Class: c.Class, MapID: "town", X: 416, Y: 608, HP: 100, Energy: 10, Level: 5, UnspentPoints: 12, UnspentSkillPoints: 4,
			Quests: []adminapi.Quest{{ID: "slime_cleanup", Status: "active", Progress: []int{3}}}}
		if err := admin.SetProgress(ctx, ada, id, p); err != nil {
			t.Fatal(err)
		}
		if c, err = admin.Character(ctx, ada, id); err != nil || c.Level != 5 || c.UnspentPoints != 12 || c.UnspentSkillPoints != 4 {
			t.Fatalf("Ada at %+v (%v); want level 5 with 12 points and 4 skill points unspent", c, err)
		}
		var level, progress int
		if err := s.db.QueryRow(context.Background(), `SELECT c.level, q.progress[1] FROM characters c JOIN character_quests q ON q.character_id = c.id WHERE c.id = $1 AND q.quest_id = 'slime_cleanup'`, id).Scan(&level, &progress); err != nil || level != 5 || progress != 3 {
			t.Fatalf("the database holds level %d, quest progress %d (%v)", level, progress, err)
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

	t.Run("refusals", func(t *testing.T) {
		for _, c := range []struct {
			name, token, id string
			status          int
			reason          string
		}{
			{"another account's character", bob, id, http.StatusNotFound, "character_not_found"},
			{"no session", "", id, http.StatusUnauthorized, "unauthenticated"},
			{"a bad id", ada, "not-an-id", http.StatusBadRequest, "bad_id"},
		} {
			_, err := admin.Character(ctx, c.token, c.id)
			if code, reason := status(err); code != c.status || reason != c.reason {
				t.Errorf("%s: %v; want %d %s", c.name, err, c.status, c.reason)
			}
		}
		err := admin.Grant(ctx, ada, id, adminapi.Grant{Key: "not a uuid", Template: "red_potion", Quantity: 1})
		if code, reason := status(err); code != http.StatusBadRequest || reason != "bad_body" {
			t.Errorf("a grant under a bad key: %v; want 400 bad_body", err)
		}
	})
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
