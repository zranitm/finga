package load

import (
	"cmp"
	"errors"
	"fmt"
	"math"
	"slices"
	"time"

	"finga/internal/shared/gamedata"
	"finga/internal/shared/kernel"
	"finga/internal/shared/progression"
)

const (
	pvpFile     = "pvp.yaml"
	fewestNames = 30 // enough that no match runs out
)

type pvpYAML struct {
	Modes map[string]modeYAML `yaml:"modes"`
	Queue struct {
		BotsAfter time.Duration `yaml:"bots_after"`
		Offer     time.Duration `yaml:"offer"`
		Window    struct {
			Start     float64 `yaml:"start"`
			PerSecond float64 `yaml:"per_second"`
			Most      float64 `yaml:"most"`
		} `yaml:"window"`
	} `yaml:"queue"`
	Rounds struct {
		Prep      time.Duration `yaml:"prep"`
		Countdown time.Duration `yaml:"countdown"`
		Between   time.Duration `yaml:"between"`
	} `yaml:"rounds"`
	Result time.Duration `yaml:"result"`
	Kit    struct {
		Level   int                                `yaml:"level"`
		Classes map[kernel.Class]gamedata.PvpScale `yaml:"classes"`
	} `yaml:"kit"`
	Scale  gamedata.PvpScale `yaml:"scale"`
	Rating struct {
		Mu    float64 `yaml:"mu"`
		Sigma float64 `yaml:"sigma"`
		Beta  float64 `yaml:"beta"`
		Tau   float64 `yaml:"tau"`
		Shown struct {
			Base     float64 `yaml:"base"`
			PerPoint float64 `yaml:"per_point"`
		} `yaml:"shown"`
		Placement int `yaml:"placement"`
	} `yaml:"rating"`
	Ranks []struct {
		ID   string `yaml:"id"`
		From int    `yaml:"from"`
	} `yaml:"ranks"`
	Bots struct {
		Most  int `yaml:"most"`
		Tiers []struct {
			ID        string        `yaml:"id"`
			Mu        float64       `yaml:"mu"`
			Reaction  time.Duration `yaml:"reaction"`
			Defend    float64       `yaml:"defend"`
			Chain     float64       `yaml:"chain"`
			BreakFree time.Duration `yaml:"break_free"`
			Aim       float64       `yaml:"aim"` // degrees
		} `yaml:"tiers"`
		Names  []string               `yaml:"names"`
		Builds map[string][]buildYAML `yaml:"builds"`
	} `yaml:"bots"`
}

type modeYAML struct {
	Name       string `yaml:"name"`
	Team       int    `yaml:"team"`
	Party      int    `yaml:"party"`
	Map        string `yaml:"map"`
	Rated      bool   `yaml:"rated"`
	RatingFrom string `yaml:"rating_from"`
	Rounds     struct {
		BestOf int           `yaml:"best_of"`
		Most   int           `yaml:"most"`
		Length time.Duration `yaml:"length"`
	} `yaml:"rounds"`
}

type buildYAML struct {
	Bar    []string          `yaml:"bar"`
	Traits map[string]string `yaml:"traits"`
	Order  []string          `yaml:"order"`
}

// loadPvp reads pvp.yaml, checks it against the classes, skills, items and maps, and works out each promoted class's kit.
func (l *loader) loadPvp(dir string) error {
	var y pvpYAML
	if err := readYAML(dir, pvpFile, &y); err != nil {
		return err
	}
	var p gamedata.PvpRules
	var err error
	if p.Modes, err = l.pvpModes(y.Modes); err != nil {
		return fmt.Errorf("%s: %w", pvpFile, err)
	}
	q, r := y.Queue, y.Rounds
	switch w := q.Window; {
	case q.BotsAfter <= 0 || q.Offer <= 0:
		return fmt.Errorf("%s: queue's bots_after and offer must be over 0", pvpFile)
	case !nonNegative(w.Start) || !nonNegative(w.PerSecond) || !(w.Most >= w.Start) || math.IsInf(w.Most, 0):
		return fmt.Errorf("%s: queue's window needs 0 <= start <= most and per_second not below 0", pvpFile)
	case r.Prep <= 0 || r.Countdown <= 0 || r.Between <= 0 || y.Result <= 0:
		return fmt.Errorf("%s: rounds' prep, countdown and between, and result, must be over 0", pvpFile)
	case !positive(y.Scale.HP) || !positive(y.Scale.Damage):
		return fmt.Errorf("%s: scale's hp and damage must be over 0", pvpFile)
	}
	p.Queue = gamedata.PvpQueue{BotsAfter: q.BotsAfter, Offer: q.Offer, Window: gamedata.QueueWindow(q.Window)}
	p.Rounds, p.Result, p.Scale = gamedata.PvpRounds(r), y.Result, y.Scale
	if p.Kits, err = l.kits(y.Kit.Level, y.Kit.Classes); err != nil {
		return fmt.Errorf("%s: kit: %w", pvpFile, err)
	}
	if p.Rating, err = ratingRules(y); err != nil {
		return fmt.Errorf("%s: rating: %w", pvpFile, err)
	}
	for i, rk := range y.Ranks {
		if rk.ID == "" || slices.ContainsFunc(p.Ranks, func(o gamedata.Rank) bool { return o.ID == rk.ID }) {
			return fmt.Errorf("%s: rank %d needs an id of its own", pvpFile, i+1)
		}
		if (i == 0) != (rk.From == 0) || i > 0 && rk.From <= y.Ranks[i-1].From {
			return fmt.Errorf("%s: ranks rise from 0: rank %s from %d", pvpFile, rk.ID, rk.From)
		}
		p.Ranks = append(p.Ranks, gamedata.Rank{ID: rk.ID, From: rk.From})
	}
	if len(p.Ranks) == 0 {
		return fmt.Errorf("%s: no ranks", pvpFile)
	}
	if p.Bots, err = l.botRules(y); err != nil {
		return fmt.Errorf("%s: bots: %w", pvpFile, err)
	}
	l.pvp = p
	return nil
}

