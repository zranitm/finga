# C10. Mobs, attack, damage and death

- **Depends on:** C09, B19
- **Features:** C1, C2, C3, C4
- **Milestone:** M2. Fight and grow
- **Order:** 10
- **Status:** Done
- **Step:** C10. Mobs, attack, damage and death

**Goal:** fight mobs, see damage, die and respawn, level up, all driven by the server's messages.

**Layer in this step:** `game/entities/mob`, `game/entities/damage-number`, `game/systems/combat-view`, `features/hud` (page 7 layer 4).

## Scope

- Mob entities from Snapshot and Delta (interpolated like players), with HP bars and the four Stage 1 mob types' animations; removed on `removed_mobs`.
- Attack: the attack key sends `Attack` with the current sequence number, respecting the class cooldown locally only to avoid spamming, with the value from the generated classes data (600 ms Warrior, 800 ms Bowman today); the server decides hits. The Warrior swing and Bowman arrow play as animations in the facing direction.
- Damage numbers from `Damage` (`p:<id>` and `m:<id>` sources), rising and fading; a miss shows MISS.
- Died: the character falls, the input sender stops sending, and the respawn overlay waits for `Respawned`, 3 s later. Away from town, Respawned is followed by `MapChange` and a Snapshot, which rebuild the scene (the same path portals use in C12). On a map that respawns in place, Respawned alone moves the player and resets prediction.
- The EXP lost to the death penalty arrives as an `ExpGained` with a negative amount (backend step 19); the HUD shows it as a loss, not a gain.
- `ExpGained` and `LevelUp` update the HUD; a level-up effect plays on the entity.
- Contact-damage knockback shows through reconciliation: the server moves the player and the next Delta differs from the prediction (C08). The invulnerability flash follows the player state's `invulnerable` flag (backend step 19), not a local timer.

## Design notes

- The client never computes damage, hit chance or EXP; page 5's formulas stay on the server.
- `MapChange` and same-map `Respawned` handling are written once here as `game/systems/map-transition`, so C12 reuses it.

## Tests first

- combat-view over plain data: a Damage for a mob updates its HP bar and queues one number; a Damage with the local player as target also pushes HP to the store
- the attack key during cooldown sends nothing; after cooldown sends exactly one Attack with the next sequence number
- map-transition: Died followed by Respawned and MapChange resets prediction history and the interpolation buffers; Respawned on the same map with no MapChange resets position and prediction only
- a negative ExpGained lowers the EXP bar and shows a loss
- a player state with `invulnerable: true` starts the flash, and one without it ends it
- HUD: LevelUp raises the level and resets the EXP bar to the server's values
- mobs are tracked and interpolated like remote players (no velocity on the wire, so no extrapolation); a Damage drops a mob's bar at once and the next state confirms or corrects it
- keyboard: the attack, pickup, window and chat keys report their action once, never as a movement key, and not while captured
- the character store records each ExpGained with its amount and keeps `dead`; the death overlay shows while dead

## Done when

- [ ] Against `make up`: in the forest, kill slimes as a Warrior and as a Bowman, see numbers, die at low HP, respawn in town with the penalised EXP shown, and reach level 2. (Not run: no browser is attached to the implementation session; owed to the author with the M1 checks.)
- [ ] M2 milestone: played with two browsers; the second sees the first's hits identically.

## Review focus

- Client copies of server rules only gate sending (here, the attack cooldown), and their numbers come from `gamedata` (page 9, Rules).
