# Large regions

> **Status: approved · 2026-11-18.** The high-level design for [B10. Large regions](../product/features/world-and-movement/b10-large-regions.md), [B11. Sprint and waypoints](../product/features/world-and-movement/b11-sprint-and-waypoints.md) and [B12. Camera zoom](../product/features/world-and-movement/b12-camera-zoom.md). It changes [Minimap and world map](./map-views.md), [Teleport](./teleport.md) and the map parts of [3D world](./3d-world.md).

## 1. What has to hold

- **The user's words** (2026-11-18): "instead of many mini maps, which inherits from maplestory - i want some large maps like blade and soul".
- **Their choices:**
  - A few big regions, seamless inside, with a load only between regions.
  - About 1 km across.
  - Sprint and waypoints, with no mounts.
  - A new world layout, not today's maps rebuilt.
  - Three regions: levels 1–12, 12–22, and 22–30 with the keep.
  - A camera that zooms out to about 40 m and still faces north.
  - Rolled out region by region.
  - Regions generated from a plan.
- **The client computes, the server validates:**
  - The client draws the land from baked files and predicts sprint.
  - The server decides every move, hit and channel, and only sends what a player can see.
- **Same rules, bigger place:** combat, skills, statuses, the lock and every system keep their rules. What changes is what scales with area and crowds.
- **A region is a map to the code.** It's one `gamedata.Map` and one instance per channel. Areas, towns and waypoints sit inside it.

## 2. Today, against a 1 km region

What grows with area or crowds, found in the code before step 2; step 2a has since bucketed the movement row and built nav at boot (section 5). A region at 0.5 m cells is about 4 M terrain samples, against about 11.5 k for one of today's maps.

| Where | Today | At 1 km, 150 players, 400 mobs |
| --- | --- | --- |
| **Frames and events** (`instance/frames.go` `broadcast`) | Every changed entity and every event goes to every player on the map; the gateway encodes one frame for all (`push/frames.go` `shared`) | Every player gets every fight in the region |
| **Join snapshot** | Everything on the map | About 600 views on each join |
| **Movement** (`sim.slide`, `Ground`, `Map.Holds`) | Scans every box, cylinder and blocker, per player, per sub-step | O(players × solids), with thousands of trees |
| **Mobs** (`mobs.Arena.Step`) | Every mob runs every tick while one player is on the map. `spread` is O(mobs²); `yield`, sight and `reached` scan every mob or player | Mobs a kilometre away still think |
| **Nav** (`nav.New`) | O(cells × solids), lazily on first use | Billions of steps; about 80 MB at 20 B a cell |
| **Map data to the client** (`cmd/mapjson`) | One JSON with every height | About 4 M numbers |
| **Client terrain** (`groundMeshes`) | One mesh of every sample, built at load | About 8 M triangles, no LOD |
| **Client props** | Thin instances per model across the map; a see-through clone and a per-frame `fade` test for each tall prop | Never culled; tens of thousands of looks tested each frame |
| **Shadows** | One 2048 map fitted to the whole map | About 0.5 m a texel, blurry |
| **Map image** (`map-image.ts`) | 4 px a metre, every pixel tested against every solid; `Math.min(...heights)` | 16 M pixels, and the spread throws past about 100 k numbers |
| **Entities** | A skinned clone, DOM label and bar each, never culled | Whatever the server sends, so they're bounded by section 5 |

## 3. Region data

### The plan: `server/data/regions/<id>.yaml`