// pvpModes checks each mode against the maps, by team size.
func (l *loader) pvpModes(modes map[string]modeYAML) ([]gamedata.PvpMode, error) {
	if len(modes) == 0 {
		return nil, errors.New("no modes")
	}
	out := make([]gamedata.PvpMode, 0, len(modes))
	for id, m := range modes {
		mp, ok := l.maps.get(m.Map)
		r := m.Rounds
		switch {
		case id == "" || m.Name == "":
			return nil, fmt.Errorf("mode %q needs an id and a name", id)
		case m.Team < 1 || m.Party < 1 || m.Party > m.Team:
			return nil, fmt.Errorf("mode %s: team must be positive, and party from 1 to the team", id)
		case !ok || mp.Kind != string(gamedata.Pvp):
			return nil, fmt.Errorf("mode %s: map %q is no pvp map", id, m.Map)
		case len(mp.Sides[0]) < m.Team || len(mp.Sides[1]) < m.Team:
			return nil, fmt.Errorf("mode %s: map %s has fewer than %d spots a side", id, m.Map, m.Team)
		case r.BestOf < 1 || r.BestOf%2 == 0 || r.Most < r.BestOf || r.Length <= 0:
			return nil, fmt.Errorf("mode %s: rounds need an odd best_of, most at least best_of, and a length over 0", id)
		case m.Rated == (m.RatingFrom != ""):
			return nil, fmt.Errorf("mode %s: a rated mode has its own rating, and an unrated one reads rating_from's", id)
		}
		out = append(out, gamedata.PvpMode{
			ID: id, Name: m.Name, Team: m.Team, Party: m.Party, Map: m.Map, Rated: m.Rated, RatingFrom: m.RatingFrom,
			Rounds: gamedata.ModeRounds{BestOf: r.BestOf, Most: r.Most, Length: r.Length},
		})
	}
	for _, m := range out {
		if m.RatingFrom == "" {
			continue
		}
		if k := slices.IndexFunc(out, func(o gamedata.PvpMode) bool { return o.ID == m.RatingFrom }); k < 0 || !out[k].Rated {
			return nil, fmt.Errorf("mode %s: rating_from %q is no rated mode", m.ID, m.RatingFrom)
		}
	}
	slices.SortFunc(out, func(a, b gamedata.PvpMode) int { return cmp.Or(cmp.Compare(a.Team, b.Team), cmp.Compare(a.ID, b.ID)) })
	return out, nil
}

// kits is each promoted class's kit at level: the root's stats and the level's points by the class's split, in each slot the item it can wear with the highest level up to it, then more stars, then the lower id, and its factors from classes, which name every promoted class and no other.
func (l *loader) kits(level int, classes map[kernel.Class]gamedata.PvpScale) (map[kernel.Class]gamedata.Kit, error) {
	if level < 1 || level > l.levelCap {
		return nil, fmt.Errorf("level %d is not from 1 to the cap %d", level, l.levelCap)
	}
	for id := range classes {
		if c, ok := l.classes.get(id); !ok || c.Parent == "" {
			return nil, fmt.Errorf("classes: %s is no promoted class", id)
		}
	}
	out := map[kernel.Class]gamedata.Kit{}
	for _, c := range l.classes.list {
		if c.Parent == "" {
			continue
		}
		f, ok := classes[c.ID]
		if !ok || !positive(f.HP) || !positive(f.Damage) {
			return nil, fmt.Errorf("classes: %s needs its hp and damage, each over 0", c.ID)
		}
		root, _ := l.classes.get(c.Line[len(c.Line)-1])
		own := progression.AddStats(root.BaseStats, autoAssigned(c.AutoAssign, (level-1)*l.stats.PointsPerLevel, l.stats.PointsPerLevel))
		kit := gamedata.Kit{Level: level, Own: own, HP: f.HP, Damage: f.Damage}
		for _, slot := range l.slots {
			best, found := gamedata.Item{}, false
			for _, it := range l.items.list {
				if it.Slot != slot.Kind || progression.Meets(it, level, c, own) != progression.NothingUnmet {
					continue
				}
				if !found || cmp.Or(cmp.Compare(it.MinLevel, best.MinLevel), cmp.Compare(it.Stars, best.Stars), cmp.Compare(best.ID, it.ID)) > 0 {
					best, found = it, true
				}
			}
			if found {
				kit.Worn = append(kit.Worn, gamedata.KitItem{Slot: slot.ID, Item: best.ID})
			}
		}
		out[c.ID] = kit
	}
	return out, nil
}

