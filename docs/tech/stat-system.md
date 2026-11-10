# Stat system: points, sixteen derived stats, damage types, and speed

> **Status: approved · 2026-11-10.** The high-level design for [C6. Stat allocation](../product/features/combat-and-progression/c6-stat-allocation.md) and the reworked [Systems → Progression](../product/systems/progression.md), [Systems → Combat](../product/systems/combat.md), and the stat requirements in [Systems → Items](../product/systems/items.md). It describes the code as it stands on `main` (after the equipment work) and the change it plans.

## 1. What has to hold

From the product pages:

- **Points:** 5 points per level, spent by the player on any of the five primary stats. Points are staged, then Applied, with Undo before that and a preview. There is an auto-assign button, plus a setting that is off by default. Each character gets one free reset; later resets cost currency (F1).
- **Every stat means the same for every class.** There are sixteen derived stats, each fed by fixed amounts per point. Rates (crit rate and the three speeds) have diminishing returns toward a limit.
- **Every attack is physical or magic.** One accuracy and one evasion decide hits for every attack. Penetration ignores either defense. Mobs name their damage type.
- **Speed:**
  - attack speed shortens basic-attack cooldowns only;
  - cooldown speed is for skills (nothing uses it until skills exist);
  - move speed makes walking, climbing, and jumping faster, in combat maps only.
- **HP and MP regenerate** at the character's regen.
- **Gear requires stats,** and only weapons keep a class. A worn item whose requirement is no longer met stays worn but adds nothing.
- **A fresh start:** the release begins with an empty database; nothing is converted.

From the code, the rules this design keeps:

- **The server decides every number.** A client copy of a formula only previews; once the server answers, its numbers win.
- **Movement is simulated identically in Go and TypeScript,** pinned by the shared vectors in `server/testdata/movement`. Any new input to it is pinned the same way.
- **The tick never waits on another service.**
- **Contracts keep their wire shape.** A field's number and type never change. While the game is in development, names may change: the services ship together on one commit (README), and the database starts fresh. A field whose meaning changes is renamed, and its commit is marked `!` with a `BREAKING-CHANGE:` footer, as the README asks.

## 2. The change at a glance

| Part | Today | After |
| --- | --- | --- |
| `data/stats.yaml` | One formula table per class: base, per level, and a coefficient for each stat | One table of what each point gives, the same for all classes. Plus a base table per class that grows with level, and the diminishing-returns limits |
| `data/classes.yaml` | Starting stats, a 5-point preset per level | New starting stats; the attack's damage type; the auto-assign split |
| `data/mobs.yaml` | `attack`, `defense` | Adds a damage type and `magic_defense`; `defense` becomes `physical_defense` |
| `data/items.yaml` | 13 rollable stats; a class on any equipment | 21 rollable stats (5 primary and 16 derived), `requires` with stat minimums, a class on weapons only |
| `internal/progression` | `ItemStats` (13), `DeriveStats` per class, auto allocation by preset | `ItemStats` (21), one `DeriveStats` with diminishing returns, requirement checks, and points |
| Character service | Stores the five stat totals; `unspent_points` exists but nothing uses it | Stores the points spent per stat, the unspent points, and whether the free reset is used. Totals are the class base plus points. |
| World | Applies the class preset at level-up; one attack/defense pair; class cooldown; one walk speed | Adds points at level-up. Handles `SpendPoints` and `ResetPoints`, damage types, and regen. Cooldown and movement speed per player. Worn items that don't meet their requirement add nothing. |
| Item service | `Equip` checks level and class | Also checks stat requirements against the character's own stats; `item_rolls` gains 8 columns |
| Client | Shows 8 derived stats; class cooldown; one walk speed | All 16 stats; spending points with a preview; reset; auto-assign; cooldown and walk speed from the server |

## 3. Game data

`data/stats.yaml` becomes one shape for every class:

