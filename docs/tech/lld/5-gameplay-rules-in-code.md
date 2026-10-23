# 5. Gameplay rules in code

The rules come from Product → Systems; this page turns them into algorithms. Numbers are starting values, kept in `data/` and tuned with playtests. Where a product decision is still open, its current default is used.

## Maps: Tiled conventions

- Tile size 32 px. Coordinates in pixels, with y growing downward.
- Map properties: `kind` (town or hunting) and, on hunting maps, `town`: the town map its players respawn in. A town is its own town.
- Object layers read by both client and server:
  - `solids`: rectangles that block movement.
  - `platforms`: one-way platforms; only the top edge counts.
  - `ropes`: vertical rectangles players can climb, for ropes and ladders.
  - `portals`: rectangles with properties `target_map` and `target_portal`.
  - `spawns`: rectangles with properties `mob`, `max`, and `respawn_seconds`.
  - `player_spawn`: where players appear; in towns, the respawn point.

## Movement

The same algorithm exists in Go (`internal/application/world/sim`) and TypeScript (`web/src/sim`). Shared test vectors in `testdata/movement/` must pass in both.

- Fixed sub-step of 1/60 s. The server runs 3 sub-steps per 20 Hz tick.
- Starting constants: walk 200 px/s, jump velocity 620 px/s, gravity 1,900 px/s², max fall speed 900 px/s, climb 140 px/s.
- Character box: 24 × 56 px.
- **Solids:** axis-separated collision: move on X and resolve, then move on Y and resolve.
- **One-way platforms:** collide only when moving down and the character's feet were at or above the platform's top in the previous sub-step.
- **Drop through:** Down + Jump on a one-way platform ignores that platform for 0.25 s.
- **Climb:** Up with the feet inside a rope's span and rope above them, or Down with rope below them, enters climbing, snapped to the rope's centre (the feet decide, not the box, so a character standing at a rope's end doesn't re-enter it every sub-step); gravity is off; the character leaves the rope once the feet pass either end; Jump leaves it with a full jump.
- **Representation (shared with the client):** position is the centre of the feet, boxes and rectangles are half-open, and the state carries the platform stood on, the dropped-through platform, and its timer. The order of operations inside `Step` is in its godoc; the TypeScript port follows it exactly.
- **Authority (TD-18):** the server simulates from inputs and never accepts a position from the client. The client predicts with the same code and corrects itself when the server disagrees.

## Stats

Five primary stats (Product → Progression): STR, DEX, INT, AGI, VIT. Each derived stat is a linear formula with per-class coefficients from `data/stats.yaml`, then capped (OD-18):

```txt
derived = base[class]
        + sum over primary stats of coef[class][stat] × stat
        + per_level[class] × (level − 1)
        + equipment bonuses
rates (crit rate, penetration, cooldown reduction) = min(derived, cap)
```

Base primary stats at level 1, from `data/classes.yaml` (the client reads the same file):

| Class | STR | DEX | INT | AGI | VIT | Level-1 HP / MP | Stage 1 level-up preset |
| --- | --- | --- | --- | --- | --- | --- | --- |
| Warrior | 12 | 5 | 4 | 5 | 10 | 204 / 28 | STR 3, VIT 2 |
| Bowman | 5 | 12 | 4 | 10 | 5 | 105 / 33 | DEX 3, AGI 2 |

Starting coefficients for the Stage 1 classes:

| Derived stat | Warrior | Bowman |
| --- | --- | --- |
| Max HP | 60 + 12×VIT + 2×STR, +18 per level | 50 + 9×VIT + 1×AGI, +12 per level |
| Max MP | 20 + 2×INT, +4 per level | 25 + 2×INT, +5 per level |
| Attack power | weapon + 1.5×STR + 0.3×VIT | weapon + 1.5×DEX + 0.3×AGI |
| Accuracy | 10 + 1.0×DEX + 0.5×STR | 15 + 1.2×DEX |
| Evasion | 5 + 0.8×AGI | 8 + 1.0×AGI |
| Crit rate | 5% + 0.2% per AGI, cap 60% | 5% + 0.3% per AGI, cap 60% |
| Crit damage | +50% + 0.5% per DEX | +50% + 0.8% per DEX |
| Defense | 5 + 1.0×VIT + armor | 3 + 0.6×VIT + armor |
| Penetration | Equipment only, cap 50% | Equipment only, cap 50% |

Max HP and max MP round down to whole points (the character table stores them as integers); attack power, accuracy, evasion, defense and the rates stay fractional for the damage formula.

Cooldown reduction only matters once skills exist (C5, Stage 2).

## Attacks

