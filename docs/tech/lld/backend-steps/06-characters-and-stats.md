# 06. Characters and stats

- **Depends on:** 02, 04
- **Features:** A2, C4
- **Milestone:** Foundations
- **Order:** 6
- **Status:** Done
- **Step:** 06. Characters and stats

> **Changed by step 20 (2026-10-27):** the pure rules (`DeriveStats`, `AwardExp`, `DeathPenalty`) live in `internal/progression`, where the character module and the world's tick both import them.

**Goal:** create and load characters, and compute their stats, EXP, and levels.

**Layers in this step:** repository, service, and pure rules. The REST handlers come in step 07.

## Scope

**Repository** (`internal/application/character/repository.go`)

- Migration for `characters` (page 2), and its sqlc queries.
- Insert; list by account; load by ID and owning account; save a checkpoint (position, HP, MP, EXP); save a level-up (level, EXP, stats, HP, MP).
- The store (`Postgres`): `InTx` with a unit of work over the character repository, plain reads on the pool, and `classify`: `characters_name_key` becomes `ErrNameTaken` (`conflict`), and no row becomes `ErrNotFound`.

**Service** (`internal/application/character/service.go`)

- Create: name rules, a class available in Stage 1, base stats, and the starting map and position from the town's player spawn. It runs in the store's `InTx`. Step 12 adds `Starter()` to that unit of work, so starter items join the same transaction.
- List the account's characters, and load one only if it belongs to the account.
- Save checkpoints and level-ups for the world, called from its worker goroutines in steps 09 and 11.

**Pure rules** (in the same package, with no database)

- Derived stats from class, primary stats, and level, using the game data's coefficients and caps (page 5, OD-18 defaults).
- EXP award and level-up: extra EXP carries over, levels cap at 30, HP and MP are restored.
- Stat points by class preset, the Stage 1 temporary choice: Warrior gets STR 3 and VIT 2, Bowman gets DEX 3 and AGI 2.
- Death penalty: 5% of the current level's requirement, never below the start of the level, none in towns (OD-06 default).

## Design notes

- Level-up takes an allocation plan as input. Stage 1 passes the class preset; Stage 2 passes the player's choice (OD-16) without changing this code.
- At the level cap, EXP stops accumulating.
- The rules are plain functions over values, so the world can call them inside a tick without touching the database.

## Tests first

**Repository** (real Postgres, through the store)

- a duplicate name in different letter case gives `ErrNameTaken`
- loading another account's character gives `ErrNotFound`
- a checkpoint saved, then loaded, matches

**Service** (fake store and unit of work)

- create: valid; a table of invalid names; Thief and Magician rejected in Stage 1; a duplicate name becomes `conflict`
- load: someone else's character gives `not_found`

**Pure rules**

- derived stats for Warrior and Bowman at levels 1, 10, and 30, with expected values worked out by hand from page 5
- caps: crit rate stops at 60%
- EXP: exactly at the threshold, carry-over, several levels at once, nothing beyond the cap
- death penalty: mid-level, near the start of a level (floored), and in town (none)

## Done when

- [ ] Tests pass, and the hand-worked numbers are in the test table.

## Out of scope

- Starter items: they need the item module and come in step 12.

## Review focus

- The formulas and rounding match page 5 exactly.