```yaml
points_per_level: 5
per_point:                 # what one point gives, every class alike
  str: {physical_attack: 1.5, max_hp: 2}
  dex: {physical_attack: 0.5, magic_attack: 0.5, accuracy: 1, crit_damage: 0.005}
  int: {magic_attack: 1.5, max_mp: 3, mp_regen: 0.05, magic_defense: 0.5}
  agi: {evasion: 1}
  vit: {max_hp: 10, physical_defense: 1, magic_defense: 0.5, hp_regen: 0.05}
diminishing:               # base + (limit − base) × points ÷ (points + k)
  crit_rate:      {stat: dex, base: 0.05, limit: 0.60, k: 200}
  attack_speed:   {stat: agi, limit: 0.50, k: 150}
  cooldown_speed: {stat: int, limit: 0.40, k: 200}
  move_speed:     {stat: agi, limit: 0.30, k: 200}
limits: {penetration: 0.50, hit_chance: [0.20, 0.99]}
classes:                   # base values per class; per_level grows them
  warrior: {max_hp: {base: 60, per_level: 18}, max_mp: {base: 20, per_level: 4}, accuracy: {base: 10},
            evasion: {base: 5}, physical_defense: {base: 5}, magic_defense: {base: 3}, crit_damage: {base: 0.5}}
  bowman:  {max_hp: {base: 50, per_level: 12}, max_mp: {base: 25, per_level: 5}, accuracy: {base: 15},
            evasion: {base: 8}, physical_defense: {base: 3}, magic_defense: {base: 3}, crit_damage: {base: 0.5}}
```

- **`classes.yaml`:** each class gains `attack.damage` (`physical` or `magic`) and `auto_assign` (the recommended split, which must sum to `points_per_level`). `base_stats` takes the new starting values, 7 in each stat for every class, and `level_up_points` goes.
- **`mobs.yaml`:** each mob gains `damage` (`physical` or `magic`) and `magic_defense`, and `defense` is renamed `physical_defense`. Cave bats deal magic damage.
- **`items.yaml`:** `attack` and `defense` in `stats` become `physical_attack` and `physical_defense`, and the other new derived stats may roll. `requires: {str: 25}` lists stat minimums. `class` is allowed on weapons only.
- **Units:** every rolled value is a whole number. Rates (crit rate, crit damage, penetration, the three speeds) are in tenths of a percent. Regen is in tenths of a point per second.
- **Validation** (`internal/gamedata/load.go`) refuses:
  - an unknown stat or class;
  - an auto-assign split that doesn't sum to `points_per_level`;
  - a limit at or below its base, or a `k` of zero or less;
  - `class` on armor or accessories;
  - a requirement on a stat that doesn't exist.
- **The client generator** (`web/scripts/gen-gamedata.ts`) emits `stats.yaml` for the first time, for the preview only (section 8), along with the auto-assign splits and item requirements.

## 4. The stats model (`internal/progression`)

- **`ItemStats` grows from 13 to 21 fields:** the five primary and the sixteen derived, in one fixed order. Gear rolls and the worn sum both use it.
- **A character's own stats** are their class's starting stats plus the points they've spent. Gear isn't part of them.
- **`DeriveStats(formulas, class, level, own, worn)`**, in order:
  1. **Primary:** own stats plus the gear's primary stats. Gear STR counts like spent STR, including toward the diminishing curves.
  2. **Flat stats:** the class base at this level, plus each point's amounts, plus the gear's flat stats. This covers both attacks, both defenses, max HP and MP, accuracy, evasion, crit damage, and both regens.
  3. **Rates:** the base, plus *(limit − base) × points ÷ (points + k)*, plus the gear's rate, held to the limit. This covers crit rate and the three speeds.
  4. **Penetration:** gear only (skills later), held to 50%.
- **`Sheet`** keeps the totals and the equipment's share of each (totals with gear minus totals without), and gains the new stats and the points.
- **Requirements:** `Meets(item, level, class, own)` checks the level, the class for weapons, and each stat minimum against the character's **own** stats, not counting gear: counting it would let one item's bonus unlock another, and taking an item off could switch others off in a chain. Worn items that don't meet their requirement are left out of the worn sum, so they add nothing until they do.
- **Shared stat vectors** go in `server/testdata/stats/`: class, level, points, and worn stats, with the expected sheet. The Go tests and the client's preview (section 8) both replay them, so the preview can't drift from the server. They compare to 1e-9; the preview is display only.

