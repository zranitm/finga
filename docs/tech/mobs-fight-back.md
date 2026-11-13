# Mobs fight back

> **Status: approved · 2026-11-13.** The high-level design for [C12. Mobs fight back](../product/features/combat-and-progression/c12-mobs-fight-back.md) and [Systems → Combat → Mobs and Field bosses](../product/systems/combat.md). It describes the code as it stands on `main` (after C11's attack that frees a player from a skill) and the change it plans.

## 1. What has to hold

- **A mob blocks a player where the player sees it.** The client predicts the block, and the server agrees with it, so a player pressing against a mob isn't pulled back.
- **Nothing traps a player, and nothing walks through.** Neither a player nor a mob ever moves further into the other, and both can always move apart.
- **The server decides every block, hit, and dodge.** The client only predicts its own movement and draws what the server says.
- **A dodge is the player's doing:** every attack shows a wind-up long enough to step out of at ordinary latency.
- **Mob behaviour is data.** Attacks, their timing, areas, and powers live in `mobs.yaml`, so tuning needs no release.
- **The balance targets hold:** hits survived, the share that lands, kills a life, and the boss's 30 potions, all assuming a player who never dodges.

## 2. The change at a glance

| Part | Today | After |
| --- | --- | --- |
| Sim `Geometry` | Static solids, platforms, ropes | Also `Blockers`: mob boxes, which stop a player only on entering one from the side |
| `Input` | Held keys | Also `view`: the server tick of the mobs the client drew |
| Arena | Patrol only; `Touch` on overlap | A mob box history; mobs stop at players; patrol, provoked chase, and attacks in phases |
| Instance | `contactDamage` every tick | `mobHits`: the attacks that land this tick, with the same guard, knockback, and death |
| `mobs.yaml` | `touch_guard`, per-mob `touch_guard` | `hit_guard`, `provoke`, an `attacks` table, and each mob's `attacks` |
| Protocol | — | `Input.view`, and a `MobAttacked` event |
| Client | Mobs drawn with no attacks | Blockers in prediction; each mob's wind-up and attack drawn |
| Balance harness | A touch every guard | An attack every cooldown, from each mob's attacks |
| Bots | Walk through mobs | Attack what blocks them, and jump when stuck |

## 3. Game data

### `data/mobs.yaml`

```yaml
hit_guard: 1500ms  # after a mob's hit or MISS, no mob's attack hits the player for this long; a hit blinks for it (was touch_guard)
provoke: 5s        # how long a mob a player hits follows them
attacks:           # the attacks mobs use; a mob names its own and may change any value
  bite:   {kind: front, windup: 600ms, reach: 24, recover: 300ms, cooldown: 2s, power: 1}
  hop:    {kind: leap, windup: 600ms, distance: 72, travel: 400ms, spread: 12, recover: 300ms, cooldown: 2500ms, power: 1}
  charge: {kind: rush, windup: 800ms, distance: 128, travel: 400ms, recover: 600ms, cooldown: 3s, power: 1}
  spit:   {kind: shot, windup: 600ms, range: 240, speed: 300, size: 12, recover: 300ms, cooldown: 2500ms, power: 1}
  slam:   {kind: around, windup: 1000ms, spread: 48, recover: 600ms, cooldown: 5s, power: 1.5}
mobs:
  - id: green_slime
    ...
    attacks: [{use: hop}]
  - id: king_slime
    ...
    attacks: [{use: hop, distance: 160, spread: 32}, {use: slam}]
```

**The kinds and their areas,** from the mob's box and facing. Every area also covers the mob's own box, and a shot also hits a player overlapping the mob as it's thrown, so standing inside a mob is never out of its reach:

| Kind | Used by | Area | Hits |
| --- | --- | --- | --- |
| `front` | Bite | `reach` px in front of the mob's front edge, the mob's height | every player in it |
| `leap` | Hop | the mob moves up to `distance` toward the player over `travel`; its box grown by `spread` on each side where it lands | every player in it, at the landing |
| `rush` | Charge | the mob moves up to `distance` forward over `travel` | the first player its box meets, where it stops |
| `shot` | Spit | a `size` square from the mob's front at mid-height, flying `speed` px/s up to `range` | the first player it meets; it ends there, at a solid, or at its range |
| `around` | Slam | the mob's box grown by `spread` on every side | every player in it |

- **Timing:** `windup`, then the act (instant, or `travel` long), then `recover`. The `cooldown` counts from the attack's start, as a skill's does.
- **Damage:** the mob's attack × `power`, with its damage type, through `model.Resolve` against the player's defense, as a touch was.
- **A leap or a rush stays on the mob's ground,** clamped to its patrol bounds. A rush stops at a player, since mobs don't walk through players.
- **Each of the 36 mobs names its family's attack:**
  - slimes `hop`;
  - the mushroom, the snails, and the scarab `charge`;
  - the boar, the rats, the mouse, and the weasel `bite`;
  - the bats, the moths, the vulture, the wisp, and the icewing `spit`.
  - Elites and bosses scale their distances and spreads with their size.
  - Every boss adds `slam`.
- **Removed:** the top-level `touch_guard`, and each boss's own `touch_guard`. A boss's danger is now set by its attacks' cooldowns.
- **Validation refuses:**
  - a mob with no attack, or an unknown `use`;
  - a kind missing a field it needs;
  - a non-positive time, distance, speed, or power;
  - a cooldown shorter than the attack's own windup, act, and recover.
- **The client generator** carries the attacks, so the client can draw them.

## 4. Mobs block players

### The sim

Both sims, `server/internal/sim` and `web/src/sim`:

- **`Geometry` gains `Blockers []Rect`,** set per step by the caller. The map's own solids, platforms, and ropes don't change.
- **In the x move, after solids,** for each blocker whose height range the player's box overlaps, **the overlap in x can't grow:**
  - The move is held where the x overlap equals what it was before this sub-step's move, and `VX` goes to 0.
  - From outside, the overlap is 0, so the player stops at the face: a wall.
  - Partly in, by lag or a landing at an edge, they can back out but not go deeper.
  - Wholly inside (every mob is at least 32 px wide, a player 24), a move either way doesn't grow the overlap, so they walk out either way.
  - For a player, who is always narrower than a mob, this is the same as: a player whose left edge is left of a mob's left face can't end with their right edge past the larger of that face and where the right edge was. It's mirrored for moving left.
  - Overlap that lag or float rounding leaves, a few pixels or one ulp, can never grow. So pressing on never walks through.
- **The y move ignores blockers.** A player can't stand on a mob: landing on one, they keep falling to the ground, and from there they're inside it.
- **A jump gets past a mob when the player's feet are above its top as they reach its side.** From there they pass over it, or come down wholly inside it and walk out the far side. One who comes down only partly in can back out or jump again.
  - A jump rises about 100 px, and every normal and elite mob is at most 60 px tall.
  - The slime bosses (84 px) can be passed with a well-timed jump, and the flying ones (96 px) only at a jump's very top. The mouse bosses (120 px) can't be passed.
- **Speed is safe:** the fastest walk moves under 4 px a sub-step, against mobs at least 32 px wide, so a player can't skip past one between sub-steps.

**New vectors** in `server/testdata/movement`, with `blockers` in the vector file:
- walking into a blocker from either side;
- wholly inside one, walking out both ways;
- partly inside, pressing deeper (held) and backing out (free);
- pressed against a blocker that moves a few pixels into the player between steps, pressing on (held, never through);
- jumping over a low one;
- landing on one and falling through to the ground;
- jumping into the side of one in the air;
- a blocker beside a solid.

### Agreeing on where the mob was

The client draws mobs about 100 ms behind the server, and predicts the player in the present. A block against the server's present mobs would disagree with what the player sees, by as much as a mob walks in that time.

- **Each input names its view:** `Input.view` is the server tick of the mob positions the client used to predict that input. That's the newest tick at or before the render clock, so the block lands within one tick's walk of where the mob is drawn.
- **The server checks that input against the same tick:**
  - The arena keeps each living mob's box for each of the last 8 ticks (400 ms), as the frame sent them.
  - `simulate` passes the boxes at the input's view as `Blockers`.
- **The view is bounded** to between 8 ticks back and the last tick sent.
  - A modified client claiming an older view could slip past where a mob was up to 400 ms ago. The fastest mob walks 40 px in that time.
  - A player with more latency than that is blocked by the oldest boxes kept, and gets small corrections near moving mobs.
- **A starved tick** that re-applies the held input reuses its view, bounded again.
- **The client keeps each mob's samples by tick** (`MobTracker`) for at least a second.
  - A tick entry in the predictor records its view, and a replay uses the same view, so it matches the server's own steps.
  - A mob removed at a tick stops blocking from that tick on both sides.

### Mobs stop at players

- `arena.Step` takes the living players' boxes as they are after `simulate`.
- A mob's move, in a patrol, a chase, or a rush, follows the same rule against each player's box: the x overlap can't grow.
  - From outside, it stops at the player.
  - Already a few pixels in from lag, it goes no deeper.
  - With a player wholly inside it, after they came down on it, its move doesn't grow the overlap, so it walks on. A player can't pin a mob by standing in it.
- **Approaching each other:**
  1. The player is held at the face of the mob as they drew it.
  2. The live mob stops a few pixels into them, the lag times its speed, and doesn't push.
  3. Later views show that overlap, and neither side can deepen it.
- Players never move mobs, and mobs never move players, except a hit's knockback.

## 5. Mobs attack

### The mob's turn, each tick in `arena.Step`

1. **In an attack:** it advances the phase. The windup ends in the act, and a leap or a rush moves the mob over its travel; then it recovers.
2. **Otherwise, it picks a target:**
   - First its provoker: the last player whose blow (hit or MISS) reached it, within `provoke`, while that player is alive and on the map.
   - Otherwise, the nearest player inside the area of one of its ready attacks, faced toward them, on either side of it.
   - If an attack is ready and the target is in its area, the mob faces them and starts its windup. A boss alternates its two attacks, taking whichever is ready if only one is.
3. **Otherwise, it moves:**
   - With a provoker, it walks toward them along its ground at its speed, without the edge pause, and stops at its bounds.
   - Without one, it patrols as today.

`mob` gains its provoker and the tick that runs out, its current attack and phase with their end ticks, each attack's ready tick, and which attack is next for a boss. A `shots` list holds flying spit.

### Landing

- **Each tick, `arena.Step` collects the attacks that land:**
  - areas whose act is now;
  - a rush meeting its first player;
  - a shot meeting its first player.
- **Each landing names** the mob, the attack, the players it hits, and the side to knock them toward: away from the mob, or along a shot's flight.
- **When a mob dies, its landings go with it.** The instance takes the rest in `mobHits`, where `contactDamage` runs today, after players' attacks, so a mob killed this tick doesn't land its hit.
- **`mobHits` is today's contact handling, per landing:**
  - It skips a dead player and one inside their hit guard.
  - Otherwise it resolves the attack × power against their defense, and starts their `hit_guard`.
  - A MISS only shows.
  - A hit blinks, knocks back 16 px with the hop (`knockedBack`, unchanged), hurts, and may kill.
- **A guarded player is still blocked and still targeted.** A rush stops at them and a shot ends on them, but neither hurts.

### Events

- **When a windup starts,** the instance adds `model.MobAttacked{Tick, Mob, Attack, Facing}`, delivered like `SkillUsed`.
- A leap's and a rush's movement reach clients as the mob's positions in each frame, as a patrol's do.
- A shot isn't sent: the client flies it from the event and the attack's data.

## 6. Protocol

- **`client.proto`:**
  - `Input.view = 6`, as `uint32`.
  - A new `MobAttacked { tick, mob, attack, facing }` in the server message's `oneof`.
  - `attack` is the index into the mob type's attacks.
- **`world.proto`:**
  - the input's `view` passed through by the gateway;
  - `MobAttacked` in `DeliverResponse`'s events.
- **All additions,** so `buf breaking` (WIRE) passes. There are no new codes.

## 7. Client

- **Prediction:**
  - Each input takes `view` from the render clock, held to the newest tick received, and sends it.
  - `step` gets `MobTracker.boxesAt(view)` as blockers, and the tick entry records the view for a replay.
- **Drawing an attack** (`entities/mob.ts`), from `MobAttacked` and the attack's data, played when the render clock reaches its tick, so it lines up with the mob's drawn position:
  - **windup:** the mob crouches (squash), pulses brighter, and shakes a little, for `windup`.
  - **front:** a lunge drawn 8 px forward, and a bite arc over the area.
  - **leap:** an arc in height over `travel`, drawn only, since the server's mob stays on its ground. Then a landing squash and a dust ring the size of the area.
  - **rush:** a forward lean and a dust trail while it moves.
  - **shot:** a ball in the mob's tint along the line. It stops at the first `Damage` from that mob, at a solid, or at its range.
  - **around:** a deep squash, then a ring the size of the area.
  - A new `MobAttacked` for the same mob, or its death, ends what's showing.
- **Unchanged:** the player's hurt blink, damage numbers, and the boss bar.

## 8. Balance

The harness (`balance_test.go`) swaps the touch for each mob's attacks, with a player who stands in reach and never dodges:

- **A hit's damage** is the mean over the mob's attacks of attack × power, with misses rolled as today.
- **Hits survived** replaces touches survived. Its bands don't change (A 10–12, B 8–10, C 6–8, D 4+), and with `power: 1` neither does the number.
- **The landed share** keeps the class bands (85–95%, the Thief 55–70%).
- **Cadence:**
  - A mob in reach attacks once per max(cooldown, `hit_guard`): 2 to 3 seconds for a normal mob, against a touch every 1.5.
  - So kills a life rise, and its minimums still hold.
  - A player who never dodges takes about 0.5 to 0.75 of a touch's damage over a fight. The harness logs it.
- **Bosses:**
  - The cadence alternates the family attack and the slam.
  - The 30-potion check for a lone melee player stays.
  - The slam's power and cooldown, and each boss's overrides, are tuned to pass it.
- **Elites keep their check:** 2–3 times the hits and EXP of their map's normal mob.

## 9. Bots and tests

- **Bots (`cmd/bot`):**
  - A bot blocked by a mob attacks it, since it's in front, until it dies, then walks on.
  - A bot stuck for a moment, 0.2 s, jumps, as the end-to-end walker does.
  - Bots stay off maps while a boss is up there, so one can't block a bot for good.
- **End-to-end tests that rely on touch become attacks:**
  - the cave bat's magic touch becomes its magic spit (`fight_e2e_test.go`);
  - the boss that "touches for 1" (`boss_e2e_test.go`);
  - the walker's knockback, and a character dying to the first touch (`loot_e2e_test.go`);
  - the caster placed so nothing touches them (`casting_e2e_test.go`).

## 10. When things fail

| Failure | What the player sees | Why it is safe |
| --- | --- | --- |
| Latency past 400 ms | Small corrections when pressing against a moving mob | The server uses its oldest kept boxes; nothing worse than today's corrections |
| A modified client sends an old `view` | It can slip past where a mob was up to 400 ms ago, 40 px at most | The view is bounded; every other block holds |
| A modified client ignores blockers | Its own screen is wrong | The server moves every player by its own sim and the boxes it kept |
| A windup is seen late | Less time to step out, about 0.6 s less latency and the 100 ms render delay | The windups are long enough to dodge at ordinary latency; evasion still rolls |
| A dodge on screen is hit on the server | A hit after stepping out | The server decides by the player's position at the hit tick; the input that moved them hadn't arrived |
| `MobAttacked` is lost | No windup drawn; the hit still shows | It's drawing only; damage comes as `Damage` |
| A mob dies mid-attack | Its attack ends; nothing lands | Its landings are dropped with it |
| A boss corners a player at a wall | Blocked on one side, hit with knockback | The boss stops at the player and never pushes; knockback moves them; jumps and drops still work |
| A mob walks toward a player who presses on | They meet a few pixels apart or in, and stop | Neither side's move can grow the overlap, so lag never lets one pass through |

## 11. Testing

- **Game data:** the attacks table, `use` and overrides, the families, each refusal, and `hit_guard` and `provoke`.
- **Sim:** the blocker vectors, replayed by Go and TypeScript.
- **World:**
  - the box history and the view bound;
  - a player stopped at a mob that the server agrees with;
  - a mob stopping at a player, and never moving deeper into one it already overlaps;
  - a mob walking on with a player wholly inside it;
  - a player and a mob walking into each other under lag, neither passing through;
  - a player inside a mob still in reach of each of its attacks;
  - each kind's area, travel, and landing;
  - a rush stopping at the first player, and a shot ending at a player, a solid, or its range;
  - the guard skipping a second hit within 1.5 s;
  - provoke, chase within bounds, and the end of chase;
  - a boss alternating;
  - a dead mob landing nothing;
  - `MobAttacked` at each windup.
- **Client:**
  - `view` from the render clock;
  - `boxesAt` across spawns and removals;
  - a replay using recorded views;
  - each kind drawn at the render clock's tick;
  - a shot stopping at the first `Damage`.
- **Balance:** the harness's bands with attacks, and the boss potions.
- **End to end:** a player blocked by a mob; a bite landing, and stepping out of a windup dodging it; a provoked mob following.

## 12. Build order

Two PRs, each step one commit, green on `make test`, `make lint`, `make cover`, and `make breaking`:

**PR 1, mobs block players:**
1. **Sim:** `Blockers` and the vectors.
2. **World:** the box history, `view` on `Input` through the gateway, `simulate` passing blockers, and mobs stopping at players.
3. **Client:** `view`, `boxesAt`, and the predictor's blockers.
4. **Bots and tests:** bots attacking what blocks them, and the walker.

**PR 2, mobs attack:**
5. **Data:** the attacks table, each mob's attacks, `hit_guard`, and `provoke`; validation; the generator.
6. **World:** the mob's turn, landings, shots, `mobHits` in place of `contactDamage`, and `MobAttacked`.
7. **Client:** drawing windups and attacks.
8. **Balance and tests:** the harness on attacks, and the end-to-end tests moved off touch.

## 13. Decisions

Agreed on 2026-11-13:

1. **Mobs block from the side only, and nothing moves deeper into the other.** A player passes over a mob whose top they're above, drops through one they land on, and walks out of one they're wholly inside. One partly in can back out or jump, but not push through. The C12 PR 1 review changed this on 2026-11-13: the first rule ignored any box already overlapping, which let lag's few pixels of overlap grow into walking through a mob that walked toward the player. A mob stops at a player and never pushes. Mobs pass through mobs, and players through players.
2. **Mobs patrol and attack a player in reach.** A mob that's hit follows its attacker along its ground for 5 seconds.
3. **One attack per family:** hop, charge, bite, and spit. Bosses add a slam and alternate.
4. **A 1.5 s hit guard after any mob's hit or MISS,** as the touch guard was.

## 14. Alternatives considered

- **Ignoring a mob the player already overlaps** (the first rule): it can't tell a real inside, from a landing or a knockback, from lag's few pixels. A player walking into a mob that walked toward them passed through it.
- **Blocking against the server's present mobs, with no view:** simpler, but a player pressing against a mob walking toward them would be corrected every few ticks.
- **The client deciding blocks:** a modified client could walk through every mob.
- **Bosses blocking from above too:** no jump would get past one, as first described to the user. Landing on a boss would then need a push off its top, which is a correction the client can't predict cleanly. It's one rule for every mob for now, and the user can choose this instead.
- **Solid mobs a player can stand on:** each mob becomes a moving platform for prediction, and a player could ride one over gaps the maps don't mean to open.
- **Mobs pushing players:** a push would move the player on the server alone, outside their own prediction, and every push would be a correction.
- **Marks on the ground for an attack's area:** clearer to dodge, but more on screen. They can come later if windups alone don't read.
- **Keeping touch damage beside attacks:** blocking already keeps players out of mobs, so touches would rarely happen and would only confuse what hurt.
