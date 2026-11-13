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
| Arena | Patrol only; `Touch` on overlap | A mob box history; mobs stop at players; sight, chase, attacks in phases, and hit reactions |
| Instance | `contactDamage` every tick | `mobHits`: the attacks that land this tick, with the same guard, knockback, and death |
| `mobs.yaml` | `touch_guard`, per-mob `touch_guard` | `hit_guard`, `provoke`, `sight`, `react`, an `attacks` table, and each mob's `attacks` and `super_armor` |
| `movement.yaml` | `jump_velocity: 496`, a jump of about 100 px | `jump_velocity: 395`, about 61 px; nine maps reworked |
| Protocol | — | `Input.view`, `MobView.pose`, and a `MobAttacked` event |
| Client | Mobs drawn with no attacks | Blockers in prediction; each mob's wind-up and attack drawn |
| Balance harness | A touch every guard | An attack every cooldown, from each mob's attacks |
| Bots | Walk through mobs | Attack what blocks them, and jump when stuck |

## 3. Game data

### `data/mobs.yaml`

```yaml
hit_guard: 1500ms  # after a mob's hit or MISS, no mob's attack hits the player for this long; a hit blinks for it (was touch_guard)
provoke: 5s        # how long a mob a player hits goes after them, whoever else is near
sight: 300         # px a mob sees a player whose feet are between its own and 96 px above them
react: {flinch: 300ms, push: 8, down_hits: 3, down_window: 1500ms, down: 1s, armor_after: 1s}  # a normal mob's staggers (section 5)
patrol: {speed_jitter: 0.15, pause: [600ms, 1400ms]}  # each mob's own speed, rolled at spawn, and each edge pause, so stacks drift apart
engage: [0ms, 400ms]  # each time someone comes into a mob's reach, after nobody was, it waits a time rolled in this before a windup
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
    super_armor: true   # every elite and boss: no flinch, push or knockdown
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
- **A leap or a rush stays on the mob's ground,** clamped to its chase bounds (section 5). A rush stops at a player, since mobs don't walk through players.
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
  - After PR 3, `jump_velocity` 395 against gravity 1216 gives v²/2g = 64.2 px. Stepped at 1/60 s, the sim peaks at 60.9 px and lands after 0.63 s, down from about 100 px and 0.82 s.
  - A running jump gets past mobs up to about 56 px tall: every normal mob (28–40 px) and the 42 and 48 px elites. It usually comes down partly through them on the way, which is harmless with no touch damage. The seven 60 px elites and every boss (84 px and up) never pass.
  - Every platform or block with no rope up to it sits at most 48 px above where it's jumped from, so B1's rule holds without leaping across from ropes.
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
2. **Flinched or down:** it stands, doing nothing else, until that ends (Hit reactions, below).
3. **Otherwise, it picks a target:**
   - First its provoker: the last player whose blow (hit or MISS) reached it, within `provoke`, while that player is alive and on the map.
   - Otherwise, the target it had, until they've been out of sight for a second.
   - Otherwise, the nearest player it sees: within `sight` px across, with their feet between its own and 96 px above them. Solids don't hide anyone, since the maps are open.
   - If an attack is ready and the target is in its area, the mob faces them and starts its windup. A boss alternates its two attacks, taking whichever is ready if only one is.
   - Each time someone comes into the mob's reach, after nobody was, it starts no windup until a time rolled in `engage` has passed. Someone is its target, or, for a mob that can't get at its target, whoever it reaches; having no target counts as nobody.
     - Without this, mobs that came into a player's reach on the same tick wound up together, and equal cooldowns kept them in step. PR 4's playtest found four cave bats spitting together four volleys running. PR 4 first rolled only when a mob picked up a target, having had none.
     - Reach is where its attacks reach, not whether one is ready: a cooldown isn't leaving reach, so it adds no wait, and nor does switching to another target in reach.
     - Meanwhile a mob whose ready attack would reach holds, facing its victim.
4. **Otherwise, it moves:**
   - With a target, it walks toward them along its ground at its speed, without the edge pause, and stops at its chase bounds or at a player. A ranged mob stops once the target is in its shot's area.
   - A mob that can't get closer to a target out of reach, at its chase bounds or held by a player in its way, attacks whoever a ready attack reaches instead. Without this, slimes a bowman provoked stood pressed against the warrior between them, never attacking him.
   - Without one, a mob outside its patrol bounds walks back into them at its pace, without pausing. Inside them, it patrols as today, at its own pace (below).

**Two sets of bounds,** each worked out once at spawn:
- **Patrol bounds:** the ground under the mob within its spawn area, as today. It patrols there.
- **Chase bounds:** the ground under the mob from end to end, cut at the nearest wall, block or portal on each side that its box would walk into. It chases, leaps, rushes, and is pushed within these.
  - A mob never steps onto a portal, so a portal is always a way out. PR 4's playtest found mobs chasing players to a dead-end portal and pinning them in the 16 px strip by its wall, just off it; a boss can't be jumped, so it would pin them for good. Without the cut, mobs could reach 16 dead-end portals on 12 maps, and bosses on 6.
  - A portal mid-floor splits the ground for mobs; no mob shares a floor with one today. The loader refuses a spawn or boss area that overlaps a portal.
  - Ground pieces level with each other join, as a platform running onto a pillar's top in the cave.
  - A ledge above the mob's head doesn't cut its bounds: it walks under.
- Patrol bounds lie within chase bounds. A platform's spawn area spans the whole platform, so there the two are the same.
- PR 2 kept mobs to their patrol bounds, and the playtest found the gap: every map's ground is split into several spawn areas with gaps between them, so a player standing just past an area's end could hit a mob that couldn't step out to reach them. A mob's hit, knocking a player back 16 px, could put them there.
- Players walk at 160 px/s and mobs at 24 to 96, so a mob following along its ground never keeps up with a player walking away. It loses them out of sight, or once `provoke` runs out.

**Its own pace.** Mobs stop at players but pass through mobs, so a player standing in a lane gathers every mob in it into one stack. Stacked, they shared one speed and one 1 s pause, and so patrolled as one blob for good after the player left: PR 1's playtest found all five forest slimes in one spot.
- Each mob rolls a speed factor in 1 ± `patrol.speed_jitter` when it spawns, from the map's random source. It walks, chases, and rushes at that factor.
- Each edge pause is rolled in `patrol.pause`, in place of the fixed `mobPause`.
- So a stack drifts apart within a patrol or two. Mobs gathered around a player in a fight stay gathered, as in Elsword, until they're free.

`mob` gains:
- its provoker, and the tick that runs out;
- its target, and when they were last seen;
- its current attack and phase, with their end ticks;
- each attack's ready tick, and which attack is next for a boss;
- its reaction (flinch, down, or rising), its end tick, and its down count;
- its speed factor;
- its chase bounds, beside its patrol bounds;
- the tick its `engage` wait runs out, and whether someone was in its reach on its last turn.

A `shots` list holds flying spit.

### Hit reactions

A normal mob staggers when it's hit, as in Elsword. `super_armor` mobs (every elite and boss) never do.

- **Flinch:** each player blow that lands (a MISS doesn't) stops the mob for `react.flinch`.
  - It pushes the mob `react.push` px away from the attacker, clamped to its chase bounds. The push follows the overlap rule against other players.
  - A windup in progress is broken off, and that attack waits for its cooldown as if used, so breaking windups costs the mob its turn.
  - An act already under way, a leap or rush on its way or the recover, plays out.
  - A shot already flying flies on.
- **Down:**
  - Each landed hit adds one to the mob's down count, which resets `react.down_window` after the last hit.
  - At `react.down_hits` the mob is down for `react.down`. It can't move or attack, and it still takes damage, though no more flinches or pushes. A knockdown ends any attack under way, a leap or rush where it is, landing nothing; a shot already flying flies on.
  - Then it rises with `react.armor_after` of super armor, and the count starts again.
- **Strikes report back:** `arena.Strike` applies reactions as it resolves each blow, so a mob staggered by the first of several blows in a tick reacts once per landed blow, in order.
- **The pose reaches clients** in `MobView.pose`: none, flinch, down, or rising. It's resent when it changes, so a player arriving sees a mob that's down.

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
  - `MobView.pose`, an enum: none, flinch, down, rising.
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
- **Reactions,** from `MobView.pose`:
  - **flinch:** a white flash and a jolt back;
  - **down:** the mob lying on its side with a puff of dust;
  - **rising:** a blink.
  - A broken-off windup ends its drawing.
- **Super armor:** elites and bosses carry a faint warm outline, so players can tell which mobs won't stagger.
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
- **Staggers aren't modelled.** The harness keeps its worst case, a mob attacking on every cooldown, so hits survived and kills a life stay true for a player who doesn't interrupt. Interrupting normal mobs only makes a fight safer, and elites and bosses can't be interrupted.

## 9. Bots and tests

- **Bots (`cmd/bot`):**
  - A bot blocked by a mob attacks it, since it's in front, until it dies, then walks on.
  - A bot stuck for a moment, 0.2 s, jumps, as the end-to-end walker does.
  - Mobs that see a bot come to it, and the bot attacks whatever is in front.
  - The bot's climb plan follows the reworked maps.
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
| Several mobs reach a player at once | They all attack, each after its own `engage` wait, and stay out of step | The hit guard keeps them from chaining hits; normal mobs can be staggered |
| A player stands on a block or a platform above a mob | The mob can't reach them, and they can hit it | Known, until mobs can cross platforms, a later feature |
| A boss corners a player at a wall | Blocked on one side, hit with knockback | The boss stops at the player and never pushes; knockback moves them; jumps and drops still work |
| A mob walks toward a player who presses on | They meet a few pixels apart or in, and stop | Neither side's move can grow the overlap, so lag never lets one pass through |

## 11. Testing

- **Game data:** the attacks table, `use` and overrides, the families, each refusal, `hit_guard`, `provoke`, and `engage`, and a spawn or boss area over a portal refused.
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
  - sight: a player in range and height seen, one too high or too far not, and a target kept for a second out of sight;
  - provoke over a nearer player, and the end of chase;
  - a chase past the patrol area along the ground, stopping at a wall, a block, a portal, and the ground's end; a boss stopping at a portal; every shipped mob and boss kept off every portal; the walk back into the patrol bounds with no target;
  - a player chased past a portal, back to a wall, walking onto it and leaving;
  - a leap, a rush, and a push clamped to the chase bounds;
  - two mobs that see a player on one tick, and a pile holding a target out of reach that comes into reach on one tick, winding up at different ticks;
  - a mob whose target leaves its reach and comes back waiting again; no `engage` wait for a cooldown, or for a switch of target in reach;
  - a flinch breaking a windup and spending its cooldown; the push and its clamp;
  - a knockdown at the third quick hit, damage while down, the rise with super armor;
  - super armor ignoring all three;
  - a stack of mobs, free of players, drifting apart by their own speeds and pauses;
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
- **End to end:** a player blocked by a mob; a bite landing, and stepping out of a windup dodging it; a provoked mob following past its patrol area.

## 12. Build order

Four PRs, each step one commit, green on `make test`, `make lint`, `make cover`, and `make breaking`:

**PR 1, mobs block players, merged as #71:**
1. **Sim:** `Blockers` and the vectors.
2. **World:** the box history, `view` on `Input` through the gateway, `simulate` passing blockers, and mobs stopping at players.
3. **Client:** `view`, `boxesAt`, and the predictor's blockers.
4. **Bots and tests:** bots attacking what blocks them, and the walker.

**PR 2, mobs attack, merged as #77:**
5. **Data:** the attacks table, each mob's attacks and `super_armor`, `hit_guard`, `provoke`, `sight`, `react`, and `patrol`; validation; the generator; and the harness on attacks, since `touch_guard`, which it read, goes here.
6. **World:** the mob's turn with sight and chase, landings, shots, hit reactions, `mobHits` in place of `contactDamage`, `MobAttacked`, and `MobView.pose`.
7. **Client:** drawing windups, attacks, reactions, and super armor.
8. **Balance and tests:** the end-to-end tests of blocking, a bite and its dodge, and provoke. The tests that walked into a mob for a touch move to attacks in step 6, so each step stays green.

**PR 3, a lower jump, merged as #74.** It comes after PR 1 and is independent of PR 2:
9. **Movement:**
   - `jump_velocity` 395, and the movement vectors made again.
   - At 395 the reach test fails 41 times over nine maps: cave, crossroads, fallen_keep, frost_trail, glacier_peak, icefall, spore_hollow, sunken_ruins and town.
   - Each is reworked, lowering platforms and adding ropes or ladders, until `TestEveryPortalSpawnAndBossCanBeReached` passes at both speeds.
   - Every platform or block with no rope of its own is at most 48 px up, so the jump reaches it from below without leaping across from a rope (B1).
10. **Bots and tests:** the bot's climb plan and the end-to-end walkers on the new maps.

**PR 4, mobs follow along their ground,** after PR 2's playtest:
11. **Data:** `engage` and its validation: two times, the first not negative and not above the second.
12. **World and tests:**
    - chase bounds at spawn;
    - chasing, leaps, rushes, and pushes within them;
    - the walk back into the patrol bounds;
    - the `engage` wait;
    - no client change, since mob positions reach it as today.
13. **After PR 4's playtest:** chase bounds cut at portals, and the loader refuses a spawn or boss area over one; the `engage` wait rolled each time someone comes into reach.

## 13. Decisions

Agreed on 2026-11-13:

1. **Mobs block from the side only, and nothing moves deeper into the other.** A player passes over a mob whose top they're above, drops through one they land on, and walks out of one they're wholly inside. One partly in can back out or jump, but not push through. The C12 PR 1 review changed this on 2026-11-13: the first rule ignored any box already overlapping, which let lag's few pixels of overlap grow into walking through a mob that walked toward the player. A mob stops at a player and never pushes. Mobs pass through mobs, and players through players.
2. **Mobs come after players, as in Elsword.** A mob sees a player on its ground within 300 px and comes to attack. One that's hit goes after its attacker for 5 seconds. This was changed on 2026-11-13: the first rule waited for a player in reach.
3. **One attack per family:** hop, charge, bite, and spit. Bosses add a slam and alternate.
4. **A 1.5 s hit guard after any mob's hit or MISS,** as the touch guard was.
5. **Hits stagger normal mobs.** A flinch breaks off a windup, and a quick burst knocks the mob down. Elites and bosses have super armor.
6. **A lower jump, peaking about 61 px,** so a jump passes one mob at a time. The nine maps it cuts off are reworked, with every platform that has no rope at most 48 px up.
7. **Each mob has its own pace:** speed within ±15% and edge pauses of 0.6 to 1.4 s, so mobs a player gathered drift apart again.
8. **Later features, in this order:** attack combos; mobs crossing platforms; launchers and air juggles; players knocked down by heavy hits. Finga has no dash: the user dropped it on 2026-11-13.
9. **A mob going after a player follows them along its whole ground,** up to a wall, a block, or the ground's end, and walks back to its patrol area once it has no target. Agreed after PR 2's playtest.
10. **A mob waits 0 to 400 ms, rolled, each time someone comes into its reach after nobody was,** so mobs that reach a player together attack one after another. Agreed after PR 2's playtest for a mob's first attack on a target; widened after PR 4's, where piles came into reach together and stayed in step.
11. **A basic attack hits the nearest mob it reaches,** as before, even in a pile. The user kept this after PR 2's playtest (see section 14).
12. **A mob never steps onto a portal,** a boss included: it chases up to a portal's edge, so a portal is always a way out. Agreed after PR 4's playtest.

## 14. Alternatives considered

- **Ignoring a mob the player already overlaps** (the first rule): it can't tell a real inside, from a landing or a knockback, from lag's few pixels. A player walking into a mob that walked toward them passed through it.
- **Blocking against the server's present mobs, with no view:** simpler, but a player pressing against a mob walking toward them would be corrected every few ticks.
- **The client deciding blocks:** a modified client could walk through every mob.
- **Bosses blocking from above too:** no jump would get past one, as first described to the user. Landing on a boss would then need a push off its top, which is a correction the client can't predict cleanly. It's one rule for every mob for now, and the user can choose this instead.
- **Solid mobs a player can stand on:** each mob becomes a moving platform for prediction, and a player could ride one over gaps the maps don't mean to open.
- **Mobs pushing players:** a push would move the player on the server alone, outside their own prediction, and every push would be a correction.
- **An 80 px jump:** only four maps to rework, but every elite could still be jumped.
- **Keeping mobs apart with a separation rule:** mobs would jostle at a player's face, and it's more work in the tick. A pace of their own spreads a stack once it's free, and in a fight mobs crowding a player is the point.
- **Flinches on every mob:** a player could interrupt an elite's or a boss's every windup, and the hardest fights would need no dodging.
- **Marks on the ground for an attack's area:** clearer to dodge, but more on screen. They can come later if windups alone don't read.
- **Mobs kept to their patrol area** (PR 2): every map's ground is split into spawn areas with gaps, so a player just past an area's end hit mobs that couldn't reach back.
- **A leash a set distance past the patrol area:** a mob held at the leash's end leaves the same safe spot a little further out.
- **A hit preferring the mob the player's last blow landed on:**
  - The problem it would solve: each hit pushes its mob 8 px back, so in a pile the next hit often lands on a neighbour, 8 of 14 times in PR 2's playtest, and a knockdown rarely completes against a crowd.
  - The user kept the nearest mob on 2026-11-13.
- **Keeping touch damage beside attacks:** blocking already keeps players out of mobs, so touches would rarely happen and would only confuse what hurt.