```yaml
name: ...
size: [1024, 1024]
cell: 1                 # terrain samples a metre apart; detail comes from solids and set pieces
seed: 11
look: {sky: day}
ground: {noise: {scale: 220, amplitude: 7, octaves: 4}}
edge: {width: 70, height: [35, 60]}           # the ring of mountains
features:                                     # applied in order
  - {ridge:   {path: [[x, z], ...], width: 60, height: 30}}
  - {plateau: {area: [[x, z], ...], height: 8, edge: cliff}}   # or ramp
  - {river:   {path: [...], width: 14, depth: 3, fords: [[x, z]]}}
  - {lake:    {area: [...], depth: 4}}
  - {road:    {path: [...], width: 5}}        # flattened, earth-coloured, kept clear
areas:
  - {id: ..., name: ..., area: [[x, z], ...], levels: [1, 4], biome: meadow, kind: hunting,
     boss: {at: [x, z], radius: 6, mob: ..., kills: 300}}
  - {id: ..., name: ..., area: [...], kind: town}
camps:
  - {area: ..., name: ..., at: [x, z], radius: 8, mob: ..., max: 5, respawn: 10s}
waypoints:
  - {id: ..., name: ..., at: [x, z]}
npcs:
  - {id: ..., at: [x, z], yaw: 180}
pieces:                                        # set pieces placed by hand
  - {model: ..., at: [x, z], yaw: 90}
  - {house: {area: [x, z, w, d], height: 4, model: ...}}
gates:
  - {name: ..., at: [x, z], radius: 1.6, to: {region: ..., gate: ...}}
start: {at: [x, z], yaw: 0}                    # the main town's arrival spot
biomes:
  meadow:
    colours: {flat: grass, slope: earth, steep: rock, high: rock}
    scatter:
      - {model: tree_oak, spacing: 9, slope: 0.4, away: {road: 4, camp: 3, town: 0}}
      - {model: rock_large, spacing: 30}
```

- **Loader rules,** beside today's map rules:
  - Areas don't overlap, and every point of the region is in exactly one area, or the edge.
  - A hunting area has levels and a boss. A town has no camps and no boss.
  - Every camp's mob level lies in its area's band.
  - Every waypoint, NPC, gate and the start stand on walkable ground.
  - Every gate links both ways.
  - Every area, waypoint, NPC and gate can be reached from the start: the reachability test of today (`TestEveryPortalSpawnAndBossCanBeReached`) is run over the region's nav.
- **`world.yaml`:**
  - `places` goes for regions: the World tab is a chart of three regions, by `regions: [{id, at}]`.
  - The old maps keep their places until they're retired.
- **Quests, NPCs and bosses** name an area where they named a map: kill objectives count kills in an area, and `npc.MapID` becomes the region with its spot.

### The generator: `internal/shared/regiongen`

- **Deterministic from the plan and its seed.** The steps:
  1. **Ground:** OpenSimplex noise, with octaves.
  2. **Edge:** the mountain ring, a smooth rise with noise.
  3. **Features,** in order:
     - ridges along their paths;
     - plateaus with cliff or ramp edges;
     - rivers and lakes carved, with fords left shallow;
     - roads flattened to their mean height along a smoothed path.
  4. **Towns:** each town area flattened toward its mean.
  5. **Steepness,** as `sim.Terrain` marks it today.
  6. **Colours:** each cell gets a biome colour, by its area and its height and slope: flat, slope, steep and high. Roads take earth, river beds sand.
  7. **Scatter:** each biome's models are scattered with Poisson-disc spacing, kept off roads, camps, towns, waypoints and steep cells by their `away` distances. Each placement becomes a prop, and its model's solid becomes a cylinder or box.
  8. **Pieces** are added as placed.
