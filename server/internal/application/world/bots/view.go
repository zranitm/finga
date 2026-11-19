// Package bots is a match's bots' brains: each decides on its beat what a player's client would send, its keys and presses, from what it sees: itself now, and the others as they were its reaction ago (docs/tech/pvp.md section 8).
package bots

import (
	model "finga/internal/model/world"
	"finga/internal/shared/gamedata"
	"finga/internal/shared/kernel"
	"finga/internal/shared/sim"
)

// View is what a bot knows as it decides at Tick: itself now, and the match's ring, which it reads as of its reaction ago.
type View struct {
	Tick     uint64
	Fighting bool // a round's fight runs
	Self     Self
	Ring     *Ring
	// Holds is whether a follow-up's condition holds for the bot now, with lock its lock, as the world judges a press.
	Holds func(root, skill string, lock model.EntityRef) bool
}

// Self is the bot itself, now.
type Self struct {
	ID        kernel.CharacterID
	Side      int
	State     sim.State
	Energy    int
	Down      bool
	Hard      bool // a hard status holds it
	CantAct   bool
	Silenced  bool
	SkillFrom uint64            // the first tick a skill may start, as its action allows
	Waiting   bool              // a skill use waits for its turn
	Attacking bool              // an attack is queued
	Ready     map[string]uint64 // the tick each skill's cooldown ends
	Buffs     []string          // the buffs running on it
}

// Seen is a fighter as the bot saw them, with their class's basic attack, which anyone can tell from their class.
type Seen struct {
	ID        kernel.CharacterID
	Side      int
	Body      model.Body
	HP, MaxHP int
	Up        bool // not down
	Hard      bool // a hard status holds them
	Attack    gamedata.Shape
}

// Started is an attack or skill a fighter started, as the world showed it at Shown: the ticks it starts, its first hit lands, and its user's next blow may start, its cancel point when it's cancelable, else its end. A blow's has its shape, the aim from where they stood, their lock, and how far they dash or leap toward the lock before it lands.
type Started struct {
	By                      kernel.CharacterID
	Shown, Start, Hit, Next uint64
	Blow                    bool
	Shape                   gamedata.Shape
	Aim                     model.Aim
	Lock                    model.EntityRef
	Toward                  float64
}

// Decision is what a bot does until its next: the keys it holds, and an attack and a skill use if it presses them.
type Decision struct {
	Keys   sim.Input
	Attack *Attack
	Skill  *model.SkillUse
}

// Attack is a basic attack's press: its aim and lock.
type Attack struct {
	Aim  float32
	Lock model.EntityRef
}
