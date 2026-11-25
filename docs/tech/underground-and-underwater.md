# Underground and underwater

> **Status: approved · 2026-11-19.** The high-level design for [B13. Underground and underwater](../product/features/world-and-movement/b13-underground-and-underwater.md). It builds on [Large regions](./large-regions.md): a place below is a small region to the code, entered through a gate. It changes the region plan, the generator and its bake, the gate hand-off, and the client's looks.

## 1. What has to hold

- **The user's words** (2026-11-19): "we should also add underground and underwater maps", and "the light underground will come from the torches".
- **Their choices:**
  - Open hunting maps, shared in channels, entered from a region with a short load. Not private copies.
  - Underwater, players walk the sea floor and fight as on land. There's no air bar and no swimming in 3D.
  - Two to start, both in the green region: one underground, one underwater. The other regions decide theirs when they're designed.
  - Deep water on the land still blocks. There's no swimming.
  - Underground, the light comes from the torches.
- **Why separate maps, with a load:** a region's ground is a height map, one height per spot, so nothing can be walked under a hill. The sim, nav, the bake and the exactness gates all rest on that. A place below is its own map, with its own height map, where the walls are ground too high to climb.
- **The client computes, the server validates:**
  - Light, darkness, water and the creatures that only decorate are the client's.
  - The server's rules don't change below: moves, hits, sight, reach and rewards.
- **Same rules below:** combat, statuses, the lock, pouches, the death penalty, the map design rules (3d-world.md section 4) and the region rules (large-regions.md sections 3–8) all hold.
- **A place below is a region to the code.** Its plan, generation, bake, channels, areas, waypoints, area boss, chunks and map tiles are a region's. What's new is:
  - carving for caves;
  - torches and a light map;
  - the paired channel at its gate;
  - two client looks, `cave` and `sea`.

## 2. Today

- **The Cave** (`maps/cave.yaml`, levels 10–14) is a 64 × 32 m hall. Mesas are its walls, the `cave` sky draws it nearly black with fog from 17 m, and a sun lights it as on the land. The green region retires it (large-regions.md section 12).
- **Regions** (large-regions.md section 3) have rivers and lakes, but nothing below them: deep water is steep ground the sim blocks.
- **Light:**
  - Every model draws with one toon material (`world3d/toon.ts`), the sun's light posterized into bands.
  - Each sky in `SKIES` (`world3d/place.ts`) gives a colour and fog. A sun casts shadows from bodies (`sunShadows` in `world3d/body.ts`).
  - Nothing else gives light.
- **Gates** (large-regions.md section 4) hand a player off to the least full channel of the region they lead to.

## 3. Data

### The plan

A place below is a plan in `server/data/regions/<id>.yaml`, like a region's, with these additions:

```yaml
name: ...
under: green                  # the region it's below; set only on a place below
size: [384, 256]              # whole 128 m tiles, as a region's
cell: 0.5                     # finer than a region's 1 m, for walls and halls
look: {sky: cave}             # or sea
rock: {height: 4.5, rough: 1.5}   # caves only: ground not carved stands this high; its line wanders up to rough metres
features:
  - {hall:   {area: [[x, z], ...], flat: true}}   # caves only: carved to the floor; flat for spitters' halls
  - {tunnel: {path: [[x, z], ...], width: 6}}     # caves only: carved along its smoothed path
torches: {spacing: 10, model: torch_mounted}      # caves only: on the walls of every hall and tunnel
areas:
  - {id: ..., name: ..., area: [...], levels: [10, 12], biome: cave_floor, kind: hunting, boss: {...}}
gates:
  - {name: ..., at: [x, z], radius: 1.6, to: {region: green, gate: ...}}
```

- **Rock** is only for caves. A sea floor is a region's ground in a basin, its `edge` a ring of steep rock.
- **Wall height:** 4–4.5 m, as the old maps' banks are. The cut-away (3d-world.md section 10) was tuned on banks that high, so it opens the walls in front of a player or a mob as it opens banks.
- **Torches standing by hand** are `pieces`, with a model that gives light (below).

### The kit

- **`models` gain an optional `light`:** `{height, reach}`, where the flame is above the ground and how far its light falls.
  - `torch_mounted` and `torch_lit` have one. Any piece of such a model, placed by hand or by `torches`, is a torch.
  - A mounted torch is looks only (`{}`), since it hangs on a wall. A standing torch is a thin cylinder.
- **New grounds:** `cave_floor`, `cave_rock`, `sea_sand` and `sea_rock`, among `grounds`.

### Loader rules

These come beside a region's rules:

