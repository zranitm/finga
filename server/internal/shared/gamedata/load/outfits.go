package load

import (
	"fmt"
	"slices"

	"finga/internal/shared/gamedata"
)

const outfitsFile = "outfits.yaml"

// weaponKinds are the kinds a class can hold, and so a weapon look can be.
var weaponKinds = []gamedata.WeaponKind{gamedata.OneHanded, gamedata.Knife, gamedata.Crossbow, gamedata.Staff}

var lookSlots = []gamedata.LookSlot{gamedata.OutfitSlot, gamedata.HeadwearSlot, gamedata.FaceSlot, gamedata.WeaponSlot}

type outfitsYAML struct {
	Pieces []pieceYAML `yaml:"pieces"`
}

type pieceYAML struct {
	ID    string       `yaml:"id"`
	Slot  string       `yaml:"slot"`
	Kind  string       `yaml:"kind"`
	Name  string       `yaml:"name"`
	Stars int          `yaml:"stars"`
	Look  string       `yaml:"look"`
	From  []sourceYAML `yaml:"from"`
}

type sourceYAML struct {
	Boss   string  `yaml:"boss"`
	Chance float64 `yaml:"chance"`
}

// loadOutfits reads the wardrobe's pieces, and gives each a boss drops its item and a place on that boss's drops (outfits.md section 3).
func (l *loader) loadOutfits(dir string) error {
	var file outfitsYAML
	if err := readYAML(dir, outfitsFile, &file); err != nil {
		return err
	}
	held := map[gamedata.WeaponKind]bool{}
	for _, c := range l.classes.list {
		held[c.Weapon] = true
	}
	for _, raw := range file.Pieces {
		p, err := l.pieceOf(raw, held)
		if err != nil {
			return fmt.Errorf("%s: piece %s: %w", outfitsFile, raw.ID, err)
		}
		l.pieces.add(p.ID, p)
	}
	return nil
}

// pieceOf checks raw and makes its piece, adding its item to items and to each boss's drops.
func (l *loader) pieceOf(raw pieceYAML, held map[gamedata.WeaponKind]bool) (gamedata.Piece, error) {
	p := gamedata.Piece{ID: raw.ID, Slot: gamedata.LookSlot(raw.Slot), Kind: gamedata.WeaponKind(raw.Kind), Name: raw.Name, Stars: raw.Stars, Look: raw.Look}
	switch {
	case p.ID == "" || l.pieces.has(p.ID):
		return p, fmt.Errorf("needs an id of its own")
	case !slices.Contains(lookSlots, p.Slot):
		return p, fmt.Errorf("slot is one of %v, got %q", lookSlots, raw.Slot)
	case p.Slot == gamedata.WeaponSlot && !held[p.Kind]:
		return p, fmt.Errorf("a weapon look names a kind a class holds, got %q", raw.Kind)
	case p.Slot != gamedata.WeaponSlot && p.Kind != "":
		return p, fmt.Errorf("only a weapon look names a kind")
	case p.Name == "" || p.Look == "":
		return p, fmt.Errorf("needs a name and a look")
	case p.Stars < 1 || p.Stars > maxStars:
		return p, fmt.Errorf("stars must be 1 to %d, got %d", maxStars, p.Stars)
	case len(raw.From) == 0:
		return p, fmt.Errorf("needs a source")
	}
	for _, src := range raw.From {
		boss, ok := l.mobs.get(src.Boss)
		if !ok || !boss.Boss {
			return p, fmt.Errorf("a source names a field boss, got %q", src.Boss)
		}
		if !(src.Chance > 0 && src.Chance <= 1) {
			return p, fmt.Errorf("from %s: chance must be in (0, 1], got %v", src.Boss, src.Chance)
		}
		p.From = append(p.From, gamedata.PieceSource(src))
	}
	p.Item = gamedata.PieceItem(p.ID)
	if l.items.has(p.Item) {
		return p, fmt.Errorf("its item %s clashes with %s", p.Item, itemsFile)
	}
	l.items.add(p.Item, gamedata.Item{ID: p.Item, Name: p.Name, Tab: gamedata.Etc, Stars: p.Stars, Description: "Adds " + p.Name + " to your wardrobe.", MaxStack: defaultMaxStack})
	for _, src := range p.From {
		k := l.mobs.idx[src.Boss]
		l.mobs.list[k].Drops = append(l.mobs.list[k].Drops, gamedata.Drop{Item: p.Item, Chance: src.Chance, Min: 1, Max: 1})
	}
	return p, nil
}
