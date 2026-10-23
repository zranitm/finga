# 11. Mobs, combat, and EXP

- **Depends on:** 06, 09, 10
- **Features:** C1, C2, C3, C4
- **Milestone:** M2. Fight and grow
- **Order:** 11
- **Status:** Done
- **Step:** 11. Mobs, combat, and EXP

**Goal:** mobs to fight, attacks that hit, death and respawn, and EXP that levels you up. This completes M2, Fight and grow.

## Scope

- `internal/application/combat`: pure functions for hit chance, damage, and crits (page 5), with a random generator seeded per map instance.
- In `internal/application/world`:
  - Mobs: spawn areas with a maximum, a respawn timer, patrolling on their platform, contact damage with knockback, and 1 second of invulnerability.
  - Attacks: the Warrior's 64 by 56 px box and the Bowman's 420 px line, with cooldowns enforced in the simulation.
  - Damage messages; death at 0 HP; respawn after 3 seconds at the town spawn with 50% HP; Died and Respawned.
  - EXP to the killer (party sharing comes in step 15), LevelUp saved immediately through the character service from a worker, never inside the tick, and the death penalty from step 06's rules.

## Design notes

- Each instance records its random seed, so a tick can be replayed exactly in a test.

## Tests first

- the damage formula with a fixed generator: hit chance clamps, defense and penetration, crits, minimum damage of 1
- the Warrior hits only the nearest mob in the box; the Bowman hits the first in line; nothing in range does nothing
- cooldowns hold even if the client spams Attack
- mobs respawn after the timer and never exceed the maximum
- contact damage, then no damage during the invulnerability window
- death, then respawn after 3 seconds at the town spawn with 50% HP, with the penalty applied (none in town)
- a kill gives EXP and a LevelUp, and the level is saved
- replaying a seed gives the same fight
- end-to-end rows for C1 to C4

## Done when

- [ ] Two test clients can fight mobs and level up.

## Review focus

- All combat resolves on the server's tick.
- Hitbox checks are consistent with the movement code's coordinates.
