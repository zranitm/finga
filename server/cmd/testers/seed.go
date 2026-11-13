package main

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/google/uuid"

	accountv1 "finga/api/gen/account/v1"
	characterv1 "finga/api/gen/character/v1"
	clientv1 "finga/api/gen/client/v1"
	itemv1 "finga/api/gen/item/v1"
	"finga/internal/gamedata"
	"finga/internal/itemroll"
	"finga/pkg/uuidv7"
)

// seeder keeps the test accounts: the player's paths where there are some, the owning service's where there are none.
type seeder struct {
	data       *gamedata.Data
	gw         gateway
	password   string
	accounts   accountv1.AccountServiceClient
	characters characterv1.CharacterServiceClient
	items      itemv1.ItemServiceClient
}

// keep makes t's account and character as plan has them, doing only what is still undone, so an account already kept changes nothing: what it did, for the log.
func (s seeder) keep(ctx context.Context, t tester) (string, error) {
	sd, err := plan(s.data, t)
	if err != nil {
		return "", err
	}
	made, err := s.gw.signUp(ctx, t.username, s.password)
	if err != nil {
		return "", fmt.Errorf("sign up: %w", err)
	}
	token, err := s.gw.login(ctx, t.username, s.password)
	if err != nil {
		return "", fmt.Errorf("log in: %w", err)
	}
	v, err := s.accounts.Validate(ctx, &accountv1.ValidateRequest{Token: token})
	if err != nil {
		return "", fmt.Errorf("the account behind the token: %w", err)
	}
	id, err := s.gw.character(ctx, token, t.name)
	if err != nil {
		return "", fmt.Errorf("character: %w", err)
	}
	did := "kept"
	if made {
		did = "made"
	}
	who := &characterv1.LoadRequest{CharacterId: id, AccountId: v.GetAccountId()}
	loaded, err := s.characters.Load(ctx, who)
	if err != nil {
		return "", fmt.Errorf("load: %w", err)
	}
	// Only a character below its seed's level is still as created; one played since keeps its own progress.
	if int(loaded.GetCharacter().GetLevel()) < sd.level {
		if err := s.progress(ctx, id, sd); err != nil {
			return "", fmt.Errorf("progression: %w", err)
		}
		if loaded, err = s.characters.Load(ctx, who); err != nil {
			return "", fmt.Errorf("load: %w", err)
		}
		did += ", levelled"
	}
	actor := &itemv1.Actor{AccountId: v.GetAccountId(), CharacterId: id}
	added, err := s.give(ctx, actor, append(sd.gear, sd.potions...))
	if err != nil {
		return "", fmt.Errorf("items: %w", err)
	}
	if added > 0 {
		did += fmt.Sprintf(", %d items given", added)
	}
	played, err := s.play(ctx, token, actor, loaded, sd)
	if err != nil {
		return "", err
	}
	if played != "" {
		did += ", " + played
	}
	return did, nil
}

// progress writes the seed's level, class, trial and full HP and MP, with the level's points unspent, as the world's checkpoint does: in town, before the character first joins.
func (s seeder) progress(ctx context.Context, id string, sd seed) error {
	town := s.data.StartTown()
	_, err := s.characters.SaveCheckpoint(ctx, &characterv1.SaveCheckpointRequest{
		CharacterId: id, MapId: town.ID, X: town.PlayerSpawn.X, Y: town.PlayerSpawn.Y,
		Hp: int32(sd.hp), Mp: int32(sd.mp), Level: int32(sd.level), Class: string(sd.class.ID), //nolint:gosec // small
		Points: &characterv1.Stats{}, UnspentPoints: int32(sd.unspent), UnspentSkillPoints: int32(sd.unspentSkills), //nolint:gosec // small
		Quests: sd.quests,
	})
	return err
}