// autoAssigned spends n points by split as Auto-assign does: whole splits of per, then what's left stat by stat.
func autoAssigned(split gamedata.Stats, n, per int) gamedata.Stats {
	whole, left := n/per, n%per
	spend := func(share int) int {
		extra := min(share, left)
		left -= extra
		return share*whole + extra
	}
	return gamedata.Stats{STR: spend(split.STR), DEX: spend(split.DEX), INT: spend(split.INT), AGI: spend(split.AGI), VIT: spend(split.VIT)}
}

func ratingRules(y pvpYAML) (gamedata.RatingRules, error) {
	r := y.Rating
	if !positive(r.Mu) || !positive(r.Sigma) || !positive(r.Beta) || !nonNegative(r.Tau) || !positive(r.Shown.PerPoint) || !finite(r.Shown.Base) || r.Placement < 0 {
		return gamedata.RatingRules{}, errors.New("mu, sigma, beta and shown.per_point must be over 0, tau and placement not below 0")
	}
	return gamedata.RatingRules{Mu: r.Mu, Sigma: r.Sigma, Beta: r.Beta, Tau: r.Tau, Shown: gamedata.ShownRating(r.Shown), Placement: r.Placement}, nil
}

// botRules checks the tiers rise, the names suffice, and every promoted class has builds it can use.
func (l *loader) botRules(y pvpYAML) (gamedata.BotRules, error) {
	b := y.Bots
	out := gamedata.BotRules{Most: b.Most, Builds: map[kernel.Class][]gamedata.BotBuild{}}
	if b.Most < 1 {
		return out, errors.New("most must be positive")
	}
	for i, t := range b.Tiers {
		switch {
		case t.ID == "" || slices.ContainsFunc(out.Tiers, func(o gamedata.BotTier) bool { return o.ID == t.ID }):
			return out, fmt.Errorf("tier %d needs an id of its own", i+1)
		case i > 0 && !(t.Mu > b.Tiers[i-1].Mu):
			return out, fmt.Errorf("tier %s: mu must rise", t.ID)
		case t.Reaction < 0 || t.BreakFree < 0 || !nonNegative(t.Aim) || !(t.Defend >= 0 && t.Defend <= 1) || !(t.Chain >= 0 && t.Chain <= 1):
			return out, fmt.Errorf("tier %s: reaction, break_free and aim not below 0, defend and chain from 0 to 1", t.ID)
		case t.Reaction >= gamedata.BotMemory:
			return out, fmt.Errorf("tier %s: reaction %s, under the %s a bot remembers", t.ID, t.Reaction, gamedata.BotMemory)
		}
		out.Tiers = append(out.Tiers, gamedata.BotTier{ID: t.ID, Mu: t.Mu, Reaction: t.Reaction, Defend: t.Defend, Chain: t.Chain, BreakFree: t.BreakFree, Aim: radians(t.Aim)})
	}
	if len(out.Tiers) == 0 {
		return out, errors.New("no tiers")
	}
	for _, n := range b.Names {
		if n == "" || slices.Contains(out.Names, n) {
			return out, fmt.Errorf("name %q is empty or twice", n)
		}
		out.Names = append(out.Names, n)
	}
	if len(out.Names) < fewestNames {
		return out, fmt.Errorf("%d names, fewer than %d", len(out.Names), fewestNames)
	}
	for id, builds := range b.Builds {
		c, ok := l.classes.get(kernel.Class(id))
		if !ok || c.Parent == "" {
			return out, fmt.Errorf("builds for %q, no promoted class", id)
		}
		for i, bd := range builds {
			if err := l.botBuild(c, bd); err != nil {
				return out, fmt.Errorf("%s's build %d: %w", id, i+1, err)
			}
			out.Builds[c.ID] = append(out.Builds[c.ID], gamedata.BotBuild{Bar: bd.Bar, Traits: bd.Traits, Order: bd.Order})
		}
	}
	for _, c := range l.classes.list {
		if _, ok := out.Builds[c.ID]; c.Parent != "" && !ok {
			return out, fmt.Errorf("class %s has no build", c.ID)
		}
	}
	return out, nil
}

// botBuild checks a bot's build is a pvp build of c, and that it uses only skills on its keys, each pressable on its own.
func (l *loader) botBuild(c gamedata.Class, b buildYAML) error {
	if err := gamedata.PvpBuildFault(c, l.skills.get, b.Bar, b.Traits); err != nil {
		return err
	}
	if len(b.Order) == 0 {
		return errors.New("an empty order")
	}
	for _, s := range b.Order {
		if !slices.Contains(b.Bar, s) || !c.Keyable(s) {
			return fmt.Errorf("order's %q is not on a key of its own", s)
		}
	}
	return nil
}
