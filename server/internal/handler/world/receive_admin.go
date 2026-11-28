package world

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"finga/internal/application/character"
	"finga/internal/handler/transport"
	"finga/internal/shared/adminapi"
	"finga/internal/shared/gamedata"
	"finga/internal/shared/kernel"
	"finga/internal/shared/progression"
)

// adminCharacters reads a character, which its account owns, and saves a checkpoint of it.
type adminCharacters interface {
	Load(ctx context.Context, id kernel.CharacterID, account kernel.AccountID) (character.Loaded, error)
	SaveCheckpoint(ctx context.Context, cp character.Checkpoint) error
	FindWaypoints(ctx context.Context, id kernel.CharacterID, found []character.Waypoint) error
}

// adminWorld runs a write the world would overwrite while it holds the character.
type adminWorld interface {
	Offline(ctx context.Context, id kernel.CharacterID, do func(ctx context.Context) error) (bool, error)
}

// AdminDeps are what the world's admin route calls, and the game data it checks progress against.
type AdminDeps struct {
	Data       *gamedata.Data
	Characters adminCharacters
	World      adminWorld
}

// adminRoutes serves a character's progress write to the admin API.
type adminRoutes struct {
	AdminDeps
	admin *transport.Admin
}

// AdminRoutes adds the world's admin route: setting a character's progress, which the world must not hold while it is written. It sits here because the world's use cases call the character's.
func AdminRoutes(d AdminDeps) transport.AdminRoutes {
	return func(a *transport.Admin) {
		s := &adminRoutes{AdminDeps: d, admin: a}
		a.Handle("PUT "+adminapi.PathProgress, s.progress)
		a.Handle("POST "+adminapi.PathWaypoints, s.waypoints)
	}
}

func (s *adminRoutes) progress(w http.ResponseWriter, r *http.Request) {
	id, _, _, err := transport.AdminOwned(r, s.Characters.Load)
	if err != nil {
		s.admin.Fail(w, r, err)
		return
	}
	var p adminapi.Progress
	if err := transport.AdminDecode(w, r, &p); err != nil {
		s.admin.Fail(w, r, err)
		return
	}
	if err := s.allowed(p); err != nil {
		s.admin.Fail(w, r, err)
		return
	}
	quests := make([]character.Quest, len(p.Quests))
	for i, q := range p.Quests {
		quests[i] = character.Quest{ID: q.ID, Status: character.QuestStatus(q.Status), Progress: q.Progress, TimesFinished: q.TimesFinished}
	}
	cp := character.Checkpoint{
		ID: id, Class: kernel.Class(p.Class), MapID: p.MapID, X: p.X, Y: p.Y, Z: p.Z, Yaw: p.Yaw, HP: p.HP, Energy: p.Energy,
		Level: p.Level, UnspentPoints: p.UnspentPoints, UnspentSkillPoints: p.UnspentSkillPoints, Quests: quests,
	}
	ran, err := s.World.Offline(r.Context(), id, func(ctx context.Context) error { return s.Characters.SaveCheckpoint(ctx, cp) })
	switch {
	case err != nil:
		s.admin.Fail(w, r, err)
	case !ran:
		s.admin.Fail(w, r, transport.ErrAdminOnline)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

// waypoints adds waypoints to a character's found ones, while the world does not hold it: each must be a waypoint of the data, and one found already keeps its time.
func (s *adminRoutes) waypoints(w http.ResponseWriter, r *http.Request) {
	id, _, _, err := transport.AdminOwned(r, s.Characters.Load)
	if err != nil {
		s.admin.Fail(w, r, err)
		return
	}
	var in adminapi.Waypoints
	if err := transport.AdminDecode(w, r, &in); err != nil {
		s.admin.Fail(w, r, err)
		return
	}
	now := time.Now()
	found := make([]character.Waypoint, len(in.IDs))
	for i, wp := range in.IDs {
		if _, _, ok := s.Data.Waypoint(wp); !ok {
			s.admin.Fail(w, r, fmt.Errorf("%w: no waypoint %q", transport.ErrAdminBadBody, wp))
			return
		}
		found[i] = character.Waypoint{ID: wp, FoundAt: now}
	}
	ran, err := s.World.Offline(r.Context(), id, func(ctx context.Context) error { return s.Characters.FindWaypoints(ctx, id, found) })
	switch {
	case err != nil:
		s.admin.Fail(w, r, err)
	case !ran:
		s.admin.Fail(w, r, transport.ErrAdminOnline)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

// allowed checks p against the game data: a known class and map, a level up to the cap, HP and energy within the level's maximums with no points spent, and known quests with a count per objective.
func (s *adminRoutes) allowed(p adminapi.Progress) error {
	bad := func(format string, args ...any) error {
		return fmt.Errorf("%w: %s", transport.ErrAdminBadProgress, fmt.Sprintf(format, args...))
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
