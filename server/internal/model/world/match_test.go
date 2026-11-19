package world_test

import (
	"encoding/binary"
	"testing"

	model "finga/internal/model/world"
	"finga/internal/shared/gamedata"
	"finga/internal/shared/gamedata/gamedatatest"
	"finga/internal/shared/kernel"
)

// sweep is enough bot ids, the same each run, for every piece a bot may wear to come up.
const sweep = 400

func botID(n int) kernel.CharacterID {
	var raw [16]byte
	binary.BigEndian.PutUint64(raw[8:], uint64(n)) //nolint:gosec // n counts up from 0
	return kernel.CharacterID(raw)
}

// picks are a look's pieces by slot, "" for the class's own.
func picks(l model.Look) map[gamedata.LookSlot]string {
	return map[gamedata.LookSlot]string{gamedata.OutfitSlot: l.Outfit, gamedata.HeadwearSlot: l.Headwear, gamedata.FaceSlot: l.Face, gamedata.WeaponSlot: l.Weapon, gamedata.WingsSlot: l.Wings}
}

// Every class's bots, over ids the same each run: one id wears one look; each slot is the class's own or a boss piece of that slot, a weapon look of the class's kind; and every such piece comes up.
func TestABotWearsBossPiecesOfItsClass(t *testing.T) {
	d := gamedatatest.Shipped(t)
	for _, c := range d.Classes() {
		wearable, seen, own := map[string]bool{}, map[string]bool{}, map[gamedata.LookSlot]bool{}
		for _, p := range d.Pieces() {
			if p.Slot != gamedata.WeaponSlot || p.Kind == c.Weapon {
				wearable[p.ID] = true
			}
		}
		for n := range sweep {
			look := model.BotLook(d.Pieces(), c.Weapon, botID(n))
			if again := model.BotLook(d.Pieces(), c.Weapon, botID(n)); again != look || look.Revision != 1 {
				t.Fatalf("%s bot %d wore %+v, then %+v", c.ID, n, look, again)
			}
			for slot, id := range picks(look) {
				if id == "" {
					own[slot] = true
					continue
				}
				p, ok := d.Piece(id)
				if !ok || p.Slot != slot || !wearable[id] {
					t.Errorf("a %s bot wears %q as its %s: %+v", c.ID, id, slot, p)
				}
				seen[id] = true
			}
		}
		for id := range wearable {
			if !seen[id] {
				t.Errorf("no %s bot of %d wore %s", c.ID, sweep, id)
			}
		}
		for _, slot := range []gamedata.LookSlot{gamedata.OutfitSlot, gamedata.HeadwearSlot, gamedata.WeaponSlot} {
			if !own[slot] {
				t.Errorf("every %s bot wore a piece as its %s, none the class's own", c.ID, slot)
			}
		}
	}
}

// A piece with any source but a boss never shows on a bot, as rank, quest and feat pieces will have, so a rank piece always means a player earned it.
func TestABotWearsNoPieceFromElsewhere(t *testing.T) {
	d := gamedatatest.Shipped(t)
	pieces := append(d.Pieces()[:len(d.Pieces()):len(d.Pieces())],
		gamedata.Piece{ID: "ranked_plate", Slot: gamedata.OutfitSlot, From: []gamedata.PieceSource{{Boss: "king_slime", Chance: 0.05}, {}}},
		gamedata.Piece{ID: "sourceless_hat", Slot: gamedata.HeadwearSlot},
	)
	for n := range sweep {
		look := model.BotLook(pieces, gamedata.OneHanded, botID(n))
		if look.Outfit == "ranked_plate" || look.Headwear == "sourceless_hat" {
			t.Fatalf("bot %d wears %+v", n, look)
		}
	}
}
