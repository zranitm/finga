package model

import (
	"github.com/google/uuid"

	"finga/internal/kernel"
	"finga/internal/progression"
)

// PromotionOffer is a trainer's promotion as its dialog shows it: each condition, with what the character has.
type PromotionOffer struct {
	Class      kernel.Class
	Conditions []Condition
}

// Met reports whether the character meets every condition.
func (o PromotionOffer) Met() bool {
	for _, c := range o.Conditions {
		if c.Have < c.Need {
			return false
		}
	}
	return true
}

// ConditionKind is what a promotion condition counts.
type ConditionKind uint8

// The condition kinds.
const (
	ConditionLevel ConditionKind = iota + 1
	ConditionStat                // own stat points, without gear
	ConditionSkill               // points invested in the skill
	ConditionQuest               // finished: need 1, and have 1 once it is
)

// Condition is one thing a promotion asks, met when Have reaches Need.
type Condition struct {
	Kind ConditionKind
	ID   string // the stat, skill or quest; empty for the level
	Need int
	Have int
}

// Promoted is the player's character in its new class, with HP and MP restored and the new sheet, as LevelUp carries it.
type Promoted LevelUp

func (Promoted) event() {}

// GiftCall gives one item under its own key, which makes it given once whatever the tries: a trainer's weapon at a promotion.
type GiftCall struct {
	Account   kernel.AccountID // scopes the key
	Character kernel.CharacterID
	Key       uuid.UUID
	Template  string
	Stats     *progression.ItemStats // equipment's roll
}
