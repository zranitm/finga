package load

import (
	"fmt"
	"slices"

	"finga/internal/shared/gamedata"
)

const outfitsFile = "outfits.yaml"

// weaponKinds are the kinds a class can hold, and so a weapon look can be.
var weaponKinds = []gamedata.WeaponKind{gamedata.OneHanded, gamedata.Knife, gamedata.Crossbow, gamedata.Staff}

var lookSlots = []gamedata.LookSlot{gamedata.OutfitSlot, gamedata.HeadwearSlot, gamedata.FaceSlot, gamedata.WeaponSlot, gamedata.WingsSlot}

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
	Rank   string  `yaml:"rank"`
	Quest  string  `yaml:"quest"`
}

// loadOutfits reads the wardrobe's pieces, and gives each a boss drops its item and a place on that boss's drops, and each a quest gives a place on that quest's rewards (outfits.md section 3); a rank gives at most one.
func (l *loader) loadOutfits(dir string) error {
	var file outfitsYAML
	if err := readYAML(dir, outfitsFile, &file); err != nil {
		return err
	}
	held := map[gamedata.WeaponKind]bool{}
	for _, c := range l.classes.list {
		held[c.Weapon] = true
	}
	ranked := map[string]string{} // each rank's piece
	for _, raw := range file.Pieces {
		p, err := l.pieceOf(raw, held, ranked)
		if err != nil {
			return fmt.Errorf("%s: piece %s: %w", outfitsFile, raw.ID, err)
		}
		l.pieces.add(p.ID, p)
	}
	return nil
}

// pieceOf checks raw and makes its piece; one a quest gives joins that quest's rewards, and one a boss drops gets its item, added to items and to each such boss's drops.
func (l *loader) pieceOf(raw pieceYAML, held map[gamedata.WeaponKind]bool, ranked map[string]string) (gamedata.Piece, error) {
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
		s, err := l.sourceOf(src)
		if err != nil {
			return p, err
		}
		if s.Rank != "" {
			if other, ok := ranked[s.Rank]; ok {
				return p, fmt.Errorf("rank %s gives %s already, and a rank gives one piece", s.Rank, other)
			}
			ranked[s.Rank] = p.ID
		}
		if s.Quest != "" {
			q := &l.quests.list[l.quests.idx[s.Quest]]
			if slices.Contains(q.Rewards.Pieces, p.ID) {
				return p, fmt.Errorf("names quest %s twice", s.Quest)
			}
			q.Rewards.Pieces = append(q.Rewards.Pieces, p.ID)
		}
		p.From = append(p.From, s)
	}
	if !slices.ContainsFunc(p.From, func(s gamedata.PieceSource) bool { return s.Boss != "" }) {
		return p, nil // granted outright, so no item holds it
	}
	p.Item = gamedata.PieceItem(p.ID)
	if l.items.has(p.Item) {
		return p, fmt.Errorf("its item %s clashes with %s", p.Item, itemsFile)
	}
	l.items.add(p.Item, gamedata.Item{ID: p.Item, Name: p.Name, Tab: gamedata.Etc, Stars: p.Stars, Description: "Adds " + p.Name + " to your wardrobe.", MaxStack: defaultMaxStack})
	for _, src := range p.From {
		if src.Boss == "" {
			continue
		}
		k := l.mobs.idx[src.Boss]
		l.mobs.list[k].Drops = append(l.mobs.list[k].Drops, gamedata.Drop{Item: p.Item, Chance: src.Chance, Min: 1, Max: 1})
	}
	return p, nil
}

// sourceOf checks one of a piece's sources: a field boss at a chance in (0, 1], a rank of pvp.yaml above the first, which everyone has, or a quest of quests.yaml.
func (l *loader) sourceOf(src sourceYAML) (gamedata.PieceSource, error) {
	s := gamedata.PieceSource{Boss: src.Boss, Chance: src.Chance, Rank: src.Rank, Quest: src.Quest}
	switch {
	case src.Rank != "":
		if src.Boss != "" || src.Chance != 0 || src.Quest != "" {
			return s, fmt.Errorf("from rank %s: a rank gives its piece outright, with no boss, chance or quest", src.Rank)
		}
		if i := slices.IndexFunc(l.pvp.Ranks, func(r gamedata.Rank) bool { return r.ID == src.Rank }); i < 1 {
			return s, fmt.Errorf("a rank source names a rank of %s above the first, got %q", pvpFile, src.Rank)
		}
		return s, nil
	case src.Quest != "":
		if src.Boss != "" || src.Chance != 0 {
			return s, fmt.Errorf("from quest %s: a hand-in gives its piece outright, with no boss or chance", src.Quest)
		}
		if !l.quests.has(src.Quest) {
			return s, fmt.Errorf("a quest source names a quest of %s, got %q", questsFile, src.Quest)
		}
		return s, nil
	}
	boss, ok := l.mobs.get(src.Boss)
	if !ok || !boss.Boss {
		return s, fmt.Errorf("a source names a field boss, a rank or a quest, got %q", src.Boss)
	}
	if !(src.Chance > 0 && src.Chance <= 1) {
		return s, fmt.Errorf("from %s: chance must be in (0, 1], got %v", src.Boss, src.Chance)
	}
	return s, nil
}
