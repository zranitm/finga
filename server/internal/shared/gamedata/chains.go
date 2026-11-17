package gamedata

import (
	"slices"
	"time"
)

// Chain is a root skill and the follow-ups its key offers in their order, each while its condition holds (skill-chains.md).
type Chain struct {
	Root   string
	Follow []FollowUp
}

// FollowUp is a skill a root's key offers while When holds.
type FollowUp struct {
	Skill string
	When  ChainCondition
}

// ChainCondition holds exactly one of its fields.
type ChainCondition struct {
	After      time.Duration // within this of the root's hit
	Controlled []string      // the lock holds one of these hard statuses
	Far        float64       // the lock farther than this across the ground, in metres
	Dodged     time.Duration // within this of the player's last dodge
}

// FollowUp finds skill among the follow-ups of the class's chain from root.
func (c Class) FollowUp(root, skill string) (FollowUp, bool) {
	for _, ch := range c.Chains {
		if ch.Root != root {
			continue
		}
		if i := slices.IndexFunc(ch.Follow, func(f FollowUp) bool { return f.Skill == skill }); i >= 0 {
			return ch.Follow[i], true
		}
	}
	return FollowUp{}, false
}

// Keyable reports whether skill may be used from its own key: a root of the class's chains, or in none of them.
func (c Class) Keyable(skill string) bool {
	for _, ch := range c.Chains {
		if ch.Root == skill {
			return true
		}
	}
	for _, ch := range c.Chains {
		if slices.ContainsFunc(ch.Follow, func(f FollowUp) bool { return f.Skill == skill }) {
			return false
		}
	}
	return true
}
