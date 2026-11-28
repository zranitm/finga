# The snow region: Hoarfell

> **Status: approved · 2026-11-28.** The design of the third and last large region, levels 22 to 30, with the Fallen Keep on its summit as the endgame, levels 31 to 33. It's step 9 of [Large regions](./large-regions.md), after [The desert](./desert-region.md). It replaces the last small maps: Frost Trail, Icefall, Glacier Peak, the Fallen Keep and the Crossroads.

## 1. What was picked

- **The user asked for it** on 2026-11-28 ("plan for snow region also"), saw the generator's preview of the draft plan, and said "go ahead".
- **A mountain climbed in terraces,** from a village at its foot to the keep on its summit. Each area is a step up, so the next is always above you.
- **Today's mobs, with no new ones:**
  - The snow mobs are at the levels the desert's build gives them (desert-region.md section 4), and the keep's are unchanged.
  - There's no place below, since there are no mobs left over to fill one.
- **The plan** is the draft the preview was drawn from, `hoarfell.yaml`. It moves into `server/data/regions/` with the build.
- **Names:**
  - The four areas keep the old maps' names: Frost Trail, Icefall, Glacier Peak and the Fallen Keep. Players who knew them find them again.
  - Hoarfell, Emberhold and Last Hearth are placeholders.

## 2. The layout

The region is 1024 m square, with mountains 72 m wide round its edge. You arrive from Sunreach in the south-west and climb toward the north-east.

| Area | Kind | Levels | Where | Height | Ground |
| --- | --- | --- | --- | --- | --- |
| **Emberhold** | town, the main one | | the south-west corner, by the West Pass | the foot | packed earth among pines |
| **Frost Trail** | hunting | 22–24 | an L along the south and the west | the foot | snow, pines and a small lake |
| **Icefall** | hunting | 25–27 | an L inside the Frost Trail's | 12 m up | snow, with cliffs of blue ice |
| **Last Hearth** | town | | where the road reaches the glacier | 24 m up | packed earth, a lodge and two torches |
| **Glacier Peak** | hunting | 28–30 | an L in the north-east | 24 m up | an ice sheet, with crags |
| **Fallen Keep** | hunting | 31–33 | the north-east corner | 34 m up | stone, broken walls and a columned hall |

- **The terraces** are three plateaus, each standing on the one below:
  - the Icefall's, 12 m;
  - the glacier's, 12 m more;
  - the summit's, 10 m more.
  - Each cliff stands on its area's line, so the foot of a cliff belongs to the area below.
- **Ice:** a new ground colour, `ice`, in the kit. The Icefall's cliffs and the glacier's flat ground are ice, so the glacier reads as an ice sheet, apart from the snowfields.
- **Frostmere** is a small lake on the Frost Trail, near Emberhold, with gentle banks.
- **Roads:**
  - **The main road** runs from Emberhold past Frostmere, ramps up to the Icefall, up again into Last Hearth, and up to the Keep Gate. It's the only way onto the summit, so the keep is a place you reach rather than wander onto.
  - **An east road** runs through the Frost Trail's snowfields.
  - **A north road** runs up the Frost Trail's west arm, climbs to the Icefall's north end, and climbs on to the glacier's west arm.
- **Waypoints:** 13:
  - Emberhold;
  - South Trail, Frost Meadows, East Snowfield, Pinewood and North Pines on the Frost Trail;
  - Icefall Steps, Frozen Shelf and North Ice on the Icefall;
  - Last Hearth;
  - Rime Ridge and Glacier Field on the glacier;
  - the Keep Gate.
- **Camps:**
  - 25: eight on the Frost Trail, seven on the Icefall, five on the glacier and five in the keep;
  - normal mobs come back after 30 s and elites after 40 s;
  - every camp keeps clear of the stones, the start, the gate and the NPCs, by the loader's rule.
- **Bosses:** one per hunting area, in its far corner from the way in, two levels over its top. The Avalanche Slime is at the Frost Trail's north-west end, the Blizzard Wing at the Icefall's, the Crystal Colossus at the glacier's west arm's end, and the Shade Lord in the keep's hall.
- **Nothing a player can stand on** in the hunting areas, as in the desert (desert-region.md section 2):
  - the icewings spit low and the snails charge;
  - pines, boulders (2.31 m), crags, the keep's walls (3.52 m) and its columns (4.5 m) are all past a jump or looks only.
- **The keep:**
  - three short rows of broken wall, and four columns round the Shade Lord's hall;
  - his body is 3.75 m across, and every gap inside his leash is far wider: the columns stand 40 m apart, and the walls are short rows with open ground round them.

