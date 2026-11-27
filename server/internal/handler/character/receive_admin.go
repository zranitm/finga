package character

import (
	"context"
	"fmt"
	"net/http"

	"github.com/google/uuid"

	"finga/internal/application/character"
	"finga/internal/application/item"
	"finga/internal/handler/transport"
	"finga/internal/shared/adminapi"
	"finga/internal/shared/kernel"
)

// Characters reads a character, which its account owns.
type adminCharacters interface {
	Load(ctx context.Context, id kernel.CharacterID, account kernel.AccountID) (character.Loaded, error)
}

// adminItems reads and grants a character's items.
type adminItems interface {
	Inventory(ctx context.Context, owner kernel.CharacterID) (item.Inventory, error)
	Add(ctx context.Context, actor item.Actor, r item.AddRequest) (item.Result, error)
}

// AdminDeps are what the character's admin routes call: the character, which item's inventory the routes read and grant into.
type AdminDeps struct {
	Characters adminCharacters
	Items      adminItems
}

// adminRoutes serves a character's progress and items to the admin API.
type adminRoutes struct {
	AdminDeps
	admin *transport.Admin
}

// AdminRoutes adds the character's admin routes: its progress, and the items it holds and is granted. They sit here because the character's use cases call the item's, and these routes call both.
func AdminRoutes(d AdminDeps) transport.AdminRoutes {
	return func(a *transport.Admin) {
		s := &adminRoutes{AdminDeps: d, admin: a}
		a.Handle("GET "+adminapi.PathCharacter, s.character)
		a.Handle("GET "+adminapi.PathItems, s.items)
		a.Handle("POST "+adminapi.PathItems, s.grant)
	}
}

func (s *adminRoutes) character(w http.ResponseWriter, r *http.Request) {
	_, _, loaded, err := transport.AdminOwned(r, s.Characters.Load)
	if err != nil {
		s.admin.Fail(w, r, err)
		return
	}
	c := loaded.Character
	skills := make(map[string]int, len(loaded.Skills))
	for _, sk := range loaded.Skills {
		skills[sk.ID] = sk.Points
	}
	transport.AdminWrite(w, http.StatusOK, adminapi.Character{Class: string(c.Class), Level: c.Level, UnspentPoints: c.UnspentPoints, UnspentSkillPoints: c.UnspentSkillPoints, Skills: skills})
}

func (s *adminRoutes) items(w http.ResponseWriter, r *http.Request) {
	id, _, _, err := transport.AdminOwned(r, s.Characters.Load)
	if err != nil {
		s.admin.Fail(w, r, err)
		return
	}
	inv, err := s.Items.Inventory(r.Context(), id)
	if err != nil {
		s.admin.Fail(w, r, err)
		return
	}
	out := make([]adminapi.Item, len(inv.Items))
	for i, it := range inv.Items {
		out[i] = adminapi.Item{ID: it.ID.String(), Template: it.TemplateID, Quantity: it.Quantity, EquipSlot: it.EquipSlot}
	}
	transport.AdminWrite(w, http.StatusOK, out)
}

func (s *adminRoutes) grant(w http.ResponseWriter, r *http.Request) {
	id, account, _, err := transport.AdminOwned(r, s.Characters.Load)
	if err != nil {
		s.admin.Fail(w, r, err)
		return
	}
	var g adminapi.Grant
	if err := transport.AdminDecode(w, r, &g); err != nil {
		s.admin.Fail(w, r, err)
		return
	}
	key, err := uuid.Parse(g.Key)
	if err != nil {
		s.admin.Fail(w, r, fmt.Errorf("%w: key: %w", transport.ErrAdminBadBody, err))
		return
	}
	if _, err := s.Items.Add(r.Context(), item.Actor{Account: account, Character: id}, item.AddRequest{
		Key: g.Key, TemplateID: g.Template, Quantity: g.Quantity, Cause: item.Cause{Type: item.CauseDrop, ID: key}, Stats: g.Roll,
	}); err != nil {
		s.admin.Fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