- **Noise:** [ojrac/opensimplex-go](https://github.com/ojrac/opensimplex-go), vetted in section 16.
- **Poisson disc** (Bridson's method, about 60 lines) and the polygon and path helpers are in-house.

### The bake: `cmd/regionbake`

- It runs the generator and writes one region's files. They're a build artifact, not committed, as `npm run maps` is today:

  | File | What | Size at 1 km |
  | --- | --- | --- |
  | `regions/<id>/region.json` | The plan's areas, towns, camps, waypoints, NPCs, gates and start, plus height range and chunk index | small |
  | `regions/<id>/terrain.bin` | Every height as float32, every steep mark and colour, for the server | about 6 MB |
  | `regions/<id>/chunks/<cx>_<cz>.bin` | Per 64 m chunk: heights (float32), steep marks, colours, props and solids | about 20–30 kB gzipped |
  | `regions/<id>/overview.bin` | Heights and colours at 8 m, for the far land | about 70 kB |
  | `regions/<id>/tiles/<z>/<x>_<y>.png` | The map image in 128 m tiles at 1 and 1/4 px a metre, shaded by height, solids dark | about 1–2 MB in all |

- **Both sides read the same bake.**
  - The server loads `terrain.bin` and the chunks' solids at boot.
  - The client fetches chunks and the overview.
  - So no float difference between machines can make the client's ground differ from the server's.
- **A golden test** keeps each region's bake hash, so a change to the generator shows in review.
- **`make region-preview`** renders the plan and its bake as a top-down PNG, with areas, camps, roads and waypoints marked, for designing a region.

## 4. Region instances and channels

- **A region is a `gamedata.Map` of a new kind, `region`.** Its terrain, solids, nav and areas come from the bake, built once and shared by all its channels.
- **Channels are instances made on demand,** with the same machinery as PvP match instances (pvp.md section 4).
  - `w.instances` keeps the fixed maps.
  - A region's channels live in a registry under its own mutex, keyed by region and channel number.
- **Joining:**
  - A player goes to the least full channel under `channel_cap` (150, in config).
  - A party member's channel comes first when it has room.
  - When every channel is full, a new one opens.
- **Closing:** a channel past the first with nobody in it for 5 minutes closes.
- **Switching:**
  - The map window shows "Channel n" with a list. Switching is a 3-second channel, cancelled as a teleport is (B9), to the same spot in the other channel.
  - B4 becomes the region's channels.
- **Region changes** go through a gate (the talk key in its ring, as a portal) or a teleport to another region's waypoint. Each is a hand-off as today, to the new region's least full channel.

## 5. The server: what each player is sent

### Interest cells

- The region is cut into **32 m cells.** A player watches the cells within 96 m of them: a block of up to 7 × 7.
- **Each tick, each cell's changes are encoded once** as a `Frame` part:
  - its players, mobs and drops that changed;
  - those that arrived in it, as full views.
- **A player's frame** is their watched cells' parts, plus their private part (`AckSeq`, energy).
  - Protobuf merges repeated fields when messages are concatenated, so the gateway joins the encoded parts in place of decoding them.
  - So the "encode once" saving of `push/frames.go` holds per cell.
- **Coming into view:** when a player's block changes (they moved cells), or an entity crosses into a watched cell, the player gets the entity's full view. Cells no longer watched give `Unseen{ids}`. The client drops what it no longer sees.
- **Events by place:** `Damage`, `SkillUsed`, `AttackUsed`, `MobAttacked` and status events go to the watchers of the cell where they happen.
  - The private ones stay private.
  - Region chat, boss announcements and the boss's bar go to the whole channel.
- **The join snapshot** is the block around the player.
- **Party:**
  - Members out of view still show on the party frames.
  - Their spot reaches the minimap through `tellParty` once a second, in place of on map changes alone.
- **PvP and the old maps** run the same code. A map smaller than a cell is one block, so nothing changes for them.

### Spatial indexes

- **Solids:**
  - `sim.Geometry` gains a uniform grid of 8 m buckets over its boxes and cylinders. Blockers, a tick's mobs, are bodies (below).
  - `slide`, `Ground` and `Map.Holds` read only the buckets a body's move touches.
  - The TypeScript sim mirrors it, and the shared vectors gain region cases.
  - As built (step 2a):
    - `sim.Indexed` lists each solid in every bucket its bounds overlap, padded 1 µm, since a turned box can hold a point a rounding step outside its bounds. `mapfile.Build` indexes every map, and the client's `indexed` does the same in `parseMap`.
    - A query visits the solids in their own order, each once, so every answer is a full scan's to the bit. `slide` reads the buckets its swept body touches. `Ground`, `SolidOver` (which `Map.Holds` uses) and the client's `ground` read the bucket under the point.
    - A geometry that isn't indexed scans every solid, as before.
    - A query over more than 16 buckets walks every solid, as a scan does, since the Go walk merges at most 16 lists. Nothing reaches it today: a slide moves at most a sub-step, a blink's 0.25 m test or a knockback, and nav's clearance reach spans at most 3 × 3 buckets. A move swept far in one go would want a bigger merge.
    - A NaN in a body or point, as a broken state might hold, reads the first bucket, where every solid's test fails as in the scan; a test holds both sides to the scan's answer.
    - A sim step in a 1 km region with 22,000 solids takes 0.56 µs against 262 µs scanned. Go allocates nothing; the client's `near` reuses one list when a query spans buckets.
- **Bodies:** mobs and players are bucketed by interest cell. These read only nearby buckets:
  - sight and targeting;
  - `reached` (strikes and skills);
  - `spread`, `yield`, `hold` and `room`;
  - pickups, NPC reach, waypoint finding, gate rings, and area and town lookups.
- **Nav:**
  - Built at boot with solids binned, so a cell tests only nearby solids.
  - Packed to 3 bytes a cell: clearance as a uint16 in steps of 2⁻¹³ m, and a byte of flags: a raised top, and which of the four sides a body may walk to. That's 12 MB a region at 0.5 m, shared by every channel.
  - As built (step 2a):
    - Clearance runs up to just under 8 m (65,535 steps). Scaling by a power of two is exact, so a radius in whole steps fits exactly as a float clearance does; off a step, a fit would be judged for the radius rounded up to the next. The loader holds every mob's radius and the player's to whole steps within the cap (`nav.CheckRadius`); any multiple of 1/32 m is one.
    - The build decides each way between side neighbours from the ground in float64, so no heights are kept and a walk is the old rule's to the bit; the fit at the far cell is the only part asked per radius.
    - A cell reads only the solids within its clearance through `Geometry.Near`.
    - `gamedata.Map.Grid` is the built grid, made by the loader.
    - On today's maps every fit check and walk move is unchanged: 2,065,952 cell-radius pairs and 12,892,986 moves, compared old against new.
    - A 1 km region's grid with 22,000 solids builds in 1.9 s and holds 12 MB, against about 6 minutes scanned.
  - Long trips go by a **road graph,** from the plan's roads, waypoints and gates. Mobs never walk far, and cmd/bot uses it for trips across a region.

### Sleep

- **A mob sleeps** when no player watches its cell. A sleeping mob:
  - finishes an act or walk home under way;
  - doesn't sense, chase or wander.
- **Camps keep filling while asleep,** so a camp a player walks to is full.
- **Waking:** a mob wakes when its cell comes into a player's view.
- Statuses and damage over time on a sleeping mob run on.

### Budget

- **The target,** on one region channel at its cap of 150 players and about 400 mobs:
  - tick p99 under 20 ms of its 50 ms;
  - each client downstream under 40 kB/s in a busy fight.
- **The test:** a soak with cmd/bot's players (section 13), whose tick p99, overruns and bytes are read from the ops metrics. `channel_cap` is set from it.
- **The inbox** (4096, 1024 drained a tick) holds about 4× a full channel's input rate. The soak checks `ErrBusy` stays at zero.

## 6. Areas, towns and what was per map

- **Areas:**
  - `gamedata.Region.AreaAt(x, z)` comes from a 4 m lookup grid baked from the area polygons.
  - A player's area is tracked each tick from their cell, and entering one sends `AreaEntered{area}`. The client shows the name and band, as on a map change today.
- **Towns:**
  - In a town area, damage is refused as in a town map today (`usable` reads the area in place of `Map.Kind`).
  - A mob's chase and leash stop at a town's edge.
- **Field bosses,** one per hunting area:
  - Kills in an area count toward its boss.
  - Its bar goes to players in the area, and rewards go to those in the area when it falls.
  - `instance/boss.go` keeps a slice of bosses by area, in place of one.
- **Death:** the player gets back up at the nearest waypoint or town of their region, by the road graph's distance. They don't hand off to a town map. On the old maps still in use, death hands off to the map's town as today.
- **Level bands** move from `Map.Levels` to areas. The loader checks camps and bosses against their area.
- **Chat:** `Instance.Chat` already reaches the whole instance, which is now a region's channel. E1 says "region and channel".

## 7. Sprint

- **The rule, in the shared sim** (`sim.Step` and `step.ts`):
  - `State.Sprint` counts the sub-steps a body walks out of combat, up to `SprintSteps`: `sprint.after` over `dt`, 120. From the next sub-step it sprints, at `sprint.speed` (1.6) × its speed, move speed and all.
  - The count ends when no direction is held, under a hold or a skill's move, in combat, and at an attack or skill pressed (`EndSprint`).
  - A jump keeps it: the count runs on in the air.
  - `movement.yaml` gains `sprint: {after: 2, speed: 1.6}`, in seconds as its other values.
- **Combat:**
  - The world marks a player in combat as C20 does: 100 ticks, 5 s, from the tick after any `Damage` naming them. It steps them with `Params.Fighting()`, which has no sprint.
  - The client's predictor keeps the same clock in the world's ticks, from each `Damage` naming the player and its tick. An input's tick is the last ack's plus the inputs since, as the world runs one a tick. So entering and leaving combat fall on the same sub-step both sides.
  - Inputs predicted before the client hears of a hit replay with no sprint as it does. The correction is those inputs' sprint alone, never the sprint before the hit.
  - A rejoin is a new player, out of combat. A respawn keeps the clock, as the world does.
- **A press:** the world ends the count from the first input past the newest it has had when an attack or skill message arrives, sent or refused. The client marks the same input, the first it sends after the press, so a sprinting player who attacks isn't pulled back.
- **The wire:** `PlayerView.sprint` (31) carries the count, as `held` and `slow` carry theirs, so the player's own reconcile replays from it. Others sprint at a count of 120, whatever their speed.
- **The look:** the run clip at 1.3× with an 8° forward lean from the feet.

## 8. Waypoints and teleport

- **Data:** waypoints come from the plan. A waypoint is a set piece, a tall stone with a glow, with a 12 m finding circle.
- **Finding:**
  - Each tick, a player's cell lists the waypoints near it.
  - Coming within 12 m of an unfound one stores it and sends `WaypointFound{id}`.
- **Storage:** migration `character_waypoints(character_id, waypoint_id, found_at)`. The waypoints a character has found arrive in `Welcome`.
- **Teleport** (teleport.md):
  - It targets a waypoint: `Teleport{waypoint}` in place of a map.
  - It's allowed to found waypoints and to each region's main town.
  - The channel and its cancels don't change.
  - Another region's waypoint is a region change at its end.
  - `TELEPORT_UNKNOWN_WAYPOINT` and `TELEPORT_NOT_FOUND` replace the map refusals.

## 9. The camera

- **Zoom:** the camera distance runs from today's 16 m to 40 m. The pitch eases from 50° to 35° along it, and the target stays on the player.
- **Input:**
  - The wheel steps the zoom: 800 px from close to far, so a 100 px notch is an eighth. A mouse that counts lines steps an eighth a notch.
  - Two new rebindable actions, `zoomIn` and `zoomOut`, sit on `Equal` and `Minus`. Each press steps an eighth. They're in the key-binding window's Windows group (key-bindings.md).
  - The camera eases to the zoom as it eases after the player.
- **Saved** in the account's settings as `zoom`, 0 to 1, as the minimap flag is. It's saved 800 ms after the wheel stops, and as the player leaves the world. A zoom outside 0 to 1 is `BAD_ZOOM` (110009).
- **Fog and the far plane** follow the zoom. Each sky's fog draws back up to 3.6 times as far: the day sky's from 45–140 m close up to about 160–500 m zoomed out. `maxZ` is 1,500 m.
- **`heldView`'s clamp** keeps the view inside the region, as it does the map.

## 10. The client: drawing a region

- **The far land:** `overview.bin`, the whole region at 8 m, is one mesh drawn under the near chunks. So mountains and ridges show to the edge.
- **Chunks:**
  - **Loading:** the 64 m chunks within 256 m of the camera's target load, nearest first, in a Web Worker that decodes them and builds their arrays. Meshes are made on the main thread from the transferred buffers.
  - **Detail** steps with distance: every sample within 64 m, every 2nd within 128 m, and every 4th beyond.
  - **Seams:** skirts hide the cracks between detail levels.
  - **Unloading:** chunks past 320 m are freed.
- **Ground colour:** per-vertex colours from the bake, through the toon material (`palette.toon` with vertex colours). There are no textures, so the flat-colour look stays.
- **Props:**
  - Thin instances per chunk and model, so frustum culling works per chunk. The ink outline is per chunk too.
  - Props past 160 m aren't drawn; the fog hides them.
- **Tall props' see-through copies** (`kitLook`) are made per chunk, for chunks within 64 m only. `fade()` reads a grid of looks, so it tests only those near the camera's line to the player.
- **Shadows:** Babylon's `CascadedShadowGenerator`, with 3 cascades to 80 m. Casters are the near chunks and the bodies in view.
- **Water:** rivers and lakes are flat planes at their level, in the toon style. Deep water is steep ground the sim blocks, as walls are.
- **Entities:** only what the server sends (section 5).
  - Name labels show for the 40 nearest, and for party members and the lock.
  - HP bars show for the party, the lock, and mobs hit in the last 5 s.
- **Models** are cached across scenes, so a region change doesn't parse them again.
- **Map views** (map-views.md):
  - **The minimap** draws the baked tiles round the player at 1 px a metre.
  - **"This map" becomes "This region":** the tiles at 1/4 px a metre with pan and zoom. It shows:
    - areas, named with their levels;
    - towns;
    - waypoints, lit when found;
    - camps, labelled with mob and level;
    - gates and NPCs;
    - party members.
  - **The World tab** charts the three regions and any old maps still in use.
  - `map-image.ts` reads its height range from `region.json`, not from a spread of every height.
- **Loading a region:** the loading screen holds until the overview and the chunks within 128 m are built, then fades as today.

## 11. cmd/bot

- **Plans by areas and camps:** a hunting spot is a camp in an area of the bot's band.
- **Long trips** follow the road graph to the camp's nearest waypoint, then nav near the camp. Once the waypoint is found, the bot teleports.
- **Gates and region changes** replace portals.
- **The soak:** `make bots BOTS=150 REGION=<id>` starts its bots in one region and reports what section 5's budget needs.

## 12. Rollout and the old maps

1. **The engine** (sections 4–5, 7, 9, 10), on today's maps first.
   - Interest cells, the indexes and sleep change nothing a player of a small map can see. Their tests run on today's maps.
   - Sprint and zoom ship then too.
2. **The pipeline:** the plan, the generator, the bake, the preview, the `region` kind, region channels, and a test region built from a small plan. The test region is never linked from the world.
3. **The green region:**
   - Designed with the user from previews first: its areas, towns, camps, waypoints and roads.
   - Then built: its NPCs and quests are re-placed, and mob levels are moved to their areas' bands, with the balance harness re-run.
   - Town, Forest and Cave are retired.
   - A gate at its edge leads to the Crossroads, whose portal back now leads to that gate.
4. **The desert,** then **the snow mountains,** each retiring the old maps whose levels it covers. The last retires the Crossroads and the gate.

- **Characters on a retired map:** `World.Join` places a character whose map no longer exists at the green region's start, in place of `ErrNoMap`.
- There's no other migration, since the game hasn't gone live.

## 13. Testing

- **Generator:**
  - each feature's shape on small plans;
  - Poisson spacing and `away`;
  - colours by slope and height;
  - the golden bake hashes.
- **Loader:**
  - areas covering the region without overlap;
  - bands;
  - towns without camps;
  - gates both ways;
  - reachability over the nav.
- **Server:**
  - **Interest:** what a player is sent at a cell's edge; entities coming into and out of view; events reaching only watchers; and the concatenated frame decoding to the union of its parts.
  - **Indexes:** each check equals a full scan on randomized cases. `rapid` (pgregory.net/rapid) is already a dependency.
  - **Sleep:** a far camp stays full, and its mobs wake as a player comes.
  - **Channels:** filling, a party kept together, a new channel opening, and a spare one closing.
  - **Areas:** the area's boss, a town's refusals, and respawn at the nearest point.
  - **Sprint** in the shared vectors.
  - **Waypoints:** finding, storage, and teleport rules.
- **Client:**
  - chunks loading and unloading round a moving target;
  - detail and seams;
  - the looks grid;
  - the zoom's angle and fog;
  - the map views from tiles.
- **Performance:**
  - The soak with 150 bots, against section 5's budget.
  - A client frame at full zoom-out in the busiest town, measured as 3d-world.md measures. The target is a median under 8 ms at 1280×800 on the reference laptop, and a region shown within 3 s of entering.
- **Live** (the player harness): a walk across the test region, then the green region end to end. It covers sprint, finding waypoints, teleports, a gate, a boss, death and a channel switch.

## 14. Build

1. **Interest cells** and the frame parts; `Unseen`; events by place; party spots.
2. **Spatial indexes and sleep,** in two PRs:
   - 2a: sim buckets (both sides, with vectors), and packed nav with its binned build;
   - 2b: body buckets and sleep.
3. **Sprint and zoom:** the sim rule, `PlayerView.sprint`, the look, the camera, the zoom actions, and saved zoom.
4. **The pipeline:**
   - `regiongen` and the bake;
   - the preview;
   - the `region` kind and its loader rules;
   - region channels;
   - areas, towns, area bosses and respawn;
   - a test region.
5. **The client for regions:**
   - the overview, chunk streaming and detail;
   - props per chunk and the looks grid;
   - cascaded shadows and water;
   - label and bar limits;
   - tiles for the map views.
6. **Waypoints and teleport:** finding, storage, `Teleport{waypoint}`, and the map views' waypoints.
7. **The soak:** cmd/bot on regions, and the budget run. `channel_cap` is set.
8. **The green region:** its design first, then its build.
9. **The desert,** then **the snow mountains.**

Steps 1–3 change nothing on today's maps that a player sees, so they can land while PvP builds. Step 4 shares on-demand instances with PvP's step 1.

## 15. Decisions

The user's choices are in section 1. These calls were made in planning and logged for the user to push back on:

1. **Terrain at 1 m,** not 0.5 m. Big rolling land needs no finer grid; towns and set pieces get their detail from solids.
2. **Seen within about 100 m:** the interest block is 96 m. The land shows to the region's edge.
3. **Channels of 150,** to be confirmed by the soak, filled least-full-first, with parties kept together.
4. **Sprint needs no key:** 2 s of walking out of combat, ×1.6. Jumping keeps it.
5. **Waypoints about every 150 m along roads,** found within 12 m. Each region's main town can always be teleported to.
6. **Death returns to the nearest waypoint or town,** found or not.
7. **The zoom runs from 16 m at 50° to 40 m at 35°,** with = and − as keys besides the wheel.
8. **Ground colours by vertex,** with no textures, so the flat toon look stays and there are no texture assets to fetch.
9. **Deep water blocks;** fords and bridges cross rivers. No swimming.
10. **A mob sleeps when no one watches its cell,** while its camp keeps filling.
11. **The bake is a build artifact both sides read,** in place of each machine generating its own.
12. **The old maps stay behind a gate** until the region that covers their levels replaces them.

## 16. Alternatives considered

- **Libraries.** These were searched and vetted for adoption, a source scan and activity. Each was cloned and read, never run.
  - **ojrac/opensimplex-go** (Unlicense) is chosen for the noise:
    - pkg.go.dev counts about 110 importing packages;
    - it has no dependencies;
    - its non-test code imports only `math` and `math/rand`, seeded per call;
    - it has no exec, network, cgo or init code.
    - It was archived in 2025 as finished. Noise math doesn't need updates, and a fork is cheap if it ever does.
  - **R-tree libraries** (tidwall/rtree and others): solids and bodies are spread evenly over a region, where a uniform grid is simpler and as fast.
  - **Babylon's own `CascadedShadowGenerator`** is part of the engine already in use, so there's nothing to add.
- **One seamless world:** the user chose regions.
- **Free camera:** the user kept north-facing.
- **Sculpting regions in Blender:** the user chose generation. Set pieces can still come from Blender, which is installed.
- **A region per process, or per-cell instances:** one instance per channel keeps today's tick, inbox and hand-offs. The soak decides whether that's enough before anything heavier.
- **Textures on the ground** (splat maps): more art to fetch and vet, and a different look from the flat toon colours of every model.
- **Per-player frames:** encoding per player would lose the shared encoding. Per-cell parts keep it.
