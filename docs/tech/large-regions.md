# Large regions

> **Changed by [Underground and underwater](./underground-and-underwater.md):** a place below a region is a small region to the code, with `under` naming the region above. Its plan adds carved halls and tunnels, torches and a light map. A gate between a region and a place below keeps the player's channel number.
>
> **Changed by [Loot pouches and timed bosses](./loot-pouches.md):** an area's boss stands at its circle from the channel's start and comes back 5 minutes after it's beaten. Kills don't count toward it, and its notices go to the area's players, not the whole channel. Pouches go in the owner's private part of the frame.

> **Status: approved · 2026-11-18.** The high-level design for [B10. Large regions](../product/features/world-and-movement/b10-large-regions.md), [B11. Sprint and waypoints](../product/features/world-and-movement/b11-sprint-and-waypoints.md) and [B12. Camera zoom](../product/features/world-and-movement/b12-camera-zoom.md). It changes [Minimap and world map](./map-views.md), [Teleport](./teleport.md) and the map parts of [3D world](./3d-world.md). The waypoint stone's look was picked from previews on 2026-11-24 (section 8).

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
- **After the live test of sprint** (2026-11-19): "should have a key to trigger the sprint or walk, for e.g if i want to walk during without combat".
  - Their choices: a key that switches between walk and sprint mode, which sprints at once when switched back while moving; Left Shift by default.
  - They asked first for Ctrl+Shift. It was left out because on Windows Ctrl+Shift switches the keyboard layout, and it's the default switch of Vietnamese typing tools such as Unikey, which then take some letter presses. Chrome also keeps Ctrl+Shift with W, T and N.
- **The waypoint stone** (2026-11-24), picked from preview stills drawn in the game's renderer:
  - the carved obelisk with a floating crystal, its halo kept pure cyan on grass, where the first stills read mint;
  - dimmer until found;
  - a short flare on finding.
- **The halo** (2026-11-25), picked from a sheet of three: a soft cyan glow over grass can't be had, as any soft fade from cyan to the grass's green passes through mint. So it's a solid cyan core with a soft edge deepening toward blue, and a small dark-teal halo on an unfound stone, dim by darkness.
- **The client computes, the server validates:**
  - The client draws the land from baked files and predicts sprint.
  - The server decides every move, hit and channel, and only sends what a player can see.
- **Same rules, bigger place:** combat, skills, statuses, the lock and every system keep their rules. What changes is what scales with area and crowds.
- **A region is a map to the code.** It's one `gamedata.Map` and one instance per channel. Areas, towns and waypoints sit inside it.

## 2. Today, against a 1 km region

What grows with area or crowds, found in the code before step 2; step 2a has since bucketed the movement row and built nav at boot (section 5). A region at 0.5 m cells is about 4 M terrain samples, against about 11.5 k for one of today's maps.

| Where | Today | At 1 km, 150 players, 400 mobs |
| --- | --- | --- |
| **Frames and events** (`instance/frames.go` `broadcast`) | Every changed entity and every event goes to every player on the map; the server encodes one frame for all (`push/frames.go` `shared`) | Every player gets every fight in the region |
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
size: [1024, 1024]      # whole 128 m map tiles, so whole 64 m chunks
cell: 1                 # terrain samples a metre apart, dividing the overview's 8 m; detail comes from solids and set pieces
seed: 11
look: {sky: day}
ground: {noise: {scale: 220, amplitude: 7, octaves: 4}}
edge: {width: 72, height: [35, 60], biome: peaks}   # the ring of mountains, coloured as its own biome: a cliff, then rising by these
colour: {slope: 0.25, high: 30}               # where flat ground's colour turns to slope's, and the height high's starts
features:                                     # applied in order
  - {ridge:   {path: [[x, z], ...], width: 60, height: 30}}
  - {plateau: {area: [[x, z], ...], height: 8, edge: cliff}}   # or ramp
  - {river:   {path: [...], width: 14, depth: 3, fords: [[x, z]], banks: gentle}}   # its path runs downstream; banks steep (the default) or gentle
  - {lake:    {area: [...], depth: 4, banks: gentle}}
  - {road:    {path: [...], width: 5}}        # flattened, dirt-coloured, kept clear
areas:
  - {id: ..., name: ..., area: [[x, z], ...], levels: [1, 4], biome: meadow, kind: hunting,
     boss: {at: [x, z], radius: 6, mob: ...}}
  - {id: ..., name: ..., area: [...], biome: meadow, kind: town}
camps:
  - {area: ..., name: ..., at: [x, z], radius: 8, mob: ..., max: 5, respawn: 30s}
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
    colours: {flat: grass, slope: dirt, steep: stone, high: stone}   # kit grounds
    scatter:
      - {model: tree_oak, spacing: 9, slope: 0.4, away: {road: 4, water: 1, camp: 3, town: 0, waypoint: 2}}
      - {model: rock_large, spacing: 30}
```

- **Loader rules,** beside today's map rules:
  - Areas don't overlap, and every point of the region is in exactly one area, or the edge.
  - A hunting area has levels and a boss. A town has no camps and no boss.
  - Every camp's mob level lies in its area's band.
  - Every waypoint, NPC, gate and the start stand on walkable ground.
  - Every gate links both ways.
  - Every area, waypoint, NPC and gate can be reached from the start: the reachability test of today (`TestEveryShippedMapCanBeCrossed`) is run over the region's nav.
- **As built (step 4b-1),** `load/regions.go` generates each plan in `data/regions` at load and reads it as a map of kind `region`: its gates are portals, its camps spawns and its start the player spawn, so the maps' spot and link rules apply to it. On top of them:
  - **Areas** are judged on 4 m squares (`regiongen.AreaCell`), each held by the area holding its south-west corner, by the generator's rule for a sample in a polygon. Two areas over one square are refused, and so is a square inside the edge in none. `AreaGrid.Holding` is the lookup step 4b-4's areas read.
    - **The squares are what plays.** An area's east-west and north-south edges must lie on the 4 m lines, so it plays where its plan draws it. An edge off them would play up to a square past it, to the north or east: the test town's (90–150, 60–110) played as (92–152, 60–112) until it moved onto the lines.
    - **A corner on a line** goes to the area east of it, and on an east-west line to the area north of it. So two areas sharing an edge, slanted or not, hold each square once, with no gap. A slanted edge plays up to a square from where it's drawn.
  - **The edge** is whole 4 m squares wide, so the ring's face lies on their lines and no square is half inside. A region's size is whole 128 m tiles, so whole squares too.
  - **Areas' own:** ids unique and a name each. A hunting area has levels, 1 ≤ lowest ≤ highest, and a boss mob, at or above its top level, its circle as wide as its body, on ground it fits, and wholly in the area: every square the circle overlaps is the area's (`AreaGrid.Under`), so a boss never wanders into a town, another band or the ring. A town has no levels, no boss and no camps. Each area has its own boss; a region's bosses aren't checked against the maps', since the test region shares theirs.
  - **Camps** name a hunting area, their circles wholly in it as a boss's is, so no mob spawns past its band. Their mobs are known, no boss, in its band, an elite only beside a normal mob, as on a map.
  - **Waypoints** have unique ids, and each one's arrival beside its stone is where a player fits. Gates, NPCs and the start are checked as portals, NPCs and the player spawn are. None of them, nor a waypoint, stands in the ring.
  - **Camps keep clear of the stones** (from the waypoint stones' playtest). There, the test region's spore slimes, their circle 13.7 m from the North Field stone, followed a player to it. They hit them every 1–2 s, and each hit cancelled the 3 s teleport. Players come to a stone to find it, land beside it and get up there after a death, so no chase should follow them there.
    - **Stones:** every camp's and area boss's circle keeps the mobs' `leash` and 4 m (20 m today) from each waypoint's stone. The arrival lies 2 m from the stone, so a player there stands 2 m past where any mob after them gives up.
    - **Why the leash, not a mob's reach:** the leash is measured on the target. A mob drops a player once they're `leash` past its circle's edge, wherever it stands and however far it spits, so a longer reach adds nothing.
    - **The start, each gate and each NPC** keep a mob's `sight` and 2 m (11.375 m today), the old maps' calm rule (3d-world.md section 4), so no mob sees a player arrive or talk. A gate hands a player off as they step in, so a chase to one costs nothing, and a town's NPCs are already out of sight.
    - Both numbers are read from mobs.yaml, so retuning either keeps the rule true. The loader refuses a plan that breaks it, naming the camp or boss, the spot and the gap. A place below keeps it too, from its gates and waypoints.
    - **The test region** moves its spore slimes and moths by North Field, and its glow slimes and snail by East Woods, out to keep it.
    - **As built:** the loader checks it for every region and every place below, after the spot checks, so a camp over a gate still says it overlaps the portal.
      - **The two distances** are `leash + 4` from a stone and `sight + 2` from the start, a gate or an NPC, from each camp's or boss's circle's edge. Exactly on the line passes.
      - **The error** names the camp or boss, the spot and the gap: `camp "spore_slimes"'s circle comes within 13.68 m of waypoint north's stone at [118 150]; it keeps 20 m, a mob's leash and 4 m more, so no chase follows a player there`.
      - **The test region's camps,** each gap measured from the circle's edge to its stone:

        | Camp | Was | Now |
        | --- | --- | --- |
        | spore slimes | [100, 155], 13.68 m | [98, 172], 24.73 m |
        | moths | [140, 150], 17 m | [145, 165], 25.89 m |
        | glow slimes | [215, 100], 19 m | [216, 114], 23.53 m |
        | snail | [170, 100], 16 m | [168, 116], 23.20 m |

      - Each stays in its own area, off the river, the lake and the plateau, and `TestEveryShippedRegionCanBeCrossed` still reaches it. The North Field boss already kept 20.08 m, just over the line.
  - **Gates** lead to a map's portal or a region's gate, never to themselves, and the two lead to each other, judged from both sides.
  - **The region** isn't named as a map is, has a name, and isn't one block holds (`gamedata.Interest.Whole`, which the instances read too).
  - Every waypoint, gate, NPC, camp and area boss is reached from the start and each gate, at every speed: `TestEveryShippedRegionCanBeCrossed`.
- **`world.yaml`:**
  - `places` goes for regions: the World tab is a chart of three regions, by `regions: [{id, at}]`.
  - The old maps keep their places until they're retired.
  - As built (step 4b-1), a place for a region is refused, and none is asked for: no region is charted yet.
- **Quests, NPCs and bosses** name an area where they named a map: kill objectives count kills in an area, and `npc.MapID` becomes the region with its spot.
- **As built (step 4a),** the generator checks what it needs to build a plan, strictly as every data file is read: an unknown key is an error. The rules above, which judge how the parts fit together, come with the loader in step 4b.
  - **Limits,** so a bad plan is refused before it can run out of memory or reach a float-to-int conversion that CPUs disagree on. Each error names its limit.
    - **The grid:** a cell is a power of two from 0.25 to 8 m. Each side is whole 128 m map tiles, up to 8192 m, with at most 2²⁴ samples.
    - **Places:** every point of a path or polygon, every spot and every house lies in the region.
    - **Sizes:** widths, radii, spacings and away distances are up to 512 m, and heights and depths within 1000 m.
    - **Scatter and noise:** scatter is at least 1 m apart. The noise's finest octave is at least a cell across.
    - **Never zero:** a plateau's height is never 0, since its ramp's width is in proportion.
    - **Areas:** at most 32,767.
    - **The kit's solids** are held to the same sizes.

### The kit: `server/data/kit.yaml`

- **`grounds`:** each ground colour by name, `'#rrggbb'`. It's the only place they're written.
  - Plans colour with them.
  - The bake carries them in its palette.
  - `npm run gen` writes the client's ground table from them, which today's maps paint with.
  - A plan's kit must have `dirt` (roads), `sand` (beds) and `water` (on the map, over beds).
- **`models`:** each model's solid, the one every scattered or set piece of it stands with:
  - a cylinder `{radius, height}`;
  - a box `{width, depth, height}`, its width across its yaw;
  - `{}` for looks only.

  The sizes are those today's maps give each model's solids.

### The generator: `internal/shared/regiongen`

- **Deterministic from the plan and its seed.** The steps:
  1. **Ground:** OpenSimplex noise, with octaves.
  2. **Features,** in order:
     - ridges along their paths;
     - plateaus with cliff or ramp edges;
     - rivers and lakes carved, with fords left shallow;
       - **As built (step 4b-2a):** once the edge is raised, a sample still under a river or a lake whose water stands more than 0.6 m over it, twice a ford's depth, is marked deep (`sim.Terrain.Barrier`, per sample, named `DeepWater` until a cave's rock took the mark too). Roads, towns and the edge reshape the ground after the water, so the marks are judged on the final heights: a causeway or a town over a river is dry, and a bank's edge is wadeable.
       - **As built (gentle banks, large regions step 8):** a river or a lake takes `banks: steep | gentle`; an unknown value is refused, and the default is steep, the carve above, so the test region and its bake hashes are as they were. Gentle rounds the whole water: the ground beside it is held under a cone that rises from the waterline at the ford's ramp, 0.5 m a metre, so a player who walks down to the water walks straight back up. A river measures along its smoothed path, from the water's edge at half its width, at the level of the nearest path point; a lake from its edge, at its level. Nothing under the water changes: the bed and the steep fall below wading depth stay, since deep water blocks anyway. A ford inside it keeps its own ramp, the lower of the two. The cost is a wider, valley-like river where the ground stands high, up to the region's highest ground over the waterline, its banks coloured as dirt by slope. Tests: the ground beside a gentle river and a gentle lake stands under the cone on a ramp up to a plateau, and no dry cell near the river is steep; the bed and the water are as steep banks leave them; a gentle river and lake on a slope leave no one-way pocket where the same plan's steep lake leaves them (nav's `TestNoRegionHasAOneWayPocket`; a steep river alone didn't strand a cell in any plan tried, so the river's cone is pinned by the generator's tests); an unknown `banks` is refused.
       - **Order matters to gentle banks:** the cone is applied inside `river()` and `lake()`, so a feature laid after one, a town's flattening or the edge, can raise the ground beside the water again. A plan lists its rivers and lakes after its ridges and plateaus, as Willowmere's does.
     - roads flattened to their mean height along a smoothed path.
  3. **Towns:** each town area flattened toward its mean.
  4. **Edge:** the mountain ring, raised last so no feature or town opens it: a cliff 10 m over the highest ground inside the ring at its inner face, then rising smoothly by between its heights to the sides, noise choosing where.
     - **As built (step 4b-1):** the ring was a smooth rise added before the features. Where its noise picked its lower height, the whole slope stayed under the walkable 0.7, and from the test region's start a player reached 21,700 m² of the ring, up to the border.
     - **Why 10 m:** the highest a player gets is about 6.7 m above the ground: 4.5 m up a pillar, the kit's tallest solid that jumps from one top to the next climb to, then a jump and a 300 ms leap at its peak. A blink, a dash or a leap alone adds no height. The cliff is measured from the highest ground inside, so a ridge or plateau beside the ring can't beat it.
     - **Tests:** nothing the walk grid reaches from the start lies in the ring (`TestNoPlayerReachesARegionsRingOrDeepWater`). On each side of every region, and of a flat one whose ground is all as high as at its face, a body at the face fires a jump and every skill's move, from the ground and the climbed 4.5 m, now or at a jump's peak, and gets no farther in than the cliff's own cell (`TestNoMoveGetsOntoARegionsRing`). A 6 m cliff fails it.
  5. **Steepness,** as `sim.Terrain` marks it today.
  6. **Colours:** each sample gets a biome colour, by its area and its height and slope: flat, slope, steep and high. Roads take dirt, river and lake beds sand.
  7. **Pieces** are added as placed.
  8. **Scatter:** each biome's models are scattered with Poisson-disc spacing, kept off roads, water, camps, towns, waypoints and steep ground by their `away` distances. Each placement becomes a prop, and its model's solid becomes a cylinder or box.
