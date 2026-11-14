# 3D world

> **Status: approved · 2026-11-14.** The high-level design for [B7. 3D world](../product/features/world-and-movement/b7-3d-world.md). It describes the code as it stands on `main`, a 2D side-scroller drawn with Phaser, and the change it plans: a 3D world drawn with Babylon.js, built on a `3d` branch until a playable slice is complete, which then replaces `main` at once (section 13).

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
| Sim | `server/internal/sim`, `web/src/sim`: walk, jump, ropes, one-way platforms, mob boxes | The same files in 3D: walk 8 ways, jump, slide along solids, slopes, mob circles; no ropes or platforms (section 5) |
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

### The ground

- **The terrain** is a height grid `cell` apart, built at load: 0, plus each hill's smooth bump, plus each mesa's flat top and its sloped edge. The height between samples is bilinear.
- **Solids** are upright boxes and cylinders. Their tops are ground for anyone standing above them.
- **The ground under a point** is the higher of the terrain there and the top of any solid whose footprint holds the point.
- **Walkable slope:** `max_slope` in `movement.yaml`, a rise per metre (0.7, about 35°).
  - At load, each terrain cell, the square between four height samples, is marked steep along x if the rise across either of its x edges exceeds `max_slope` × `cell`, and steep along z likewise. That's one multiplication and one comparison, with no trigonometry.
  - A walking body can't move uphill into a cell that is steep along the way it moves. A steep slope is climbed only by jumping, and only if it's low enough.
  - The sim (section 5), the walk grid (section 7), and the reach check all read these marks, so they can't disagree.
  - The terrain is what its samples say: an edge narrower than a cell is spread across it. At `cell: 0.5`, a rise of more than 0.7 m within one cell's width is always steep, wherever the samples fall.
- **A solid's lip is `step` (0.3 m):** a top up to that far above the feet is stepped onto, and a higher one is a wall.
- **Props are looks only.** A `scatter` is expanded by the client from its seed, since nothing collides with it.

### Validation, beyond today's rules

- Every spawn circle, the boss area, NPC, portal, and player spawn sits on walkable ground inside the map.
- Spawns and the boss area don't overlap a portal.
- The boss's circle is wide enough for its body.
- The map's own reach check passes (below).

### Reach check

`load/reach_test.go` keeps its goal: every portal, spawn, the boss area, and every NPC can be reached from the player spawn and from every arrival, at every move speed.
- It floods the walk grid (section 7).
- A step between neighbouring cells is allowed by walking unless it goes uphill into a steep cell (above) or onto a solid more than `step` higher. It's allowed by jumping if the rise is within a jump's rise less a margin.
- Any drop is allowed.

`TestRopesStandApart` goes with the ropes.

### The client's copy

Today `copy-maps.mjs` copies the `.tmj` files to `web/public/maps/`, git-ignored, stripping the boss layer, and `gen-gamedata.ts` writes only map properties to `src/gamedata`. After, `copy-maps.mjs` writes each map's geometry as `web/public/maps/<id>.json`, still git-ignored and with the boss's kills stripped, and `pkg/tiled` goes.

### Placeholders

A one-off converter turns each 2D map into a plain 3D one, so the slice keeps every quest, level band, and boss working.
- The map becomes a corridor 12 m deep and as long as the 2D map.
- Its ground, platforms, and ropes flatten to one floor.
- Spawns become circles along the middle.
- Portals stay at the ends.

Town and forest are designed by hand. The other 12 keep their placeholders until their own PRs (section 13).

## 5. Movement sim

`server/internal/sim/sim.go` and `web/src/sim/step.ts` are rewritten together. The vectors in `server/testdata/movement` are made again from the Go side, and both sides replay them.

### Types

- **`Params`:** `dt`, `walk_speed`, `jump_velocity`, `gravity`, `max_fall_speed`, `radius`, `height`, `step` (0.3 m), `max_slope` (0.7, section 4), and `snap` (0.5 m), the most the ground may fall away under a walking body before it falls instead. `Faster` scales the speeds as today.
- **`Geometry`:**
  - the height grid;
  - the solids;
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
   - **Terrain:** on the ground, an axis move uphill into a cell steep along that axis (section 4) is a wall on that axis. In the air, an axis move is a wall only if the terrain there would be above the feet. So a cliff low enough can be jumped onto.
   - **Mobs** keep today's rule, made round: a move may not bring the centres closer while the body overlaps a mob circle. A body can still walk out of an overlap, and it can jump over a mob.
   - **The map's edges** are walls.
5. **y:** the body moves by `VY`.
   - It lands when its feet reach the ground under it.
   - A body walking on the ground follows the ground down by up to `snap`, so it walks down slopes instead of hopping.

### Vectors

They replace today's 30:
- walking each of the 8 ways, and opposite keys;
- walking at a move speed factor;
- sliding along a box and along a cylinder at an angle;
- a box corner;
- walking up a ramp under `max_slope` and stopping at a cliff over it, both on an axis and diagonally;
- jumping onto a low cliff, and short of a high one;
- walking down a slope without hopping;
- a jump, a jump onto a box top, a jump short of a too-tall one;
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

