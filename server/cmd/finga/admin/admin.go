// Package admin serves the private admin API (internal/adminapi) through the modules' facades, so the item and idempotency rules hold for what it writes.
package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"finga/internal/adminapi"
	"finga/internal/gamedata"
	"finga/internal/kernel"
	"finga/internal/progression"
	"finga/pkg/transient"
	"finga/services/account"
	"finga/services/character"
	"finga/services/item"
)

// maxBody bounds a request's body; the largest, a character's progress, is a few kilobytes.
const maxBody = 64 << 10

// Accounts resolves a session's token.
type Accounts interface {
	Validate(ctx context.Context, token string) (account.Principal, error)
}

// Characters reads and sets a character's progress.
type Characters interface {
	Load(ctx context.Context, id kernel.CharacterID, account kernel.AccountID) (character.Loaded, error)
	SaveCheckpoint(ctx context.Context, cp character.Checkpoint) error
}

// Items reads and grants a character's items.
type Items interface {
	Inventory(ctx context.Context, owner kernel.CharacterID) (item.Inventory, error)
	Add(ctx context.Context, actor item.Actor, r item.AddRequest) (item.Result, error)
}

// World runs a write the world would overwrite while it holds the character.
type World interface {
	Offline(ctx context.Context, id kernel.CharacterID, do func(ctx context.Context) error) (bool, error)
}

// Deps are what the admin API calls, the game data it checks progress against, and where it logs a failure of its own.
type Deps struct {
	Log        *slog.Logger
	Data       *gamedata.Data
	Accounts   Accounts
	Characters Characters
	Items      Items
	World      World
}

// Handler is the admin API over d.
func Handler(d Deps) http.Handler {
	s := server(d)
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+adminapi.PathCharacter, s.character)
	mux.HandleFunc("PUT "+adminapi.PathProgress, s.progress)
	mux.HandleFunc("GET "+adminapi.PathItems, s.items)
	mux.HandleFunc("POST "+adminapi.PathItems, s.grant)
	return mux
}

type server Deps

var (
	errBadBody     = errors.New("bad_body")         // a body that isn't the path's JSON
	errBadProgress = errors.New("bad_progress")     // progress the game data doesn't allow
	errOnline      = errors.New("character_online") // the world holds the character, and would overwrite the write
)

// owned is the path's character as its account's, the bearer token's; any other is not found.
func (s server) owned(r *http.Request) (item.Actor, character.Loaded, error) {
	token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok {
		return item.Actor{}, character.Loaded{}, kernel.ErrUnauthenticated
	}
	p, err := s.Accounts.Validate(r.Context(), token)
	if err != nil {
		return item.Actor{}, character.Loaded{}, err
	}
	id, err := kernel.ParseCharacterID(r.PathValue("id"))
	if err != nil {
		return item.Actor{}, character.Loaded{}, err
	}
	loaded, err := s.Characters.Load(r.Context(), id, p.AccountID)
	if err != nil {
		return item.Actor{}, character.Loaded{}, err
	}
	return item.Actor{Account: p.AccountID, Character: id}, loaded, nil
}

