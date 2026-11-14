// Package adminapi is the game's private admin API, for the dev tools where no player path exists: its paths, bodies and client. cmd/finga serves it on its own address, and only when admin.enabled is set.
package adminapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"finga/internal/progression"
)

// The paths; every request carries a player's session as a bearer token and acts only on that account's characters.
const (
	PathCharacter = "/admin/characters/{id}"          // GET: its progress
	PathProgress  = "/admin/characters/{id}/progress" // PUT: set its progress
	PathItems     = "/admin/characters/{id}/items"    // GET: its items; POST: grant one
)

// Character is a character's progress.
type Character struct {
	Class              string         `json:"class"`
	Level              int            `json:"level"`
	UnspentPoints      int            `json:"unspent_points"`
	UnspentSkillPoints int            `json:"unspent_skill_points"`
	Skills             map[string]int `json:"skills"` // points in each skill that has some
}

// Progress puts a character at a level, with every stat point unspent and no exp, as the world's checkpoint saves it: a skill not listed keeps its points.
type Progress struct {
	Class              string  `json:"class"`
	MapID              string  `json:"map_id"`
	X                  float64 `json:"x"`
	Y                  float64 `json:"y"`
	HP                 int     `json:"hp"`
	Energy             int     `json:"energy"`
	Level              int     `json:"level"`
	UnspentPoints      int     `json:"unspent_points"`
	UnspentSkillPoints int     `json:"unspent_skill_points"`
	Quests             []Quest `json:"quests"` // every quest it has; one not listed goes
}

// Quest is a character's state of one quest.
type Quest struct {
	ID            string `json:"id"`
	Status        string `json:"status"`             // active or finished
	Progress      []int  `json:"progress,omitempty"` // a count per objective, while active
	TimesFinished int    `json:"times_finished"`
}

// Item is one of a character's items.
type Item struct {
	ID        string `json:"id"`
	Template  string `json:"template"`
	Quantity  int    `json:"quantity"`
	EquipSlot string `json:"equip_slot,omitempty"` // where it is worn; empty in the inventory
}

// Grant gives a character an item as a drop, once under Key.
type Grant struct {
	Key      string                 `json:"key"` // a UUID
	Template string                 `json:"template"`
	Quantity int                    `json:"quantity"`
	Roll     *progression.ItemStats `json:"roll,omitempty"` // required for equipment, refused on anything else
}

// Error is the body of every failed answer: the rule's reason, or internal.
type Error struct {
	Reason string `json:"reason"`
}

// StatusError is a request the API answered with an error status.
type StatusError struct {
	Status int
	Reason string
}

func (e StatusError) Error() string {
	return fmt.Sprintf("admin API: status %d: %s", e.Status, e.Reason)
}

// Client calls the admin API at URL.
type Client struct {
	URL  string
	HTTP *http.Client
}

// Character is the progress of the token's account's character id.
func (c Client) Character(ctx context.Context, token, id string) (Character, error) {
	var out Character
	err := c.do(ctx, http.MethodGet, "/admin/characters/"+url.PathEscape(id), token, nil, &out)
	return out, err
}

// SetProgress sets the progress of the token's account's character id.
func (c Client) SetProgress(ctx context.Context, token, id string, p Progress) error {
	return c.do(ctx, http.MethodPut, "/admin/characters/"+url.PathEscape(id)+"/progress", token, p, nil)
}

// Items are the items of the token's account's character id.
func (c Client) Items(ctx context.Context, token, id string) ([]Item, error) {
	var out []Item
	err := c.do(ctx, http.MethodGet, "/admin/characters/"+url.PathEscape(id)+"/items", token, nil, &out)
	return out, err
}

// Grant gives the token's account's character id an item, once under g.Key.
func (c Client) Grant(ctx context.Context, token, id string, g Grant) error {
	return c.do(ctx, http.MethodPost, "/admin/characters/"+url.PathEscape(id)+"/items", token, g, nil)
}

func (c Client) do(ctx context.Context, method, path, token string, body, out any) error {
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			return err
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, c.URL+path, &buf)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode >= http.StatusBadRequest {
		var e Error
		raw, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
		if json.Unmarshal(raw, &e) != nil || e.Reason == "" {
			e.Reason = string(raw)
		}
		return StatusError{Status: res.StatusCode, Reason: e.Reason}
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(res.Body).Decode(out)
}