### Knockback and flinch

- A mob's hit pushes the player 0.5 m away from the mob in x and z (today 16 px), stopped by solids. It also hops them with `VY` = 6.25 m/s.
- A mob's flinch push of 8 px becomes 0.25 m, away from the attacker.

## 7. Mobs

`application/mobs/arena.go` and `attacks.go` are rewritten. Timings, powers, poses, reactions, engage, provoke, and `hit_guard` don't change.

### Bodies and ground

- A mob is a cylinder with `radius` = width ÷ 64 and `height` = height ÷ 32 from today's `mobs.yaml`.
- It stands on the ground under its centre, and never jumps or falls. A hop is drawn as an arc.

### The walk grid

`server/internal/nav` (new) is shared by mobs, bots, and the reach check.
- **Cells** are 0.5 m apart, built at load from the terrain and the solids.
- **Each cell** keeps its ground height and its clearance: the distance to the nearest blocked cell. A cell is blocked under a solid taller than `step`.
- **Fit:** a body fits where the clearance is at least its radius.
- **A\*** runs 8-connected over cells that fit. A move between neighbours is refused if it goes uphill into a terrain cell steep along an axis it moves on: the sim's own rule, read from the same marks.
- **Paths** are short, inside a leash, so the search is bounded.

### Behaviour

- **Wander:** pick a point in the spawn circle that fits, walk there at the patrol pace, pause for `patrol.pause`, and repeat. `speed_jitter` stays.
- **Sight:** a player within `sight` (9.4 m) in x and z, with feet from 1 m below the mob's to 3 m above. The 3 m is today's 96 px. The 1 m below is new: today's lower bound is the mob's level, but terrain now dips. Solids don't hide players, as today. The target rules stay as they are: the provoker first, then the kept target, then the nearest seen.
- **Chase:** follow an A\* path to the target, repathing when the target has moved over 1 m or every 0.5 s.
  - **Leash** (`leash: 16m` in `mobs.yaml`): a mob farther than this from its spawn circle's edge drops its target and walks home. This replaces today's chase bounds.
  - Mobs stop at players by the circle rule of section 5.
  - Mobs don't avoid each other, as today.
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

## 8. Other rules that use positions

- **Portals:**
  - A player stands in a portal when their feet centre is inside its circle and within 1 m of its ground.
  - `EnterPortal` is unchanged.
  - The client sends it on a fresh press of the talk key in a portal, in place of up.
  - Arrival is the target portal's centre, facing away from it.
- **Talk:** reach (60 px, now 1.9 m) is a 3D distance from the NPC to the player's middle.
- **Drops** scatter on the ground around the killer: within 0.75 m, each further drop 0.6 m round a ring. Collection, its delay, and its fly-to-owner look are unchanged.
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

## 10. Client

### Babylon.js

- **Packages:** `@babylonjs/core` and `@babylonjs/loaders`, pinned to an exact version (9.36 at writing). Imports are per file, not barrels, so the bundle shakes. The size is measured in spike 1 (section 13).
- **Engine:** WebGL2. WebGPU is a later option.
- **Mounting:** the renderer mounts in `#game` behind today's `GameHandle` (`attach`, `enter`). React and its stores don't change.
- **What goes:** Phaser, `game/scenes`, `game/entities`'s sprite views, `effects/draw.ts`, `depth.ts`, `keys.ts`, `grey-art.ts`, `placeholders.ts`, the 2D assets, `testing/fake-phaser.ts`, and `recording-scene.ts`.
- **What stays** is the logic in `game/systems`:
  - `ActionClock`, `skill-keys`, `potion-keys`, `hit-stop`, `arrival`, `map-transition`, and `feed-watch`;
  - `prediction`, `interpolation`, `combat-view`'s tracking, `drops`, and `portals`, each with x, y, and z;
  - `entities/beats.ts`.
- **Tests:** scene logic runs on Babylon's `NullEngine` under vitest.

### The scene

- **The ground** is a mesh from the height grid, in a toon material keyed by `look.ground`.
- **Solids and props** load their kit models by key. Every instance of a model shares one copy.
- **Sky, fog, and light** follow `look.sky`. A sun casts soft shadows from characters and mobs only.
- **The camera** is a fixed-angle follow camera: 50° down, 16 m back, looking north, easing toward the player at 0.15 a frame as today. A map's edge holds it.
- **Fading:** a solid or prop between the camera and the player's head fades to 30%.

### Characters and mobs

- **Models** are glTF from the packs (section 11), keyed in `classes.yaml` and `mobs.yaml` as `look: {model, scale, tint}`.
- **Animation:**
  - Each action plays its clip from `look.clips` (class) or `look.clip` (skill), time-scaled so the clip's strike lands at the action's hit and its end at the ready point.
  - A cut stops it with a blend of at most 80 ms, so a cancel reads as instant.
  - Walk, run, jump, fall, hit, flinch, down, and death come from the same clip sets.
  - Idle and walk blend by speed.