func (s server) character(w http.ResponseWriter, r *http.Request) {
	_, loaded, err := s.owned(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	c := loaded.Character
	skills := make(map[string]int, len(loaded.Skills))
	for _, sk := range loaded.Skills {
		skills[sk.ID] = sk.Points
	}
	write(w, http.StatusOK, adminapi.Character{Class: string(c.Class), Level: c.Level, UnspentPoints: c.UnspentPoints, UnspentSkillPoints: c.UnspentSkillPoints, Skills: skills})
}

func (s server) progress(w http.ResponseWriter, r *http.Request) {
	actor, _, err := s.owned(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var p adminapi.Progress
	if err := decode(w, r, &p); err != nil {
		s.fail(w, r, err)
		return
	}
	if err := s.allowed(p); err != nil {
		s.fail(w, r, err)
		return
	}
	quests := make([]character.Quest, len(p.Quests))
	for i, q := range p.Quests {
		quests[i] = character.Quest{ID: q.ID, Status: character.QuestStatus(q.Status), Progress: q.Progress, TimesFinished: q.TimesFinished}
	}
	cp := character.Checkpoint{
		ID: actor.Character, Class: kernel.Class(p.Class), MapID: p.MapID, X: p.X, Y: p.Y, HP: p.HP, Energy: p.Energy,
		Level: p.Level, UnspentPoints: p.UnspentPoints, UnspentSkillPoints: p.UnspentSkillPoints, Quests: quests,
	}
	ran, err := s.World.Offline(r.Context(), actor.Character, func(ctx context.Context) error { return s.Characters.SaveCheckpoint(ctx, cp) })
	switch {
	case err != nil:
		s.fail(w, r, err)
	case !ran:
		s.fail(w, r, errOnline)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

// allowed checks p against the game data: a known class and map, a level up to the cap, HP and energy within the level's maximums with no points spent, and known quests with a count per objective.
func (s server) allowed(p adminapi.Progress) error {
	bad := func(format string, args ...any) error {
		return fmt.Errorf("%w: %s", errBadProgress, fmt.Sprintf(format, args...))
	}
	class, ok := s.Data.Class(kernel.Class(p.Class))
	if !ok {
		return bad("no class %q", p.Class)
	}
	if _, ok := s.Data.Map(p.MapID); !ok {
		return bad("no map %q", p.MapID)
	}
	if p.Level < 1 || p.Level > s.Data.LevelCap() {
		return bad("level %d outside 1 to %d", p.Level, s.Data.LevelCap())
	}
	maxHP := progression.DeriveStats(s.Data.StatRules(), class.ID, p.Level, s.Data.StartClass().BaseStats, progression.ItemStats{}).MaxHP
	if p.HP < 0 || p.HP > maxHP {
		return bad("HP %d outside 0 to %d, a level %d %s's", p.HP, maxHP, p.Level, class.ID)
	}
	if p.Energy < 0 || p.Energy > s.Data.Energy().Max {
		return bad("energy %d outside 0 to %d", p.Energy, s.Data.Energy().Max)
	}
	for _, q := range p.Quests {
		quest, ok := s.Data.Quest(q.ID)
		if !ok {
			return bad("no quest %q", q.ID)
		}
		if q.Status != string(character.QuestActive) {
			continue
		}
		if len(q.Progress) != len(quest.Objectives) {
			return bad("quest %s: %d counts for %d objectives", q.ID, len(q.Progress), len(quest.Objectives))
		}
		for k, n := range q.Progress {
			if n < 0 || n > quest.Objectives[k].Count {
				return bad("quest %s: count %d outside 0 to %d", q.ID, n, quest.Objectives[k].Count)
			}
		}
	}
	return nil
}

func (s server) items(w http.ResponseWriter, r *http.Request) {
	actor, _, err := s.owned(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	inv, err := s.Items.Inventory(r.Context(), actor.Character)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	out := make([]adminapi.Item, len(inv.Items))
	for i, it := range inv.Items {
		out[i] = adminapi.Item{ID: it.ID.String(), Template: it.TemplateID, Quantity: it.Quantity, EquipSlot: it.EquipSlot}
	}
	write(w, http.StatusOK, out)
}

func (s server) grant(w http.ResponseWriter, r *http.Request) {
	actor, _, err := s.owned(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var g adminapi.Grant
	if err := decode(w, r, &g); err != nil {
		s.fail(w, r, err)
		return
	}
	key, err := uuid.Parse(g.Key)
	if err != nil {
		s.fail(w, r, fmt.Errorf("%w: key: %w", errBadBody, err))
		return
	}
	if _, err := s.Items.Add(r.Context(), actor, item.AddRequest{
		Key: g.Key, TemplateID: g.Template, Quantity: g.Quantity, Cause: item.Cause{Type: item.CauseDrop, ID: key}, Stats: g.Roll,
	}); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// fail answers err with its status and reason: a rule the caller broke, or internal, logged.
func (s server) fail(w http.ResponseWriter, r *http.Request, err error) {
	status, detail := http.StatusInternalServerError, ""
	reason, ok := kernel.ReasonOf(err)
	switch {
	case errors.Is(err, errBadBody):
		status, reason, detail = http.StatusBadRequest, errBadBody.Error(), err.Error()
	case errors.Is(err, errBadProgress):
		status, reason, detail = http.StatusUnprocessableEntity, errBadProgress.Error(), err.Error()
	case errors.Is(err, errOnline):
		status, reason = http.StatusConflict, errOnline.Error()
	case errors.Is(err, kernel.ErrUnauthenticated):
		status = http.StatusUnauthorized
	case errors.Is(err, kernel.ErrCharacterNotFound):
		status = http.StatusNotFound
	case errors.Is(err, kernel.ErrBadID):
		status = http.StatusBadRequest
	case kernel.IsRule(err):
		status = http.StatusUnprocessableEntity
	case errors.Is(err, transient.ErrUnavailable), errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		status = http.StatusServiceUnavailable
		if !ok {
			reason = "unavailable"
		}
	default:
		reason = "internal"
		s.Log.ErrorContext(r.Context(), "admin call failed", "method", r.Method, "path", r.URL.Path, "err", err)
	}
	write(w, status, adminapi.Error{Reason: reason, Detail: detail})
}

func decode(w http.ResponseWriter, r *http.Request, v any) error {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("%w: %w", errBadBody, err)
	}
	return nil
}

func write(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body) // the caller gone, there's no one to tell
}