- **Warrior:** a melee box of 64 × 56 px in front, cooldown 0.6 s, hits the nearest mob in the box.
- **Bowman:** a straight shot along the facing direction, range 420 px, cooldown 0.8 s, hits the first mob in line.
- Each attack resolves inside the tick:

```txt
hit_chance = clamp(0.80 + 0.02 × (accuracy − target.evasion), 0.20, 0.99)
raw        = attack_power × uniform(0.90, 1.10)
defense    = target.defense × (1 − penetration)
damage     = max(1, round(raw × 100 / (100 + defense)))
on crit (random below crit_rate): damage = round(damage × (1 + crit_damage))
```

- Randomness comes from a per-map-instance generator with a recorded seed, so a tick can be replayed exactly in tests.

## Mobs

- Data per mob: level, HP, attack, defense, accuracy, evasion, EXP, speed, size, drop table.
- **Behavior:** patrol on the platform they spawned on: walk, pause, turn at edges. No chasing in Stage 1's content.
- **Contact damage:** touching a mob hurts the player through the same damage formula; the player is then knocked back (16 px away from the mob with a small hop) and invulnerable for 1 s, whether or not the hit landed. One mob can touch a player per tick.
- **Patrol bounds:** a mob walks the solid or platform directly under its spawn point, cut to its spawn area's width and trimmed by half its own width; with no room it stands still. So a spawn area's width is the patrol: the slimes stay in the slime field and the mushrooms in theirs. A spawn area fills on the first tick, then one mob returns per expired respawn timer.
- **Attack geometry:** the Warrior's box stands on the feet line in front of the character; the Bowman's shot leaves at chest height (half the character box) and hits the first mob crossed within 420 px. Randomness is drawn in a fixed order (hit, variation, crit; a miss draws once) from the instance's seeded generator.
- **Spawns:** each spawn area keeps up to `max` mobs; a missing mob returns after `respawn_seconds`.

## Death and EXP

- At 0 HP the player can't act; after 3 s they respawn at the linked town's player spawn with 50% HP. Away from the town this is a transfer: the player gets Respawned and MapChange, the checkpoint names the town, and the town's instance sends the new Snapshot.
- **Portals:** EnterPortal while the character's feet are inside a portal rectangle (bottom edge included) takes them to the linked portal's arrival point, the bottom centre of its rectangle, with HP, MP, EXP and level untouched. The order is the same as a respawn transfer: MapChange, a checkpoint naming the target, leave, join. Off a portal, EnterPortal does nothing. An arriving character stands on the target portal, so EnterPortal again goes straight back.
- **Towns:** no mob spawns (the loader refuses them), and the instance runs no combat at all: attacks resolve nothing and contact does no damage.
- **Death penalty (OD-06 default):** lose 5% of the EXP needed for the current level, never below the start of the level. No penalty in towns.
- **EXP curve (cap 30, OD-13):** `exp_to_next(level) = round(20 × level^1.7)`, retuned after playtests.
- **Level-up:** extra EXP carries over, stat points are added (the allocation plan decides where), HP and MP are restored, and the character is saved immediately.
- **Party EXP (OD-04 default):** members of the killer's party who are alive on the same map instance share it. Total = base × (1 + 0.1 × (members − 1)) rounded down, split equally and rounded down; the remainder goes to the killer. Dead members and members on other maps are not counted.

## Drops

- On death the mob rolls its drop table. Each entry (an item, or coins from Stage 2; a chance; a quantity range) rolls independently.
- Drops land near the mob: on its ground line, up to 24 px either side of it, 20 px apart when one mob drops several. They live in the map instance's memory only; each gets a wire ID and a UUID that is the idempotency key of its pickup. **Loot window (OD-05 default):** for 10 s only the killer or their party can pick up; then anyone. Drops vanish after 60 s.
- Pickup range: 40 px from the centre of the character's box. PickUp takes the nearest eligible drop in range; a second PickUp in the same tick finds it reserved and does nothing. A failed pickup (for example `inventory_full`) reaches the player as a Response with `ok: false` and the code, and the drop stays on the ground.

## Inventory

- Tabs: equip, use, and etc, with 24 slots each.
- Use and etc items stack up to 100; equipment never stacks.
- A pickup goes to the first stack with room, then the first free slot; otherwise it fails with inventory_full.

## Chat and party

- **Chat:** up to 120 characters, control characters stripped, always shown as plain text on the client, never as HTML.
- **Party:** up to 4; invites expire after 30 s; one party per character; if the leader leaves, the longest-standing member becomes leader; offline members stay and show as offline. Parties live in memory (TD-19). There is no create command: a character without a party invites someone on the same map, and the party exists, with the inviter as leader, when the invite is accepted. Only the leader invites, kicks and promotes. PartyState goes to every member after a membership change and after a change the world reports (level-up, respawn, map change, connect, disconnect), not after every hit.
