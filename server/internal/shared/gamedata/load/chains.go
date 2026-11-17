package load

import (
	"errors"
	"fmt"
	"slices"
	"time"

	"finga/internal/shared/gamedata"
)

type chainYAML struct {
	Root   string       `yaml:"root"`
	Follow []followYAML `yaml:"follow"`
}

type followYAML struct {
	Skill string   `yaml:"skill"`
	When  whenYAML `yaml:"when"`
}

type whenYAML struct {
	After      *time.Duration `yaml:"after"`
	Controlled []string       `yaml:"controlled"`
	Far        *float64       `yaml:"far"`
	Dodged     *time.Duration `yaml:"dodged"`
	// ControlledSelf is true when given: the player holds a hard status.
	ControlledSelf *bool `yaml:"controlled_self"`
}

// The window an after or dodged condition may hold for.
const (
	shortestWindow = 200 * time.Millisecond
	longestWindow  = 5 * time.Second
)

// chainsOf checks a class's chains as statuses.yaml has them: each has a root and follow-ups, each follow-up a skill and exactly one condition in range; checkChains holds their skills to skills.yaml.
func (l *loader) chainsOf(raw []chainYAML) ([]gamedata.Chain, error) {
	var out []gamedata.Chain
	for _, c := range raw {
		if c.Root == "" || len(c.Follow) == 0 {
			return nil, errors.New("a chain needs a root and at least one follow-up")
		}
		ch := gamedata.Chain{Root: c.Root}
		for _, f := range c.Follow {
			when, err := l.conditionOf(f.When)
			if err != nil {
				return nil, fmt.Errorf("chain %s: follow-up %q: %w", c.Root, f.Skill, err)
			}
			ch.Follow = append(ch.Follow, gamedata.FollowUp{Skill: f.Skill, When: when})
		}
		out = append(out, ch)
	}
	return out, nil
}

func (l *loader) conditionOf(w whenYAML) (gamedata.ChainCondition, error) {
	keys := 0
	for _, set := range []bool{w.After != nil, w.Controlled != nil, w.Far != nil, w.Dodged != nil, w.ControlledSelf != nil} {
		if set {
			keys++
		}
	}
	window := func(d *time.Duration) bool { return d == nil || *d >= shortestWindow && *d <= longestWindow }
	switch {
	case keys != 1:
		return gamedata.ChainCondition{}, errors.New("when holds exactly one of after, controlled, far, dodged and controlled_self")
	case w.ControlledSelf != nil && !*w.ControlledSelf:
		return gamedata.ChainCondition{}, errors.New("controlled_self is true when given")
	case w.Controlled != nil && len(w.Controlled) == 0:
		return gamedata.ChainCondition{}, errors.New("controlled names at least one status")
	case !window(w.After) || !window(w.Dodged):
		return gamedata.ChainCondition{}, fmt.Errorf("after and dodged run from %v to %v", shortestWindow, longestWindow)
	case w.Far != nil && !(*w.Far > 0):
		return gamedata.ChainCondition{}, errors.New("far is more than 0 metres")
	}
	for _, id := range w.Controlled {
		if s, ok := l.statuses.get(id); !ok || s.Group != gamedata.Hard {
			return gamedata.ChainCondition{}, fmt.Errorf("controlled: %s isn't a hard status in %s", id, statusesFile)
		}
	}
	c := gamedata.ChainCondition{Controlled: w.Controlled, ControlledSelf: w.ControlledSelf != nil}
	if w.After != nil {
		c.After = *w.After
	}
	if w.Far != nil {
		c.Far = *w.Far
	}
	if w.Dodged != nil {
		c.Dodged = *w.Dodged
	}
	return c, nil
}

// checkChains holds each class's chains to skills.yaml: the class can learn each root and follow-up, none is a passive, no root starts two chains, no follow-up is its own root or listed twice in one chain; a break-free skill comes up only while the player is held, and only it does, since while held every other skill is refused; and each class's own break-free is in one of its chains, so no key holds it alone.
func (l *loader) checkChains(string) error {
	for _, c := range l.classes.list {
		var roots []string
		for _, ch := range c.Chains {
			if slices.Contains(roots, ch.Root) {
				return fmt.Errorf("%s: class %s: %s roots two chains", classesFile, c.ID, ch.Root)
			}
			roots = append(roots, ch.Root)
			ids := []string{ch.Root}
			for _, f := range ch.Follow {
				if slices.Contains(ids, f.Skill) {
					return fmt.Errorf("%s: class %s: chain %s: %s is its root or listed twice", classesFile, c.ID, ch.Root, f.Skill)
				}
				ids = append(ids, f.Skill)
				if s, ok := l.skills.get(f.Skill); ok && s.BreaksControl != f.When.ControlledSelf {
					return fmt.Errorf("%s: class %s: chain %s: %s: a break-free skill follows on controlled_self, and only one does", classesFile, c.ID, ch.Root, f.Skill)
				}
			}
			if s, ok := l.skills.get(ch.Root); ok && s.BreaksControl {
				return fmt.Errorf("%s: class %s: %s breaks control, so it's a follow-up on controlled_self, never a root", classesFile, c.ID, ch.Root)
			}
			for _, id := range ids {
				s, ok := l.skills.get(id)
				switch {
				case !ok || !c.Learnable(s):
					return fmt.Errorf("%s: class %s: chain %s: %s isn't a skill the class can learn", classesFile, c.ID, ch.Root, id)
				case s.Kind == gamedata.Passive:
					return fmt.Errorf("%s: class %s: chain %s: %s is a passive, which no key uses", classesFile, c.ID, ch.Root, id)
				}
			}
		}
		for _, id := range c.Skills {
			if s, ok := l.skills.get(id); ok && s.BreaksControl && !slices.ContainsFunc(c.Chains, func(ch gamedata.Chain) bool {
				return slices.ContainsFunc(ch.Follow, func(f gamedata.FollowUp) bool { return f.Skill == id })
			}) {
				return fmt.Errorf("%s: class %s: %s breaks control but follows in none of its chains; it goes from its escape root's key alone", classesFile, c.ID, id)
			}
		}
	}
	return nil
}
