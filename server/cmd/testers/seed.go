package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	clientv1 "finga/api/gen/client/v1"
	"finga/internal/adminapi"
	"finga/internal/gamedata"
	"finga/pkg/uuidv7"
)

// seeder keeps the test accounts: the player's paths where there are some, the game's admin API where there are none.
type seeder struct {
	data     *gamedata.Data
	gw       gateway
	admin    adminapi.Client
	password string
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
	id, err := s.gw.character(ctx, token, t.name)
	if err != nil {
		return "", fmt.Errorf("character: %w", err)
	}
	did := "kept"
	if made {
		did = "made"
	}
	loaded, err := s.admin.Character(ctx, token, id)
	if err != nil {
		return "", fmt.Errorf("load: %w", err)
	}
	// Only a character below its seed's level is still as created; one played since keeps its own progress.
	if loaded.Level < sd.level {
		if err := s.progress(ctx, token, id, sd); err != nil {
			return "", fmt.Errorf("progression: %w", err)
		}
		if loaded, err = s.admin.Character(ctx, token, id); err != nil {
			return "", fmt.Errorf("load: %w", err)
		}
		did += ", levelled"
	}
	added, err := s.give(ctx, token, id, append(sd.gear, sd.potions...))
	if err != nil {
		return "", fmt.Errorf("items: %w", err)
	}
	if added > 0 {
		did += fmt.Sprintf(", %d items given", added)
	}
	played, err := s.play(ctx, token, id, loaded, sd)
	if err != nil {
		return "", err
	}
	if played != "" {
		did += ", " + played
	}
	return did, nil
}

// progress writes the seed's level, class, trial, full HP and full energy, with the level's points unspent, as the world's checkpoint does: in town, before the character first joins.
// The login kicks a session playing the character, whose map then saves it: progress waits that long at most.
const leaveWait = 15 * time.Second

func (s seeder) progress(ctx context.Context, token, id string, sd seed) error {
	town := s.data.StartTown()
	p := adminapi.Progress{
		Class: string(sd.class.ID), MapID: town.ID, X: town.PlayerSpawn.X, Y: town.PlayerSpawn.Y, HP: sd.hp, Energy: sd.energy,
		Level: sd.level, UnspentPoints: sd.unspent, UnspentSkillPoints: sd.unspentSkills, Quests: sd.quests,
	}
	deadline := time.Now().Add(leaveWait)
	for {
		err := s.admin.SetProgress(ctx, token, id, p)
		if e, ok := errors.AsType[adminapi.StatusError](err); !ok || e.Status != http.StatusConflict || time.Now().After(deadline) {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond): // still on a map, or its last save under way
		}
	}
}

// give adds each piece the character holds too few of, as drops, under keys of the character and the piece, so a run cut short and run again adds none twice: how many it added.
func (s seeder) give(ctx context.Context, token, id string, pieces []piece) (int, error) {
	inv, err := s.admin.Items(ctx, token, id)
	if err != nil {
		return 0, err
	}
	held := map[string]int{}
	for _, it := range inv {
		held[it.Template]++
	}
	owner, err := uuid.Parse(id)
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
		if err := s.admin.Grant(ctx, token, id, adminapi.Grant{Key: key, Template: p.template, Quantity: p.quantity, Roll: p.roll}); err != nil {
			return added, fmt.Errorf("%s: %w", p.template, err)
		}
		added++
	}
	return added, nil
}

// play joins the world, as a client, for what only a player does: spending the level's points, unless spent already, and wearing each piece of gear its slot doesn't hold yet.
func (s seeder) play(ctx context.Context, token, id string, c adminapi.Character, sd seed) (string, error) {
	spendStats := sd.stats.Total() > 0 && c.UnspentPoints == sd.unspent
	spendSkills := len(sd.skills) > 0 && len(c.Skills) == 0 && c.UnspentSkillPoints == sd.unspentSkills
	inv, err := s.admin.Items(ctx, token, id)
	if err != nil {
		return "", fmt.Errorf("items: %w", err)
	}
	wear := toWear(inv, sd.gear)
	if !spendStats && !spendSkills && len(wear) == 0 {
		return "", nil
	}
	sess, _, err := s.gw.join(ctx, token, id)
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
func toWear(items []adminapi.Item, gear []piece) []wearing {
	worn := map[string]string{}
	bag := map[string][]string{}
	for _, it := range items {
		if it.EquipSlot != "" {
			worn[it.EquipSlot] = it.Template
		} else {
			bag[it.Template] = append(bag[it.Template], it.ID)
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