### The gate

| Gate | Where | Leads to |
| --- | --- | --- |
| **West Pass** | Emberhold's west edge | Sunreach's East Pass, which led to the Crossroads |

It's the region's only gate: Hoarfell is the end of the world's road.

### NPCs

- **Emberhold:** the Crossroads Guide moves here as the Mountain Guide (`mountain_guide`):
  - "Welcome to Emberhold. Warm your hands; the climb is cold."
  - "The Frost Trail circles the mountain's foot. The Icefall's cliffs are the way up."
  - "Past the glacier, Last Hearth keeps a fire for climbers. Above it stands the Fallen Keep, and few come back."
  - "If you fall out there, you'll wake at the nearest waypoint."
- **Last Hearth:** a new Hearth Keeper (`hearth_keeper`), who only talks:
  - "Come in from the wind. This is the last fire on the mountain."
  - "The keep's lord still walks his hall. Go up ready, or don't go up."
  - "The rime rats chew anything left outside. Keep your packs close."
- **Quests need no change:** none names a snow or keep mob, or one of their maps.

## 3. The mobs

No mob moves in this step. The snow mobs moved with the desert's build, and the keep's never move.

| Area | Normal | Elite | Boss |
| --- | --- | --- | --- |
| Frost Trail, 22–24 | Frost Slime 23 | Snow Weasel 24 | Avalanche Slime 26 |
| Icefall, 25–27 | Icewing 26 | Glacier Snail 27 | Blizzard Wing 29 |
| Glacier Peak, 28–30 | Crystal Slime 29 | Rime Rat 30 | Crystal Colossus 32 |
| Fallen Keep, 31–33 | Shade Bat 31 | Gloom Slime 33 | Shade Lord 35 |

- The four bands run from the desert's top, 22, to the keep's, with no level left out.
- Players at the level cap, 30, have Glacier Peak's rime rats at their own level and the keep above them.

## 4. Retiring the last maps

- **The maps go:** `frost_trail`, `icefall`, `glacier_peak`, `fallen_keep` and `crossroads` leave `data/maps`, and their places leave the world chart.
  - After this, `data/maps` holds only the PvP maps.
  - The chart shows the three regions alone.
- **The chart** gains Hoarfell, east of Sunreach, where the snow maps stood.
- **Sunreach's East Pass** leads to Hoarfell's West Pass.
- **Characters on a retired map** join at Willowmere's start, as large-regions.md section 12 says.
- **The kit** gains `ice`, and `npm run gen` writes it into the client's ground table.
- **Bots and tests** that walk the old maps move to Hoarfell, or to fixtures of their own where a test only needs a small map.

## 5. The build

One PR, after the desert's, since it relinks the desert's East Pass:
- the `ice` ground in the kit;
- the plan in `data/regions`;
- the five maps retired and the East Pass relinked (section 4);
- the Mountain Guide and the Hearth Keeper;
- the chart, the teleport list, bots and tests.
- Hoarfell's hunting areas then pass the three design tests (large-regions.md section 14):
  - no mob sees an arrival;
  - each mob reaches a player anywhere in its leash;
  - no solid leaves a gap only a player fits.

  These may move a camp a few metres, as in Willowmere.

**Then:** the generator's previews of the built region, and a live walk from the West Pass up the terraces to the keep. Both are heavy runs, approved by the user first.

## 6. Decisions

The user asked for the region and approved the preview (section 1). These calls were made in design and logged for the user to push back on:

- **Terraces, with the keep reached only by the main road,** so the climb is the region's shape and the keep is somewhere you arrive.
- **The old maps' names for the four areas.**
- **No new mobs and no place below.** An ice cave with three new mobs was offered, and could come later under the glacier.
- **The snow levels set in the desert's build** (desert-region.md section 4): the Icefall's mobs moved two levels, not three, and Glacier Peak's stayed, so the bands run 22–24, 25–27 and 28–30 with no gap.
- **Two NPCs:** the Crossroads Guide moved to Emberhold, and a new Hearth Keeper in Last Hearth.
- **The `ice` ground** for the Icefall's cliffs and the glacier.

## 7. Alternatives considered

- **An ice cave below with three new mobs:** more variety, but another batch of art. It can come later as a place under the glacier.
- **The keep as a place below,** an interior: the keep is the region's landmark, on the summit above everything else, so it stays outside.
- **The glacier in snow like the rest:** from above, the three snowy areas read as one, so the glacier is ice.