## 5. Points: where they live and how they're spent

### In the character service

- The five stat columns of `characters` change meaning, from totals to **points spent** in each stat. `unspent_points` comes into use, and `free_reset_used boolean` is added.
- Totals are always the class base (game data) plus points. Retuning a class's starting stats therefore changes every character consistently, and a reset is "points to zero".
- `Load` and `SaveCheckpoint` carry points, unspent points, and the reset flag alongside the level and EXP.
- The REST character list derives its numbers with the new `DeriveStats`, without gear.

### In the world

- **Level-up:** `AwardExp` loses its allocation plan. Each level adds `points_per_level` to unspent, and the world checkpoints at once, as it does on a level-up today. `LevelUp` carries the sheet with the unspent count.
- **`SpendPoints{expected_unspent, points}`** comes from the client through the gateway with a Response, like `Equip`. On the tick:
  1. **Refusals:**
     - `points_changed` when `expected_unspent` isn't the character's unspent count now;
     - `not_enough_points` when the points add up to more than that;
     - `bad_points` for any negative number, or no points at all.
  2. Otherwise it adds the points and lowers the unspent count.
  3. It re-checks the worn items' requirements and re-derives.
  4. It sends `StatsChanged` and queues a checkpoint at once.
- **Why `expected_unspent` and not an idempotency key:** the world holds this state in memory, with no key table. The comparison makes a retried Apply harmless: the first one lands, a retry finds the count changed and is refused, and the client already has the result from `StatsChanged`. The client treats `points_changed` as "already done", not as an error.
- **`ResetPoints{expected_unspent}`:**
  - It is refused with `no_free_reset` once the free reset is used.
  - Otherwise every point goes back to unspent (`points_per_level × (level − 1)`), the flag is set, and the world re-derives.
  - A worn item may become inactive, which is proposal 7 in the product pages.
  - It sends `StatsChanged` and checkpoints.
  - When currency arrives (F1), a paid reset becomes an economic action: the item service charges the wallet under an idempotency key, and the world applies the reset after the charge commits.
- **Auto-assign runs in the client,** so the world never has to read the account setting at join, which would be one more dependency for it. When the account setting is on and a `LevelUp` arrives, the client spends every unspent point by its class's split, as the Auto-assign button does, with the `expected_unspent` from the `LevelUp`. Level-ups only happen while playing, so the client is always there to do it.
  - Two `LevelUp`s close together send two requests; the first is refused with `points_changed`, and the second spends everything.
  - The client reads the settings at login, so the switch is known before the first level-up.
  - The setting lives beside the control preset in the account service: `PUT /api/settings` gains `auto_assign_points`, false by default.
  - The Auto-assign button sends the same request on demand.
- **Durability:** points, like level and EXP, live in the world and are saved by checkpoint. A world crash between Apply and its checkpoint loses the change, and the points come back unspent. That window is the same one a level-up has today.

## 6. Combat

- **Damage type:** a player's basic attack takes its type from the class (`attack.damage`), and a mob's contact damage from the mob (`damage`). Skills will carry their own.
- **`model.Resolve`:**
  - The attacker gains both attacks and the defender both defenses, and `Resolve` picks the pair that matches the attack's type.
  - Penetration applies to either defense.
  - The hit chance is unchanged: accuracy against evasion, held between the limits in `stats.yaml` (20% and 99%), for every attack in both directions.
- **Regen:**
  - Once a second (every `tick_rate_hz` ticks), each living player gains their HP and MP regen. Fractions are carried in thousandths, so 0.05 a second adds up exactly, and HP and MP never go past their maximums.
  - It runs in towns too, since the combat page has HP and MP come back over time everywhere. It never runs while dead: HP stays 0 until the respawn sets it.
  - HP changes go out in the Delta as any change does, and the party hears each member's HP once a tick when it changed; the party copy keeps only a member's newest.
  - MP isn't in the Delta, so a second in which regen raises MP sends the player a `StatsChanged`.
- **Inactive gear:** when a character's own stats change (spending, a reset) or an item is equipped, the world rebuilds the worn sum from the items whose requirements are met.

## 7. Speed

### Attack speed