// give adds each piece the character holds too few of, as drops, under keys of the character and the piece, so a run cut short and run again adds none twice: how many it added.
func (s seeder) give(ctx context.Context, actor *itemv1.Actor, pieces []piece) (int, error) {
	inv, err := s.items.Inventory(ctx, &itemv1.InventoryRequest{CharacterId: actor.GetCharacterId()})
	if err != nil {
		return 0, err
	}
	held := map[string]int{}
	for _, it := range inv.GetItems() {
		held[it.GetTemplateId()]++
	}
	owner, err := uuid.Parse(actor.GetCharacterId())
	if err != nil {
		return 0, err
	}
	nth, added := map[string]int{}, 0
	for _, p := range pieces {
		k := nth[p.template]
		nth[p.template]++
		if k < held[p.template] {
			continue
		}
		key := uuidv7.Derived(owner, "testers:"+p.template+":"+strconv.Itoa(k)).String()
		if _, err := s.items.Add(ctx, &itemv1.AddRequest{
			Actor: actor, IdempotencyKey: key, TemplateId: p.template, Quantity: int32(p.quantity), //nolint:gosec // small
			Cause: &itemv1.Cause{Type: itemv1.CauseType_CAUSE_TYPE_DROP, Id: key}, Stats: itemroll.Proto(p.roll),
		}); err != nil {
			return added, fmt.Errorf("%s: %w", p.template, err)
		}
		added++
	}
	return added, nil
}

// play joins the world, as a client, for what only a player does: spending the level's points, unless spent already, and wearing each piece of gear its slot doesn't hold yet.
func (s seeder) play(ctx context.Context, token string, actor *itemv1.Actor, loaded *characterv1.LoadResponse, sd seed) (string, error) {
	c := loaded.GetCharacter()
	spendStats := sd.stats.Total() > 0 && int(c.GetUnspentPoints()) == sd.unspent
	spendSkills := len(sd.skills) > 0 && len(loaded.GetSkills()) == 0 && int(c.GetUnspentSkillPoints()) == sd.unspentSkills
	inv, err := s.items.Inventory(ctx, &itemv1.InventoryRequest{CharacterId: actor.GetCharacterId()})
	if err != nil {
		return "", fmt.Errorf("items: %w", err)
	}
	wear := toWear(inv.GetItems(), sd.gear)
	if !spendStats && !spendSkills && len(wear) == 0 {
		return "", nil
	}
	sess, _, err := s.gw.join(ctx, token, actor.GetCharacterId())
	if err != nil {
		return "", fmt.Errorf("join: %w", err)
	}
	defer sess.close()
	var did []string
	if spendStats {
		p := sd.stats
		if err := sess.do(ctx, &clientv1.SpendPoints{ExpectedUnspent: int32(sd.unspent), Points: &clientv1.Stats{ //nolint:gosec // small
			Str: int32(p.STR), Dex: int32(p.DEX), Int: int32(p.INT), Agi: int32(p.AGI), Vit: int32(p.VIT), //nolint:gosec // small
		}}); err != nil {
			return "", fmt.Errorf("spend points: %w", err)
		}
		did = append(did, "points spent")
	}
	if spendSkills {
		if err := sess.do(ctx, &clientv1.SpendSkillPoints{ExpectedUnspent: int32(sd.unspentSkills), Points: sd.skills}); err != nil { //nolint:gosec // small
			return "", fmt.Errorf("spend skill points: %w", err)
		}
		did = append(did, "skill points spent")
	}
	for _, w := range wear {
		if err := sess.do(ctx, &clientv1.Equip{IdempotencyKey: uuidv7.NewString(), ItemId: w.item, EquipSlot: w.slot}); err != nil {
			return "", fmt.Errorf("equip %s: %w", w.slot, err)
		}
	}
	if len(wear) > 0 {
		did = append(did, fmt.Sprintf("%d worn", len(wear)))
	}
	return strings.Join(did, ", "), nil
}

type wearing struct{ item, slot string }

// toWear pairs each piece of gear whose slot doesn't hold its template, empty or holding a starter, with an item of its template still in the bag.
func toWear(items []*itemv1.Item, gear []piece) []wearing {
	worn := map[string]string{}
	bag := map[string][]string{}
	for _, it := range items {
		if it.GetEquipSlot() != "" {
			worn[it.GetEquipSlot()] = it.GetTemplateId()
		} else {
			bag[it.GetTemplateId()] = append(bag[it.GetTemplateId()], it.GetId())
		}
	}
	var out []wearing
	for _, p := range gear {
		if worn[p.slot] == p.template || len(bag[p.template]) == 0 {
			continue
		}
		out = append(out, wearing{item: bag[p.template][0], slot: p.slot})
		bag[p.template] = bag[p.template][1:]
	}
	return out
}
