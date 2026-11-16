# 3D world

> **Status: approved · 2026-11-14.** The high-level design for [B7. 3D world](../product/features/world-and-movement/b7-3d-world.md). It describes the code as it stands on `main`, a 2D side-scroller drawn with Phaser, and the change it plans: a 3D world drawn with Babylon.js, built on a `3d` branch until a playable slice is complete, which then replaces `main` at once (section 13). As of 2026-11-16 the slice is complete on `3d`: "today" in the plan below means the 2D game, and the "As built" notes record where the build went differently.

## 1. What has to hold

- **Only the world changes.** Stats, progression, combat math (`model.Resolve`), actions and cancels, energy, 1 s skills that leave movement free, skills, items, quests, party, chat, and the React HUD and windows keep their rules and their code. The balance harness doesn't change: it reads no geometry.
- **The server stays authoritative.** It moves every player with the shared sim, moves every mob, and decides every hit. The client predicts its own movement and replays it against the server's, as today.
- **One sim, written twice, pinned by vectors.** The 3D movement sim is Go in the server and TypeScript in the client, op for op. Shared JSON vectors replay on both sides. It uses no trigonometry, and Go wraps products in `float64(...)` as today, so arm64 and amd64 and the browser agree to the bit.
- **The client computes, the server validates.** Auto-aim, the outline, ground telegraphs, the camera, and every look are the client's. The server takes the aim with each action, since a player may face any way anyway.
- **Data, not code, describes the world.** Maps are YAML that the team writes and tests: terrain, obstacles, spawns, and portals. Models and looks are keys in data.
- **The 2D game stays playable on `main` until the slice replaces it.**

## 2. The change at a glance

| Part | Today | After |
| --- | --- | --- |
| Coordinates | Pixels; x across, y down; feet centre; `facing` ±1 | Metres; x east, y up, z north; feet centre; a heading (section 3) |
| Maps | 14 Tiled `.tmj` files of rectangles; `pkg/tiled` | 14 YAML maps: a heightfield, solids, props, spawn circles, portal circles (section 4) |
| Sim | `server/internal/shared/sim`, `web/src/sim`: walk, jump, ropes, one-way platforms, mob boxes | The same files in 3D: walk 8 ways, jump, slide along solids, slopes, mob circles; no ropes or platforms (section 5) |
| Input | `{left, right, up, down, jump}` + `view` | Unchanged; up and down walk north and south |
| Player attacks | Boxes, a horizontal line, a radius, toward ±1 | Arcs, shots, a radius, toward an aim the client picks (section 6) |
| Mobs | Glued to one ground segment, moving in x | Wander a spawn circle, chase on a walk grid with A*, leash home; 3D attack areas fixed at the wind-up (section 7) |
| Protocol | x, y, ±1 facing | x, y, z, a heading; `aim` on `Attack` and `UseSkill`; the 3D point of a mob attack (section 9) |
| Client drawing | Phaser 4: 6,741 lines (not counting tests) in `web/src/game`, most of it drawing | Babylon.js: a new renderer behind the same `GameHandle` (section 10) |
| Art | 54 2D sprite PNGs | CC0 glTF characters, monsters, and kits (section 11) |
| Bots | Walk in x, climb ropes by hand-written routes | Walk the same grid as mobs (section 12) |

## 3. Units and coordinates

- **1 metre is 32 px.** Every length in data converts once, in the data step (section 13):
  - The character is a cylinder 0.75 m across and 1.75 m tall, from 24 × 56 px.
  - Walking is 5 m/s, from 160 px/s.
  - Jumping is 12.34 m/s against 38 m/s² of gravity, a 1.9 m rise. The falling clamp is 22.5 m/s.
  - These are starting values. The slice playtest tunes the jump, which may be high for 3D.
- **Axes:** x east, y up, z north. The camera looks north, so up on screen is +z. Babylon's left-handed default has the same axes, so positions need no flipping.
- **Heading:** the way a body faces, as a unit vector `(hx, hz)`.
  - The sim sets it from the keys, one of 8 directions. Diagonals use √½ written as the constant `0.7071067811865476`, so no trigonometry is needed.
  - An action's aim is a separate angle, a yaw (below).
- **Yaw** is an angle in radians from +z toward +x: north 0, east π/2. The client and server use `atan2`, `sin`, and `cos` for yaw outside the shared sim only. That covers aiming, hit shapes, and drawing, where a last-bit difference changes nothing the server decides.
- **Data angles** are written in degrees.

## 4. Maps

### Format

`server/data/maps/<id>.yaml` replaces `<id>.tmj`. Every rule `load/maps.go` checks today carries over: the portal graph, towns, the start town, level bands, spawns and their mobs, and boss areas and kills. The geometry rules change.

```yaml
name: Forest
kind: hunting
town: town
levels: [1, 4]
look: {ground: grass, sky: day}          # client art keys
size: [72, 40]                           # metres, x by z; the edges are walls
terrain:
  cell: 0.5                              # metres between height samples
  hills:                                 # smooth bumps
    - {at: [24, 14], radius: 7, height: 1.5}
  mesas:                                 # raised flat ground with sloped edges
    - {area: [44, 24, 12, 8], height: 1.2, edge: 0.5}   # x, z, width, depth; a cliff, jumped onto
    - {area: [8, 30, 10, 6], height: 1.5, edge: 4}      # a ramp, walked up
solids:                                  # nothing walks through them; a top low enough can be stood on
  - {box: [10, 6, 3, 1.5], height: 2.5, yaw: 15, model: rock_large}
  - {cylinder: [16, 20], radius: 0.6, height: 7, model: tree_oak}
props:                                   # looks only
  - {at: [12, 8], model: grass_tuft}
  - {scatter: {model: flowers, area: [0, 0, 72, 40], count: 120, seed: 7}}
portals:
  - {name: from_town, at: [2, 20], radius: 1.2, to: {map: town, portal: to_forest}}
spawns:
  - {name: slimes, at: [22, 20], radius: 8, mob: green_slime, max: 5, respawn: 8s}
player_spawn: {at: [4, 20], yaw: 90}
npcs:
  - {id: bowman_trainer, at: [30, 24], yaw: 180}
boss: {at: [58, 20], radius: 9, mob: king_slime, kills: 300}
```

As built in spike 3, and renamed in slice PRs 4 and 5:
- The maps live in `server/data/maps/`, and the sim's constants in metres in `server/data/movement.yaml`. The format and its build are `server/internal/shared/mapfile`. Spike 3 kept them in `maps3d` and `movement3d.yaml` beside the 2D ones; the `.tmj` files and `pkg/tiled` went in slice PR 4.
  - `load.Dir` builds and checks them at startup, so a broken one stops the game.
  - A map's walk grid is built at its first use (`Map.Grid`), not at load. The grid is the heaviest part of a map, and the tests load the data hundreds of times.
- A box is `[x, z, width, depth]` about its centre. Its width runs across its yaw and its depth along it. An area is `[x, z, width, depth]` from its south-west corner.
- A solid's `height` is above the terrain at its centre.
- The converted placeholders start with a `# Converted from` line, which tells them from designed maps.

### The ground

- **The terrain** is a height grid `cell` apart, built at load: 0, plus each hill's smooth bump, plus each mesa's flat top and its sloped edge. The height between samples is bilinear.
  - A hill is `height × (1 − d²/r²)²`, smooth at its top and its foot.
  - A mesa falls in a straight line across `edge` outside its area, with rounded corners.
- **The same bits on every server and client.** Every number the sim reads is made of + − × ÷ and `sqrt`, each product rounded on its own. That covers the heights and the solids' tops and axes.
  - A box's axis comes from its yaw through the loader's own sine and cosine, a Taylor series reduced to a quarter turn. A multiple of 90° gives exactly 0 and ±1.
  - Go's `math.Sincos` isn't used: on arm64, Go fuses its polynomial into FMA, and 1 angle in 50 differs from amd64 by a last bit.
  - CI hashes every map's geometry and walk grid on amd64 and arm64.
- **Solids** are upright boxes and cylinders. Their tops are ground for anyone standing above them.
- **The ground under a point** is the higher of the terrain there and the top of any solid whose footprint holds the point.
- **Walkable slope:** `max_slope` in `movement.yaml`, a rise per metre (0.7, about 35°).
  - At load, each terrain cell, the square between four height samples, is marked steep along x if the rise across either of its x edges exceeds `max_slope` × `cell`, and steep along z likewise. That's one multiplication and one comparison, with no trigonometry.
  - A walking body can't move uphill into a cell that is steep along the way it moves. A steep slope is climbed only by jumping, and only if it's low enough: in the air, a body moves uphill into a steep cell only with its feet at or above that cell's highest sample, so a cliff is jumped onto whole or not at all.
  - A body walking into a steep cell doesn't follow the ground down: it falls off the cliff (section 5).
  - The sim (section 5), the walk grid (section 7), and the reach check all read these marks, so they can't disagree.
  - The terrain is what its samples say: an edge narrower than a cell is spread across it. At `cell: 0.5`, a rise of more than 0.7 m within one cell's width is always steep, wherever the samples fall.
- **A solid's lip is `step` (0.3 m):** a top up to that far above the feet is stepped onto, and a higher one is a wall.
- **Props are looks only.** A `scatter` is expanded by the client from its seed, since nothing collides with it.

### Validation, beyond today's rules

- Every spawn circle, the boss area, NPC, portal, and player spawn sits on walkable ground inside the map.
- Spawns and the boss area don't overlap a portal.
- The boss's circle is wide enough for its body.
- Every map passes the reach check (below), a test over the shipped maps rather than a check at load.
- Walkable ground is ground level: a spot on a solid more than `step` tall, such as up a tree, is refused. The loader checks each spot at the spot itself (`nav.FitsAt`), by the rule the walk grid applies at its cells' centres.