- The world computes each player's cooldown in ticks: *⌈class cooldown ÷ (1 + attack speed) ÷ tick⌉*.
- The sheet carries it as `attack_cooldown_ticks`.
- The client's attack gate (`combat-view.ts`) uses that number instead of the class cooldown, so the gate and the server count the same ticks.

### Move speed

- `sim.Params` gains a per-player `SpeedFactor` *f*, which speeds up the whole of the character's movement, as if it played faster:
  - walking: `WalkSpeed × f`;
  - climbing: `ClimbSpeed × f`;
  - jumping: `JumpVelocity × f`, `Gravity × f²`, and `MaxFallSpeed × f`. A jump then keeps its height and its length and takes 1/*f* of the time.
- **Why jumps keep their shape:** a faster launch alone would make jumps higher by *f²*, so +30% speed would mean jumps 69% higher. Players could then reach platforms the maps weren't built for. Scaling the whole arc keeps every map's reach exactly as designed, and only the pace changes.
- **The factor** is 1 + move speed, rounded to thousandths, so Go and TypeScript multiply by the same number. *f²* is computed as `f × f` the same way on both sides, with each product rounded explicitly, as the sim already does against fused multiply-adds.
- **The drop-through time** (0.25 s) stays a time; at a faster fall it still clears a platform.
- **Towns:** *f* is 1 there; both sides read the map's kind from the shared map data.
- **Vectors:** the movement vectors gain cases at *f* = 1.3, the most move speed gives (walking, climbing a rope, a jump onto a platform, a fall at the clamp). Both the Go and TypeScript sims must match them. The sim knows no map kind, so the world's tests, and the client's, check that a town keeps *f* at 1.
- **Speed changes take a moment to reach the client:**
  - gear, points, or a level-up change the factor on the server from the next tick;
  - the client switches when `StatsChanged` arrives;
  - a prediction made in between is off by a few pixels and corrected by the usual reconciliation.
  - These changes are rare, so a small correction is accepted rather than tagging inputs with a factor.

## 8. Equipment

- **`item_rolls`:**
  - `attack` is renamed `physical_attack` and `defense` is renamed `physical_defense`;
  - eight columns are added (magic attack, magic defense, penetration, both regens, and the three speeds), each `int NOT NULL DEFAULT 0`.
  - `Add` and `AddMany` take the 21-stat roll; check 11 is unchanged.
  - Stored replies use the new names, and an `Add` fingerprint covers all 21 stats under a new version: the release starts from an empty database, so no reply or retry from before it remains.
- **`Equip`:** the world sends the character's own primary stats alongside the level and class. The item service checks `requires` in the same transaction and refuses with `stats_too_low`.
- **Inactive items** need no protocol flag. The client applies the same rule (own stats from the sheet, requirements from the game data) to grey a worn item out. The server's sheet is what counts.

## 9. Protocol

### Client protocol (`api/proto/client/v1/client.proto`), all additive

| Message | Change |
| --- | --- |
| `ItemStats` | `attack` (1) is renamed `physical_attack` and `defense` (2) `physical_defense`, keeping their numbers. Adds `magic_attack`, `magic_defense`, `penetration`, `hp_regen`, `mp_regen`, `attack_speed`, `cooldown_speed`, `move_speed`. |
| `DerivedStats` | `attack_power` is renamed `physical_attack` and `defense` `physical_defense`, keeping their numbers; adds the same eight |
| `StatSheet` | Adds `Points points` (unspent, spent per stat, whether the free reset is left) and `attack_cooldown_ticks` |
| `SpendPoints` (client) | New: `expected_unspent`, `Stats points`; answered with a Response |
| `ResetPoints` (client) | New: `expected_unspent`; answered with a Response |

- **New error codes:**
  - `not_enough_points`: "you don't have that many points";
  - `points_changed`: "your points changed; try again";
  - `no_free_reset`: "your free reset is used";
  - `stats_too_low`: "your stats are too low for that item";
  - `bad_points`: "spend at least one point, and none below zero".
- **REST:** `auto_assign_points` in settings (`api/openapi.yaml`).
- **Renames are allowed by moving buf's breaking rules from `FILE` to `WIRE`** (`server/buf.yaml`), in its own commit before any rename.
  - `WIRE` still refuses what would break a running client: a changed field number or type, or a field deleted without its number reserved.
  - It allows new names, since the binary protocol carries numbers, not names.
  - Only the client's optional JSON mode (`finga.v1.json`, a development aid) sees names, and it ships with the server.
  - Before a public launch, the rules can move back to `WIRE_JSON` or `FILE`.
- Golden contract files cover every new and changed message; `make breaking` passes.

### Internal contracts

- **`world.v1`:** `SpendPoints` and `ResetPoints`, unary; the sheet fields; `ItemStats` renamed and grown like `item.v1`'s, since the gateway reads both through one interface.
- **`character.v1`:** in `Load` and `SaveCheckpoint`, `stats` is renamed `points` (same number and type, now the points spent), and `unspent_points` and `free_reset_used` are added. Since the meaning changes, the commit is marked `!` with a `BREAKING-CHANGE:` footer.
- **`item.v1`:** `Equip` gains the character's own `stats`, and `ItemStats` is renamed and grows as in the client protocol.
- **Game data and Go and TypeScript code** use the correct names throughout (`physical_attack`, `physical_defense`), since none of them is a contract.

## 10. Client

- **gamedata:** the per-point table, diminishing returns, class bases, auto-assign splits, and item requirements.
- **progression** (`web/src/progression`, a layer beside `sim`): the TypeScript port of `DeriveStats`, the requirement check, and the auto-assign split, replaying `server/testdata/stats`.
- **sim:** `SpeedFactor` from the sheet in hunting maps; prediction uses it.
- **game:** the attack gate uses `attack_cooldown_ticks`.
- **state:** the character store keeps the sheet with its points; a staged-points draft lives in the window.
- **Character window (`features/character`):**
  - the sixteen derived stats in four groups, with move speed marked "combat maps";
  - **+** beside each primary stat while points remain, staging into a draft;
  - **Apply** sends `SpendPoints`, and **Undo** clears the draft;
  - the preview runs a TypeScript port of `DeriveStats` over the generated data, checked against the shared stat vectors;
  - **Auto-assign** and **Reset**; Reset asks for confirmation and shows whether the free one is left;
  - a draft belongs to the unspent count it was staged from: if that count changes (a level-up, or an Apply that landed after its reply was lost), the draft is dropped, so a retried Apply never spends twice;
  - worn items below their requirement are greyed out.
- **features/settings:** the auto-assign switch.
- **app/wiring:** on `LevelUp`, when the setting is on, send the split.
- **HUD:** the unspent-points badge.
- **Item card:** stat requirements in red when the character's own stats fall short.

## 11. A fresh start

There is no conversion code, as the product page decided.

- **Migrations only change the schema:**
  - the stat columns of `characters` change meaning, from totals to points;
  - `free_reset_used` is added;
  - `item_rolls` columns are renamed and added.
  - Rows from before would be wrong, so none are kept.
- **Locally:** `make down` (which drops the volume), then `make up`.
- **Production:** the release drops and recreates the database once. That step goes in the release runbook in `infra/`, with the infrastructure session, before this ships.

## 12. When things fail

| Failure | What the player sees | Why it is safe |
| --- | --- | --- |
| `SpendPoints` reply lost, client retries | Nothing unusual | The retry finds the count changed and is refused with `points_changed`; the client already has `StatsChanged` |
| World crash between Apply and its checkpoint | After the rejoin, those points are unspent again | Same window as a level-up today; nothing is lost twice or gained |
| The preview differs from the server | The numbers settle to the server's after Apply | The preview is display only, and the shared vectors keep the two in step |
| Speed changes mid-run | A small correction | Normal reconciliation |
| An old client during the deploy | It can't spend points, so they wait unspent | The server no longer spends them by preset; the new client ships in the same release |

## 13. Testing

- **Game data:** the new validation rows.
- **Stats model:**
  - table tests per stat;
  - property tests: the equipment's share is the total minus the total without gear; rates never reach their limits from points alone; adding a point never lowers a stat;
  - the shared stat vectors.
- **Requirements:** met, unmet, and becoming unmet after a reset (the item stays worn and adds nothing).
- **Character service:** points stored and loaded, the reset flag, and checkpoints with points.
- **World:**
  - level-up adds unspent points;
  - `SpendPoints` lands once, refuses stale, refuses too many, and refuses negatives;
  - `ResetPoints` gives back every point and refuses a second free reset;
  - combat picks attack and defense by type; penetration works on both; bats deal magic damage;
  - regen adds up exactly, stops at the maximum, and skips the dead;
  - cooldown ticks follow attack speed;
  - move speed applies in hunting maps and not in towns, and a jump at a higher speed lands where the same jump lands at speed 1.
- **Movement vectors:** speed cases for walking, climbing, jumping, and falling, replayed by both sims.
- **Item service:** `Equip` with stat requirements; 21-stat rolls in `Add` and `AddMany`; the property tests.
- **End to end:**
  - level up, spend, see the sheet;
  - a stale spend refused;
  - the reset once;
  - gear refused below its requirement, and going inactive after a reset;
  - a magic hit from a bat.
- **Client:**
  - Vitest for the preview against the stat vectors, the draft, auto-assign on level-up, and the speed factor in prediction;
  - Testing Library for the window's points controls and the settings switch.

## 14. Build order

On a branch with one PR, as before. Each step is one commit, green on `make test`, `make lint`, and `make cover`:

0. **Breaking rules:** `server/buf.yaml` moves from `FILE` to `WIRE`, with a comment saying why and when to move back. Nothing else changes.
1. **Stats model and game data:** the new `stats.yaml`, `classes.yaml`, `mobs.yaml`, and `items.yaml`, with validation, the client generator, `ItemStats` (21), `DeriveStats` with diminishing returns, and the shared stat vectors. Until step 4 the world still spends level-up points by the class's auto-assign split on the server, so the game keeps working between steps.
2. **Item service:** `item_rolls` columns, 21-stat rolls, and stat requirements in `Equip`.
3. **Combat:** damage types, magic defense, penetration on both defenses, bats dealing magic damage, and regen.
4. **Points:**
   - the character service storing points, the unspent count, and the reset flag;
   - the world's `SpendPoints` and `ResetPoints`;
   - level-ups giving points, with the server-side split removed;
   - inactive gear.
5. **Speed:** attack cooldown ticks, and move speed in the sim with its vectors.
6. **Gateway and protocol:** messages, routes, error codes, the REST setting, and contracts.
7. **Client:** the 16-stat sheet, spending with a preview, reset, auto-assign and its setting, the HUD badge, requirements and inactive gear, the attack gate, and move speed in prediction.
8. **Content and balance:** starting stats, splits, item requirements and new rollable stats, mob damage types and defenses, retuning, a `make bots` run, and the production database reset in the runbook.

## 15. Decisions

All decided on 2026-11-10:

1. **Requirements are checked against the character's own stats, not counting gear.** Counting gear would let one item's bonus unlock another, and taking an item off could switch others off in a chain.
2. **Auto-assign runs in the client.** Otherwise the world would have to read the account setting at join, which means one more dependency for it.
3. **Regen runs everywhere,** towns included, but not while dead.
4. **Move speed** speeds up walking, climbing, and jumping, with jumps keeping their shape (section 7).
5. **Fields are renamed in place,** with buf's breaking rules moved to `WIRE` for the development phase (section 9).

## 16. Alternatives considered

- **An idempotency key per spend in the world:** it would need a key store in a service that keeps this state in memory. Comparing against the expected unspent count gives the same safety with nothing stored.
- **A server round trip for the preview:** exact, but every **+** would cost a request. A display-only port, pinned by shared vectors, answers instantly.
- **Store stat totals, as today:** a reset would then need the class's starting stats at the time each point was spent, and retuning a class would leave existing characters behind. Storing points spent avoids both.
- **Convert existing characters:** no longer needed, since the release starts fresh.
- **Keep `FILE` rules and add new fields beside deprecated ones:** passes the check without changing it, but leaves wrongly named tombstones in every contract during development, for compatibility nothing needs.
- **Switch the breaking check off:** simplest, but it would also stop catching real wire breaks, which `WIRE` still does.