- **The beats** (`beats.ts`) still time strike, follow-through, and ready. The clip carries them, and the lean and lunge of today's pose go.
- **Hit-stop** freezes the attacker's and the target's clips for its 50 ms, by `speedRatio` 0.
- **The afterimage** is a frozen copy of the cut pose:
  - white with a dark rim;
  - fading from 0.8 over 300 ms;
  - drifting back 0.44 m, today's 14 px.

### Effects

- **Swings:** a `TrailMesh` from the weapon bone through the strike beat.
- **Sparks:** GPU particles at the hit point. They are white for physical hits, violet for magic, bigger for a crit, and a grey puff for a MISS (C13).
- **Skill looks:** keep their ids and the manifest scheme. Each is rebuilt from meshes, particles, and `GlowLayer`.
- **Telegraphs:** red ground decals in each attack's shape (section 7), filling over the wind-up.
- **Damage numbers, name tags, and NPC markers:** DOM elements over the canvas, placed by projecting their world points. Today's rules for popping, stacking, and keeping clear carry over in screen space.

### Input

- The presets keep their keys. Up and down walk north and south, and talk also enters portals.
- The auto-aim and outline of section 6 live in a new `systems/aim.ts`, tested on its own.

## 11. Art

All CC0. Each pack is recorded in `web/public/models/LICENSES.md` with its source.

| Use | Pack |
| --- | --- |
| Classes | KayKit Adventurers (free set): Warrior: Knight; Thief: Rogue; Bowman: Ranger; Magician: Mage. The Beginner is chosen in spike 1 |
| Their animations | KayKit Character Animations, the same rig family, so nothing is retargeted: one-handed and two-handed melee, dual wield, bow, spellcasting, hit, death, and movement |
| Monsters | Quaternius Ultimate Monsters and its other monster packs, and KayKit Skeletons |
| Places | KayKit Forest and Dungeon, Quaternius Stylized Nature and Medieval Village, and Kenney's castle and town kits |

- **The look:**
  - Every model draws with one toon `NodeMaterial`: stepped light with flat colour.
  - Characters and mobs get an inverted-hull outline.
- **Mixamo isn't used.** Its terms forbid handing out the raw files, which a web game sends to every browser.
- **Mob models:**
  - Spike 1 maps the forest's three mobs (green slime, orange mushroom, and the king slime) to models.
  - Each later map does its own.
  - A mob with no fitting model is renamed or reskinned to one that fits (the user agreed).

## 12. Bots, testers, and the balance harness

- **Bots** (`cmd/bot`) keep their planning: quests, promotion, skills, spending, and the portal graph.
  - Their walking follows `nav` paths, turning each waypoint into keys.
  - `climb.go` goes.
  - A hunting spot becomes a circle.
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
   - It's drawn with the toon material, the outline, and a swing trail.
   - Measure the bundle size and the frame time.
   - Pick the Beginner's model and map the forest's mobs.
2. **Sim:** the 3D sim in Go and TypeScript, with the vectors of section 5, replayed on both. It adds a CI job on GitHub's free `ubuntu-24.04-arm` runner, since the repo is public, that replays the vectors in Go and TypeScript, so arm64 is checked on every PR beside amd64.
3. **Maps:** the YAML format, its loader and validation, `nav` and its A\*, the reach check, and the converter, on town and forest.

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

## 14. Testing

- **Sim:** the vectors, on Go and TypeScript, on CI's amd64 and arm64 jobs.
- **Maps:**
  - each validation refusal;
  - the reach check on every map, at every move speed;
  - the converter on all 12.
- **`nav`:**
  - clearance;
  - the steep marks: a move the sim refuses uphill into a steep cell, A\* and the reach check refuse too, on a ramp, a cliff, and a cliff whose edge straddles two cells;
  - A\* around a box, a cylinder, and a mesa edge;
  - no path past the leash;
  - bounded search.
- **World:**
  - each shape at its edges: an arc's angle and radius, a shot's lane and first target, an area's radius;
  - `aim` used as sent;
  - knockback stopped by a solid;
  - each mob attack's area fixed at the wind-up, a player stepping out of it unharmed, and a charge and a spit stopped by solids;
  - wander, chase around a solid, the leash home, and sight's height band;
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
- **Live:** a playtest of the slice for every class. The checks are the camera, aim, telegraphs, cancels, moving through a skill, the frame rate, and the download size.

## 15. When things fail

| Failure | What the player sees | Why it's safe |
| --- | --- | --- |
| A last-bit difference between sims | Nothing: the vectors fail first | No trigonometry in the sim; products wrapped; `sqrt` is correctly rounded |
| The client aims at a mob the server sees elsewhere | The swing may whiff | The server resolves the shape with the sent aim, against its own positions |
| A modified client sends any aim | It faces that way | Walking can face any way anyway; reach and timing are still the server's |
| A mob's path is cut off | It stops at the nearest reachable cell, then walks home after the leash | A\* is bounded; the leash always ends a chase |
| A slow device | A lower frame rate | Spike 1 measures it; shadows and the glow can be turned off |

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
- **Building on `main`:** the game would be broken for weeks of PRs. The branch keeps `main` playable.
- **Converting all 14 maps by hand before the slice:** the slice would come much later. Placeholders keep the rest working while the feel is judged.