**Authoring:** a barrier meant to stop players is a solid whose top is more than 2.2 m above the ground beside it (`step` plus the jump's 1.9 m peak), or terrain rising more than 1.9 m within one cell. A 1 m crate stops nobody. A cliff spread over several steep cells can be climbed in jumps, one cell at a time, so hills and mesas are for ground that players can reach.

### Reach check

It keeps the 2D `load/reach_test.go`'s goal: every portal, spawn, the boss area, and every NPC can be reached from the player spawn and from every arrival, at every move speed. For the 3D maps it's a test in `server/internal/shared/nav`.
- It floods the walk grid (section 7).
- A step between neighbouring cells is allowed by walking unless it goes uphill into or out of a cell steep that way (above) or onto a solid more than `step` higher. It's allowed by jumping if the rise is within a jump's rise less a margin.
- Any drop is allowed.

As built in spike 3, it's nav's test `TestEveryShippedMapCanBeCrossed`. It floods each shipped map once from each start, once per test run. The maps come only from the repo, so CI guards them as well as a check at server start would.
- As a load rule, its floods ran on every load of the data. CI's coverage run of the load package's tests took 101 s against main's 5.6 s, and each end-to-end stack took 14 s to start against 0.2 s.
- A jump into a steep cell must clear the cell's highest sample, and a jump onto a solid its top less `step`, as the sim requires.
- **Jumps and drops may cover two cells** along x or z.
  - A body takes off from beside a solid, and lands there when it drops off. That cell's centre is nearer the solid than the body's radius, yet the body stands in it touching the solid, as it does in the sim.
  - The cell jumped over needs room for the body among the solids too tall to clear, so a gap narrower than the body stays shut.
- **A jump's rise is the sim's own peak, played by the test.** The sim is stepped, so it peaks a little lower as the speed rises: 1.90 m at 1×, 1.87 m at 1.3×.
  - The fastest a player moves is the `move_speed` curve's limit in `stats.yaml`, 30%. That limit also caps gear, passives and buffs.
  - So the limit is the peak at that speed, less a 0.1 m margin: 1.77 m. It holds at every speed.

### Mob reach check

As built in the cave's PR, nav's test `TestEveryMobReachesAPlayerAnywhereInItsLeash` keeps ledges and nooks from making safe spots. On every hunting map, for each spawn and the boss, every spot a player reaches inside the mob's leash must be one where the mob can land a blow.
- The player's flood is the reach check's, at the highest jump, 1×, from the player spawn and every portal. The mob's walks the grid as its A\* does, with its own radius, on cells inside its leash.
- A spot is reached if, from ground the mob walks, one of its blows meets the player's height within its distance, as the mobs' own reach test measures it:
  - its body's touch, a bite's arc and a slam's circle as they are;
  - a hop's landing circle and a charge's body, from ground it walks: a hop or a charge stops where the mob can't walk, as a ball stops at a rise;
  - a shot's ball from beside the player, its lane short and clear.
- Ground a map may keep from its mobs is named in `unreachedGround`, each with why. The forest keeps its mesa, under the scoped ledge rule until mobs climb. The check also found the forest's boulder, which the forest-boulder PR raised past standing on.
- The cave's first layout failed it: 109 spots on the shelf for the bats, whose steps lay outside their leash, and 2 for the vampire bat in a nook by the exit. It takes 0.02 s over the 12 hunting maps.

`TestRopesStandApart` goes with the ropes.

### The client's copy

Today `copy-maps.mjs` copies the `.tmj` files to `web/public/maps/`, git-ignored, stripping the boss layer, and `gen-gamedata.ts` writes only map properties to `src/gamedata`. After, `copy-maps.mjs` writes each map's geometry as `web/public/maps/<id>.json`, still git-ignored and with the boss's kills stripped, and `pkg/tiled` goes.

As built in slice PR 4, `server/cmd/mapjson` (`npm run maps`) replaces `copy-maps.mjs`. It writes each map's geometry as Go builds it, with its art and without its boss, to `web/public/maps/<id>.json`, and removes only the `.json` files it wrote before.

### Placeholders

A one-off converter turns each 2D map into a plain 3D one, so the slice keeps every quest, level band, and boss working.
- The map becomes a corridor 12 m deep and as long as the 2D map.
- Its ground, platforms, and ropes flatten to one floor.
- Spawns become circles along the middle.
- Portals stay at the ends.

As built, `server/cmd/tmj2yaml` was the converter. It went with the `.tmj` files in slice PR 4; each placeholder's first line still names it.
- A portal goes to the end nearer its 2D spot, beside any other already there.
- A spawn's or boss's circle is half its 2D width, kept inside the corridor and off the portals.
- NPCs stand beside the middle line.
- The look keys are the 2D map's.
- The terrain's `cell` is the widest whole metre that divides the corridor, as flat ground needs no finer height samples.

Town and forest are designed by hand. The other 12 keep their placeholders until their own PRs (section 13).

As built in the forest-boulder PR, the forest's boulder at (36, 8) stands 2.31 m, its model drawn 1.9 times its size and its circle 2.52 m. At 1.21 m a player jumped onto it, and the middle of its top lay out of every forest mob's reach: a safe spot to hit them from. Past a jump's 1.9 m peak and the 0.3 m step onto a top, nobody stands on it.

As built, the cave is the first of the 12.
- Its shelf, 1.2 m, has steps at both ends: the boars walk up from the west and the bats and the vampire bat from the east, as the mob reach check (above) requires. With steps at the west end only, the bats stood below the shelf's east half, too low to spit at a player on it.
- A spit flies level at its mob's mid-height: a cave bat never hits a player standing more than 0.69 m above it. So the bats' hall is flat, and no other ground in the cave rises more than 0.6 m.

As built, the shroom grove is the second, 72 by 36 m: a grassy hollow under banks too steep to climb, from the cave's way in through the slimes' meadow, round a fairy ring of mushrooms where the lone snail lives, to the Spore Mother's mound before the way to the spore hollow.
- Its mushrooms are one model at two sizes, 3.57 and 2.45 m tall. Their caps start below a player's head, so each one's circle is its caps' reach, and nothing overhangs it.
- The ring's gaps are 3.8 m, wider than the Spore Mother, and every other gap between solids and banks is either as wide or too narrow for a player.
- A snail's only blow is its charge, which lands touching, so no solid a player can stand on sits inside a snail's leash. The stumps and the rock stand in the meadow, where only the slimes hop.
- The mound is a hill 1.5 m high. A mushroom on its slope could be jumped onto from the uphill side, so the mound stays open: the Spore Mother's 5 m hop has room.

As built, the spore hollow is the third, 75 by 36 m: a dell, its floor 12 m wide between slopes 4 m long up to shelves 1.5 m high under banks too steep to climb, from the grove's way in west, past the Moth Empress's glade, to the way up to the toadstool heights.
- The dust moths spit, and their leashes cover nearly the whole map, so no top a player can stand on rises more than 0.75 m: a moth's spit flies level at its mid-height, and a rock of 1.13 m would be out of its reach. Its stumps are 0.6 m.
- The giant mushrooms stand on the shelves against the banks, too tall to jump onto from any ground beside them; the smaller ones stand on the flat floor, since one on a slope could be jumped onto from uphill.
- The middle of the floor is kept open for the Moth Empress, 3.75 m across.

As built, Toadstool Heights is the fourth, 76 by 36 m: the woods climb east in two terraces between steep banks, from the hollow's way in past the glow slimes' meadow to the Lantern Slime's summit and the way to the crossroads.
- Each terrace is one mesa wider than the map, 2 m up a ramp 5 m long, so its edges fall outside the map and the ramp spans it whole. Every rise is a slope every mob walks, with no ledge a player jumps onto, so a terrace above or below a mob is ground it reaches.
- Rock ridges 3 m high run along both ramps, each leaving one pass 9 m wide: north on the first, south on the second.
- An elder snail's only blow is its charge, which lands touching. So the stump and the rock, the only solids a player can stand on, sit in the meadow, 29 m or more from the nearest snail and beyond every snail's leash. The toadstools are 2.45 and 3.57 m tall, and none stands on a ramp, where the uphill side would bring its top within a jump.
- The summit stays open round the Lantern Slime: the nearest solid is 1.9 m outside its circle, so its 5 m hop has room.
- Every gap between solids, ridges and banks is either as wide as the biggest mob that comes there or too narrow for a player. The first layout left a 1.4 m nook between the second ridge and a toadstool, which the Lantern Slime's 3 m body couldn't enter.

## 5. Movement sim

`server/internal/shared/sim/sim.go` and `web/src/sim/step.ts` are rewritten together. The vectors in `server/testdata/movement` are made again from the Go side, and both sides replay them.

As built: spike 2 put the 3D sim beside the 2D one, and slice PR 4 deleted the 2D sim and gave the 3D one its place, `server/internal/shared/sim` and `web/src/sim`, with its vectors in `server/testdata/movement`.
- `sim.Push` moves a body by a given distance, x then z, stopped where a step would stop it. The server alone uses it: for knockback, a mob's flinch push, and every mob move.

### Types

- **`Params`:** `dt`, `walk_speed`, `jump_velocity`, `gravity`, `max_fall_speed`, `radius`, `height`, `step` (0.3 m), `max_slope` (0.7, section 4), and `snap` (0.5 m), the most the ground may fall away under a walking body before it falls instead. `Faster` scales the speeds as today.
- **`Geometry`:**
  - the height grid;
  - the solids. A box carries a unit axis `(ux, uz)` instead of its yaw, and an axis-aligned box is `(1, 0)`. Go alone makes the axis: the map loader (spike 3) computes it once, and the client reads it from Go-generated JSON, never from its own `Math.cos`, which may differ from Go's in the last bit;
  - the map's size;
  - `Blockers`: mob circles (centre, radius, top, bottom), lag-compensated through `view` as today.
- **`Input`:** unchanged, `{left, right, up, down, jump}`.
- **`State`:** `X, Y, Z`, `VX, VY, VZ`, `OnGround`, and `HX, HZ` (the heading). `Climbing`, `Ground`, `DropPlatform`, and `DropTimer` go. `Casting` is already gone ([Combat feel](./combat-feel.md) section 15).

### `Step`, one sub-step at 1/60 s

1. **The heading and the walk:**
   - The keys give a direction, the heading follows it, and the walk velocity is that direction times the walk speed. Opposite keys cancel.
   - Actions don't touch movement: a player using a skill moves as one who isn't.
2. **Jump:** from the ground, `VY` gets the jump velocity.
3. **Gravity:** `VY` falls by gravity × dt, clamped to the falling speed.
4. **x, then z.** Each axis moves on its own, so a body slides along whatever stops the other axis:
   - **Solids** whose top is above the feet plus `step` stop the body at contact. A box is tested circle against rectangle; a cylinder circle against circle. Contact uses `sqrt`, which IEEE rounds the same everywhere.
   - **Terrain:** on the ground, an axis move uphill into a cell steep along that axis (section 4) is a wall on that axis. In the air, an axis move uphill into such a cell is a wall unless the feet are at or above that cell's highest sample, so a cliff low enough is jumped onto whole, and a higher one is never entered. Any other move in the air is a wall only if the terrain there would be above the feet, so a body leaving a cliff top falls clear of it.
   - **Mobs** keep today's rule, made round: a move may not bring the centres closer while the body overlaps a mob circle. A body can still walk out of an overlap, and it can jump over a mob.
   - **The map's edges** are walls.
5. **y:** the body moves by `VY`.
   - It lands when its feet reach the ground under it.
   - A body walking on the ground follows the ground down by up to `snap`, so it walks down slopes instead of hopping. Moving into a cell steep along the way it moves, it doesn't: it falls off the cliff.

### Vectors

They replace today's 30:
- walking each of the 8 ways, and opposite keys;
- walking at a move speed factor;
- sliding along a box and along a cylinder at an angle;
- a box corner;
- a turned box slid along, and its corner;
- walking up a ramp under `max_slope` and stopping at a cliff over it, both on an axis and diagonally, and a cliff whose rise the samples spread over two cells;
- jumping onto a low cliff from its foot, and short of a high one;
- stepping off a cliff top, and landing on a steep cell;
- walking down a slope without hopping;
- a jump, a jump onto a box top, a jump short of a too-tall one, a jump onto a stump, and a jump rising past a step's lip;
- stepping onto a box within `step`;
- falling off a mesa edge, and the falling clamp;
- mob circles: walking into one, out of one, along one, and jumping over one;
- the map's edge.

### The server and the client around it

- **The server's input pipeline is unchanged:** the queue sorted by seq, the gap and backlog rules, 3 sub-steps a tick, `ack_seq`, and `view` lag compensation.
- **The client's `Predictor` is unchanged in shape:** history, replay, and paces. Its casting marks are already gone ([Combat feel](./combat-feel.md) section 15).
  - Its state type and `differs` gain z and the heading.
  - It resets past 0.125 m, today's 4 px threshold of `RESET_DISTANCE_PX` in metres.
  - `fromWire` loses `platformUnder`.
- **Interpolation** lerps x, y, and z, and turns the drawn heading the short way round.

As built in slice PR 7:
- **Any difference resets.** A Delta's state that differs from the prediction in any field resets and replays: the sims run alike to the bit, so a difference is the world's own, a push or a blocker the client didn't see. The count shows in the `?fps` readout.
- **Each input is predicted under its own seq,** read after `tickStart` sends it.
- **A reset part way through a tick** takes the world's whole tick and steps it no further.
- **The view the world blocks against:** an input blocks against the mobs at its view, held within the 8 frames the world keeps at the tick it runs it, as the world's `seen` does. That tick is one input a tick on from the last ack. The mobs are listed in id order, as the world keeps them.
- **A reset is drawn as a shove.** The sim takes the world's state at once, but the drawn player eases from where they were drawn to it over 100 ms, fast at first. The first 3D playtest saw each knockback jump 0.5 m sideways and 0.27 m up in one frame, which read as a teleport.
- **The keys outlive a map.** One `InputSender` serves every map, so a key held through a portal keeps walking on the other side.

As built after slice PR 8's playtest:
- **A lasting input lead drains.** The queue was cut only past 200 ms, so a hitch that queued three or four inputs kept the world that many ticks behind the client for as long as input came steadily. A knock then landed on a player the world had some way behind, and drew with no hop.
  - A queue that has held more than `inputKeep`, two ticks, on every tick for a second now drops its oldest input each tick until it holds two.
  - Four inputs at once, then one a tick, made a lead of 3 that lasted for as long as the input did. It now holds for 1 s, drops two inputs, and is down to 1 by 1.1 s.
  - Each input dropped while the player moves costs the client one reset, and none while they stand still.
  - Input whose queue falls to two or fewer within each second, steady or jittery, drops nothing.

## 6. Combat in 3D

### Shapes

`model.Reached` and `MeleeBox` are rewritten. `Resolve` and everything above it stay. A shape starts at the attacker's feet centre, toward its aim yaw.

| Kind | Data | What it hits |
| --- | --- | --- |
| `arc` (melee) | `radius`, `angle`, `height` | Bodies whose circle reaches into the sector of `radius` and `angle` about the aim, and whose height span meets 0 to `height` above the attacker's feet |
| `shot` (ranged) | `range`, `width` | Bodies whose circle meets the lane of `width` from the attacker's chest, along the aim, up to `range`. Nearest first, up to the action's `targets` |
| `area` | `radius` | Bodies whose circle meets the circle of `radius` about the attacker, as today |

- **Conversion from today's data:**
  - A melee `width` becomes `radius` = 0.375 m + width ÷ 32. The Warrior's 64 px becomes 2.375 m.
  - `height` converts to metres.
  - `angle` is 120° for basic attacks; each skill names its own.
  - A ranged `range` divides by 32, and `width` is 0.6 m.
  - An area `radius` divides by 32.
- **Targets** are ordered nearest first, as today, and `most` still caps them.
- **Party heal** (`hurtInReach`, and `healReach` in `skill-keys.ts`) uses the 3D distance from the caster's middle to the nearest point of the member's body.

### Auto-aim

On the client, as an action starts:
- Its target is the nearest living mob whose body is within the action's reach plus its own radius, within ±`aim_cone` (70°, in `actions.yaml`) of the heading.
  - Ties go to the smaller angle.
  - A heal aims at no one.
- The aim is the yaw from the player to that mob, or the heading's yaw with none. The player turns to it.
- `Attack` and `UseSkill` carry `aim`.
  - The server sets the player's drawn facing to it and uses it for the action's shape.
  - It doesn't check the pick: walking already lets a player face any way.
- **The outline** marks the mob a basic attack would pick now. It's recomputed each frame from the same rule, and drawn with Babylon's `HighlightLayer`.

As built in slice PR 8:
- **An action's reach** is its shape's: an arc's radius, a shot's range, an area's radius.
- **Distances are across the ground.** A mob on a ledge or in the air counts by how far away it stands, not by how high it is.
- **Ties** are mobs within a micrometre of the same distance, so the smaller angle wins however the sums round.
- **A heal** faces the heading, never a mob, even with one in reach.
- **The turn:** the player, and each other player from their `AttackUsed` or `SkillUsed`, faces the aim until the action's ready point, then their heading again.

### Knockback and flinch

- A mob's hit pushes the player 0.5 m away from the mob in x and z (today 16 px), stopped by solids and, as built after the first 3D playtest, by the mobs where they stand. It also hops them with `VY` = 6.25 m/s.
- A mob's flinch push of 8 px becomes 0.25 m, away from the attacker.

As built in slice PR 5:
- **Heights:** only an arc checks one. A shot and an area reach a body at any height.
- **Arc angles:** 120° for every class's basic attack, Strike and Heavy Strike; 180° for Slash Blast; 90° for Double Stab.
- **The aim** doesn't turn the server's heading, which stays the keys'. The server uses the aim for the action's shape and sends it on `PlayerView`, `AttackUsed`, and `SkillUsed`, and the client turns the drawn body to it.
- **A heal** must be an `area`: the loader refuses any other shape for one.

## 7. Mobs

`application/mobs/arena.go` and `attacks.go` are rewritten. Timings, powers, poses, reactions, engage, provoke, and `hit_guard` don't change in this rework. `hit_guard` went from 1.5 s to 0.5 s later, on 2026-11-16, so a crowd is dangerous ([Mobs fight back](./mobs-fight-back.md) decision 4).

### Bodies and ground

- A mob is a cylinder with `radius` = width ÷ 64 and `height` = height ÷ 32 from today's `mobs.yaml`. Seven elites, the snails, the scarab, the elite rats and the weasel, were lowered from 1.875 m to 1.25 m (the scarab to 1.125 m) to be jumped over.
- A player escapes a crowd by jumping; every non-boss mob is low enough to jump over. A mob stays solid to players: its circle stops a player whose feet are below its top. So a jump clears a mob only if the feet stay above its top from first touch until past its centre, which a jump made pressed against the mob counts. nav's `TestEveryShippedMobButABossCanBeJumpedOver` plays this through the sim at the slowest and the fastest move speed.
- It stands on the ground under its centre, and never jumps or falls. A hop is drawn as an arc.
- As built in slice PR 4, a mob's `look` is an optional `model`, a `tint`, and a `scale`; the 2D `sprite` is gone. Only 3 of the 36 mobs have a model so far (green slime, orange mushroom, king slime), so slice PR 7 needs a stand-in model for the rest. As built in slice PR 7, 13 have one, every slime taking the Green Blob, and the other 23 draw stand-ins.

### The walk grid

`server/internal/shared/nav` (new) is shared by mobs, bots, and the reach check.
- **Cells** are 0.5 m apart, built from the terrain and the solids.
- **Each cell** keeps its ground height and its clearance: the distance to the nearest blocked cell. A cell is blocked under a solid taller than `step`.
- **Fit:** a body fits where the clearance is at least its radius.
- **A\*** runs 8-connected over cells that fit. A move between neighbours is refused if it goes uphill into a terrain cell steep along an axis it moves on: the sim's own rule, read from the same marks.
  - As built in the steep-exit PR, a move uphill out of such a cell is refused too. The sim judges each small step by where it ends, so a body at a steep cell's centre can't climb out of it that way, while A\* judged only the cell moved into. Where steps meet a cliff, the cliff's face slopes gently along it: mobs walked along the face, A\* sent them straight up off it, and the sim stopped them, so they rocked there for good. The cave's shelf showed it, a bat stuck half way up its south face.
- **Paths** are short, inside a leash, so the search is bounded.

As built in spike 3:
- A cell's ground is the terrain at its centre, or the top of a solid over it. Its clearance is the exact distance from its centre to the nearest wall: a solid more than `step` above that cell's ground, or the map's edge.
- A diagonal move needs both of its two side routes walkable, as the sim moves x then z, so a body never cuts a wall's corner.
- A search keeps state only for the cells in its leash's square, about 4,200 cells at 16 m, however big the map is. A* is the repo's own (section 17).
- A path from inside the destination's cell is that cell alone, so `nil` always means there's no way.
- Distances are the same bits on every CPU (`nav.Distance`, each product rounded), so the loader's spot checks give the same verdict on the amd64 CI and an arm64 server.
- The reach check's flood, which also jumps, lives in nav's tests (section 4). A* and the mobs only walk.
- **For the world step (slice PR 5):** the data doesn't change once loaded, so each map's grid is built once per process and shared, or when its instance first starts.
  - `Map.Grid` already builds on first use.
  - Clearance is checked at cell centres only. Beside a tree there's a band a few centimetres wide where nav allows a step the sim blocks, so mob AI must handle a blocked step, by sliding or a new path.
  - A box standing on steep terrain skips the steep rule in `walk`, which returns early for a solid's top. Fixed in slice PR 5, below.
  - `Path` with a leash that isn't finite isn't handled yet. Fixed in slice PR 5, below.
  - What must not happen is each of dozens of end-to-end stacks rebuilding every map's grid at start: under the race detector and coverage, that multiplied the stacks' start time.

As built in slice PR 5:
- **One grid per process:** `mapfile` shares a map's grid among every load of the same geometry and params, keyed by their hash, and builds it at the first look.
- **A blocked step:**
  - A mob walk that makes under a quarter of its step stalls: the mob drops its path and routes round the cell it couldn't enter for 2 s.
  - A body pressed against a solid can stand in a cell too tight for it at its centre. A path from there starts at the nearest cell within 1.5 m that the body fits, walked to first.
  - A chase that gets nowhere for 3 s gives up and walks home, and a walk home caught for 3 s stops and wanders.
- **Steep ground under a solid's top:** `walk` reads the terrain under the top as the sim does, so a top on a slope rising past the feet is no way on.
- **A leash that isn't finite** has no cells inside it: `Path` returns nil.
- **Cost:** one `nav.Finder` per arena keeps its buffers between searches, so a search allocates only its path. An arena runs at most 4 searches a tick, and a mob searches at most every 0.1 s. The turns start one mob further on each tick, so no mob waits behind the others for its searches. Thirty mobs chasing a player who runs circles round a rock take about 55 µs a tick with 3 allocations (`BenchmarkThirtyMobsChaseARunningPlayer`, Apple M3).
- **An empty map rests:** with nobody on it, respawns, spits in flight, acts under way, and walks home go on, but no mob looks for anyone or wanders. A windup under way plays out on nobody rather than landing on whoever comes next.

### Behaviour

- **Wander:** pick a point in the spawn circle that fits, walk there at the patrol pace, pause for `patrol.pause`, and repeat. `speed_jitter` stays.
- **Sight:** a player within `sight` (9.4 m) in x and z, with feet from 1 m below the mob's to 3 m above. The 3 m is today's 96 px. The 1 m below is new: today's lower bound is the mob's level, but terrain now dips. Solids don't hide players, as today. The target rules stay as they are: the provoker first, then the kept target, then the nearest seen.
- **Chase:** follow an A\* path to the target, repathing when the target has moved over 1 m or every 0.5 s.
  - **Leash** (`leash: 16m` in `mobs.yaml`): a mob whose target is farther than this from its spawn circle's edge drops them and walks home. This replaces today's chase bounds.
  - Mobs stop at players by the circle rule of section 5.
  - **Mobs chasing one player spread around them** (decided 2026-11-15). Spike 3 measured the need: six slimes chasing a player on open ground ended on one spot, with their paths sharing their last cells.
    - Each chasing mob aims at its own spot on the player's contact ring, at the player's radius plus its own.
    - The mobs chasing the same player are ordered by their approach angle about the player, with ties broken by mob id.
    - Each gets the slot nearest its angle that is at least its own width along the ring from its neighbours'.
    - **A full ring overflows outward:** a mob that finds no free slot takes one on the next ring, a mob width further out, by the same rule, and so on. A slime's first ring, about 5.5 m round, holds about 5. A mob on an outer ring moves in when a nearer slot frees at a repath, so the crowd reads as a crowd and stays deterministic.
    - A\* goes to that spot, and the spots are worked out again at each repath.
    - A mob whose attack already reaches stops there, as today, so ranged mobs keep their distance.
    - Only idle mobs that still overlap get a light push apart, server-only and in id order, so the result is deterministic.
    - The client draws positions as sent and needs nothing new.
- **Attacks:** each starts when the target is within the attack's reach.
  - At the wind-up's start, the mob turns to the target and fixes the attack's area.
  - `MobAttacked` carries the yaw and, for a hop or a charge, its end point.
  - A player who leaves the area before the landing isn't hit.

| Kind | Name | Area, fixed at the wind-up |
| --- | --- | --- |
| `front` | bite | An arc: the mob's radius + `reach`, 90° |
| `leap` | hop | The mob moves toward the target's spot then, up to `distance`, stopped by solids and the leash. Its landing circle is the mob's radius + `spread` |
| `rush` | charge | A lane as wide as the mob, `distance` along the yaw, stopped by solids. It hits the first player its body meets |
| `shot` | spit | A ball of `size` flying `speed` along the yaw, up to `range`. It hits the first player it meets and stops at solids |
| `around` | slam | A circle of the mob's radius + `spread` |

- `react.push`, `sight`, and every px length in `mobs.yaml` convert at 32 px a metre.
- The lag-compensated `BoxesAt` becomes `BodiesAt`, with the same 8-tick history.

As built in slice PR 5:
- **`BlockersAt`,** not `BodiesAt`: the mob circles a frame drew, kept for 8 ticks.
- **A melee mob closes in:** it walks on to its spot even when its attack already reaches, attacking as its cooldowns allow, and attacks only from a place no mob standing still overlaps. **A ranged mob holds** where its shot reaches.
  - As built in the line-of-fire PR, a shot reaches a player only if its ball, flown from where the mob stands in the steps it flies, meets them before a solid or a rise stops it. Otherwise the mob keeps chasing, round the solid or up the rise. Before, a bat in range stood spitting into a pillar or a cliff between them, and a player behind it was safe. A charge and a hop still go when a solid is in the way and stop at it, but neither mob holds: it chases again while the attack cools.
- **A mob waiting** at its spot, its target out of its reach, counts as stuck, as one stalled or with no way does: it attacks whoever it can reach.
- **Spots are claims,** not worked out again from the crowd's angles at each repath, which kept the crowd shuffling:
  - A chasing mob claims a slot about its target: a ring and an angle, which move with the player. It keeps the claim while the slot stays clear of other claims and of mobs standing still, and moves to a nearer ring when one has room, at a repath.
  - The rings are the contact ring, then a ring at the edge of the mob's reach when that falls short of the next one, then a mob width further out each. Without the reach's ring, a mob waiting a ring out could never step into room in reach that wasn't on the contact ring.
  - A mob looks for a slot on the half of each ring facing it, nearest its angle first, before the next ring out, so it doesn't walk round the player and through the crowd. It tries steps of half a width from its own angle and the angles just clear of each mob held on that ring, so it finds a gap exactly one mob wide.
  - A slot in a portal slides out along its ray to the portal's edge, so a player standing in a portal can still be reached.
  - With six boars, or ten slimes that bite, on one player, the crowd ends still: those whose attack reaches at the player's side, the rest waiting on an outer ring, no two overlapping. When one at the player's side dies, a waiting one moves in.
- **A hop lands touching its target,** where their bodies meet, not on the target's spot.
- **Portals:** a mob never steps into one, walking or rushing, and a hop stops at its edge, so a player can always leave by one.

As built after slice PR 7's playtest:
- **A mob never ends a tick inside a player.** A player steps against the mobs their view drew, up to 8 ticks back, so a mob closing in since, or a hop landing as they move, could end up inside them: the first 3D playtest saw a boar's centre 0.81 m from the player's, against the 1.25 m their circles allow. At the end of each mob step, a mob inside a living player moves out to touch their side, stopped by solids and portals as a walk is. The mob gives way, not the player, since the client predicts only the player. A crowd of every attack kind round a walking player tests it.

As built in slice PR 8, after the playtest of PR 7's fixes:
- **A mob steps out beside the player, clear of the others.**
  - Straight out left a mob on a neighbour already touching the player: two slimes there came to 0.22 m apart.
  - It now takes the nearest point touching the player that no other mob or player stands on, trying 5° further round each way. It takes that point only within a mob's width of where it stands; else it goes straight out.
  - Mobs still inside the player don't count against a point, since they step out after.
  - In the crowd test, 53 of 229 push-outs ended 0.1 m or more into another mob before; 7 of 75 do now.
- **A mob deep in another starts no attack, walking or not.** Two mobs on one spot took turns attacking from it, each starting while the other had a path for a tick or two. Now a mob that another overlaps by more than 0.25 m starts no attack and walks to its slot first. Any overlap with a mob standing still still stops it, as before.
- **An idle mob makes way for a busy one.** Only two idle mobs drifted apart, so a mob recovering from a hop on a waiting one, or walking into it, left the waiting one standing in it. Now an idle mob gives way to a busy one by the whole overlap, at the same 1 m/s, and two idle ones still each give half.
  - In the hop case, the waiting slime starts moving off as the hopper recovers. Before, it stood 0.67 m deep until the hopper walked off. Both part about 1.4 s after the landing either way, since the hopper walking to its own slot is what parts them.
- **A charge stops at mobs.** After the playtest, the user's call was that mobs are solid to each other while charging.
  - A charge's end is fixed at its windup, against the mobs where they stand then. It's cut where the charger's body would first touch another mob in its lane, as it's cut at a wall. The windup's end, which the telegraph's lane runs to, shows the cut.
  - A mob that steps into the lane after the windup stops it too, touching, as a player does, though the telegraph showed the whole lane.
  - A lane that would go no more than 0.25 m into a mob only grazes it and passes, the overlap a walking mob may have before it's deep. Side by side at a player, chargers have each other's edges in their lanes: when edges stopped them, a ring of mushrooms touching a still player hit once in 62 s, against 21 in 45 s before.
  - A player the charge meets before it would go 0.25 m deeper into any mob than it already is counts as met first, at the windup and on the way. A neighbour walking past and already overlapping a charger's side cut its lane to nothing, so a player a few cm ahead went unhit: 5 of 236 charges in finga-player's re-check of a1c2ad2d, all while the player moved.
  - A charge cut to nothing still meets a player touching it, within 1e-9 m for the sim's float error.
  - Only charges change. Hops and leaps keep the two rules above, and walking mobs still pass through each other while the making way above parts them.
  - This changes mobs-fight-back.md's "mobs pass through mobs" for charges. The switch PR updates that page.
- The crowd test is a walking player among 8 mobs, six runs of 400 ticks. The last column counts tick-pairs of mobs beside the player more than 0.25 m into each other.

| | Attacks begun | Begun over 0.25 m into another mob | Deep pairs beside the player |
| --- | --- | --- | --- |
| Before | 343 | 93 | 669 |
| No attack while deep | 330 | 2 | 817 |
| And idle mobs make way | 340 | 4 | 332 |
| And charges stop at mobs | 359 | 4 | 88 |
| And a player just ahead is met first | 360 | 4 | 95 |

## 8. Other rules that use positions

- **Portals:**
  - A player stands in a portal when their feet centre is inside its circle and within 1 m of its ground.
  - `EnterPortal` is unchanged.
  - The client sends it on a fresh press of the talk key in a portal, in place of up.
  - Arrival is the target portal's centre, facing away from it.
- **Talk:** reach (60 px, now 1.9 m) is a 3D distance from the NPC to the player's middle.
- **Drops** scatter on the ground around the killer: within 0.75 m, each further drop 0.6 m round a ring. Collection, its delay, and its fly-to-owner look are unchanged.

As built in slice PR 5:
- **Talk** reach is 1.875 m, from the NPC's point on the ground under it to the player's middle.
- **Drops** fall around where the mob died, not the killer: on a ring 0.75 m out, each 0.6 m along it from the last, on the ground there. A spot off the map or in a solid is skipped, and with none clear a drop falls where the mob died.
- **The field boss** spawns at its circle's centre and wanders the circle. Its other rules are unchanged.
- **Falling out** (`fellOut`): below the map's lowest ground by 10 m, or NaN. Rescue is unchanged.

## 9. Protocol and persistence

`client.proto` changes are additive, with the removed fields reserved, so `make breaking` passes. `PROTOCOL_VERSION` goes to 2.

| Message | Change |
| --- | --- |
| `Input` | Unchanged |
| `Attack`, `UseSkill` | `aim` (float, yaw) |
| `PlayerView` | `z`, `vz`, `hx`, `hz`, `aim` (the current action's yaw). `facing` and `climbing` are reserved |
| `MobView` | `z`, `yaw`; `facing` is reserved |
| `DropView`, `Died` | `z` |
| `Respawned`, `MapChange` | `z`, `yaw` |
| `SkillUsed`, `AttackUsed` | `aim` replaces `facing` |
| `MobAttacked` | `yaw`, and `to` as x, y, z, replacing `facing` and `to_x` |

- **The gateway** maps the new fields (`push/wire.go`, `receive/messages.go`, `local/world.go`, `local/deliveries.go`).
- **The world module's contract** (`events.go`, `contract.go`) carries them.
- **Persistence:** a migration adds `pos_z` and `yaw` to `characters`, and the checkpoint saves them. Before go-live, a fresh database is fine. `cmd/testers` seeds the start town's spawn with z.

As built in slice PR 6, every field above is added, and nothing is removed yet:

| Message | Fields added |
| --- | --- |
| `Attack` | `float aim = 1` |
| `UseSkill` | `float aim = 2` |
| `PlayerView` | `double z = 17`, `vz = 18`, `hx = 19`, `hz = 20`; `float aim = 21` |
| `MobView` | `double z = 9`; `float yaw = 10` |
| `DropView` | `double z = 7` |
| `Died` | `double z = 4` |
| `Respawned` | `double z = 6`; `float yaw = 7` |
| `MapChange` | `double z = 4`; `float yaw = 5` |
| `SkillUsed` | `float aim = 5` |
| `AttackUsed` | `float aim = 4` |
| `MobAttacked` | `float yaw = 5`; `double to_y = 6`, `to_z = 7`, which with `to_x` make `to` |

- **Still on the wire:** `facing` and `climbing` on `PlayerView`, `facing` on `MobView`, `SkillUsed`, `AttackUsed`, and `MobAttacked`, and `to_x`. The 2D client draws from them. Slice PR 8 removes those fields and reserves their numbers.
  - As built, slice PRs 4 and 5 removed them, with the 2D client, and reserved their numbers. `to_x` stays: it is the x of `to`.
- **Until slice PR 5,** the world fills the new fields from the 2D state:
  - z is 0, and the heading is the facing: `hx` ±1, `hz` 0.
  - A yaw is the facing's: east π/2, west −π/2. That covers mobs, respawns, map changes, and the checkpoint.
  - A saved yaw turns into a facing on join: west when its sine is negative, else east.
  - `aim` is the yaw of the player's latest attack or skill. It is told on `AttackUsed`, `SkillUsed`, and `PlayerView`, and nothing aims by it yet.
- **The gateway** refuses an aim that is NaN or infinite with `MALFORMED_BODY`.
- **The 2D client** sends its facing's yaw as the aim.
- **Persistence:** migration 00005 adds `pos_z` and `yaw`, both `real`, defaulting to 0. The admin API's progress takes `z` and `yaw`. `cmd/testers` writes 0 for both until the spawns have their own, in slice PR 4. As built in slice PR 4, it seeds the start town's spawn and its yaw.

## 10. Client

### Babylon.js

- **Packages:** `@babylonjs/core` and `@babylonjs/loaders`, pinned to an exact version (9.36.0). Imports are per file, not barrels, so the bundle shakes.
  - Spike 1 measured what entering the 3D world downloads: about 530 kB of JavaScript gzipped (2.0 MB raw), loaded only then, beside Phaser's 357 kB, which slice PRs 4 and 5 removed.
  - About 71 kB of that is the glTF loader's PBR material and its BRDF table, which the toon material replaces. Making the toon material in the loader would save it.
- **Loading:** every glTF loads with `alwaysComputeSkeletonRootNode`. Quaternius's skins name a joint that isn't their joints' common root, and without it Babylon skins those models lying down at 100 times their size.
- **Engine:** WebGL2. WebGPU is a later option.
- **Mounting:** the renderer mounts in `#game` behind today's `GameHandle` (`attach`, `enter`). React and its stores don't change.
- **What goes:** Phaser, `game/scenes`, `game/entities`'s sprite views, `effects/draw.ts` (as built, rewritten for Babylon instead), `depth.ts`, `keys.ts`, `grey-art.ts`, `placeholders.ts`, the 2D assets, `testing/fake-phaser.ts`, and `recording-scene.ts`.
- **What stays** is the logic in `game/systems`:
  - `ActionClock`, `skill-keys`, `potion-keys`, `hit-stop`, `arrival`, `map-transition`, and `feed-watch`;
  - `prediction`, `interpolation`, `combat-view`'s tracking, `drops`, and `portals`, each with x, y, and z;
  - `entities/beats.ts`, now `world3d/beats.ts`.
- **Tests:** scene logic runs on Babylon's `NullEngine` under vitest.

As built, slice PRs 4 and 5 removed Phaser, the 2D client and its sprites, since the rework may break the 2D game and the 3D renderer stood beside it. `#game` mounts the 3D renderer alone. `prediction` went with it and comes back in slice PR 7; `interpolation` blends the 3D state, and the client's talk reach went, since it needs the terrain.

### The scene

- **The ground** is a mesh from the height grid, in a toon material keyed by `look.ground`.
- **Solids and props** load their kit models by key. Every instance of a model shares one copy.
- **Sky, fog, and light** follow `look.sky`. A sun casts soft shadows from characters and mobs only.
- **The camera** is a fixed-angle follow camera: 50° down, 16 m back, looking north, easing toward the player at 0.15 a frame as today. A map's edge holds it.
- **Fading:** a solid or prop between the camera and the player's head fades to 30%.

As built in slice PR 7:
- **The camera** eases over time, not per frame: a 103 ms lag is 0.15 of the gap in a 60 fps frame, and as much in the same time at any frame rate.
- **Kit models** come from a registry, `world3d/kit-models.ts`, giving each model key its file, scale and lift. A key with no file draws a stand-in of its collision shape. Each kit model is loaded once per key, baked into model space, and every solid and prop drawn with it is a thin instance of it.
- **Fading:**
  - A faded solid swaps to its 30% material, since a node material ignores visibility. Each 30% material is compiled while the map loads, so a first fade doesn't stall.
  - A kit solid fades by hiding its instance and showing a faded copy of itself.
  - Props are low scatters that never stand between the camera and the head, so they don't fade.
- **The map builds under a veil.**
  - A map that doesn't load tries again after a backoff.
  - If a MapChange or Welcome names another map while one loads, that one builds instead.
- **The `?fps` readout** sits bottom right, clear of the HUD and chat. It shows the ms of work a frame, the frame rate, and the prediction's resets.

### Characters and mobs

- **Models** are glTF from the packs (section 11), keyed in `classes.yaml` and `mobs.yaml` as `look: {model, scale, tint}`.
- **Animation:**
  - Each action plays its clip from `look.clips` (class) or `look.clip` (skill), posed each frame by its beats: the clip's strike lands on the action's hit and its end on the ready point. The strike is the frame where the weapon hand moves fastest, measured once per clip. An action whose hit lands as it starts, as every attack and Strike do today, starts at its clip's strike.
  - A cut fades the cut clip out over 80 ms, held at its pose, while the new one fades in, so a cancel reads as instant.
  - Walk, run, jump, fall, hit, flinch, down, and death come from the same clip sets.
  - Idle and walk blend by speed.
  - **Moving through an action:** movement stays free during attacks and skills, so a body that moves plays the action's clip on its upper body only, through an `AnimationGroupMask` from the spine up. Its legs play walk, run, or jump by its motion. A body standing still plays the clip on the whole body. The mask's switch at the start or end of a move blends over the same 80 ms.
- **The beats** (`beats.ts`) still time strike, follow-through, and ready. The clip carries them, and the lean and lunge of today's pose go.
- **Hit-stop** freezes the attacker's and the target's clips for its 50 ms by holding their poses.
- **As built in slice PR 7:**
  - **Tints:** a class's tint colours its cape alone. A mob's tint replaces its model's main material.
  - **Mobs without a model** draw a stand-in: primitives of their family's shape (flyer, shell, beast, or brute), filling their circle and height, in their tint. A stand-in lunges on its beats and tips over when down or dead.
  - **NPCs:** a trainer looks like its class, and the guide is the Barbarian with hat and cape.
  - **Heights:** a mob's height in the data already holds its look's scale, so the model stands at it. As built after the first 3D playtest, a mob's model is never drawn wider than its circle: it's scaled to its height, or smaller where that would draw it wider. The Green Blob stood 1.31 m across its 1.0 m circle, so slimes settled touching looked overlapped.
  - **Stand-ins fit too,** as built in slice PR 8: each family's parts reach no farther than 0.98 of the radius, as a model's. The boar drew 1.88 m across its 1.75 m circle and the bat 1.87 m across 1.25 m. Brutes, which filled about 55% of their circle, are broadened to 85%.
  - **Strikes,** each measured as the frame where the weapon hand moves fastest:

    | Clip | Strike |
    | --- | --- |
    | slice | 0.38 |
    | chop | 0.54 |
    | stab | 0.24 |
    | crossbow shot | 0.14 |
    | spell shot | 0.09 |
    | raise | 0.13 |

  - **A same-map respawn** places the player from the newest frame of them that says alive, since the world sends a tick's frame before its events.
- **The afterimage** is a frozen copy of the cut pose:
  - white with a dark rim;
  - fading from 0.8 over 300 ms;
  - drifting back 0.44 m, today's 14 px.

### Effects

- **Swings:** a `TrailMesh` from the weapon bone through the strike beat.
- **Sparks:** GPU particles at the hit point. They are white for physical hits, violet for magic, bigger for a crit, and a grey puff for a MISS (C13).
- **Skill looks:** keep their ids and the manifest scheme. Each is rebuilt from meshes, particles, and `GlowLayer`.
- **Telegraphs:** red ground decals in each attack's shape (section 7), filling over the wind-up.
- **Damage numbers, name tags, and NPC markers:** DOM elements over the canvas, placed by projecting their world points. Today's rules for popping, stacking, and keeping clear carry over in screen space. As built, slice PR 7 draws names, quest markers, and portal labels this way; damage numbers come with slice PR 8.

As built in slice PR 8:
- **No `GlowLayer`:** it would cost a full-screen pass every frame. The looks glow by blending instead. Each is made of meshes in shared unlit materials, added to what's behind, drawn over it, or laid on the ground, and faded by their visibility.
- **Swings:** the weapon's `TrailMesh` from slice PR 7, and a band swept across the shape's arc at its radius. A basic attack's band is pale cream for physical and pale violet for magic, over a dark edge so it shows on pale ground. A skill's is in its colour.
- **Sparks** are 2D's shapes in metres, not particles, which at this camera were a few pixels and never showed. Each is one flat mesh facing the camera, set in front of the target toward it, over a dark edge.
  - A spark's size follows the target's: half its larger side, from 0.3 to 1.1 m, rounded to 5 cm.
  - Each kind's shapes are made the first time a map shows that kind at that size. Every later spark like it shares them.
  - At most 32 show at once. Past that, the oldest goes.
- **Shapes are shared.** The effects keep each shape they make, hidden, till the map ends: sparks, balls, rings, stones, and an arrow's shaft and head. Each play gets its own mesh over the shared geometry. The sizes come from fixed specs, so a map keeps a few dozen at most.
  - A physical hit is 8 rays and a magic one a ring and dots. A crit adds 12 gold rays and a core, and a MISS is a grey puff rising.
- **Particles** are CPU `ParticleSystem`s, for trails, dust, flames and sparkles: too few to need the GPU's, and `NullEngine` runs them under test. Each system keeps the one soft dot they share when it's disposed, since Babylon's dispose would take it too.
- **Skill looks** are drawn in `effects/looks.ts`, each sized to its real shape and run on its beats. Each keeps its id. A manifest entry replaces one with a sprite sheet, drawn as a billboard at 128 px a metre, and a sound. The manifest is empty today.
  - The tiers draw differently: Strike's third cracks the ground, Volley's throws three stones, and Arc's third leaps between its foes.
- **Shots** fly at 0.04375 m/ms from the caster's chest along the aim. A shot's hits show as it reaches its target.
- **Hit-stop** holds the player's pose and the mob's for 50 ms, and at most once in 100 ms, so a skill hitting three foes holds once.
- **Telegraphs** aren't decals, which would land on every mesh, players too. Each is a grid laid on the ground under the attack's area, 4 cm over it at each corner, in a material that writes no depth.
  - The area tints the ground under it red, multiplying it by its colour, and what has filled of it is laid over that at 60%. That reads red on grass, the cave's stone and sand. As first built, the area at 22% and the fill at 45% over the ground read orange-brown on grass.
  - **The fill runs on the player's own clock:** the server tick the drawn player stands at, from their latest input, its ack and how far through the drawn step is. It is full as the world strikes the player, so a step out any time before then is in time. On the render clock, 2 ticks or more behind the newest frame, a bite struck at 84% of the fill in the first fight playtest.
    - A windup shows once that clock reaches its tick, its area where the mob stood then, from the frames kept for blocking. Its hit and a hop's or a charge's end are in whole ticks, rounded up as the world counts them.
    - The cost: a windup shows about a round trip plus the input lead late on screen, so it starts part filled. At a high round trip a short one pops in well filled or, past its length, not at all.
    - The mob's clip plays on the same clock. Before an ack, and once the render clock passes a player held still by death or a stall, the render clock stands in.
    - **A hop or a charge carries its mob on that clock** from its windup's place to its end, fixed at the windup, at its travel's pace, so it lands as the world lands it. It blends in over 150 ms of render time from where the mob is drawn, so a jump in the player's clock can't jolt it, and back once the drawn frames reach the act's end. A charge stops where its run meets the player, as the world's does. It looks only along the way it went since the last frame, so a player it ran past, or who steps into the lane after the run, can't pull it back.
      - As built in the charge-snap PR, a charge's run also stops at a drawn mob that stepped into its lane after the windup, as the world's `mobAhead` stops it each tick: touching it, passing one it only grazes by less than 0.25 m, and meeting the player first if they come before it would sink 0.25 m deeper into the mob. The windup's end, which `MobAttacked` carries, is cut only against the mobs standing then. A last playtest saw 6 of 121 forest mushroom charges in a crowd drawn running 0.5 to 4 m through such a mob while the world stopped them short, 5 snapping back about 0.5 m in a frame.
      - As built in the charge-windup PR, a charge looks for the player only once its run has begun, and only a player whose body overlaps the mob's, from its feet to its top, as the world's `first` does. Before, a player who stood touching the start on any windup frame, or jumped over it, fixed the stop there for good: about 1 in 75 charges in the charge-snap playtest, both before and after that PR.
      - As built in the charge-sweep PR, the mobs a charge sweeps are where the world has them on the player's clock, not as drawn: the drawn mobs lag it by the interpolation's delay, so a mob stepping out of the lane at the hit still cut the run, which froze through it and lurched 3–4 m at its end (2 of 333 charges in the windup PR's playtest), and one stepping in was missed. A mob mid-hop or mid-charge itself is swept where its own act carries it that frame, cut at the stop it had by the frame before, so two charges never wait on each other; any other at its newest frame up to the player's clock, moved on by its velocity between its last two frames for at most 200 ms.
        - Its playtest, 536 forest mushroom charges, saw no mob leaving the lane drawn wrong, in 26 cases. Its first build swept a charging mob at its place the frame before, 0.16 m back at a charge's 10 m/s, which cut a charge following another 1.26 m behind, wider than their 1.125 m touch.
        - Accepted: the start-line case, a mob stepping in from just behind a charge's start at the graze line, which the world cuts at 0 m and the client passes, on the moved-on frames (2 charges). It then stops at the player and snaps back, or eases back.
        - Accepted: a cascade, a charge drawn wrong cutting a charge whose lane it crosses, which the world's runs on past (1 charge). Sweeping a charging mob at its frames instead would bring back the lag at a charge's start.
    - **A spit flies on that clock** from the mob's place at mid-height, so its ball reaches the player as the world's lands on them. It stops on the player there, and its Damage comes after, with the round trip.
  - **The result waits for the world:** past the hit, the area stays full till the world says it acted. That is the mob's Damage, or the drawn frames reaching the act's end, whichever comes first. Then the impact shows.
    - A flinch or knockdown the frames tell of in the windup breaks the look off, red and all, though the player's clock is past its hit. So does a knockdown in a hop's or a charge's act, or the mob's death by its end, as the world drops its landing. A thrown ball is taken back with it.
    - The frames tell of those about a round trip after the player's clock passed them, so an attack broken late shows its full red for that long, and a carried mob goes back. Before, a late stagger looked like a hit: the red finished and the impact fired.
  - A bite's area runs from the mob's edge to its reach. The server's arc counts from its centre, but the body hides that part.
  - A player is hit when their body touches the red area, since the server grows each hit test by the player's radius. The area shows the mob's reach, not where a player's centre must be.
  - A spit's lane is as wide as its ball. It runs to where the ball's front stops: its range plus half its size, or sooner at the first solid or rise, worked out as the windup starts.
  - The area is laid on the ground once. What has filled of it is worked out from the area's corners each frame, with no ground read, so it lies on the area.
  - The impact follows the result: a bite's flash, a ring where a hop lands or a slam strikes. Dust follows a charge.
  - A spit's ball flies from the mob's mid-height at its speed. It stops at the first solid or rise, on the player, at its Damage, or where the mob died.
  - A Damage doesn't say which attack it came from, so any Damage from the mob ends its ball. A vampire bat's slam landing while its spit flies ends the ball early. Five bosses have both.
- **The afterimage** is a still copy of the cut pose, posed by node name. It's made ahead for each figure, so the first cancel doesn't hitch.
- **Damage numbers** stack within 24 px across and 2 lines of 16 px of any number started in the last 300 ms, as today.
- **A killed mob** lying its second still shows a late hit's spark and number, such as a shot landing after the drawn death.
- **A mob hit before it's drawn** gets its spark and number where the frames put it at the hit's tick. A mob killed on the tick it spawned is never drawn, so the world sends its view with its removal in that tick's frame.
- **Heal's sparkles** come off a cylinder 0.15 m wider than the healed body, from feet to head, and rise over it. From a ball at the feet, most rose inside the body.
- **Names:** a player's name lifts clear of the portal labels, NPC names and markers, and the names before it. The player's own comes first, then others' in order of arrival. A name's size is measured once each time its text changes, not every frame.
- **Mob HP bars:** 32 by 4 px over each mob's head, as today. An elite's has a 2 px gold frame. A boss has none, since the bar at the top shows its HP, and a killed mob's goes.
- **A model still loading as its map goes** is dropped quietly. Before, its failed load was an unhandled promise rejection.

### Input

- The presets keep their keys. Up and down walk north and south, and talk also enters portals.
- The auto-aim and outline of section 6 live in a new `systems/aim.ts`, tested on its own.

## 11. Art

All CC0. Each pack is recorded in `web/public/models/LICENSES.md` with its source.

- **Model files:** as built in the model-size PR, `npm run models` (`web/scripts/models.ts`) makes the served files in `web/public/models` from the packs' files in `web/models-src`. It is a script run by hand, never part of the build.
  - A look's file keeps only the clips its looks play, read from `world3d/model-looks.ts`, and the gear one of them shows. A look hides the rest in its `hide` list.
  - Then it resamples the clips, prunes and dedups, and quantizes the meshes with KHR_mesh_quantization, on one grid for the whole scene, so a character's one skin stays whole. Babylon reads it with no decoder.
  - A look that plays a new clip needs the script run again; the script fails on a clip or gear its source lacks.

| Use | Pack |
| --- | --- |
| Classes | KayKit Adventurers Character Pack 1.0, for every class: Beginner: the Barbarian without hat, cape, or shield, its one-handed axe standing in for the club; Warrior: the Knight with one sword and no shield; Thief: Rogue; Bowman: Rogue_Hooded with the crossbow; Magician: Mage |
| Their animations | Built into each 1.0 character, the same 76 clips on the same rig, so nothing is retargeted: one-handed and two-handed melee, dual wield, one-handed and two-handed ranged (the crossbow), spellcasting, block, dodge, hit, death, and movement |
| Monsters | Quaternius Ultimate Monsters and its other monster packs, and KayKit Skeletons |
| Places | KayKit Forest and Dungeon, Quaternius Stylized Nature, Ultimate Nature and Medieval Village (the MegaKit and the older Medieval Village Pack), and Kenney's castle and town kits |

- **The look:**
  - Every model draws with one toon `NodeMaterial`: the sun's light posterized into shade, mid, and lit bands, with flat colour.
  - Characters, mobs, and props get an inverted-hull outline: a clone of each mesh sharing its geometry and skeleton, its back faces pushed out 3 cm along their normals after skinning, so every model's line is the same width. Babylon's own outline renderer pushes in each mesh's own units before skinning, so its line's width changed from model to model.
  - Quaternius's flat colours were authored under bright light: drawn at 3.6 times their glTF values, their eye white reads white, as the pack's renders show.
  - As built in the cave's PR, ground the sim marks too steep to walk draws at 60% of its map's ground colour. A rise facing the sun, and so the camera, took the floor's band and vanished: the forest's mesa showed only the shadow of its north edge.
- **Mixamo isn't used.** Its terms forbid handing out the raw files, which a web game sends to every browser.
- **Mob models:**
  - Spike 1 mapped the forest's three mobs, all Quaternius Ultimate Monsters from Poly Pizza:
    - green slime: Green Blob;
    - orange mushroom: Mushnub, its cap orange;
    - king slime: Green Blob three times over, a darker green.
  - Each later map does its own.
  - A mob with no fitting model is renamed or reskinned to one that fits (the user agreed).
  - As built in slice PR 7, every slime is the Green Blob in its own tint. So are the two slime bosses that hop with a slime's cylinder three times over, the Spore Mother and the Crystal Colossus. The other mobs draw stand-ins until their maps' PRs map them.
  - As built from the shroom grove's PR, the plan changed: the map PRs stay map-only and name their stand-ins, and the mobs get their models in one later pass across the designed maps, in its own PR. Sourcing a model can need a download the user fetches by hand, and it shouldn't hold up the maps. Picking every map's models at once keeps them consistent. The cave's boars and bats and the grove's mossback snail draw stand-ins until then.
- **Place models:** as built after slice PR 7, the town's and the forest's solids and props draw kit models.
  - KayKit Forest gives the trees, rocks and grass; Ultimate Nature the stump and flowers; the older Medieval Village Pack the houses, the Inn, the market stand, the well and the crates.
  - The village pack is drawn at one scale, ×4, which makes its doors 2.08 m beside the 1.75 m player. At that size the Inn moved to [39, 24], and the Warrior Trainer to [36, 14] in front of it.
  - Each solid's collision shape was refitted to its model, and round ones became cylinders.
  - Quaternius's place models draw at their glTF colours: the monsters' 3.6 lift bleached their stone.
- **The cave's rocks,** as built in its PR, are KayKit Forest's too: Rock_1_N the pillars, Rock_1_M the big rocks, Rock_1_E the small ones and Rock_2_B the pebbles, at 0.35. Rock_3_Q, the crags on the cliff tops, is a prop only: 1.6 m of it sinks below its base.
- **The grove's mushrooms,** as built in its PR, are Stylized Nature's Mushroom_Common, a cluster of five, at ×8, ×5.5, and as props at ×1.2 and ×0.7. Its texture is shrunk to 256 px and kept as JPEG, 38 KB for the model, and the region its caps' tops sample is tinted the spore slimes' purple, 0xb07ad0. Drawn as it comes, the pale caps read as rocks from the game's camera. KayKit Forest's Bush_2_C gives the bushes on the banks.
- **The hollow's dead trees,** as built in its PR, are KayKit Forest's Tree_Bare_2_B at ×1.4 and Tree_Bare_1_B at ×1.2, props on the bank tops alone, where nobody walks; their branches spread from a metre up, so as solids they would want circles wider than their trunks.

## 12. Bots, testers, and the balance harness

- **Bots** (`cmd/bot`) keep their planning: quests, promotion, skills, spending, and the portal graph.
  - As built, slice PRs 4 and 5 deleted `cmd/bot` and `make bots`, since all its walking was 2D. Slice PR 9 brings its planning back from the history.
  - Their walking follows `nav` paths, turning each waypoint into keys.
  - `climb.go` goes.
  - A hunting spot becomes a circle.
  - As built in slice PR 9:
    - A bot presses the key, or the two of a diagonal, nearest the way to its next waypoint. Paths keep 0.25 m more room than the body needs where they can, since eight keys stray off a path's line.
    - The sim stops a body at contact one axis at a time, with no slide. A bot pressing one way on the ground that moves under 5 cm in 4 decisions has stalled; a new way starts the count, since its first steps take a round trip to show. Its first stall jumps, the next steps aside, and each later step aside goes to the other side.
    - Where `nav` finds no way, such as onto the forest's 1.2 m ledge that only a jump climbs, the bot walks straight at the target and looks again a second later. It leaves a mob it has no way to for 30 s, and rolls a roam point it has no way to again.
    - A quest's mob is hunted at its widest spawn circle. EXP is hunted in a circle round every spawn of the bot's prey on the map. With none of its prey there, the bot walks to points rolled in the circle.
    - A living mob at the bot's height within 0.5 m of its body, and within 60° of the way it presses, is fought until it dies, since the sim holds the bot against it.
    - A bot aims each attack and skill at its mob, from 70% of its basic attack's reach.
    - With a field boss up, a bot keeps hunting its map, clear of where the boss could see it: 3 m past its sight, round where the bot last saw it, or round its circle. It waits in the town while the boss is near a portal on its way, where it comes out or the NPC it goes to, or covers the whole spot it hunts: the level 5 to 10 spot's centre is in the forest boss's sight, but most of the spot isn't. The 2D bots kept off the whole map. A boss comes every 300 kills on its map and stays up to 10 minutes, so 4 bots spent about half of a 40-minute run waiting in the town and reached only level 8. Field bosses are never prey.
    - A town visit talks to each NPC, each next the nearest, from where players appear. A bot talks only standing on the ground within half the talk reach, since the reach counts to its middle in 3D.
    - A test walks the bots' maps by `sim.Step`, from where players appear and from each portal, to every portal, NPC and roam point.
- **Testers** seed a position with z.
- **The balance and rotation harness** doesn't change, since it reads no geometry. Mob attack areas change how often a player who dodges is hit, which the playtest judges.

## 13. Rollout and build order

### The branch

- **`3d` is cut from `main`.**
  - Build PRs target it.
  - CI already runs on every pull request, whatever its base.
  - Each build PR is squash-merged into it, one commit per PR.
- **`main` keeps the 2D game.** Fixes and geometry-free work still go to `main`. New 2D-only work stays held.
- **`main` is synced into `3d` often,** through a PR from `main` into `3d` merged as a true merge commit (`--merge`), never squashed. A squash would replay `main`'s changes on every sync and conflict, and a direct push would run no CI.
- **As soon as the slice is complete,** green, and through its live playtest, `3d` replaces `main` in one PR. The user said not to wait for their approval: "do the switch on the fly".
- **`deploy.yml`** fires when CI passes on `main`, so the switch would deploy. It's skipped today, since there's no AWS account yet.

### Spikes

Three PRs into `3d`, kept if they work:

1. **Client:** Babylon in `#game` on a test ground.
   - A KayKit character walks, runs, and jumps.
   - Its basic attack is cut by a skill clip at the cancel point, within 80 ms.
   - Walking and jumping through an attack and a skill: the clip plays on the upper body over the legs' walk.
   - It's drawn with the toon material, the outline, and a swing trail.
   - Measure the bundle size and the frame time.
   - Pick the Beginner's model and map the forest's mobs.
2. **Sim:** the 3D sim in Go and TypeScript, with the vectors of section 5, replayed on both. It adds a CI job on GitHub's free `ubuntu-24.04-arm` runner, since the repo is public, that replays the vectors in Go and TypeScript, so arm64 is checked on every PR beside amd64.
3. **Maps:** the YAML format, its loader and validation, `nav` and its A\*, the reach check, and the converter, on town and forest. As built, the 12 other maps are converted placeholders, since the portal graph and the bosses need every map.

### The slice

PRs into `3d`, each green on `make test`, `make lint`, `make cover`, and `make breaking`:

4. **Data:**
   - metres throughout `movement.yaml`, `classes.yaml`, `skills.yaml`, and `mobs.yaml`;
   - the new shape kinds;
   - `aim_cone`, `leash`, and the looks' model keys;
   - town and forest designed in 3D, the other 12 maps converted;
   - `.tmj`, `pkg/tiled`, and `copy-maps.mjs` removed.
5. **World:**
   - the sim wired into the input pipeline, knockback, and falling out;
   - the shapes and `aim`;
   - mob AI and attacks;
   - portals, talk, drops, and the boss;
   - `worldtest` and the world and e2e tests.
6. **Protocol and persistence:** the proto, the gateway, the migration, and testers.
7. **Client world:** the scene, camera and fading, characters and mobs with animation and beats, NPCs, portals, drops, prediction and interpolation, and input.
8. **Client combat:** auto-aim and the outline, telegraphs, sparks, hit-stop, trails, the afterimage, skill looks, damage numbers and tags. Phaser and the 2D assets are removed.
9. **Bots.**

As built, slice PRs 4 and 5 went in as one PR, with parts of 8 and 9: Phaser, the 2D client, the 2D wire fields, and the bot were removed, since the rework may break the 2D game. The `3d` branch has no playable client until slice PR 7 draws the world.

As built, slice PR 8 draws the fight, with nothing left of the 2D client to remove (section 10). It also carries the fixes from the playtest of PR 7's fixes: stand-ins fitted to their circle (section 10) and mobs stepping out clear of each other (section 7).
- **PR 7 wires or deletes** each piece of client logic the 2D client called and nothing calls now:
  - in `game/systems`: `arrival`, `drops`, `feed-watch`, `hit-stop`, `input-sender`, `interpolation`, `map-transition`, `potion-keys`, and `skill-keys`;
  - in `lib`: `RingBuffer`, `FixedStep`, and `tickTiming`;
  - in `progression`: `markerFor`, `npcsOn`, and `moveSpeedWithout`.

  As built, slice PR 7 wires every one, and `drops`' `departure` too.

### Then

Once the slice has replaced `main`:
- the other 12 maps are designed in 3D, one PR each;
- looks are polished.

### Docs at the switch

The PR that replaces `main` brings the 3D world into the rule pages:
- `world.md`, `screens-and-controls.md`, Systems → Combat (shapes, mob attacks, Pace in metres), and Systems → Classes and skills (shapes);
- B1–B3, B5, and B6;
- C1, C2, C11 (the pose), C12, and C13;
- the tech pages: mobs fight back, combat feel (the sim), combat effects, classes and skills, field bosses, and auto pickup (the scatter).

As built, a docs PR into `3d` did this before the switch, so the switch is the merge alone. The product pages state the 3D rules. The tech pages, being dated records, keep their 2D text and gain an "As built in 3D" note at the top; the NPC, equipment, stat-system and energy pages had their stray pixels and sprites fixed too.

## 14. Testing

- **Sim:** the vectors, on Go and TypeScript, on CI's amd64 and arm64 jobs.
- **Maps:**
  - each validation refusal;
  - the reach check on every map, at every move speed (nav's `TestEveryShippedMapCanBeCrossed`);
  - the mob reach check: a mob lands a blow wherever a player stands inside its leash (nav's `TestEveryMobReachesAPlayerAnywhereInItsLeash`);
  - the converter on all 12. As built, it ran once and was removed with the `.tmj` files; the loader's tests and the reach check cover the 12.
- **`nav`:**
  - clearance;
  - the steep marks: a move the sim refuses uphill into a steep cell, A\* and the reach check refuse too, on a ramp, a cliff, and a cliff whose edge straddles two cells; and a path off a cliff's face, where steps meet it, is one the sim walks at a mob's pace;
  - A\* around a box, a cylinder, and a mesa edge;
  - no path past the leash;
  - bounded search.
- **World:**
  - each shape at its edges: an arc's angle and radius, a shot's lane and first target, an area's radius;
  - `aim` used as sent;
  - knockback stopped by a solid;
  - each mob attack's area fixed at the wind-up, a player stepping out of it unharmed, and a charge and a spit stopped by solids;
  - wander, chase around a solid, the leash home, and sight's height band;
  - six mobs chasing one player ending on distinct ring spots with no pair overlapping, over several seeds, with and without solids in the way;
  - ten slimes overflowing to a second ring with no pair overlapping, and an outer one moving in when an inner one dies;
  - portals, talk, drops, the boss, and falling out.
- **Protocol:** the samples and contracts with the new fields; `make breaking`.
- **Client:**
  - `aim.ts`: the cone, the reach, ties, and none in front;
  - the camera's fading;
  - clip timing against the beats;
  - a cut within 80 ms;
  - hit-stop;
  - telegraph shapes from `MobAttacked`;
  - prediction replays and interpolation;
  - all on `NullEngine`.
- **End to end:**
  - a player walks diagonally, slides along a solid, and jumps onto a rock, with no prediction correction;
  - an aimed attack hits the mob it turned to;
  - a mob's hop misses a player who stepped out.
  - As built, these went elsewhere. The walk and the jump, with no correction, are `prediction.live.test.ts`, which runs against a live stack when `FINGA_LIVE_URL` is set, and the playtests. An aimed attack hitting its mob is part of `TestFightAndGrow`, which aims at the nearest slime. A hop missing a player who stepped out is a unit test in `mobs/attacks_test.go`.
- **Live:** a playtest of the slice for every class. The checks are the camera, aim, telegraphs, cancels, moving through a skill, the frame rate, and the download size.

## 15. When things fail

| Failure | What the player sees | Why it's safe |
| --- | --- | --- |
| A last-bit difference between sims | Nothing: the vectors fail first | No trigonometry in the sim; products wrapped; `sqrt` is correctly rounded |
| The client aims at a mob the server sees elsewhere | The swing may whiff | The server resolves the shape with the sent aim, against its own positions |
| A modified client sends any aim | It faces that way | Walking can face any way anyway; reach and timing are still the server's |
| A mob's path is cut off | It stops at the nearest reachable cell, then walks home after the leash | A\* is bounded; the leash always ends a chase |
| A slow device | A lower frame rate | Spike 1 measured it; the looks need no glow layer (section 10) |

## 16. Decisions

Agreed with the user on 2026-11-14:

1. **Full 3D, like Blade & Soul,** not a 2.5D side-scroller. The world, movement, maps, and mobs are redone.
2. **Babylon.js** for the client.
3. **Free CC0 packs** for the art: KayKit, Quaternius, and Kenney. Mixamo is out, since its terms forbid handing out raw files.
4. **The stat system, the combat style, and the skill system stay.**
5. **The camera** looks down at about 50° from a fixed angle and follows the character in the middle of the screen. The mouse doesn't turn it.
6. **Keyboard only, with auto-aim:** the nearest mob in front within reach, outlined.
7. **Arcs, shots, and areas,** converted at 32 px a metre.
8. **Open 3D zones** written as YAML, with jumping and no ropes or platforms.
9. **Mobs wander, chase around obstacles, and leash home,** with red ground areas fixed at the wind-up.
10. **A `3d` branch,** and a slice of town and forest with every class before the switch. The switch doesn't wait for the user's approval: it happens as soon as the slice passes its playtest.

## 17. Alternatives considered

- **2.5D:** 3D models on today's side-scrolling maps, as in Elsword. It would keep the server as it is, but the user chose full 3D.
- **A third-person camera turned by the mouse:** the user chose a fixed overhead camera.
- **Aiming with the mouse cursor, as in Lost Ark:** the user chose the keyboard with auto-aim.
- **Mixamo animations:** their terms forbid handing out raw files, and KayKit's clips share its characters' rig.
- **A physics engine (Havok) on the client:** a second, different sim would break parity with the server's, and its wasm adds weight. Babylon only draws.
- **Maps built in Blender as glTF scenes:** that needs an artist and an extraction step for collision. YAML is written and tested by the team, and the kits dress it.
- **A Recast/Detour navmesh (go-detour):** heavier, with its last commit in 2025-04. A 0.5 m grid with clearance and A\* is enough for mobs in open zones, and it's ours to test.
  - Spike 3 first searches for an established Go library for grid A\* and for the heightfield, as the repo's rule asks, and notes in its PR what it found and why it used or passed on each.
  - It found none that fits:
    - `fzipp/astar` copies the whole path and re-sums its cost on every push. A failed chase in a 16 m leash took 6.1 ms and 5.4 MB against our 0.4 ms and 0.1 MB.
    - `quasilyte/pathing` caps a path at 56 steps and can't make passability depend on a move's way or a body's size.
    - No Go heightfield library is bit-exact with the client.
- **Building on `main`:** the game would be broken for weeks of PRs. The branch keeps `main` playable.
- **Converting all 14 maps by hand before the slice:** the slice would come much later. Placeholders keep the rest working while the feel is judged.