- **The same bits on every CPU,** by 3d-world.md's rule for the sim:
  - **Allowed math:** only + − × ÷, `Sqrt`, `Floor`, `Ceil`, `Trunc`, `Abs`, `Min` and `Max`, each product rounded on its own (`float64(a*b)`). Smooth rises are polynomials.
  - **Randomness:** only a seeded PCG's `Uint64`, its 53 high bits making a float.
  - **Maps:** no map's order reaches the output; every map is read in sorted order.
  - **Enforced** three ways:
    - a lint rule forbids the rest of `math` and `math/rand` in the package;
    - `make fma-check`, run by CI on arm64, disassembles the compiled `sim`, `mapfile`, `nav` and `regiongen` and finds no fused multiply-add. It fails if the build fails or none of a package's functions is disassembled;
    - the golden hashes match on amd64 and arm64.
- **Every number the bake stores is a float32's,** rounded by the generator itself: heights, solids, props, river levels. So the server keeps exactly what the client reads.
- **Noise:** OpenSimplex 2D, ported from [ojrac/opensimplex-go](https://github.com/ojrac/opensimplex-go) with every product rounded (section 16).
- **Poisson disc** (Bridson's method) and the polygon and path helpers are in-house.
- **As built (step 4a):**
  - **Ground:** each octave has its own seed (the plan's plus its number), half the last's scale and weight. The sum is scaled to within about ±`amplitude`.
  - **Edge:** the rise is a smoothstep from nothing `width` in to the sides. Its height lies between the two, picked by noise at the width's scale.
  - **Ridge:** `height × (1 − d²/r²)²` at `d` from its path, `r` half its width.
  - **Plateau:** `height` is added over its area.
    - A cliff falls over 2 samples, too steep to walk.
      - **As built (land cliffs, from #400's review):** a steep face is walked up slantwise, so a raised cliff's face is a barrier, as a cave's rock is. Each sample on it carries the mark, unless one of the four cells round it is no longer steep on the final ground, as under a road onto the plateau or by a ford's ramp.
      - A road onto a cliff plateau eases its face and stays open, the only way onto its top, and a pit's cliff marks nothing. Nobody drops off a cliff: the barrier stops a body at the top's edge too.
      - Banks, fords, ramps and the ring keep their steep marks only. A cell steep along one axis made steep along both was tried instead and dropped: it closed the wide plan's ford at (700, 540) and left two one-way pockets by the test region's river.
      - **Test:** over every plan, a player from the start, walking or jumping, reaches no cliff plateau's top but by a road onto it, and does reach it by one (`TestNoPlayerClimbsACliff`).
      - **Only the road:** with the ramps of the roads onto a plateau taken out (every cell of such a road's footprint within 20 m of the face), a player from the start or any gate not on the plateau, walking or at the fastest jump, reaches no cell of its top (`TestAPlateauIsReachedOnlyByItsRoads`). A plan whose plateau has a second way up fails it, and a fixture with a stair against a face proves it can.
    - A ramp falls at half `max_slope`, walkable.
  - **River:** its path is smoothed (Chaikin's method, 3 passes).
    - **Water:** 0.5 m below the lowest ground along it so far, so it never rises downstream.
    - **Bed:** `depth` below the water.
    - **Banks:** `min(width/4, 2 m)` wide, too steep to climb at full depth.
      - **No one-way pockets** (nav's `TestNoRegionHasAOneWayPocket`, from 4b-2b's playtest): every cell a player reaches from a shipped region's start leads back to it, walking and jumping. The test region's banks pass, though most are steep going away from the water, since walking along them reaches a ford or lower ground. A walled pit fails it.
    - **Fords:** within half the width plus a bank of a ford, the bed is 0.3 m below the water and its banks are walkable.
      - **As built (the ford ramp, after 4b-2b):** the banks alone weren't. The water sits under the lowest ground seen yet, so where the ground rises downstream a ford's banks stood a metre or two over its bed. The tiny test plan's rose 1.45 m in a 2 m cell, and one of the wide plan's two fords was as steep: only a jump got out, and no mob followed. So the ground round each ford is held under a cone, flat at the ford's floor and rising 0.5 m a metre (under `max_slope`'s 0.7) until it meets the ground. It lowers ground at most 22 m from a ford, the river's banks elsewhere stay steep, and in every plan it changes no deep-water mark.
      - **As built (a road through a ford):** a road laid after the river once flattened its corridor to the land's mean, a dry causeway over the ford whose shoulders fell square to the water (the test region's road crossing, 11-25). A road whose path crosses a ford's disc now stays under that ford's ramp, so it wades the ford, the floor still 0.3 m under the water; one that only passes a ford bakes as before. In a ford it crosses, a road's sample is wet where its ground is under the water, as the wide plan's road ending in its ford now is; everywhere else a road over water is a dry causeway, as it always was. Tests check every ford of the test plans and shipped regions holds water over its middle, wet and not deep, and no road sample is a barrier.
      - **On the river:** a plan whose ford lies more than half the river's width from its smoothed path, the path the generator carves, is refused, as the ramp takes its floor from the river's samples near the ford.
      - **The gate** (nav's `TestEveryFordIsCrossedBothWays`): every ford of every shipped region and test plan is walked across both ways as a mob walks, with no jump, on a path held near the ford, so a causeway elsewhere doesn't count.
    - **Its ends lie in the ring** (from 4b-2b's playtest): a plan whose river ends short of the ring's face is refused. The carve stops at a path's end, and the ring is raised after the water, so the test region's river, ending 6 m short, left a dry strip a cell wide at the cliff on each side, and a player walked across there. An end in the ring has the deep band meet the cliff.
      - A river may end in a lake when a plan needs it: relax this rule then, and the crossing gate still holds.
      - **The gate** (nav's `TestARiverIsCrossedOnlyAtItsFordsAndCauseways`): in every shipped region and test plan, a player beside each ford reaches the other side, walking and jumping, and reaches nothing past it once the cells of the river's fords and causeways are taken out of the walk grid. With the old ends it fails on the test region.
  - **Lake:** its water is 0.5 m below the lowest ground along its edge, its bed `depth` below that, between banks `min(depth, 2 m)` wide.
  - **Road:** its path is smoothed.
    - **Level:** each point's level is the mean of the ground within 15 m along the road. The road is level across its width.
    - **Shoulder:** as wide as the road, easing back to the land by smoothstep.
    - **Over water:** across water, a road is a causeway, dry.
  - **Town:** flat at the mean of its ground, easing into the land around over 12 m.
  - **Colours** are chosen in order:
    - a road's dirt;
    - a bed's sand;
    - steep, where a cell touching the sample is marked steep;
    - high, at or above `colour.high`;
    - slope, past `colour.slope` across the sample's neighbours;
    - otherwise flat.

    Outside every area, the edge's biome colours. Where areas overlap, the first holds the ground.
  - **Scatter:** set pieces go first, then each biome in name order, entry by entry.
    - **Points:** the disc covers the box of the biome's ground. Each new point is tried in the ring `r` to `2r` round an active one, up to 30 times, found by rejection in a square, so no angle is taken.
    - **Ground:** a point is kept on its biome's walkable ground, no steeper than `slope`.
    - **Clearances:** a point is also clear of each road, water, camp, town and spot by its `away` distance plus the model's reach.
      - The spots are waypoints, NPCs, pieces and the start (2 m each), and gates (their radius).
      - Clearance is judged at the nearest sample.
    - **Solids:** no solid comes within 0.5 m of another's reach. A box's reach is its half diagonal.
    - **Yaw:** whole degrees. A model with a box solid turns by quarters, so its axis stays exact.
  - **Measured:** `testdata/wide.yaml` is a 1 km plan laid out as a real region: 21,244 props and 8,535 cylinders. On the reference Mac it generates in 0.25 s, and its walk grid takes 1.0 s more. CI's arm64 job prints the generation's time in each run, the bake included: 0.48 s for it and the 256 m plan together, on #291's first run.
  - **Boot budget:** a 1 km region is generated and its grid built within 3 s, and all three regions within 10 s. Past that, the server reads the bake rather than generating (below).

### The bake: `cmd/regionbake`

- **The server generates; the client reads the bake.**
  - **Server:** builds each region from its plan at load, once per process. Every channel shares it, and its walk grid is built as the world starts, as the maps' grids are. Nothing is built on a tick.
  - **Client:** reads the bake, which the same generator writes.
  - **Why the ground matches:** the generator gives the same bits on every CPU. That's the guarantee today's maps have.
  - **This reverses the plan** (section 15, call 11), which had both sides read one bake. Generating on the server needs no artifact to build and ship with it, and the server can't run on a bake older than its plan. Generation is cheap, and the exactness was needed anyway, for the golden hashes to match on arm64.
- **Where it's built:** `npm run maps` and the web image's build write it to `web/public/regions/<id>/<hash>/`, a build artifact, not committed.
  - `<hash>` is the first 16 hex digits of the bake's hash. From step 4b the server names the hash it built, so a client never reads a bake older than its region.
    - **As built (step 4b-1):** the loader bakes each region it generates and keeps the folder's name as `gamedata.Map.Bake` (`regiongen.Bake.Folder`, which regionbake names its folder by too). `cmd/mapjson` writes a region's JSON from it: its sky and gates, and `region: {id, hash}`, with no models, since the bake draws its solids and props. From 4b-2b it has no geometry either: the client's sim reads the ground and solids from the bake's chunks (section 10).
    - **Measured** on the reference Mac at 1 km: generating takes 0.32–0.38 s and baking every file 46–48 ms, so the full hash is named at boot. The 256 m test region takes 12 ms and 2 ms, and its walk grid 68 ms, built as the world starts.
  - A new bake clears the old ones. Each is written to `<hash>.part` and renamed whole, so a crash leaves nothing that looks complete. The next run clears it.
- **The files,** all little-endian. A run of float32s starts on a 4-byte boundary, so a reader can view it in place:

  | File | What | Size at 1 km |
  | --- | --- | --- |
  | `region.json` | The format, name, sky, size, cell, chunk size and grid, overview spacing, and height range; how many cylinders and boxes the region has, which chunks index into, and the farthest any reaches from its centre; the palette (each ground's name and colour) and the model names, which chunks index; each river's path, width and levels and each lake's area and level; and the plan's areas, camps, waypoints, NPCs, gates and start | 6 kB |
  | `chunks/<cx>_<cz>.bin` | `FRCK`, the format, its x, z and 65 samples a side, its edges shared with its neighbours; then 67 × 67 heights (float32), its own with a border one sample wide held to the region at its edges, 65 × 65 colour indices and 65 × 65 deep-water marks (1 or 0), row by row from the south-west; then the props (model, x, z, yaw), cylinders (index, x, z, radius, top) and boxes (index, x, z, half width, half depth, axis, top) whose centres it holds, each solid's index its place in the region's list | 29 kB, 17 kB gzipped |
  | `overview.bin` | `FROV`, the format, its samples along x and z and its spacing; then heights and colour indices every 8 m, for the far land | 83 kB, 61 kB gzipped |
  | `tiles/1/<x>_<z>.png`, `tiles/4/<x>_<z>.png` | The map image in 128 m squares from the south-west, at 1 and 4 m a pixel: each ground's colour shaded by its slope toward the north-west light, water over beds, solids dark | 450 kB in all |

  - **Steep marks aren't stored:** both sims mark them from the heights by the same rule.
  - **Format 3** (step 4b-2b) added the deep marks, the solid indices and `region.json`'s solid counts and reach, for the client's sim. Format 2 added the heights' border.
- **Golden hashes:** `testdata/regions/bakes.sha256` keeps the bake hash of each test plan and each region, so a change to the generator shows in review.
  - The hash covers our own bytes and the tiles' pixels, not the PNG encoding, so a Go upgrade can't change it.
  - It runs on amd64 under `-race` alone, as the coverage run leaves it out (19 s there for nothing more covered), and plainly on arm64.
- **`make region-preview`** draws every plan in `data/regions`, or `PLAN=path/to/plan.yaml`, to `server/bake/preview/<id>.png`, for designing a region:
  - the map image scaled up toward 1024 px;
  - each prop a dot;
  - area outlines in white, camps red, bosses orange, waypoints cyan, gates magenta, NPCs yellow, the start green.

## 4. Region instances and channels

- **A region is a `gamedata.Map` of a new kind, `region`.** Its terrain, solids, nav and areas come from the bake, built once and shared by all its channels.
- **Channels are instances made on demand,** with the same machinery as PvP match instances (pvp.md section 4).
  - `w.instances` keeps the fixed maps.
  - A region's channels live in a registry under its own mutex, keyed by region and channel number.
- **Joining:**
  - A player goes to the least full channel under `world.channels.cap` (150, in config/config.yaml).
  - A party member's channel comes first when it has room.
  - When every channel is full, a new one opens.
- **Closing:** a channel past the first with nobody in it for 5 minutes closes.
- **Switching:**
  - The map window shows "Channel n" with a list. Switching is a 3-second channel, cancelled as a teleport is (B9), to the same spot in the other channel.
  - B4 becomes the region's channels.
- **Region changes** go through a gate (the talk key in its ring, as a portal) or a teleport to another region's waypoint. Each is a hand-off as today, to the new region's least full channel.
- **As built (step 4b-1),** a region runs as one instance in `w.instances`, as a map does, until channels come. It's its own town: one who dies in it gets up at its start, in it, and pays the death's EXP, as on a hunting map. A teleport to its id landed at its start, until step 6b reached a region by its waypoints alone.
- **As built (step 4b-3a, the server):**
  - **Channel 1** is the region's instance in `w.instances`, and never closes. The World opens the rest as they're needed, under the runner matches use (`opened`), each under its own cancel so one can be shut alone. An instance knows its number (`Instance.Channel`, 0 off regions) and counts its players in an atomic.
  - **The registry** (`world/channels.go`) holds each region's open channels under one mutex, with `pending`: the hand-offs picked for a channel that haven't landed. A channel's load is its players plus its pending, read under the lock, so two hand-offs at once never both take the last place.
  - **A pick,** for every join and every hand-off into a region (a relog, a gate, a teleport, a dead player's join): the channel asked for while it has room; else the one holding most of the player's party with room; else the least full with room; else a new one with the lowest free number. Ties go to the lowest number.
    - Back from a match, the return point keeps the channel the player left, and the way back asks for it, so a party that queued side by side comes back side by side while it has room.
    - A gate below a region (B13) will ask for the player's own number the same way, through `instance.Dest`.
    - A channel's number isn't saved: a relog picks again.
  - **The cap,** `world.channels.cap` (150), is soft on a switch alone: a switch checks for room as it starts, and lands even if the channel filled in its 3 s. Joins, gates and teleports never pass it.
  - **Closing:** every 10 s a sweep takes off a channel past the first that it has found empty, with none on their way, for `world.channels.idle` (5 min), then shuts it. So none is picked while it closes, and it closes 5 to about 5⅓ minutes after its last player left. A switch's start restarts its target's idle clock, so it can't close under the switch.
    - A closing channel holds no pouch or box: they last 60 s and 2 min ([Loot pouches and timed bosses](./loot-pouches.md)). Its area bosses go with it, and a channel opened again with that number starts with them up, as any new channel does.
    - One who dies in a channel gets up in it, as in 4b-1: no hand-off.
  - **Switching** (`SwitchChannel{channel}`, client message 39) is refused off a region, to the player's own channel, to one not open and to a full one (`NOT_A_REGION`, `SAME_CHANNEL`, `NO_SUCH_CHANNEL`, `CHANNEL_FULL`, 330042–45). Otherwise it runs on the teleport's channel: the same 3 s and the same cancels (`TeleportCancelled`), then a hand-off at the player's own spot and facing. It shares the teleport rate limit.
  - **The list** (`Channels`, client message 40) answers with each open channel's players, those on their way included, and the cap, in `Response.channels`. The client turns players against the cap into words. It has its own limit, one a second with a burst of 3.
  - **The client always knows its channel:** `Snapshot.channel` (8) comes with every join and map change. A hand-off's `MapChange` goes out before the channel is picked, so it carries none.
  - **The party frame:** `PartyMember.channel` (9) comes from the member's map through the social module. A hand-off tells 0 until the landing's join tells the channel picked.
  - **Metrics:** the tick metrics gain a `channel` label (0 off regions), and `finga_channels_open{region}` counts the open channels. The dashboard splits ticks by channel, and cmd/bot's report keys a region's channels apart.
  - **Tests:**
    - the pick's rules and the sweep's clock, as units;
    - through a running World: joins filling channels, a party's channel first then the least full, a teleport in, an idle channel closing and its number opened again while the first stays, a death in channel 2 getting up there, the four refusals, a switch landing at the same spot, another cancelled, and a cancelled switch keeping its channel open;
    - a match from a channel going back to it;
    - through the server, both messages with their arguments and codes;
    - one e2e through the stack, with two a channel: the third player in channel 2, the list, and a switch landing among channel 2's players, who see the player come.
- **As built (step 4b-3b, the client):**
  - **The channel** the latest Snapshot named is kept in `state/map-view.ts`, 0 off regions.
  - **This map** in a region shows "Channel n" over the open channels. The list is asked for as the tab shows, and every 5 s while it does.
    - Each channel's load is in words: quiet under half the cap, busy below it, full at it.
    - Each has a Switch, except the player's own, a full one, and every channel for a dead player.
  - **A switch** runs on the teleport's bar, "Switching to Channel n". A cancel says "Channel switch cancelled: …", even when the key that cancelled it took the bar down first.
  - **Its MapChange** names the same region, and the client builds it again under the veil, as for any map change, so the old channel's players and mobs go with it.
  - **Names:** the minimap is captioned with its map's name, plus "· Ch. n" in a region. The party window says "Test Region · Ch. 2".
  - **Same map:** a party member in another channel of the region isn't marked on the map views, or counted as hurt for a heal. The party's own word of their channel decides it, before the server's spots do.
  - **Not done:** the world chart's party names carry no channel, since regions aren't on the chart yet.

## 5. The server: what each player is sent

### Interest cells

- The region is cut into **32 m cells.** A player watches a block of 7 × 5 round their own: 3 columns each side, 3 rows ahead (north) and 1 behind, since the camera always faces north. That's at least 96 m to each side and ahead, and 32 m behind.
- **Each tick, each cell's changes are encoded once** as a `Frame` part:
  - its players and mobs that changed;
  - those that arrived in it, as full views.
- **A player's frame** is their watched cells' parts, plus their private part (`AckSeq`, energy, their own pouches).
  - Protobuf merges repeated fields when messages are concatenated, so the server joins the encoded parts in place of decoding them.
  - So the "encode once" saving of `push/frames.go` holds per cell.
- **Coming into view:** when a player's block changes (they moved cells), or an entity crosses into a watched cell, the player gets the entity's full view. Cells no longer watched give `Unseen{ids}`. The client drops what it no longer sees.
- **Events by place:** `Damage`, `SkillUsed`, `AttackUsed`, `MobAttacked` and status events go to the watchers of the cell where they happen.
  - The private ones stay private.
  - Region chat, the boss's notices and its bar go to the whole channel.
- **The join snapshot** is the block around the player.
- **Party:**
  - Members out of view still show on the party frames.
  - Their spot reaches the minimap through `tellParty` once a second, in place of on map changes alone.
- **PvP and the old maps** run the same code. A map smaller than a cell is one block, so nothing changes for them.
- **As built (step 1a):**
  - **Data:** `world.yaml`'s `interest` holds the cell and the block: `behind: 1`, `ahead: 3`, `side: 3`.
  - **The loader** checks that nothing reaches past the rows behind (`behind × cell`): mob sight, every mob attack, every class attack and every skill's shape. Each mob is read with its own body and lengths, so an elite's or a boss's size counts as it is. Today's longest is Arrow Rain's 17 m.
  - **A map one block holds from every cell** is sent whole: one part for everyone, the events to everyone, and no cells kept. That is a map whose columns, less one, are at most `side`, and whose rows, less one, are at most the smaller of `behind` and `ahead`: at 32 m cells, at most 128 m wide and 64 m deep. Every shipped map and arena is one today; a region of ~1 km is always cut.
  - **On a larger map, each tick** the instance files every changed body in its cell. Each cell with changes gets one part: its changed views, the bodies that came to it, and the removals of those that left the map from it.
    - A body leaving the map is filed in the cell the last frame left it in, so every player who saw it hears.
    - A mob killed on the tick it spawned comes with its view and its removal where it stood.
  - **A full part** (every body in the cell) is made the first time in a tick that a player's block reaches a cell. A player gets a cell's changes part only if their block already held it.
  - **Each player keeps what they've seen.** A body that leaves their block, or whose cell their block leaves, comes in `Delta.unseen`. The client drops it with no death: a mob goes as it stands, a player as one who left.
  - **The field boss** also goes in the map's own part, which every player is sent: `Delta.boss`, and `Snapshot.boss` on a join. The client feeds the boss bar from it and never draws from it, so the boss is drawn once its cell is in the block.
  - **The server** encodes each part once, and keeps a frame's parts while the frame lives (step 1b). A player's message is their parts' bytes and then their own fields: ack, energy, `unseen` and their pouches. JSON merges the parts instead.
  - **Events** go to the watchers of either end: the source's cell or the target's. A mob attack's target is where it lands. A shot into a block shows its hit there, and a shot out of it the shooter's swing. Deaths, respawns, chat, the boss's notices and matches go to the whole map.
  - **A map nobody is on** isn't cut. The first player to come files every mob again, as some may have come or gone, and nobody is told of those who left before.
  - **The client** keeps drawing a body that comes back before its removal is drawn past, as at a cell line a player steps back and forth over.
  - **Measured** on an Apple M3, 150 players walking a 1 km map among 400 mobs (`internal/dependencies/interestbench`):

    | Block | World's tick | Its allocations | Bytes to each client | The server, for all 150 |
    |---|---|---|---|---|
    | Whole map | 0.45 ms | 794, 266 KB | 30,100 | 0.35 ms |
    | 7×7 | 0.51 ms | 1,200, 396 KB | 1,550 | 0.25 ms |
    | 7×5 | 0.51 ms | 1,200, 394 KB | 1,120 | 0.25 ms |

    At 20 frames a second, 7×5 sends each client about 22 kB/s, under the soak's 40 kB/s; the whole map would send 600 kB/s.

  - **Today's maps** cost what they did before step 1a, in runs alternating with main on one machine: a 50-player town's tick takes 49.8 µs against 50.9 µs; a frame's encoding for 10, 50 and 200 players is within 4% either way, with 4 more allocations a frame; and the world's copy of it for the server is within 2%.
- **As built (step 1b):**
  - **Party spots** come from the instance, not through `tellParty`, so the social module's party store isn't rewritten every second.
    - Once a second, on a map cut by cells, each player in a party is told a private `PartySpots`: their party members on the map whom they don't see, and where they stand. It goes when it's empty too, so the client hears within a second of a member leaving the map, its channel or the game. A map sent whole tells nothing.
  - **The client** marks a member who isn't drawn on the minimap and the world map, and their party frame says "Out of sight" in place of their statuses, which the frames don't bring; never an empty row. That's only on a map cut by cells, for a member online and on the map by the party's news, and only while the server's latest spots list them. Between their leaving view and the server's next word, it keeps them where last drawn.
  - **The server decides who's here,** not the party's map id: a member in another channel of the map has the same id, and one gone offline keeps theirs.
  - **The side edge:** on a map cut by cells, the client draws a body faded from 88 m to the side, and not at all past 96 m (`side × cell`, which the block holds from anywhere in the player's cell).
    - A body past it has no name, bar, shield or status marks, so nothing pops in and out at the block's sides as the player crosses a cell line.
    - Only a wide screen at the far zoom sees that far: the top corners are 76 m to the side at 16:9, and 100 m at 21:9.
    - A hidden mob's attack still shows its warning area, as the attacks are drawn on their own clock.
    - The lock's marks for enemy players aren't cut at the edge: only the arenas have enemies, and they're sent whole.
  - **The server** keeps each frame's encoded parts while the frame lives, by a weak pointer that a cleanup drops, and the last frame's at hand for its run of players. So the frames of several maps can interleave: two maps of 50 players each take 0.18 ms, against 1.97 ms with one frame kept.
  - **A player whose join failed** never reached a frame, so nobody is told they left.

- **As built (the slim update, protocol 3):**
  - **Why:** the soak's downstream went to the players in view. A `PlayerView` is 138 bytes on the wire (its id, name, class, look and eight doubles), sent whole each tick a player moves; at a camp with 105 other players in the block that is 12 kB of a client's 13 kB a tick, and a mob's 45 bytes add 0.7 to 2.3 kB at 5 to 15 mobs a camp (`BenchmarkACrowdInACamp`: 150 players clumped on six camps, the worst case the cap allows).
  - **A step, not a view:** when a body changed only in where it is and how it moves, its frame carries a `PlayerStep` or `MobStep`, which names it by a number and gives each channel as a small whole number against the last view the client holds of it.
    - A player's step: its `slot` (a number the map gives each join and never reuses, in place of the 36-character id), the place as whole centimetres from its last place (`sint32`, so a small move is a byte or two), and the fields that differ: velocity in mm/s, heading in thousandths, `on_ground`, `hp`, `sprint`, `held`, `aim`. A field that didn't change is left out.
    - A mob's step: its id, the place the same way, and `yaw` in thousandths of a radian, `hp` and `pose` when they changed.
    - **The heading is two numbers still,** its east and north parts in thousandths: the sim keeps them as a unit pair, and one angle would round-trip through `atan2` and `sincos` on every CPU. A walking player's heading rarely changes, so it's mostly absent.
  - **A whole view still goes** when anything a step can't say changed (level, look, a status, a dodge, the shield, the side, the move or slow state), and when the body has no baseline yet. A part a player gets as a cell's full part, and a snapshot, are whole. A body that *came to a cell* goes out as a step in the part of the cell it left, to those who hold it, and whole in an arrival part, one for each pair of cells (from, to) a body came between that frame, to those who see its new cell without being sent the old cell's part (the strip that came into their block with it, and a block that moved off the cell it left that frame). A watcher whose block moved can be sent both, the step in a cell still in their block and the whole view in the cell new to them, so a reader applies a frame's steps to what it held before the frame and takes the whole views after them: a whole view is a frame's last word on a body (`Expander.Delta`, `ViewExpander.delta`).
  - **The baseline** is the view last sent, kept per body and moved by every frame, step or whole. A view handed out whole to one player (a join's snapshot, a cell's full part) never moves it: a view that differs from the baseline drops it instead, and the body's next frame goes whole to everyone, so a change the join showed and the frames did not (a hit guard the clock ended) is not left out of the watchers' next step. The client's is the same view: its held place rounded the same way, so a step lands the same on both. Positions are rounded `floor(v × 100 + 0.5)`, in Go as in JavaScript, so the two agree to the bit on every CPU (shared vectors in `server/testdata/quant.json`, read by `quant_test.go` and `expand.test.ts`).
  - **Units,** and why: whole centimetres for a place, in a `sint32`, so a step's small delta takes one or two bytes where a `float32` takes four and a double eight. A centimetre is below what a frame can show: the client interpolates between ticks that are 25 cm apart at a sprint. Velocity in mm/s and a heading in thousandths are the same trade.
  - **Exact where it matters:** the server keeps its own state as it is and quantises only the wire. A player's own view is sent whole, exact, in their private part (`Delta.players`), once: it is left out there when the view already went whole in one of the parts they are sent. Each tick a player moves that is about 140 bytes, 2.8 kB/s of a client's budget, counted in the bench's numbers below. The client reconciles its prediction against the same numbers as before; it ignores its own steps. What a client holds of others is within half a centimetre. Everything a rule reads is the server's exact state, never the view.
  - **The client** (`protocol/expand.ts`) and the bot and e2e kit (`protocol.Expander`) turn each frame's steps back into whole views against the last held, so the rest of each reads `Delta.players` and `Delta.mobs` as before. A body that goes (`removed`, `unseen`) drops its baseline. No library: protobuf's own `sint32` and optional scalars are the codec, and the rounding is one line.
  - **Measured** on an Apple M3, 150 players clumped on six camps, 105 in a client's block (`BenchmarkACrowdInACamp`):

    | Mobs a camp | Bytes a client a tick, before | After | Players, before | After | Mobs, before | After | At 20 Hz, after |
    |---|---|---|---|---|---|---|---|
    | 5 (15 in view) | 12,973 | 1,542 | 12,212 | 1,394 | 744 | 130 | 30.8 kB/s |
    | 15 (48 in view) | 14,496 | 1,860 | 12,212 | 1,376 | 2,316 | 467 | 37.2 kB/s |

  - **Against the budget:** the line is now **40 kB/s with a 150-player crowd in view** (105 others in a client's block, the bench's), where it was met at 259 to 290 kB/s before. The bench is steady state: every player walks, none fights, and a fight's statuses and hits add whole views and events. The soak decides whether the busiest second stays under; if it doesn't, the next lever is distance tiers (a body past the near cells at a lower rate), a PR of its own, and a cap on the players shown is a product call.
  - **Judged on the walking clump:** the first soak with 150 bots (item 30) had every bot's busiest second at 61 to 64 kB/s after the join, over the line the bench said was met. The bench's players half stood still and 105 were in a block; the soak's all 150 were in one block walking straight. `BenchmarkAClumpWalking` is that case (150 players joining 4 a tick in a start corner, then all setting off at once toward six camps of 15 mobs), and the budget is judged on it from now on. It is 53.2 kB/s: a client's tick is 1.2 kB of player steps, 0.45 kB of whole views (3 to 5 a tick: each walker comes to a new cell about every 5 s on each axis), 0.87 kB of mob steps. The soak ran about 9 kB/s over the bench, for fights and statuses.
  - **As built, whole views for arrivals only** (the arrival parts above): 44.8 kB/s on the same bench, no whole view sent in the clump, where every watcher already held the body.
  - **As built, a crowd's players step every second tick:** where more than `interest.crowd` (60) players stand in a cell and the eight round it, a player's step that says motion alone (place, velocity, heading) goes out on every second tick, by the parity of the tick and the player's slot, and the next step is the move since the last one sent: the baseline stays where every watcher holds it, so there is no second baseline and no whole view when the tier changes. A test that builds `gamedata.Interest` with no `Crowd` gets 0, which is always crowded: harmless there, since the tier changes nothing it reads. A cell goes slow past the crowd and fast again at 4/5 of it or fewer, so a crowd's edge doesn't flip each tick. Anything more is sent at once, from any tier: a whole view, HP, aim, the sprint's count (so the first two seconds of a walk go every tick), a hold, the ground flag, a removal, an event, and a body that came to a cell. A step that waits keeps its player on the next frame's list, so one who stops on a held tick is sent where it stopped. A view handed out whole to a joiner or a cell's full part shows such a player as last sent, so a join doesn't drop its baseline. Mobs are not tiered: the client predicts a player's own moves against a mob's place each tick, which a mob's missed tick would leave guessed. The client needs nothing: its render clock sits two ticks behind the newest, and a player's newest state is never more than one tick older than that; with wall time moving the render clock, a frame 50 to 100 ms late lets a held player (newest state a tick old) run on the one-tick extrapolation while a full-rate body is still interpolated, so a step shows a tick late and no more. `BenchmarkAClumpWalking`: 44.8 → 37.4 kB/s in the busiest second, which is the start (every walker's sprint count rising, so nothing waits yet); the crowd walking on is 26 to 33 kB/s.
  - **The busiest second is judged after the join:** a join sends the Welcome and a snapshot of everyone in view, whole, about 120 bytes a player: 17.9 kB at 145 players with 20 mobs, 19.7 kB with 60 (the bench, one joiner into the clumped crowd). With a second of ordinary frames on top, a bot's first second is far over a steady one. The soak's first run (150 bots, 3× camps, protocol 3) had a mean of 22.8 kB/s a bot and every bot's busiest second at 52 to 59, which fits that and nothing else about a fight. The bot now reports the busiest second after the first 5 s of its connection, when it fell, and the join's own beside it; the budget's second is the first. Spreading the snapshot over a few ticks was left out: the join is not what a player feels, and it would add protocol for a one-off.
  - **A mob leaves a map only by a kill** (a blow or a poison), which every frame's removed list carries; the baseline of a mob killed while the map is empty has no frame to go with, so an empty map clears its baselines.

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
  - As built (step 2b-1), the bodies a move stops at:
    - The Go sim buckets a step's blockers in 32 m squares: `sim.Blockers`, lent to a geometry by `Geometry.Blocking`. `slide` and a blink's end read only those near the body, in their own order, so a move is a full scan's to the bit.
    - `Set` copies the list, and an index is read only while a geometry's `Blockers` are that copy. A geometry whose `Blockers` were set any other way, or cleared for a move through mobs, scans them.
    - The arena keeps one for the portals and the living players (mob moves), one for the portals alone (leaps, `ahead`), one per kept frame (a player's step) and one for the mobs where they stand (knockbacks).
    - The client steps among the mobs it was sent, as before.
    - A step among 400 mobs in a 1 km region takes 219 ns against 662 ns scanned, and a rebuild 6.6 µs.
  - As built (step 2b-2), the arena's queries:
    - The cell is data: `world.yaml`'s `interest.cell`, 32 m.
    - Players are bucketed by the cell under their centre once a tick, since they don't move during it. Mobs are kept in theirs as they move, each cell's ordered by id.
    - A query reads the cells within its rule's reach and walks them merged in the old order, so every choice, sum and tie comes out as before. The reaches:
      - sight;
      - an attack's (`mob.reach`: its body, its longest bite, leap, rush or slam, or a shot's range), plus the widest player;
      - a swept ball or body, plus both widths;
      - overlap, for `spread`, `yield`, `room` and `crowded`;
      - a strike's shape, plus the widest mob.
    - `yield` asks again after each step out, since the mob's next overlap is from where it now stands.
    - A target or provoker is found by its ID.
    - `hold` still reads every mob. A claim is held beside the player, wherever its mob stands, and only chasing mobs ask, each reading a field or two per mob.
    - Pickups, NPC reach, waypoints, gates and area lookups are the instance's, and wait for its cells in step 1.
    - A tick of 400 mobs and 150 players on a 1 km map takes 0.63 ms, against 1.2 ms reading every body.
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

Built with step 1, which says who watches a cell.

- **A mob sleeps** when no player watches its cell. A sleeping mob:
  - finishes an act or walk home under way;
  - doesn't sense, chase or wander.
- **Camps keep filling while asleep,** so a camp a player walks to is full.
- **Waking:** a mob wakes when its cell comes into a player's view.
- Statuses and damage over time on a sleeping mob run on.
- **As built (step 1c):**
  - **Who watches:** a mob is awake while some player's block holds its cell, as the last frame left the blocks, and asleep otherwise. Every mob a player is sent is awake. On a map sent whole, every mob is awake while anyone is on it.
  - **A sleeping mob** goes on with a push, an act and a walk home under way. One that was after someone gives up and walks home, as at its leash, and a standing boss still awake gives up and calms at its spot.
  - **An empty map** sleeps every mob the same way: `Rest` is a sleep for all, so #314's rule for a boss awake as its map empties is the one sleep uses.
  - **Waking** is the first tick a player's block holds the cell. Mobs asleep that overlap still drift apart, but two standing ones never do.
  - **Measured** on an Apple M3, the 1 km region with 400 mobs (`internal/dependencies/interestbench`), against step 1b:

    | Channel | World's tick | Its allocations |
    |---|---|---|
    | Quiet, 20 players | 0.21 ms, against 0.29 ms | 338, against 539 |
    | Full, 150 players | 0.48 ms, against 0.49 ms | 1,166, against 1,189 |

    A full channel watches nearly every cell, so sleep saves little there; a quiet one sleeps about half.

### Budget

- **The target,** on one region channel at its cap of 150 players and about 400 mobs:
  - tick p99 under 20 ms of its 50 ms;
  - each client downstream under 40 kB/s in a busy fight, with a 150-player crowd in view (`BenchmarkAClumpWalking`: all 150 walking in one block).
- **The test:** a soak with cmd/bot's players (section 13), whose tick p99, overruns and bytes are read from the ops metrics. `world.channels.cap` is set from it.
- **The inbox** (4096, 1024 drained a tick) holds about 4× a full channel's input rate. The soak checks `ErrBusy` stays at zero.

## 6. Areas, towns and what was per map

- **Areas:**
  - `gamedata.Region.AreaAt(x, z)` comes from a 4 m lookup grid baked from the area polygons.
  - A player's area is tracked each tick from their cell, and entering one sends `AreaEntered{area}`. The client shows the name and band, as on a map change today.
- **Towns:**
  - In a town area, damage is refused as in a town map today (`usable` reads the area in place of `Map.Kind`).
  - A mob's chase and leash stop at a town's edge.
- **Field bosses,** one per hunting area:
  - Its boss stands at its circle from the channel's start and comes back 5 minutes after it's beaten, as [Loot pouches and timed bosses](./loot-pouches.md) made the maps' bosses. No kills count toward it: `kills` is gone from the plan's boss and the map file's (step 4b-1).
  - Its bar goes to players in the area, and rewards go to those in the area when it falls.
  - `instance/boss.go` keeps a slice of bosses by area, in place of one.
- **Death:** the player gets back up at the nearest waypoint or town of their region, by the road graph's distance. They don't hand off to a town map. On the old maps still in use, death hands off to the map's town as today.
- **Level bands** move from `Map.Levels` to areas. The loader checks camps and bosses against their area.
- **Chat:** `Instance.Chat` already reaches the whole instance, which is now a region's channel. E1 says "region and channel".
- **As built (step 4b-4a):**
  - **Data:** `gamedata.Map` carries a region's areas (id, name, kind, levels, boss), its waypoints and its 4 m `AreaGrid`, from the plan. `AreaAt` finds a spot's area; `TownAt` is true on a town map and in a town area, and `TownAreaAt` in a town area alone. The loader adds one rule: every town area holds a waypoint.
  - **Where a player is:** each tick, after teleports, a player's area is read at their feet. A change sends them a private `AreaEntered{area}` (server message 39), the area's id; `Snapshot.area` (9) names it on every join and map change. The client reads the area's name, kind and levels from region.json's `areas`, and shows its name, with a hunting area's levels, along the top as a map's is: on each step into another, and as each Snapshot lands. While a region builds, an `AreaEntered` after its Snapshot is held with the map's other messages until region.json is in.
  - **A town area is safe:** a player's skills that harm are refused with `NOT_IN_TOWN` and their basic attacks dropped, as on a town map, on both sides; the client holds the lock there too. A hit lands nothing from a town wherever it started, as one wound up outside comes due after its player stepped in: `strike` and `afflict`, where every hit and status skill lands, check where its player stands. A mob's blow that lands on a player in a town is dropped with whatever it inflicts, even one already under way, and damage over time does no harm there, running out as it would.
  - **Movement differs from a town map:** a town area keeps the region's speed, move speed and sprint, so the client's prediction needs no area each step.
  - **Mobs at the line:** every move of a mob's body goes through one push that stops at a town area's line, to 1/256 of the push, as a wall stops it: walks, chases, leaps, charges, knockbacks, status pushes and drifting apart. The line holds one way, so a mob somehow inside a town walks out. The push judges where a move ends. A body moves a tick's step at a time, well under a metre, so it can't step over a town; only a leap's or charge's planned end, judged whole, could pass over a town narrower than its move, at most 6 m, and towns are tens of metres across. A mob doesn't see a player in a town, and one whose target or provoker steps in gives up and walks home, as past its leash. Its spots on a player's rings skip any in a town. There's no loader rule: a camp's leash may reach into a town, as the test region's boars' does by 6 m.
  - **Death:** a region's dead get up at its waypoint nearest where they fell in a straight line, found or not, a town's among them. The straight line stands in for the road graph's distance until there is one. The lookup reads the player's own instance's map, so a place below (B13) will get up at its own.
  - **Chat:** a line said on a region reads `[Ch. n]`, its channel when it was heard, in place of `[Map]`.
- **As built (step 4b-4b), the field bosses:**
  - **One per hunting area:** `instance/boss.go` keeps a slot per boss, a hunting map's one or each hunting area's in a region, all raised as the channel starts, each back its own 5 minutes after it falls.
  - **Who hears of one:** on a map, everyone, as before. In a region, only those in its area: its `FieldBoss` as it comes back, its `FieldBossGone` as it falls, and its bar in the frame's part of its own, however far from it they stand. A Snapshot names the area's boss, and `AreaEntered` brings the boss of the area stepped into, up or beaten with its time back, so stepping in raises no "has appeared!"; a town brings none, and the client's boss state clears.
  - **Who it pays:** those alive when it falls, in its area or within its leash of its spot, as its fight may go over the area's line; a party's damage still counts all its members' tallies. Its fall is told before what it pays.
  - **The town line:** a boss holds at it as any mob does, through the one push.
  - This map's crowns for each area's boss, with their time back on request, came with step 5c-3b's map views (section 10).

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

### Walk mode

- **The key:** the `walk` action, Left Shift by default (key-bindings.md), switches between sprint mode, the default, and walk mode. A held key's repeats don't switch it again.
- **The rule, in the shared sim:**
  - `sim.Input` gains `Walk`, the mode, and `State` gains `Walk`, the mode last stepped.
  - In walk mode the count stays 0, so the body never sprints, and others see it walk.
  - Back to sprint mode (`State.Walk` set, `Input.Walk` not) while walking out of combat, the count goes straight to `SprintSteps`, so it sprints from the next sub-step.
  - Switched back while standing, or in combat, the count starts as usual, so the 2 s wait applies.
  - A hold empties the keys but keeps the mode.
- **The wire:**
  - `Input.walk` (7) carries the mode with every input, so both sides step a switch at the same sub-step, with no marking as a press needs.
  - `PlayerView.walk` (32) carries `State.Walk`, so the player's own reconcile replays from it. Others need only the count.
- **No check:** walk only slows a player. The sprint at once is what any player gets from two presses out of combat.
- **Kept** with the account's settings as `walk`, as the zoom is. A rejoin starts in the saved mode. Bots never walk.
- **Shown:**
  - Each switch gives a notice, "Walk" or "Sprint".
  - While walk mode is on, a walk icon from game-icons.net (credited as C16's are) comes first in the player's own frame row (frame-icons.md). Its hover reads "Walk mode: no sprint. <key> to switch."
  - A player who enters the world with walk mode saved gets one info notice with the same words, once the account's settings are read; not on a map change, a teleport or a reconnect, and nothing when the saved mode is sprint.
- **As built:**
  - **The first sub-step:** walk mode stops a sprint on the switch's own sub-step, as combat does, so `sprinting` reads `Input.Walk` as well as the count.
  - **A hold** keeps the mode in the input itself: `Input{Walk: in.Walk}`.
  - **The vectors:**
    - `walk-never-sprints`;
    - `walk-switch-while-walking`;
    - `walk-switch-standing`;
    - `walk-switch-in-combat`;
    - `walk-held-keeps-mode`.
  - **The client:**
    - The mode is `settings.walk`, which the input sender reads for every input.
    - The zoom's watcher, now `watchPlaySettings`, saves it 800 ms after the last switch, so a quick double switch makes one PUT.
    - The notice is an info toast.
  - **The icon:**
    - Delapouite's `walk`, in `public/icons/hud/`, on a slate tile edged in the text colour.
    - Its path is in `features/shared/hudIcons.ts`, which the credits read too.

## 8. Waypoints and teleport

- **Data:** waypoints come from the plan. A waypoint is a set piece, a tall stone with a glow, with a 12 m finding circle.
- **The look** is the client's, from the found set that `Welcome` and `WaypointFound` give it:
  - **The stone:**
    - A six-sided obelisk, about 2.8 m tall on a low octagonal plinth, in the palette's stone grey.
    - Above it floats a six-sided crystal, its top at about 3.55 m, in the map's waypoint cyan.
    - Rune strips are carved up three of the obelisk's faces.
  - **Unfound:** a dim teal crystal, dark runes and a small dark-teal halo, dim by darkness rather than thinness. It still reads 22 m off and in a dark cave.
  - **Found:** a near-white cyan crystal, lit runes and a wider, bright cyan halo.
  - **The halo** is solid cyan to about 45% of its radius, then a soft fade tinting toward a deeper blue. So it reads cyan on grass, about 184° found, as on dark ground. Only the unfound halo's outer edge goes a little dark-mint on grass. The crystal draws in front of it.
  - **The flare on finding:**
    - The crystal and runes light within 0.3 s.
    - A beam rises and is gone by 0.6 s.
    - A ring spreads 7 m on the ground.
    - Then the found look holds. No light column stays over a found stone.
    - **When it plays:** the first time the crystal is on screen after the find. At the near zoom the whole stone shows only within about 9 m of the player, and finds come at 12 m, so a flare played at once would mostly go unseen. The notice comes at once, and the map views show it lit at once.
      - Until its flare, the stone keeps its unfound look. The flare lights it.
      - A stone not on screen within 10 s of its find turns lit then, with no flare.
      - One stone flares at a time. A newer find takes the flare over: a stone still waiting turns lit with no flare, and one mid-flare jumps to lit.
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
- **As built (step 6a), finding and keeping:**
  - **Data:**
    - `gamedata.Waypoint` gains `Arrive`, `Yaw` and `Main`.
    - **The arrival:** the loader picks each waypoint's as the first of eight spots 2 m round its stone that a player fits on in the stone's own area. One arrives there facing north, the camera's way. A waypoint with no such spot is refused.
      - **The order:** south first, then the south diagonals, east and west, the north diagonals, and north last. The camera looks north, so a stone south of one who arrives stands between them. The stones' live pass found north-first arrivals hidden by the stone and its halo after every teleport and get-up.
    - **The main town** is the town area holding the region's start; a region with towns must start in one. Its waypoints are `Main`.
    - **Ids** are unique across every region, so a waypoint is named by its id alone.
    - `TestEveryShippedRegionCanBeCrossed` starts from each arrival too.
  - **The stone itself,** its model and its 0.5 m solid, comes with the look (step 6c-2). Until then nothing stands at a waypoint.
  - **Finding:**
    - Each instance lists, for each 32 m interest cell, the stones whose 12 m circle reaches into it; an edge cell holds what's past it.
    - Each tick, after the areas, a living player finds the stones their cell lists within 12 m, the edge counting, that they hadn't found. A match's bots find none.
    - `WaypointFound{waypoint}`, server message 41, tells them. The find is theirs for good.
  - **Storage:**
    - `character_waypoints(character_id, waypoint_id, found_at)` is migration 00015.
    - The waypoints found travel with the player across hand-offs. Each checkpoint adds those not yet stored, keeping each one's first time, and drops none.
    - `Welcome.waypoints`, field 13, lists their ids.
  - **Death:** a region's dead get up at their nearest waypoint's arrival, facing its way, no longer on the stone's spot.
    - **Leaving dead** saves that same arrival with half HP, and a character saved at 0 HP joins there. The getting up, the checkpoint and the join share one rule, `Data.GetUp`; the old maps keep their town's spawn. In the waypoints playtest, both had come back at the region's start.
- **As built (step 6b), teleport:**
  - **The request:** `Teleport` keeps `map_id` (1) and gains `waypoint` (2) beside it. A `oneof` round both would move field 1 into it, which `buf breaking` refuses even at WIRE. A set waypoint is the target; `map_id` is read only without one.
  - **`map_id`** is for the old maps alone, until the last is retired. A region's id is `TELEPORT_UNKNOWN_MAP`, as a match's map's is, and the player's own old map `TELEPORT_SAME_MAP`.
  - **A waypoint** that no region has is `TELEPORT_UNKNOWN_WAYPOINT` (330046, 404). One the player hasn't found, outside a main town, is `TELEPORT_NOT_FOUND` (330047, 409). Dead and busy are refused after these, as before.
  - **The landing:** at the channel's end, the player is handed off beside the stone, at its arrival, facing its way, and checkpointed there.
    - Into a region, from an old map or another region, the World picks the channel as for any join.
    - Within their own region, even to the stone they stand by, they go back into their own channel, which takes them even if it filled in the 3 s, as a switch's does. The client hears a `MapChange` and shows the veil, as for any.
    - No same-stone check refuses a teleport to the stone one stands by: it doubles as a way out for anyone wedged near one.
  - **The test region** is reached by its main town's waypoint, `town`.
  - **The stone lists** now come from the same `interest.of` that files a player: each stone goes in every cell holding a spot of the square 13 m round it. Edges built as `k × 32 m` could disagree with `interest.of` by a rounding on a cell's line.
  - **Copy rows:** test/rules' `TestEveryEventAndPushHasACopyRow` fails the build for a map event, world stream event or server push with no row in its conversion's copy test. `AreaEntered`, `ArenaBar` and `MatchState` had none and gained theirs.
- **As built (step 6c-1), the map window:**
  - **Data:** mapjson writes a region's waypoints into its map JSON: each one's id, name, stone's spot and `main`, from the Go loader, so the client works out no main town itself. The scene hands them to the map views' store, and `layoutOf` gives them for a map picked on the World tab.
  - **Found:** a `waypoints` store holds the ids found, from `Welcome`'s list and then each `WaypointFound`. A find also says "Waypoint found: <name>" as a notice, the name from the player's own region. Leaving the character clears it.
  - **Marks:** This map, and a region picked on the World tab, mark each stone with a diamond, cyan once found and grey till then. The minimap marks none.
    - The player, party, lock and boss markers drawn over them let a click through to a mark, so the stone one stands by can be picked.
    - This map's labels keep clear of a mark's pick ring, 11 px round, with a pixel to spare, not just its diamond. Drawn text runs a little past the box the labels are placed by.
  - **Picking a mark** names it under the plan as "Found", "Main town, always open" or "Not found yet". Teleport here shows for a found one or a main town's while the player is alive, and sends `Teleport{waypoint}`. The bar says "Teleporting to <name>".
  - **A region on the World tab** reads "Region" on its node, its card and its picked header, in place of levels, which its areas hold. It offers no Teleport here by its id.
  - **No region is charted yet,** so a test charts the test region by a fixture.
- **As built (step 6c-2), the stone:**
  - **The model** is our own, made in Blender by `web/scripts/waypoint_stone.py` as `waypoint/waypoint_stone.glb`. The client lights its `Crystal` and `Rune` meshes.
  - **The solid:** the generator stands a 0.5 m cylinder at each waypoint, up to the obelisk's top at 2.82 m. The crystal floats out of reach above it. The bake draws no prop for it.
    - The loader no longer asks that a player fit on a waypoint's spot, as the stone stands there; its arrival must still fit.
    - The reach check counts a waypoint reached within 12 m, its finding circle.
  - **Where it's drawn:** mapjson adds the ground's height at each stone. The region's place draws the stones from its first frame, before any chunk comes, and lets them go with itself. They cast no shadow, even with 5b-3's cascades (section 10).
  - **Found or not:** the model is copied twice, painted dim and lit, and each copy is drawn at the stones in its state. A stone found swaps its copy as its flare starts, within the Look's 0.3 s.
  - **The halo** is drawn over the ground rather than added to it, as an added one read mint on grass.
    - Even drawn over, a cyan thinning to clear over grass passes through mint. So each halo is a soft dot whose colour deepens toward blue across it: solid out to 45% of its radius, then its alpha falls evenly to nothing at the rim.
    - **Found:** 2.6 m across at alpha 0.9, `#00d8f0` in the middle to `#00b4ff` at the rim.
    - **Unfound:** 1.35 m at alpha 0.7, a dark teal, `#006a8a` to `#00587a`. It's dim by its darkness, not by being thin.
    - It stands 0.3 m north of the crystal and 0.3 m below it, behind it along the camera's look, so the crystal draws in front of it at either zoom.
    - **Over the test region's grass,** the found halo's solid part blends to 183°–189°, with pure cyan about 183°, and the unfound one's to about 176°.
  - **The flare** comes only from a `WaypointFound`; a join's list lights stones without one.
    - The beam is gone by 0.6 s, and the ring spreads 7 m across over 0.8 s.
    - The halo swells to half as wide again and settles. A swell to twice as wide hid the obelisk behind a solid disc.
    - **When it plays:** a find leaves its stone dim until the crystal's middle is first inside the camera's view, then lights and flares it. The notice comes at once. A stone not seen within 10 s of its find lights with no flare, and a join's list lights one still waiting, unflared.
    - One stone flares at a time; the newest find to come on screen takes the flare over.
    - The beam and ring keep `#00d0f4`, a touch bluer than the crystal's cyan.
    - **The ring** lies flat, so as it spreads it rides the highest ground under it. On East Woods' slope the flat ring had sunk into the ground for up to 98° of its circle.
  - **See-through:** a stone between the camera and the player fades as a tall prop does, its halo ahead of it (below), and goes at the lens. They're a region's only see-through looks so far.
    - **While faded,** it leaves its thin instances and draws its own copy at the step's alpha, made when its model loads.
    - **Its glow goes first (as built after the wardrobe stills):** at the stone's own steps, a found halo at the faded 0.3 still tinted the player behind it teal at the near zoom. A bright glow over a body reads far stronger than a see-through solid at the same alpha.
      - The halo now steps 1, 0.3, then nothing from the stone's faded step on, so it's gone by the time the stone sits at 0.3. The beam of a flaring stone takes the same steps.
      - A stone fades only when it stands between the camera and the player, so its glow there has nothing to mark. The see-through stone still shows where it stands.

## 9. The camera

- **Zoom:** the camera distance runs from today's 16 m to 40 m. The pitch eases from 50° to 35° along it, and the target stays on the player.
- **Input:**
  - The wheel steps the zoom: 800 px from close to far, so a 100 px notch is an eighth. A mouse that counts lines steps an eighth a notch.
  - Two new rebindable actions, `zoomIn` and `zoomOut`, sit on `Equal` and `Minus`. Each press steps an eighth. They're in the key-binding window's Windows group (key-bindings.md).
  - The camera eases to the zoom as it eases after the player.
- **Saved** in the account's settings as `zoom`, 0 to 1, as the minimap flag is. It's saved 800 ms after the wheel stops, and as the player leaves the world. A zoom outside 0 to 1 is `BAD_ZOOM` (110009).
  - **A read of the settings,** such as opening Settings, first saves a change still waiting and waits for every save under way, so it reads what the player set. A setting changed while it waits or is out keeps its change, and the read sets the others. Before this, opening Settings inside the 800 ms undid a zoom step or a walk switch.
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
  - **As built (step 4b-2a):**
    - **The sim:** a terrain cell with any deep sample is a wall to a move into it from out of it, at any height, in Go and in the client (`slide`). So no walk, jump, dash, leap, knockback or blink gets in, and a body already in one walks out. Moving downhill stays open everywhere else, which is why the banks alone didn't stop anyone.
    - **Shots fly over it:** a mob's spit moves its ball by the same `Push` with the marks left out, as the client draws it, so a bat spits across a river and its line-of-shot check agrees.
    - **Nav:** a cell over deep water is `barred` (named `deep` until a cave's rock took the mark too). Nobody fits there, no way into it opens from dry ground, a jump in the reach check lands short of it, and a path from inside it leads out by the nearest dry cell.
    - **The client** read the marks from the map's JSON (`deep_water`, per sample, now `barrier`) until 4b-2b moved its ground onto the bake, which carries them per chunk.
    - **Tests:**
      - the vectors `deep-*`: a walk, jump, dash, leap and blink stopped at the bank, the ford crossed, a slide along the bank into it, cells deep at one corner only, and a body walking out;
      - the generator's marks: a river's channel and a lake 0.8 m deep are deep, a ford, a causeway, a bank's edge and a pond 0.5 m deep aren't;
      - a deep river crossed only at its ford, and nothing in a shipped region reaching deep water; every ford walked across both ways (the ford ramp);
      - a bat's spit across a strip of deep water landing on the far side.
    - Before it, a player in the test region reached 13,039 cells of riverbed, about 3,300 m², walking under up to 2.5 m of water.
- **As built (step 5b-1): the looks grid.**
  - **The grid:** a place files each look under every 8 m cell its box covers. Each frame the world steps only two kinds of look:
    - those in the cells under the box round the camera-to-head line, grown by 2 m, the lens's reach;
    - any look not yet whole again, wherever the camera went.
  - Maps use the grid too, so both fade the same way.
  - **A region's tall props:** each chunk makes a look for each prop taller than half a player once its kit model is in, as maps do. It takes them out of the grid as it's freed, and a chunk freed before its models came makes none. The waypoint stones' looks are filed with them.
  - **Copies at the first fade:**
    - A look starts as its model's box, placed by its instance.
    - Its see-through copies are cloned from the region's hidden kit model the first time it fades, under the chunk's props, so they go with the chunk.
    - So every drawn chunk has its looks, not only those within 64 m as planned, and a chunk's props cost no meshes till one fades.
  - **No stall at a first fade:** as each tall kit model loads, one hidden copy of each part is made, and its see-through steps are compiled on it, once for the region.
  - **Shadows:** the copies cast none here. 5b-3's cascades made both the props and their copies cast (below).
  - **Tests:**
    - the grid alone: cells either side of 0, the camera's 2 m, a look on its way back kept then dropped once whole, one taken out, a line longer than the cells filed, and a turned model's box;
    - a tiny-bake chunk's oak fading: its instance and ink hidden, its copy alone made and placed, then whole again;
    - looks filed and freed with their chunks, and none for a prop no taller than half a player;
    - the see-through steps warmed once per tall kit model;
    - the world taking a 14 m tower at the camera's end of its line from the view, which a grid read round the head alone misses.
- **As built (step 5b-2): water.**
  - **Reading it:** the region reads `region.json`'s rivers and lakes, and its palette's water colour. A palette with no water is refused, as a plan's kit must have one.
  - **A river** is a ribbon along its smoothed path, at each point's level:
    - its channel's half width plus 2 m out on each side, square to the line from the point before to the one after;
    - the 2 m runs under the banks' ground, which stands at least 0.5 m over the water.
  - **A lake** is its area grown 2 m past every side, at its level:
    - each corner moves out along the line between its two sides', at most 6 m, so a sharp corner doesn't spike;
    - it's cut into triangles by earcut (Mapbox's, ISC, no dependencies). Vetted: the source scanned, no install scripts, no OSV advisories.
  - **Drawn** as the region loads, one mesh each:
    - toon at 0.85 in the water's colour, taking the sun's shadows;
    - neither streams with the chunks: a river is a few dozen triangles, a lake fewer;
    - the whole channel is covered, so a ford shows as shallow water over its bed.
  - **Tests:**
    - the tiny bake's river laid along x = 96 from 91 to 101 m at its levels;
    - a bend's pairs square across it, either way it runs;
    - the test region's lake grown 2 m from each side's line, covered once, either way its corners run;
    - a sharp corner capped at 6 m;
    - every triangle facing up, wound as the ground's are;
    - the water's colour and alpha, and the meshes going with the region;
    - region.json read with a lake, with none written as null, and refused with no water.
  - **After 5b-2's review:**
    - **A bend tighter than the ribbon's half** folded its inside back over itself. Each vertex's reach across is now cut so its bank's edge still goes downstream by a tenth of each step, along both ends' ways. Gentle bends keep the whole reach. The cut is local to each step, so a river that coils back across itself, as a spiral, can still overlap. Test: a hairpin's triangles all face up, either way it runs.
    - **A lake whose grown surface crosses itself,** as a U whose arms stand closer than its two banks, is refused with the plan. The generator grows the lake's area as the client does (`regiongen/surface.go`) and refuses it if two sides not meeting at a corner touch or cross. Tests: a U 3 m apart is refused, and one 10 m apart builds. The two growths are kept in step by `server/testdata/water/lakes.json`: the generator writes three lakes' rings to it (`-update`), and the client's water test grows the same areas to them.
    - **Draw order:** the water draws first of the blended meshes (`WATER_ORDER`, before the areas and the lock ring), so a near faded prop or a halo isn't hidden behind its region-wide surface.
- **As built (step 5b-3): cascaded shadows.**
  - **The sun:** a region's sun casts through Babylon's `CascadedShadowGenerator`:
    - 3 cascades out to 80 m from the camera;
    - held steady as the camera moves (`stabilizeCascades`);
    - cast by see-through meshes too.
    - **Every toon light takes the view matrix,** which a cascaded receiver needs to pick its cascade. Without it, a receiver's shader didn't compile on WebGL 2, so a region drew only its ink, with no ground or props.
  - An engine that can't cascade, such as WebGL 1, gets the one shadow map a map has, with the same casters out to about 220 m in it, so its shadows are coarser. Maps keep theirs.
  - **Casters:**
    - each chunk's props, and their faded copies, while the chunk's nearest edge is within 128 m of the camera's target, so props to about 220 m (the cascades reach 80 m from the camera, which stands up to 40 m back);
    - bodies within 25 m, as on maps;
    - never the inks.
  - **Why near ones only:** Babylon 9.36 fits each cascade's depth range to the box round every caster each frame (`_computeMatrices`, with its default depth clamping). Far casters would widen that range and cost the near shadows depth precision, and each draws into every cascade it overlaps.
  - **The waypoint stones still cast none.** They're thin instances of one mesh across the region, so as a caster their box would span it.
  - **Tests:**
    - the generator: 3 cascades, 80 m, steady, see-through casting, and the one map where the engine can't cascade;
    - the world asks for cascades on a region only;
    - a chunk's props cast within 128 m and stop past it, bodies but not inks;
    - a faded copy casts;
    - a freed chunk leaves none of its meshes in the generator's list.
- **Entities:** only what the server sends (section 5).
  - Name labels show for the 40 nearest, and for party members and the lock.
  - HP bars show for the party, the lock, and mobs hit in the last 5 s.
  - **As built (step 5c-1):**
    - **On every map, not only regions,** so the game has one rule; the old maps are going.
    - **Names:** another player is named while among the 40 nearest the camera's target, in the party, or locked. Those past it keep their bodies and lose only the name.
      - After 5c-1's review: a named player keeps their name till another comes 2 m nearer than them, so the 40th in a crowd doesn't flicker (`world3d/named.ts`). With 40 or fewer shown, everyone is named with no sort. Past 40, a frame measures each player once into arrays kept from frame to frame, and sorts only when someone crosses the 2 m band or a named player goes.
    - **Bars:** a mob's bar shows while it's locked or for 5 s after its HP last fell, whoever hit it. A heal, regeneration included, is no hit. A mob is seen anew when it comes back into sight or on a rejoin or map change, so a fall while out of sight doesn't count.
    - **The party's bars** are its HUD frames, as before. No player has a bar over their head.
    - **Tests:**
      - 40 allies within 3 m named; a stranger 41st nearest not; a party member and the locked enemy past them named;
      - a mob's bar: none till hurt but the lock's, gone 5 s after, kept for the lock, none for a heal, none for a mob back in sight or on a map anew at lower HP.
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
  - **As built (step 5c-2a): the tiles.**
    - **What the views get:** the world hands them a map's ground as one of two kinds (`MapGround` in `state/map-view.ts`):
      - a map's one image, drawn as it loads, as before;
      - a region's tile folder in its bake.

      The World tab's picked layouts carry the same. The region image drawn from the overview is gone, and so is the client's work making it.
    - **Which tiles:** `MapMarks` draws the tiles that meet the view, at the coarsest level whose pixel spans at most 2 screen pixels:
      - the minimap, at 4.5 px a metre, draws up to four at 1 m a pixel;
      - This region draws every tile: at 1 m on the 256 m test region (1.25 px a metre), at 4 m on a kilometre (0.31 px a metre).
    - **Seams:** each tile is drawn a screen pixel wider and taller, over its east and south neighbours, so no seam shows between them.
    - **Pan and zoom** in This region come in 5c-2b.
    - **Tests:**
      - the minimap's tiles as the player walks: two across a line, four at a corner, one inside, and none past a view edge on the line;
      - This region's level at four sizes, 256 to 1,024 m, which pins the 2-pixel rule between 1.67 and 2.5;
      - a picked region's layout drawing its tiles;
      - the world handing a region's tile folder and a map's image.
  - **As built (step 5c-2b): This region pans and zooms.**
    - **The tab** reads "This region" on a region, "This map" elsewhere.
    - **Zoom:**
      - the wheel zooms about the pointer, twice per 400 px of scroll, or twice a notch where it counts lines or pages, as Firefox and line-mode mice send;
      - Zoom in and Zoom out buttons, in the plan's top right, double or halve it about the middle;
      - from whole to where a metre spans 4 screen pixels, a tile's finest: 12.8 times on a kilometre, 3.2 on the 256 m test region.

      The plan keeps its size on screen. Its labels, marks and tiles are drawn at the zoomed scale, and only the tiles in view are drawn.
    - **Pan:** a drag with the main button pans it. A press that moves under 4 px is still a click, so a waypoint is picked as before. A drag that starts on one picks nothing.
      - Only the click the button's up makes is swallowed; a later click picks, as a screen reader's does with no press before it. A cancelled press, and one held as the window closes, let go at once.
      - Moves aren't coalesced per frame on top: browsers already deliver pointermove once a frame.
    - **Held inside:** the view stays inside the region. A map change starts the new map's plan whole.
    - **Where it applies:** wherever the ground is tiles, including a region picked on the World tab. A map keeps its fit, with no buttons, and the wheel scrolls the page as before.
    - **Tests:**
      - the buttons' steps and limits, the tiles drawn finer as it zooms in, and the size on screen kept;
      - the wheel about the pointer, with no page scroll, and no further out than whole;
      - a drag held at both corners, the right button's none, and no pan after the button's up;
      - a drag over a waypoint picking nothing, one let go off the plan not swallowing the next click, a short press picking;
      - a wheel in lines and in pages; a cancelled press panning no more; a press held as the window closes let go;
      - a 720 m map keeping its fit;
      - the tab's name.
  - **As built (step 5c-3a): the areas.**
    - **The data:** the generated map data gives each region its areas, from the plan: id, name, kind, a hunting area's levels, its corners, and its boss's mob and spot. A map has none.
    - **The outlines:** both views draw each area's outline, faint white, and a town is tinted faintly gold.
    - **The names:** This region names each area at its middle by area, which for an uneven area isn't its corners' average:
      - a hunting area as "West Field Lv 1–4";
      - a town by its name alone, in the NPCs' gold.

      The names join the fixed labels after the camps and before the gates and NPCs, each lifted clear of those before it. The minimap names none.
    - **Tests:**
      - the generated areas against each plan;
      - the test region's five names and their middles;
      - an L-shaped area named at its middle by area (11, 11), not its corners' average (13.3);
      - each outline, a town's tinted, north up;
      - the minimap outlining without names, and a map outlining none.
  - **As built (step 5c-3b): the bosses' crowns.**
    - **The query:** `Bosses` (client message 41) answers in `Response.bosses` (field 7) with each hunting area's boss on the player's channel, in the plan's order: up, or its time back in ms. It's refused off a region with `NOT_A_REGION`, as `Channels` is, and has its own limit, one a second with a burst of 3.
      - The instance answers on its tick, through its inbox, as the slots are its tick's alone.
      - This region asks as it opens and every 5 s while it shows, as it asks for the channels. The minimap never asks.
    - **The crowns** stand at each area boss's spot from the generated data:
      - red while it's up, and grey with "<Boss>: back in m:ss" counting down while it's beaten, each label kept clear as a map's is;
      - the player's own area's from its frames, as a hunting map's: grey while beaten, and while up only its live crown where it is, or red at its spot while the frames don't show it. Its frames are fresher than the query.
      - None for an area until the query's first answer. A picked map on the World tab shows none.
      - **A channel switch** or a map change clears the answer and asks again at once, so one channel's crowns never show on the next. An answer that lands after the move is dropped.
    - **The player's area** reaches the map views from the world, from the Snapshot and each `AreaEntered`. A map change clears it till the new frames say.
    - **Tests:**
      - the instance: both areas up at the start, then the beaten one's time back as of the tick that answered, and none on a map;
      - the world: the test region's four up in order, refused in town and from another connection;
      - the adapter's milliseconds, the server's answer, and the protocol's sample and golden;
      - This region's crowns and countdowns from an answer, none before it, the player's own area from its frames over the query's (red at its spot out of their view), the 5 s asks and their stop, a channel switch clearing them and asking at once with the old channel's late answer dropped, none off a region, and none on a picked map;
      - the minimap's own area: red at its spot out of the frames' view, only the live crown in it;
      - the minimap's own crown alone, and the area cleared by a map change;
      - the world handing the area on.
- **Loading a region:** the loading screen holds until the overview and the chunks within 128 m are built, then fades as today.
- **As built (step 5a):**
  - **Wiring:** a map's JSON may name `region: {id, hash}`. The world then draws that bake in place of the map's own ground and props, and tells it the camera's target each frame. No map names one until 4b serves a region, so only tests and the preview draw one.
  - **Building chunks:** two Web Workers decode and build them (`chunk-workers.ts`), four asked at a time, nearest first. Two meshes are made a frame, so streaming never holds a frame up.
    - A chunk built after the target moved out of its reach isn't made.
    - A chunk that fails to load isn't asked again; the overview stands in for it. (4b-2b asks again after a backoff, below.)
    - The workers start at a region's first chunk, and end as the world leaves the map or the game.
    - A chunk freed before its worker answers is still built, then dropped: at most four are asked at once, so cancelling saves little.
  - **The overview** is cut under every chunk drawn, and whole again where one is freed.
    - Each change rebuilds its index list over every cell, about 16,000 cells for 1 km. A larger region wants each chunk's cells as a fixed index range instead.
  - **Seams:**
    - The bake (from format 2, now format 3) gives each chunk's heights a border one sample wide. Normals take central differences over it, so a shared edge gets one normal from both chunks and no toon band steps along the seam.
    - Each detail's skirt hangs 0.5 m below the most an edge strays from the line any coarser neighbour draws: a chunk every 2nd or 4th sample, or the overview every 8 m. The 4-sample line can sit twice as far from a sample as the 8 m one.
    - The overview hangs skirts of its own from its edge where a drawn chunk meets one not drawn, as deep as that chunk's and facing into it. So a chunk's edge dipping under the overview's straight line leaves no slit.
    - Stills with a magenta sky and no fog show no crack across the 64 m and 128 m steps or at the overview's edge.
  - **Props:** each chunk copies each kit model's parts, with their ink, as one thin instance per prop.
    - Every copy has a geometry of its own. Babylon keeps thin instances' matrices on the geometry, so copies sharing one all drew the last chunk's trees. Only real Chrome showed it; the null engine draws nothing.
    - Each kit model loads once for the region. A chunk freed before its models come gets none.
  - **Measured** on the wide test plan (1 km), M3 on Metal, 1280×800, full zoom, walking 12 m/s for 60 s:
    - frames: a median 6.8 ms, p95 10.7 ms, the longest 27 ms;
    - draw calls: a median 81, at most 200, with no shadows yet;
    - the first view shown 0.47 s after the overview, and a chunk drawn a median 30 ms (p95 45 ms) after it comes within 256 m;
    - 15.5 MB of buffers and a 123 MB JS heap.
    - At 32:9 (2560×720), props reach the sides of the view at full zoom.
  - **Not yet:**
    - the looks grid, cascaded shadows and water (5b; built in 5b-1, 5b-2 and 5b-3, below);
    - label and bar limits, and map tiles (5c);
    - the loading screen's hold, with 4b, when a player first enters a region (built in 4b-2b, below).
    - A river's bed steps 1 m at a time along its banks until 5b's water covers it (5b-2 does).
- **As built (step 4b-2b): the sim walks the bake.**
  - **The ground:** a region's map JSON has no geometry. `loadMap` reads its `region.json` and overview, and the map's geometry is a `RegionGround`: one Float32Array of heights for the region, its steep and deep marks a byte a cell or sample, and its solids at the server's indices, all empty till their chunks come.
    - Each chunk fills its own samples and the cells between them as the worker returns it, drawn or dropped. `markSteep` marks the cells, the same function a whole map's terrain runs, so both mark alike.
    - Its solids go to the server's index in the region's lists and are filed in the 8 m buckets by their bounds, each bucket kept ascending. So a box over a chunk's edge is in the buckets on both sides, and the sim visits solids in the server's order, which decides the cylinder a body rounds when two cut a move alike.
    - A chunk that doesn't fit is refused before it changes anything: one ending early or running on, outside the region, of another size, or naming a solid index past the region's count or held already.
    - Teardown drops the map before the next one loads, so the ground goes with it.
  - **The predictor, effects, the lock ring and mob attacks** read the same geometry object, so they follow the ground as it fills. NPCs, their marks and portal labels are stood on it again as each chunk lands.
  - **The hold:** a chunk not in yet reads flat at 0 with no solids, and a solid travels with the chunk holding its centre. So while any chunk is missing within the farthest one input could carry the player, past their radius, and the region's widest solid past that, the player is held:
    - every tick still sends an `Input`, with every key let go but the walk mode, and the predictor steps the same input. Both sims stay in step, and the server doesn't step the last keys on after its 250 ms gap;
    - attacks, skills and the other actions wait, as on a stalled world;
    - the HUD says "Loading the ground…".
    - **The radius:** the longest skill move in the data, a 10.5 m blink, plus the body's 0.375 m: out of a match only a mob's 0.5 m knockback pushes a player. Then `region.json`'s widest solid: 4.61 m in the test region, 9.28 m (a large house) in the wide plan, so 15.5 m and 20.2 m.
    - It's judged every frame where the player stands, so every arrival meets it: a spawn, a teleport, a respawn, a reconnect.
  - **The loading screen** lifts once the chunks within 128 m of the camera's target are drawn, at once on a map.
    - **Those chunks are asked all at once,** as built in the fetch order PR, and the rest four at a time, nearest first. Four at a time, a fresh load into the test region on Fast 3G waited on them in turns: 12.2–12.4 s, against 11.0 s all at once. After the arrival they're drawn long before the player nears them, so only an arrival asks this way.
  - **A chunk that fails,** or doesn't fit, is asked again after a backoff from 0.5 s, doubling to 8 s. After five failures the HUD says so and the page reloads 3 s later, fetching the bake afresh, a stale client included.
    - Each wait is equal jitter, in the upper half of its window (from the playtest: full jitter broke in 2.5 s). So the four waits come to 3.75–7.5 s, plus each try's own time, and the reload 3 s after that. A map that doesn't load retries the same way; the socket's reconnect keeps full jitter, spreading every client after a restart.
    - **Once the player leaves the region,** for another map or out of the game, the world disposes the region's place along with its scene. The place lets go of its chunk work, so a try failing after that breaks nothing, not even with a reload, and it takes the waypoint stones' listener with it.
  - **The minimap and map window** draw a region from its overview, a pixel a metre: its palette's colours blended between the 8 m samples, shaded by height. The bake's colours mark steep ground already, and a river shows as its sand bed until 5b draws water. The minimap blows it up about 4.5×, so it looks soft until 5c's tiles. The maps' 4 pixels a metre would be a 64 MB image at 1 km. Step 5c-2a replaced it with the bake's tiles (below).
  - **Measured** on the wide test plan (1 km), headless Chrome, its bake served gzipped as nginx serves it, sprinting (8 m/s) on the real region place, workers and ground, the body stopping while held:
    - across the middle (720 m) and south to north and west (1,030 m), unthrottled and on Chrome's Fast 3G preset (562.5 ms latency, 1.44 Mbit/s): no hold at all;
    - the loading screen: 0.19–0.21 s unthrottled, 5.6–8.8 s on Fast 3G, where a chunk takes a median 0.8 s to come and build;
    - on Fast 3G the hold first shows at 4× a sprint, one of 133 ms over 720 m, and at 8× it holds 5.8 s in four.
  - **Tests:**
    - the bake read back in the client: deep marks, every solid index filled once, and each refusal;
    - the ground: its grid, steep and deep marks as a whole map's, each solid as its chunk records it, and its buckets as indexing every solid at once, whatever order the chunks come in;
    - eleven movement vectors on two test plans' bakes, which Go replays on its generated ground and the client on a `RegionGround` from the chunks, to the bit. The tiny plan's: its seam, a house over it, its river and ford, a blink stopped by an oak a neighbouring chunk carries. The steep plan's, as steep as the wide plan gets (its ring a 42.9 m step in a cell, the wide plan's 42.1 m): cliffs, the barrier at a cliff's top, the road up it, a ridge;
    - the world: the hold on arrival, its radius pinned at a missing chunk's edge, the veil, the notice let go with the region, a gate's label lowered onto the river bed, the reload once chunks fail every try, and none from a try failing after the player has left.

## 11. cmd/bot

- **Plans by areas and camps:** a hunting spot is a camp in an area of the bot's band.
- **Long trips** follow the road graph to the camp's nearest waypoint, then nav near the camp. Once the waypoint is found, the bot teleports.
- **Gates and region changes** replace portals.
- **The soak:** `make bots BOTS=150 REGION=<id>` starts its bots in one region and reports what section 5's budget needs.
- **As built (step 7, the bots):**
  - **A hunting spot in a region** is one camp, not a circle round the region's prey.
    - The camps are those of the bot's prey in every hunting area whose band holds its level, as bands may overlap. With none, they're those in the highest band under its level.
    - The bot's index picks among them, so bots spread over a band's camps.
    - An area's boss is never prey. Every area's boss circle is kept clear, up or not, since a bot hears only its own area's boss.
  - **Long trips:**
    - The bot teleports to the camp's nearest waypoint once it has found it, when that lands it 60 m nearer the camp than it stands. It holds still through the channel.
    - A refused or cancelled teleport is left for 10 s.
    - Otherwise it walks, finding waypoints on the way. The road graph waits for a region whose trips need it, as the test region's 256 m don't.
  - **`REGION`:** the bots teleport into the region's main town and only hunt there: no quests, town visits or promotion. A map that isn't a region with a main town is refused.
  - **Sign-ups** run no more password hashes at once than the server does (`account.password.concurrent`, 4, a test holds the two equal), and each call refused 429 or 503 is tried again after 0.5 s, doubling to 16 s. Started together, 150 bots would find the slots taken, and a refused bot gave up, so the soak would run short.
  - **The report** says how many bots played of those asked for, and names any that never got in. It adds each bot's downstream kB/s, and its busiest whole second's after the first 5 s of its connection (when it fell, and the join's own beside it), against the 40 kB/s budget, over the bots that played. It lists the refusals by request and code. It also counts the refusals coded `UNAVAILABLE`, which a full inbox answers with. Ticks were already keyed by map and channel.
  - **The budget run** waits for a region near section 5's size, about 1 km with 400 mobs. The test region has about 25.

## 12. Rollout and the old maps

1. **The engine** (sections 4–5, 7, 9, 10), on today's maps first.
   - Interest cells, the indexes and sleep change nothing a player of a small map can see. Their tests run on today's maps.
   - Sprint and zoom ship then too.
2. **The pipeline:** the plan, the generator, the bake, the preview, the `region` kind, region channels, and a test region built from a small plan. The test region is never linked from the world.
   - **As built (step 4b-1):** `data/regions/test_region.yaml`, 256 m on the small test plan's ground: a town and four hunting areas of real mobs, levels 1–4, 10–14, 15–18 and 19–20, each with its own boss, and no NPCs and no gates. No portal or gate leads there and the chart doesn't show it: only a teleport reaches it. Since step 6b, that's one to its main town's waypoint, `town`, which the player harness uses; anyone may send it by hand, as to every main town.
3. **The green region:**
   - Designed with the user from previews first: its areas, towns, camps, waypoints and roads.
   - Then built (green-region.md): its NPCs are re-placed and its quests need no change; four areas get new mobs, and only the Cave Bat and the Vampire Bat move down, to levels 12 and 14.
   - Town, Forest and Cave are retired.
   - A gate at its edge leads to Shroom Grove, the mushroom woods' first map, whose portal to the Cave now leads to that gate. The Crossroads comes after the woods, as today. (Planned first as a gate to the Crossroads, whose only way back leads into the woods' top map.)
   - **As designed** in [The green region](./green-region.md): Willowmere, its five hunting areas and two towns, twelve new mobs for levels 5 to 14, and its cave and shrine.
   - **As built (green-region PR 2):** the three plans are in `data/regions` and Town, Forest and Cave are gone. Willowmere's plan has `beginning: true`, so it's the one map new characters start in (the loader takes a town or a region); a region on the land may have a place on the world chart, and Willowmere's is where the three stood. The area bosses of regions count as placing a boss, as a map's own boss did.
4. **The desert,** then **the snow mountains,** each retiring the old maps whose levels it covers. The last retires the Crossroads and the gate.
   - **As designed** in [The desert](./desert-region.md): Sunreach, levels 12 to 22, wet in the south-west and dry in the north-east, with a fungus cave and a buried temple below. It retires the mushroom woods and the desert maps, moving their mobs three or four levels down, and the snow mobs move down to follow on.
   - **As built (desert PR 2):** the plans, the eighteen mobs, the links and the retired maps, with the bots, tests and chart moved off them (desert-region.md section 8).
   - **As designed** in [The snow region](./snow-region.md): Hoarfell, levels 22 to 30, a mountain climbed in terraces with the Fallen Keep on its summit. It retires the snow maps, the keep's and the Crossroads, the last of the small maps.
   - **As built (snow region PR):** the plan, the five maps retired and the East Pass relinked; the tests that walked the old maps use fixtures of the test data (snow-region.md section 5).

- **Characters on a retired map:** `World.Join` places a character whose map no longer exists at the green region's start, in place of `ErrNoMap`. Built with the plan: `World.Join` starts a character saved on a map that's gone at the start map's spawn.
- There's no other migration, since the game hasn't gone live.

## 13. Testing

- **Generator:**
  - each feature's shape on small plans;
  - Poisson spacing and `away`;
  - colours by slope and height;
  - the golden bake hashes.
  - As built (step 4a), also:
    - the noise against the library's own amd64 output;
    - the bake read back as the client will;
    - every stored number a float32's;
    - a bad plan or kit refused;
    - `make fma-check`;
    - the lint rule, proved by planted calls.
- **Loader:**
  - areas covering the region without overlap;
  - bands;
  - towns without camps;
  - gates both ways;
  - reachability over the nav.
  - As built (step 4b-1):
    - a 128 m region of the fixture's mobs, read as a map: its spots, an NPC placed on it, its geometry the generator's, its walk grid, and its bake named as the bake's folder;
    - each rule refused by a one-line change to the plan, a map or world.yaml, a case each, an area's straight edge off the 4 m lines among them;
    - the area grid: each square held by the area holding its corner, a shared side going to the area north or east of it, a corner on a slanted line to the area east of it, and two areas over one square refused;
    - every shipped region playing on its plan's lines: each square held by the area its centre lies in, but where a slanted edge crosses it;
    - the test region reached by nothing, its JSON with no models and its bake's name, and on no chart or link;
    - the ring a cliff over the highest ground inside, which a river and a ridge reaching into it neither cut nor raise.
- **Server:**
  - **Interest:** what a player is sent at a cell's edge; entities coming into and out of view; events reaching only watchers; and the concatenated frame decoding to the union of its parts.
    - As built (step 1a):
      - a join at each corner of a cell is sent exactly its block, itself, and every mob within the 96 m and 32 m;
      - players going out of each other's sight, the one ahead first, and back whole;
      - a still player is sent nothing more of still mobs at the block's every edge and corner;
      - a shot across a cell line, heard by the watchers of either end, its kill included;
      - a leave on the tick another's block moves off it;
      - a mob that came back while the map was empty, in the next joiner's snapshot, and that joiner told nothing of those who left before;
      - a map a row taller than the block from its south edge, cut by cells;
      - a rejoin that failed, leaving no body;
      - a mob killed on the tick it spawned;
      - the field boss's bar far off, with no figure made;
      - six players wandering a crowded map and fighting: each client then holds what a join on its spot holds, on three seeds;
      - through the whole stack, on the forest cut in 6 m cells: a player crossing into another's block comes whole to each, a swing three cells off is heard inside the block and not from outside it, and leaving it sends each the other as unseen;
      - the loader refusing a sight, shot, leap, class attack or skill past the rows behind.
    - As built (step 1b):
      - a party member out of sight told once a second where they stand, and an empty list once after they're back; nothing on a map sent whole;
      - a join that failed told to nobody, on both paths;
      - a frame's encodings kept across another map's frame, dropped with it, and a live frame's kept as others go;
      - on the client: an out-of-sight member where last drawn till the server's next spots, then where they list them; dropped when the spots stop listing them, or the party's news has them offline or on another map; and on a map sent whole, never out of sight;
      - bodies fading toward 96 m to the side and none past it on a map cut by cells, and every body whole on a 128 m map sent whole.
  - **Indexes:** each check equals a full scan on randomized cases. `rapid` (pgregory.net/rapid) is already a dependency.
  - **Sleep:** a far camp stays full, and its mobs wake as a player comes.
    - As built (step 1c):
      - a camp nobody's block holds stands where it fell asleep for 30 s, a killed one comes back meanwhile, and a player come within its block wakes it;
      - a boar chasing a player gives up as he leaves and nobody watches it, and is home 20 s later;
      - the mobs at a block's four corner cells are awake;
      - a standing boss struck on the tick its map empties, before it took a target, calms, and lets a stranger beside it be.
  - **Channels:** filling, a party kept together, a new channel opening, and a spare one closing.
  - **Areas:** the area's boss, a town's refusals, and respawn at the nearest point.
  - **Sprint** in the shared vectors, walk mode too:
    - walk mode never sprints;
    - switching back while walking sprints from the next sub-step;
    - switching back while standing or in combat waits the 2 s;
    - a hold keeps the mode;
    - a reconcile mid-switch replays with no correction.
  - **Waypoints:** finding, storage, and teleport rules.
- **Client:**
  - chunks loading and unloading round a moving target;
  - detail and seams;
  - As built (step 5a), on the tiny bake, which `TestTheClientsFixtureIsTheTinyBake` keeps current:
    - the order chunks are asked in, four at a time, two made a frame, freed past 320 m;
    - a late chunk, a failed one, a chunk freed before its models came, and a region gone while a kit model loaded;
    - the overview's holes and the skirts round them, each detail's triangles and skirts, and shared edges' normals;
    - the workers ended at close, and closed as the world leaves a map.
    - The tests hand each chunk and kit model over themselves and wait on those, never on a timer.
    - props within 160 m, each chunk's copies on their own geometry, culled by where they stand.
  - the looks grid;
  - the zoom's angle and fog;
  - the map views from tiles.
- **Performance:**
  - The soak with 150 bots, against section 5's budget.
  - A client frame at full zoom-out in the busiest town, measured as 3d-world.md measures. The target is a median under 8 ms at 1280×800 on the reference laptop, and a region shown within 3 s of entering.
- **Live** (the player harness): a walk across the test region, then the green region end to end. It covers sprint, finding waypoints, teleports, a gate, a boss, death and a channel switch.

## 14. Build

1. **Interest cells,** in three PRs:
   - 1a: the cells and the frame parts, `Unseen`, events by place, the field boss channel-wide, and the loader's reach check;
   - 1b: party spots once a second, and the fade at the block's side edges;
   - 1c: **sleep,** next to the cells that say who watches.
2. **Spatial indexes,** in three PRs:
   - 2a: sim buckets (both sides, with vectors), and packed nav with its binned build;
   - 2b-1: blocker buckets in the Go sim, which mob moves, kept frames and knockbacks read;
   - 2b-2: body buckets in the arena, for sight, targeting, reach, `spread` and the rest.
3. **Sprint and zoom:** the sim rule, `PlayerView.sprint`, the look, the camera, the zoom actions, and saved zoom. Then **walk mode** (section 7), its own PR: the sim rule and vectors, `Input.walk` and `PlayerView.walk`, the `walk` action and setting, the notice and the icon.
4. **The pipeline,** in two PRs:
   - 4a: `regiongen`, the kit and the bake, and the preview;
   - 4b: the `region` kind and its loader rules, region channels, areas, towns, area bosses and respawn, and a test region. The client's sim walks a region's ground from its bake, and the loading screen holds for the near chunks. The loader refuses a region one block would hold, so none is ever sent whole.
     - As built, in six PRs: 4b-1 the region kind, the loader, the ring's cliff and the test region (#332); 4b-2a deep water as walls (#334); 4b-2b the client's sim on the bake's chunks, the hold while the ground round the player comes, and the loading screen's hold (section 10, #337); 4b-3 channels, in two PRs: 4b-3a the server (section 4), 4b-3b the client; 4b-4 in two: 4b-4a areas, towns and respawn (#354), and 4b-4b the area bosses.
     - From step 8, every shipped region's hunting areas also pass the maps' three design tests: no mob sees an arrival, each mob reaches a player anywhere in its leash, and no solid leaves a gap by steep ground only a player fits. The generator then needs a scatter rule keeping solids off cliffs. Run through them once in 4b-1, the test region, a fixture, passes the first after one camp moved and fails the other two: mobs miss a few spots, some in a riverbed that 4b-2a's deep water closes, and trees at the foot of cliffs, the ring's included, leave gaps only a player fits.
5. **The client for regions,** in three PRs:
   - 5a: the overview, chunk streaming and detail, and props per chunk;
   - 5b: the looks grid, cascaded shadows and water;
   - 5c: label and bar limits, and tiles for the map views.
     - In a region, This map crowns each area's boss: its spot from the generated map data, and its time back from a `bosses` query, asked for as This map shows, as the channels are.
6. **Waypoints and teleport:** finding, storage, `Teleport{waypoint}`, and the map views' waypoints.
7. **The soak:** cmd/bot on regions, and the budget run. `world.channels.cap` is set.
   - As built: the bots (section 11). The budget run and the road graph wait for a full-sized region.
   - Hollow cut models (3d-world.md section 10) cost about 0.25 ms on a dense woods view. If regions run tight, the per-instance swap is the fix: only the instances an opening shows draw hollow.
8. **The green region:** its design first, then its build.
   - Designed in [The green region](./green-region.md), section 9 for its build: gentle banks (#442), the new mobs, then the region and its places with the old maps retired.
   - When regions join the world chart, its party names say each member's channel ("Bo · Ch. 2"), in a picked map's list and under its node. 4b-3b left these two lists alone, as no region was charted.
     - **As built:** Willowmere is charted, and each member's name takes `withChannel`, as the Party window's does: "Bo · Ch. 2" in a region, and the bare name on a land map, whose channel is 0. It's in a picked map's list and under its node on the World chart, from the channel the party state already gives each member, so the client alone.
9. **The desert,** then **the snow mountains.**
   - The desert is designed in [The desert](./desert-region.md), section 8 for its build: the snow mobs moved, then the region and its places with six old maps retired.
   - The snow region is designed in [The snow region](./snow-region.md), section 5 for its build: one PR after the desert's, retiring the last five maps.

Steps 1–3 change nothing on today's maps that a player sees, so they can land while PvP builds. Step 4 shares on-demand instances with PvP's step 1.

## 15. Decisions

The user's choices are in section 1. These calls were made in planning and logged for the user to push back on:

1. **Terrain at 1 m,** not 0.5 m. Big rolling land needs no finer grid; towns and set pieces get their detail from solids.
2. **Seen within about 100 m:** the interest block is 96 m to each side and ahead, and 32 m behind: lopsided, because the camera always faces north. The land shows to the region's edge.
3. **Channels of 150,** to be confirmed by the soak, filled least-full-first, with parties kept together.
4. **Sprint needs no key held:** 2 s of walking out of combat, ×1.6. Jumping keeps it. Walk mode (section 1) turns it off.
5. **Waypoints about every 150 m along roads,** found within 12 m. Each region's main town can always be teleported to.
6. **Death returns to the nearest waypoint or town,** found or not.
7. **The zoom runs from 16 m at 50° to 40 m at 35°,** with = and − as keys besides the wheel.
8. **Ground colours by vertex,** with no textures, so the flat toon look stays and there are no texture assets to fetch.
9. **Deep water blocks;** fords and bridges cross rivers. No swimming.
10. **A mob sleeps when no one watches its cell,** while its camp keeps filling. One asleep mid-chase gives up and walks home, as at its leash, so nobody returns to find it frozen where the chase left it.
11. **The server generates each region; the client reads a bake the same generator writes.** Planned as one bake both sides read, and reversed in step 4a: the generator gives the same bits on every CPU, which the golden hashes needed anyway. So the server needs no artifact and can't run on one older than its plan (section 3).
12. **The old maps stay behind a gate** until the region that covers their levels replaces them.
13. **Walk mode shows** as a notice at each switch and a walk icon first on the player's own frame row, since the mode is kept between logins and a player who logs in walking should see why.
14. **Only a switch made while moving sprints at once.** Switched back while standing or in combat, the 2 s wait applies, so a switch in a fight doesn't store a sprint for after it.

## 16. Alternatives considered

- **Libraries.** These were searched and vetted for adoption, a source scan and activity. Each was cloned and read, never run.
  - **ojrac/opensimplex-go** (Unlicense) is chosen for the noise:
    - pkg.go.dev counts about 110 importing packages;
    - it has no dependencies;
    - its non-test code imports only `math` and `math/rand`, seeded per call;
    - it has no exec, network, cgo or init code.
    - It was archived in 2025 as finished. Noise math doesn't need updates, and a fork is cheap if it ever does.
    - **As built (step 4a), it's ported, not imported.** On arm64 Go fuses its multiply-adds, so its values differ from amd64's: 521,241 of 1,050,625 samples, a 1 km grid at 1 m with seed 11, differed in their last bits.
      - **The port:** `regiongen/noise.go`, 2D only, from v1.0.2 (commit 67ecc326). It rounds every product and equals the library's amd64 output bit for bit on both CPUs, tested against its hashes.
      - **Licence:** the Unlicense is noted in `regiongen/LICENSES.md`.
  - **R-tree libraries** (tidwall/rtree and others): solids and bodies are spread evenly over a region, where a uniform grid is simpler and as fast.
  - **Babylon's own `CascadedShadowGenerator`** is part of the engine already in use, so there's nothing to add.
- **One seamless world:** the user chose regions.
- **Free camera:** the user kept north-facing.
- **Sculpting regions in Blender:** the user chose generation. Set pieces can still come from Blender, which is installed.
- **A region per process, or per-cell instances:** one instance per channel keeps today's tick, inbox and hand-offs. The soak decides whether that's enough before anything heavier.
- **Textures on the ground** (splat maps): more art to fetch and vet, and a different look from the flat toon colours of every model.
- **The waypoint stone's look**, offered with the previews:
  - a rough natural standing stone in place of the carved obelisk;
  - the same glow found or not, which leaves the map as the only way to tell;
  - no glow until found, which makes unfound stones hard to spot from afar;
  - a quieter find, with no beam or ring;
  - a lasting light column over found stones, easier to steer by but busier on screen.
  - **For the halo on grass:**
    - the firm disc first built: pure cyan, but a hard edge that reads as a sticker;
    - a soft fade bluer at the rim: closest to the first stills, but its haze reads green-teal, and the unfound halo is barely there;
    - a fade fitted to the ground ring by ring, which went grey-blue;
    - the first stills' soft glow, mint on grass.
  - **For a find made off-screen:**
    - finding nearer, about 7 m, which would change B11's 12 m;
    - letting the flare play off-screen, since many play zoomed out.
- **Per-player frames:** encoding per player would lose the shared encoding. Per-cell parts keep it.