- `under` names a region, and that region isn't itself below another.
- Every gate of a place below leads to its `under` region, and back. A place below has at least one gate.
- It has a waypoint within 20 m of each gate.
- It has one hunting area at least, and no town.
- `rock`, `hall`, `tunnel` and `torches` appear only with `sky: cave`.
- Every hall and tunnel lies inside the region, clear of the edge.
- Every hall and tunnel is wide enough that a metre stays open past both walls' wander, a body's radius and a cell (as built, the walls' follow-up).
- The reachability test runs from its gates rather than a start, and so do the mob reach and calm tests (3d-world.md section 4).

## 4. The generator

The steps of large-regions.md section 3, with these added. Everything keeps the generator's rules: only + − × ÷, `Sqrt`, `Floor`, `Ceil`, `Trunc`, `Abs`, `Min` and `Max`, every product rounded on its own, a seeded PCG for chance, and maps read in sorted order. So the golden hashes match on amd64 and arm64, and `make fma-check` covers the new code.

### Carving (caves)

1. **Rock:** after the ground's noise, every sample is raised `rock.height` above it.
2. **Halls and tunnels** are carved back down to the ground's noise, in the plan's order:
   - A hall carves its polygon. With `flat`, its floor is flat at the mean of its ground, as a town's is.
   - A tunnel carves along its path, smoothed by Chaikin's method as a road is. Its floor is levelled along it, as a road's is.
     - Where it runs through a hall, or a tunnel before it, its floor is held to theirs, and it never carves below their floors inside them. Away from those joins it keeps within half the walkable slope of them (as built, the walls' follow-up).
   - **The wall's line wanders:** a sample's distance to the carved space is offset by noise at a scale of 8 m, up to `rough` metres. So halls aren't polygons and tunnels aren't pipes.
   - **The wall** rises over 2 samples, as a plateau's cliff does.
   - **The rock is a wall at any height** (as built, the walls' follow-up): its samples carry deep water's mark from a body's radius before the wall's foot. The sim judges steepness along x and z apart, so a wall at a slant to them was walked up along it.
3. **Colours:** carved floor takes its area's biome. Rock and walls take `cave_rock`, darkened at the top, since its tops are only ever seen in the dark.

### Torches

- **Placement:** along both walls of every hall and tunnel, one every `spacing` metres.
  - Each hangs on the wall's face at its flame's height, facing into the carved space, and the two walls alternate.
  - Walking a wall's foot is in-house, on the samples where carving stopped, in a fixed order, so the placement is the same on every CPU.
  - A torch keeps 3 m from gates, waypoints and other torches, and out of camp and boss circles, where mobs would stand through it.
- **Each torch is a prop** with an index. Its flicker group is its index modulo 2, so neighbours along a wall flicker apart.

### The light map

- **What it holds:** 1 m texels over the place, two bytes each: the light from each flicker group's torches, 0–255.
- **A torch's light** at a texel `d` from it, within its reach `r`, is `(1 − d²/r²)²`, the ridge's falloff.
  - It counts only where the torch sees the texel's ground: that ground is below the flame, and no sample on the straight line between them, stepped every half cell, rises above it.
  - So light stops at walls, and rock tops stay dark.
- **Summed** per group, clamped to 1, and stored as `Floor(v × 255)`.
- **The server computes it too** (as built, step 1b). Light changes no rule, but the loader bakes each region to name its folder by the hash of every file, `light.bin` among them. A torch's 8 m reach is about 200 texels of 16 steps each, so it's cheap.

### Seas

- No new steps. The sea floor is ground noise in a basin of `edge`, with plateaus for reefs and ledges, scattered with the sea biome's models.
- Kelp, coral, shells and rocks are scattered as trees are.
- The shrine's stones are set pieces.

## 5. The bake

- **`region.json`** gains:
  - `under`, the region above;
  - `lights`: each torch's x, z, flame height and group, for its flame's flicker.
- **`light.bin`:** `FRLT`, the format, its size in texels along x and z, then the texels' two bytes each, row by row from the south-west. It's written only for a place with torches: about 196 kB for 384 × 256 m, much less gzipped.
- **The golden hashes** cover it as they cover the chunks.

## 6. The server

### A place below is a region

- It loads as a `region` map, with its channels, areas, area boss, waypoints and nav. Nothing in the tick, interest cells or sleep changes.
- `gamedata.Region.Under` is the region above, which the hand-off and the loader read.

### The paired channel

- **Through a gate between a region and a place below,** a player goes to the channel with the same number, opened if it's closed, when it's under `channel_cap`. Otherwise they go to the least full, as through any gate.
  - So the players seen going in are there, and a party in one channel stays together.
  - A party split across the region's channels goes down split: the gate keeps each player's own channel, not the party's.
- **Gates between regions,** and teleports, keep the least full channel.
- **Closing:** a place below's channels close as a region's do, 5 minutes after the last player leaves, the first one included, since the gate opens one when it's needed.

### Waypoints and death

- A place below's waypoint is found and teleported to as any. A teleport to it is a region change at its end.
- Death inside gets the player up at the nearest of the place's waypoints, by the rule of large-regions.md section 6. A place below is its own region there.

### Bosses, notices and chat

- The area boss works as on the land: up from the start, back 5 minutes after it's beaten, calm until provoked (loot-pouches.md). Its notices go to the area's players.
- `Instance.Chat` reaches the place's channel, so General inside is the place's own.

### Bots

- A place below's camps are hunting spots like any.
- A bot goes in through the gate, which the road graph already links (large-regions.md section 11).

## 7. The client

### Underground: the `cave` look

- **No sun and no sky:**
  - The sky is black, with fog from 14 to 40 m, out to 3.6 times as far zoomed out, as every sky's is.
  - The cascaded shadows are off.
  - Each body has a soft round shadow on the ground under it. All of them are one thin-instanced disc, within 25 m of the camera's target, as other players' shadows are today.
- **Light from the torches:**
  - The toon material gains a light-map term for a place with one. Every place material, body, drop, outfit piece and stand-in samples `light.bin` at its fragment's x and z.
  - Its light is a floor of 0.3, plus the rest warmed toward the torches' orange by the two groups:
    `floor + (1 − floor) × min(1, a × flickerA(t) + b × flickerB(t))`.
  - It's banded as the sun's light is, so the toon look holds. The bands are judged on the preview.
  - **Why a floor:** dim, never black. A mob, a player and a warning always show, and darkness never decides a fight.
- **Flames:**
  - A torch model's flame mesh draws unlit, at full colour, with a small glow card behind it. Both are thin instances.
  - Each scales with its group's flicker: two slow sums of sines at different rates, about ±8%.
- **What isn't lit:** warnings and their ghosts, glows, effects, labels and bars draw as they do on the land.
- **The KayKit models** are `torch_mounted` and `torch_lit`, with candles, rubble and pillars for camps, from the pack the treasure box came from (section 9).

### Underwater: the `sea` look

- **Sky and sun:**
  - The sky is deep blue-green, with fog from 20 to 60 m, scaled out with the zoom.
  - The sun is nearly overhead, cyan-tinted and softer. Its cascaded shadows stay, falling almost straight down.
- **Light rippling on the ground:** the same material term draws caustics on surfaces facing up, from 3D Worley noise in the shader, by x, z and time. There's no texture to fetch. (As built, step 4a: the plan had two scrolling layers of value noise.)
- **Rays:** six to eight long, faint, additive cards slant down from above, drifting slowly. They stay out of the camera's line to the player's chest.
- **Kelp and sea grass sway:** a kit model marked `sway` in `kit-models.ts` bends in the vertex shader by its height above its base. Its phase comes from its x and z, so a bed sways as a wave.
- **Bubbles:**
  - Thin streams rise from vents, which are props marked `bubbles`.
  - A few rise from each body's head every 3–5 s, within 25 m.
  - Both are one particle system each.
- **Fish:**
  - Small schools circle over the floor, 4–8 m up, above every body. Each school is one thin-instanced model of a dozen fish on a slow loop.
  - They're the client's alone, like the rays.
- **Characters:** wings flap, capes hang and hair stays as on the land. Looks are looks only.

### Map views and teleport

- **The minimap and the This region tab** draw the place's own tiles while the player is inside, named for it.
- **On the region's map,** each gate to a place below shows as a cave mouth or a dive spot icon, with the place's name and levels from its `region.json`.
- **Party:** a member in a place below the player's region shows at the gate to it. A member in the region above shows at the way back.
  - The client maps their spot by `under` and the gates, with no new message.
- **Teleport:** the waypoints of a place below are listed under the region above.

### Cost

- **A cave adds** one texture read and a few operations to each lit fragment, the flames and the shadow discs. It saves the cascaded shadows.
- **A sea adds** the caustics' operations, the rays, kelp's vertex sway, two particle systems and the schools.
- **The target** is the region's: a median frame under 8 ms at 1280 × 800 on the reference laptop, measured in the busiest camp with a fight on, both places.

## 8. Sea mobs

- **Four new mobs and a boss,** each from today's attacks (`mobs.yaml`): bite, hop, charge, spit and slam, with the statuses mobs already give.
- **As built** (B13 step 5), five fish that swim as flyers fly, drawn at their height's middle:

  | Mob | Level | Attack | Length |
  | --- | --- | --- | --- |
  | Piranha | 9 | bite | 1 m |
  | Lionfish | 10 | a poisoning spit, magic damage | 1.2 m |
  | Swordfish | 10 | charge, along a lane | 1.6 m |
  | Goblin Shark, the elite | 11 | a bite reaching 1.5 times as far | 1.8 m |
  | Anglerfish, the boss | 13 | spit and slam | 3.5 m |

  - **The levels are provisional,** set with the shrine's layout (B13 step 6). If the layout moves them, only the levels change; the stats follow `mobs.yaml`'s rule.
  - **Stats** sit with the cave's boar at like levels: below level 15 the balance harness only logs, as for the forest and the cave. The Goblin Shark takes about 2.5 times the Piranha's hits and gives 2.5 times its EXP. The Anglerfish takes 2.7 to 3.4 minutes alone, with its cooldowns the Vampire Bat's (spit 4.25 s, slam 8.25 s), the smallest that keep a melee player within 30 red potions.
  - **The four fish ship in `mobs.yaml` unplaced.** The Anglerfish's entry waits for step 6, since every boss needs a map that places it. Its tuned entry is in #333's description, and it's added with the shrine. Step 6's map brings them all under the balance tests.
  - **Each drops its own material:** a Piranha Tooth, a Lionfish Spine, a Swordfish Bill and a Goblin Shark Jaw. Each also drops red potions and the cave's band of gear, the boar's, with the next band rarely.
- **No new mechanic.** If a sea mob needs one, it stops and is brought back for a decision.
- **Stats** follow `mobs.yaml`'s rule for their area's levels, checked by the balance harness.
- **Models are picked with the user from previews:**
  - first from the Ultimate Monsters pack already in use, whose Fish blob and Squidle are drawn today;
  - any other pack only once it's vetted for its source, licence and files, as the packs in use were.
  - **As built,** they're from Quaternius's Animated Cute Fish Pack (section 9).
- **The cave's mobs** are today's Cave's: wild boars, cave bats and the Vampire Bat, moved to the area's levels.

## 9. Art

- **KayKit Dungeon Remastered 1.0** (CC0) gives the torches, candles, rubble, pillars, columns and broken walls.
  - It's the pack of the treasure box (loot-pouches.md), already vetted.
  - Each texture is shrunk to 256 px, as the chest's was, and listed in `web/public/models/LICENSES.md`.
- **The shrine** is built from the same pack's pillars, columns and broken walls, in sea-worn colours.
- **The sea mobs** are Quaternius's Animated Cute Fish Pack (CC0), from the author of the monsters in use.
  - The pack's own download fails, so each fish comes from its Poly Pizza page by Quaternius, which marks it CC0.
  - The files are glTF 2.0 from FBX2glTF, in flat colours, 1,270 to 1,940 triangles each, on one 6-joint rig. They hold nothing from outside the file.
  - They ship as Poly Pizza has them, as Mushnub and the Green Blob do: they face +z, and the client fits each to its mob's height and circle.
  - Each fish plays Swimming_Normal standing, Swimming_Fast moving, Attack, Swimming_Impulse when hit, and Death.
  - Poly Pizza's "Shark" comes from an older pack and isn't used.
  - The Anglerfish's lure may glow in the sea look, as looks only.
- **Coral, kelp, sea grass, shells and the small fish** are modelled in Blender, in flat colours.
  - No CC0 pack from KayKit, Quaternius or Kenney with them was found.
  - The coral packs found are sold on marketplaces under their own licences.
- **The cave mouth** is an arch of KayKit Forest rocks against the cliff, dark inside.
- **The dive spot** is a carved stone with a soft glow, like a waypoint's.
- **Previews:** each look's step ends with stills of a test cave and a test sea, for the user, before the places are designed.

## 10. Testing

- **Generator:**
  - halls and tunnels carved to the floor, walls too steep, `flat` halls flat;
  - torches every `spacing` metres along both walls, kept from gates, waypoints, camps and bosses;
  - the light map's falloff, stopped at walls, and dark rock tops;
  - the golden hashes on amd64 and arm64;
  - a bad plan refused: rock on a sea, a hall outside the region, a torch model with no light.
- **Loader:**
  - `under` and gates both ways;
  - a waypoint by each gate;
  - reachability, mob reach and calm from the gates;
  - the terrain-gap test on the cave's walls.
- **Server:**
  - **The hand-off:** through the gate from channel 2 lands in the place's channel 2, opened if closed. A full one sends the player to the least full. Gates between regions keep the least full.
  - **Death** inside gets the player up at the place's waypoint.
  - **The boss's notices** reach only players inside, and **chat** inside reaches only the place's channel.
- **Client:**
  - **Light:** a body under a torch draws brighter than one between torches, never under the floor. A warning draws the same in a cave as on the land.
  - **Flames:** each flame flickers with its group.
  - **Sea decorations:** the rays and fish never cross the camera's line to the chest. Kelp sways by its height.
  - **Map views:** a party member below draws at the gate.
- **Performance:** frames in the busiest camp of each place with a fight on, against section 7's target.
- **Live** (the player harness):
  - into the cave by its mouth, a fight between torches, a death and getting up inside, and out again;
  - a dive, a fight with sea mobs and their boss, and back up;
  - a party member shown at the entrance.

## 11. Build

These steps follow the regions' build (large-regions.md section 14).

1. **The generator,** after regions step 4b:
   - `under`, rock, halls and tunnels, torches and the kit's `light`, the light map and `light.bin`;
   - the loader's rules for a place below;
   - test plans for a cave and a sea, with golden hashes.
   - **As built (step 1a): rock, carving and the loader.**
     - **The plan:** `under`, `rock` and the `hall` and `tunnel` features. Torches, `light` and the light map come in 1b.
     - **The kit** gains `cave_floor`, `cave_rock`, `sea_sand` and `sea_rock`. Their colours are provisional, set with the stills of steps 3 and 4. A new ground moves every palette index, so every golden hash and the client's two bake fixtures moved with it.
     - **Carving:**
       - The rock is raised after the ground's noise and before the other features.
       - A sample's distance to a hall's polygon or a tunnel's line is offset by noise 8 m across, its own seed, scaled to ±`rough`.
       - Past the line, the wall rises to the rock over 2 samples, as a cliff does. At 0.5 m that's 4.5 m over 1 m, far past walkable.
     - **Colours:** rock and walls take `cave_rock`, as roads take `dirt`. "Darkened at the top" isn't a colour of its own: no torch sees a rock top, so the light map leaves it dark (step 3).
     - **Nothing is scattered on the rock,** only on carved floor.
     - **The generator refuses:**
       - rock on any sky but `cave`;
       - a hall or a tunnel with no rock;
       - one whose walls would reach the ring: every point keeps half its width, `rough` and the wall's rise inside the ring's face;
       - a cave plan when the kit has no `cave_rock`.
     - **The loader:**
       - the region above is `Map.Under`, not a `gamedata.Region` field;
       - a place below is under a region on the land, has a gate, leads every gate up to that region with a waypoint within 20 m, and has no town;
       - "one hunting area at least" follows, as every square inside the edge is in an area;
       - `linkMaps` checks each gate leads back.
     - **Not yet:** reachability, mob reach and calm from the gates, since no place below ships until step 6. The shipped regions' crossing test already starts from every gate.
     - **Test plans:** `regiongen/testdata/cave.yaml` (0.5 m, three halls and two tunnels) and `sea.yaml` (a basin with two reefs), hashed with the others. A test checks the test cave's gate, waypoint, start, camp and boss stand on carved floor.
   - **As built (step 1b): torches and the light map.**
     - **The kit's `light`:** a reach up to 32 m. `torch_mounted` is `{light: {height: 2.2, reach: 8}}`, and `torch_lit` a cylinder 0.15 m round and 1.9 m tall, with `{height: 1.8, reach: 8}`. The numbers are provisional until step 3's stills.
     - **The plan's `torches`** need rock, a model with a light, and a spacing of 6 m at least, as each keeps 3 m from the next.
     - **Placement:**
       - Spots are taken along the plan's lines, not the wall's samples: round a hall's polygon from half a spacing, and along a tunnel's smoothed path. The wall stays within `rough` of those lines.
       - From each spot the generator walks straight out, half a cell a step, to the first rock. The torch stands on the last carved spot, facing back into the carved space.
       - Its yaw is the nearest of 16, taken from `mapfile.Axis`, since the generator takes no angle from a direction.
       - Nothing hangs where no wall comes within reach (a tunnel's mouth, an opening between spaces), or where the walk starts in rock.
     - **A tunnel's left wall is hung first, then its right,** staggered half a spacing. Alternating walls in the props' order would put each wall's neighbours in one flicker group.
     - **A torch stands at the wall's foot on the floor,** its flame `light.height` over the ground there. Step 3 lifts the mounted model to its flame.
     - **Flames** are every prop of a model with a light, set pieces first, in the props' order. A flame's group is its index modulo 2.
     - **The light map** is sampled at texel centres, with distances across the ground. Its line of sight reads the ground's interpolated height.
     - **`light.bin`'s header** has a third field, 2, for the bytes in each texel, as every bake header carries three.
     - **The server builds the light map too,** overriding the design's "only the bake writes it". The loader names a region's folder by the hash of every baked file, `light.bin` among them, and the map is cheap.
     - **`region.json`** has `under` (empty on the land) and `lights`, each `{at, y, group}`, `[]` without torches.
     - **The test cave** hangs torches every 10 m, and has one standing torch as a piece.
2. **The server,** after regions step 6 (waypoints):
   - the paired channel at a gate below;
   - death, notices and chat inside, by tests;
   - bots through the gates.
   - **As built (the walls' follow-up, from #388's and #389's reviews).**
     - **The rock is a wall at any height.**
       - A walking-only flood of the test cave reached about 64,000 rock-top points. The wall beside its first tunnel, which runs nearly along x, rose 0.3–0.4 m a metre along x, under `max_slope`, while too steep across.
       - So every sample the carving leaves past a body's radius before the wall's foot carries deep water's mark, `sim.Terrain.Barrier`. The sim, nav and the bake already stop any move into a cell holding one from outside, at any height: a walk, a jump, a dash, a leap, a knockback or a blink. Shots still stop on the rock's height.
       - **The name:** `DeepWater` became `Barrier` in its own change, `Deep` `Barred`, and nav's `deep` flag `barred`, in Go and in the client (`barrier`, `barred`). The movement vectors' key is `barrier`.
       - `Generate` takes the body (`sim.Params`) for its radius, and the loader names a bake's folder by its slope and radius.
       - **Test:** nothing a player reaches from the start or any gate of a plan with rock, walking or jumping, lies deep in the rock, and the walk from each still reaches every camp and waypoint.
     - **Width:** a tunnel or hall narrower than a metre plus twice the wander, a body's radius and a cell is refused, and never under twice the `rough` + 1 m a torch steps in from a hall's edge. At 0.5 m with `rough` 1.5 that's 5.75 m, so the test cave's second tunnel widened from 5 to 6 m.
       - A hall is measured square across from each edge every metre, to the edges sharing no corner with it, so a sharp corner's wedge doesn't count.
     - **Joins:** a tunnel's floor is held to a hall's, or an earlier tunnel's, where its path runs through them, the lowest where several meet. Between joins it keeps within half the walkable slope of them, or halfway where two are too far apart in height for that. Inside a hall or an earlier tunnel it never carves below their floor, so its flat band doesn't dig into a sloping hall.
       - **Tests:** two seeds of a hall, a tunnel from inside it and a branch off that. Seed 10's tunnel dug a pit into the hall without the floor rule, and seed 28's branch stepped down into the tunnel without the join. Neither has a steep cell away from the walls now.
     - **Torches** stand a cell and a half short of the first rock on their walk, where the ground is the floor's. Half a cell short, they stood 0.6–1.3 m up the wall's slope, and their flames as much too high. The test cave's torches all have floor a metre ahead and the wall risen 2 m behind.
     - **Scatter's `away`** gains `torch`, kept from every prop with a light, since a mounted torch stands no solid. The test cave keeps 1 m.
     - **Channels:** a sweep shuts each channel it closes under the channels' lock, so a pick can open that number again at once. A paired crossing between the sweep and the shut had hit "instance … is open already".
     - **A party goes down split:** a gate below keeps each player's own channel, not their party's, so members in the region's channels 1 and 2 land in the place's 1 and 2, though 1 has room.
   - **As built (step 2).**
     - **The paired channel:**
       - Through a gate between a region and the place below it, either way, the instance asks the World for the player's own channel number (`Dest.Paired`).
       - The pick opens that channel if it's closed, though another has room. If it's open but full, the usual pick follows: the party's, the least full, then a new one.
       - Any other gate, and every teleport, asks for none.
     - **A place below has no fixed first channel.** None opens at the start, and its open count starts at 0. A join, a gate or a teleport opens one, and every channel closes once idle, the first included.
     - **Death, notices and chat** needed no change:
       - A place below is its own town, so the dead get up at its nearest waypoint.
       - Its boss's notices and its chat stay within its instance.
       - The World's tests check all three on a test cave below the test region. That cave is added through `worldtest.LoadDataWith`, which can now add a file, so no place below ships until step 6.
     - **Bots** needed none either: `cmd/bot` walks every map a portal leads to, so a place below's camps are hunting spots once a gate leads there.
3. **The cave look,** after regions step 5b:
   - the material's light-map term, flames, the round shadows and the black sky;
   - KayKit's torches;
   - map-view icons and party at the gate;
   - stills for the user.
   - **In four PRs:** 3a-1 the light, 3a-2 the round shadows, 3b KayKit's torches and their flames, 3c the map views and teleport; the stills follow 3b.
   - **As built (3a-1, the light).**
     - **The bake, read:** `Region` gains `under` and `lights` (each flame's x, z, height and group). `light.bin` is fetched with `region.json` when there are flames, and refused unless it holds two groups and is whole. The map carries the light map beside its overview.
     - **A cave region has no sun:** no directional light and no shadow generator, so `Lighting.shadows` is null there. The old open-topped Cave map, which isn't a region, keeps the sun till step 6 replaces it.
     - **The texture:** the light map as RGBA, red and green its groups', sampled bilinearly between texel centres and held at its edges, at `x / nx` and `z / nz`. A cave with no torches gets one dark texel.
     - **The term:** `min(1, a × fA(t) + b × fB(t))`, each group's flicker `1 + Σ amp × sin(rate × t + phase)` over two sines whose amplitudes add to 0.08. `t` is the engine's seconds (`RealTime`), the same for every material, so the flicker runs in the shader.
       - It's banded as the sun's light is: floored into thirds and stretched to 0, ½ and 1. The tone runs from the floor, 0.3 grey a little cool, to the torches' warm light, (1, 0.86, 0.66), both sRGB.
       - With a torch's 8 m reach, the full band holds within about 3.4 m of a lone flame, and the half band to about 5.2 m. The colours and bands are provisional until the stills.
       - Decals and glows aren't toon materials, so warnings, ghosts and glows draw as on the land.
     - **The sky:** black, with fog from 14 to 40 m. The old open-topped Cave map shares the sky's key, so it takes the same black and fog under its sun till step 6 replaces it.
     - **Tests:**
       - a small cave below tiny (`hollow`, a hall and a tunnel at 2 m), baked by regionbake's tests into the client's fixtures: its flames in both groups, light under each flame, none on the rock, and a light map refused;
       - the material's own node graph run block by block for a texel: the full band under a flame at every flicker, the floor where no torch reaches, each group swinging ±8% at its own rates, and two torches held to full;
       - a cave region in the world: no sun, its ground sampling the light map, its sky black; a region on the land keeps the sun.
   - **As built (3a-2, the round shadows).**
     - **One disc, thin-instanced,** in a cave region only. Each frame, once the bodies are drawn, it lays one under the player, the others, the mobs (the dying too) and the NPCs within 25 m of the camera's target, as far as bodies cast the sun's shadow on the land.
     - **Its size and place:** 1.6 times the body's radius (a mob's own, the sim's 0.375 m for a player or an NPC), on the ground under the body's feet and 4 cm over it: a solid's top it stands on, a platform or a bridge, as the sim finds its ground. A jump leaves it on the ground.
     - **On a slope** it's sheared to the terrain's plane, from the terrain's rise across the disc each way, so its uphill edge isn't buried; on a solid's top it lies flat. A shear was picked over a tilt: the footprint stays round from above, and it's two numbers in the instance's matrix.
     - **Its look:** black, 0.55 at its middle, held to 35% of its radius, then fading to nothing at its rim. It blends, writes no depth, and clears the stencil as the place does, so a hidden telegraph's ghost shows over it. It draws first among the blended meshes on the ground, under a red area and the lock ring.
     - **Only a body drawn gets one:** none under one whose model is still loading, or one hidden.
     - **No garbage:** the buffer is written in place each frame and doubles when it's full, and the walk over the bodies runs `forEach` with callbacks made once.
     - **Tests:** discs under the bodies within the reach and none past it, each at its ground and radius, blended, clearing the stencil, under the areas; only the frame's own drawn after a crowd, the buffer grown; on a hillside with a platform, a disc on the platform its body stands on, flat, and one on the slope sheared each way; in the world, the player's disc, none under a model still loading, then Robin's; none on the land.
   - **As built (3b, KayKit's torches and their flames).**
     - **The files:** `torch_lit` and `torch_mounted` from KayKit Dungeon Remastered 1.0, each texture shrunk to 256 px as the chest's was. `web/scripts/torches.ts` makes `models-src`'s from the pack's files, the same bytes on every run; `LICENSES.md` names it and says what was done to each.
       - **The flame:** `torch_lit`'s flame triangles, told by their atlas colour (the flame's orange and yellow), cut out into `torch_flame`. `torch_mounted` has no flame of its own and takes the same one.
       - **The origin:** each file has its cup's centre at its origin, the flame's too, so a torch stands raised by its kit model's `y`, kit.yaml's light height, and its flame draws at its light.
     - **The scale:** 0.71, the characters' (the Knight's 2.467 units drawn 1.75 m), so the torches keep KayKit's own proportions beside the bodies. A mounted torch is 0.75 m from its bracket's foot to its cup's rim.
     - **The standing torch:** at that scale KayKit's torch is a 0.75 m hand torch. Its handle's shaft is stretched into a stake, so its cup stands 1.8 m up, as kit.yaml's light has it; kit.yaml's thin cylinder, 0.15 m round and 1.9 m tall, is kept.
     - **The mounted torch** faces out from the wall, its bracket behind it at −z. It stands a cell and a half short of the rock, so its bracket may hang clear of the wall; the stills judge it.
     - **Flames:** one thin-instanced mesh of `torch_flame`, drawn unlit, at full colour and fogged, one instance a light.
       - Each scales with its group's flicker. `flicker()` runs the light's FLICKERS table in JS on the toon materials' clock, the engine's seconds, so a flame swings with the light it gives.
       - **The glow card:** a plane 0.9 m across per flame, turned to the camera about the vertical and set 12 cm behind the flame from it, so the flame draws over it. It draws after the ground's blended meshes and before every effect, so a fight's effects draw over it. It's the torches' orange, 0.5 at its middle and fading to its rim, added to what's behind it, writing no depth. It's fogged, so a far glow fades into the cave's black with its flame.
     - **Tests:**
       - the test cave gains a standing torch in its hall, so it has both models;
       - in the world, each torch's cup at its light, mounted and standing alike; each flame at its light, unlit, at its group's flicker for the frame's moment; each card turned to the camera and behind its flame; the card added, fogged, out in gamma;
       - the JS flicker against the material's own graph, at every moment.
4. **The sea look:**
   - caustics, rays, sway, bubbles and fish;
   - the Blender models;
   - stills for the user.
   - **The models** came first, in #325: coral, kelp, sea grass, shells and a small fish, our own work.
   - **In three PRs:** 4a the light (the sky, the sun, the caustics and the rays), 4b the sea props in the kit and their sway, 4c the bubbles and the fish. The stills follow 4c.
   - **As built (4a, the light).**
     - **The sky:** deep blue-green, `#15576a`, with fog from 20 to 60 m.
     - **The sun:** nearly straight down, along (0.1, −1, 0.18). Its shadows are cascaded, as on the land's regions, so they fall almost under what casts them.
     - **The tone:** the sun's light is banded as on the land, from a blue-green shade, (0.46, 0.6, 0.66), to the sun's cyan, (0.84, 0.97, 1), both sRGB. The shade is nearer the light than the land's, so the light is softer. The colours are provisional until the stills.
     - **The caustics:** one layer of 3D Worley noise, Babylon's own block. Its web is where a fragment's nearest two points are nearly as near as each other: F2 − F1 under 0.08 of a cell.
       - Its cells are 2.5 m across, over x and z only, so a body's head and the floor under it share the web.
       - Time is its third axis, at 0.3 cells a second, so the web shifts. It also drifts 0.05 and 0.02 cells a second along x and z.
       - It adds (0.3, 0.42, 0.42), linear, times the web, on faces turned up: none at 0.5 of straight up, all of it from 0.9.
       - It adds only where the sun reaches, and the more as it does: the full band takes it all, the middle band half and the shade none, so a shadow has no caustics in it.
     - **The rays:** seven cards, one thin-instanced plane. Each is 24 m long and 1.6 to 3.4 m wide, slanting up the sun's way from 2 m under the player's feet.
       - They stand in a 48 m square round the player, fixed in the world as they drift. Each drifts at 0.25 m a second, all much the same way, and wraps to the square's far side.
       - Each swells and thins by 15%, slowly, and turns about its length to the camera.
       - A ray narrows to nothing over the 4 m before the square's edge, so none pops as it wraps. Near the camera's line to the chest, it narrows to keep 1 m from that line.
       - **Its look:** the sun's cyan, 0.12 at its brightest. It fades to nothing at its sides, rises from nothing over the lower 70% of its length, and fades out over its top 10%. It's added to what's behind it and writes no depth. It's fogged into black, not the sea's colour, so a far ray fades away rather than tinting the water.
     - **The shaders aren't compiled in CI:** the unit tests run on a null engine. The stills show them, and no shipped map uses the sea sky until step 6.
     - **Tests:**
       - **The fixture:** a small sea below tiny (`shoal`, a basin with a reef, at 2 m), baked by regionbake's tests into the client's fixtures.
       - **The material's own node graph, run block by block for a fragment:**
         - a cyan top band, and a shade nearer the light than the land's;
         - caustics on the web where a face turns up in the sun; none inside a cell, on a wall or in shade; half in the middle band;
         - the web by x, z and time;
         - none on the land or in a cave.
       - **The rays:**
         - six to eight, faint, added, fogged into black;
         - slanting from above, turned to the camera;
         - never across the camera's line to the chest, over 400 spots, cameras and moments;
         - drifting under a metre a second, and round the player as they go;
         - thinned to nothing as they wrap.
       - **A sea region in the world:** a sun from nearly overhead with shadows, caustics on its ground, its sky and fog, and rays round the player, drifting.
5. **Sea mobs:**
   - their models picked with the user from previews;
   - their data and stats, with the balance harness.
6. **The two places:** designed with the green region's layout (regions step 8), from the generator's previews, and built with it.

## 12. Decisions

The user's choices are in section 1. These calls were made in planning and logged for the user to push back on:

1. **A place below is a small region to the code.** It reuses every part of the regions' engine, and adds only carving, torches, the light map, the paired channel and two looks.
2. **Separate maps, behind a short load,** because the ground is a height map (section 1).
3. **The same channel number in and out,** so the players seen going in are there, and parties stay together.
4. **Torch light is baked into a light map,** not drawn with lights.
   - It's one texture read for any number of torches.
   - Babylon draws a material with 4 lights by default. Choosing the nearest 4 makes light pop as torches swap.
   - A torch's own shadows would take 6 renders each.
5. **Dim, never black:** a light floor of 0.3, so darkness never hides a mob, a player or a warning.
6. **No sun underground:** bodies get round shadows on the ground, and the cascaded shadows are off.
7. **Each place has its own General chat,** since each is its own place and channel.
8. **Walls 4–4.5 m,** the height the cut-away was tuned on.
9. **Caustics in the shader,** with no texture to fetch or vet.
10. **Sea props modelled in Blender,** since no vetted CC0 pack has them.
11. **Death inside gets the player up inside,** at the place's waypoint.
12. **A party member below shows at the entrance,** worked out by the client from the gates.
13. **Light changes no rule:** mobs see as far in the dark, and nothing hides in it.

## 13. Alternatives considered

- **Caves under the region's own ground, with no load:** the ground would need overhangs, so meshes or voxels in place of the height map. The sim, nav, the bake, the golden hashes and the cut-away would all change, and the camera would have to look through hills. That's weeks of work, for one short load saved.
- **Lights per torch:** Babylon draws a few at once, so light would pop as torches swap, and each torch's shadows would take 6 renders.
- **A torch the player carries:** the user chose torches in the place. A carried light would also make the dark a choice of gear.
- **An air bar, or swimming up and down:** the user chose walking on the sea floor.
- **Private copies for a party:** the user chose open maps. Party dungeons (C7) can reuse these places later.
- **Coral and kelp packs from marketplaces:** their licences aren't CC0, and none comes from a source already vetted.
