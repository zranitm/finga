# The desert: Sunreach

> **Status: approved · 2026-11-28.** The design of the second large region, levels 12 to 22, and the two places below it. It's step 9 of [Large regions](./large-regions.md), and its places follow [Underground and underwater](./underground-and-underwater.md). It replaces the mushroom woods (Shroom Grove, Spore Hollow, Toadstool Heights) and the desert maps (Dune Road, Sandstone Cliffs, Sunken Ruins).

## 1. What was picked

- **Wet to dry, with today's mobs moved down** (the user's call on 2026-11-28). The other two options offered:
  - a desert-only desert with about twelve new mobs, as the green region got;
  - every mob keeping its level, which would put the desert's mobs in the snow region's range and the mushroom woods' in the desert's.
- **Why moving works now:**
  - Each mob type has one level, and the old maps held those levels in place.
  - Once the desert and the snow region replace every old map, nothing holds them, so a mob can move.
  - The woods' and the desert's eighteen mob types fill the desert's 12 to 22 as six areas of three: a normal mob, an elite and a boss, as each green area has.
- **Why wet to dry:**
  - The woods' mobs come first by level, so they live in the south-west, by the gate from Willowmere: a shaded river gorge of giant mushrooms.
  - The land dries northward and eastward to the oasis, the dunes and the sandstone mesa, where the desert's mobs live.
  - Willowmere already has a sunny mushroom glade. The gorge sets its mushrooms in a river valley under terraces, and Spore Hollow takes them underground.
- **The layout** was drawn by the region generator from three draft plans, and the user saw the previews on 2026-11-28 ("ok that could work"):
  - `sunreach.yaml`, the region;
  - `spore_caves.yaml` and `ruin_crypt.yaml`, the places below.
  - They move into `server/data/regions/` with the build.
- **Names are placeholders:** Sunreach, its areas, its town and its places can be renamed later without moving anything.

## 2. The layout

The region is 1024 m square, with mountains 72 m wide round its edge. You arrive from Willowmere in the south-west and climb north and east through the levels.

| Area | Kind | Levels | Where | Ground |
| --- | --- | --- | --- | --- |
| **Sporewood Gorge** | hunting | 12–13 | south-west, along the river | grass under giant mushrooms and oaks |
| **Spore Hollow** | hunting, below | 14–15 | under the gorge, through the Hollow Mouth | a fungus cave (section 5) |
| **Toadstool Ledges** | hunting | 16–17 | west, on terraces 8 m up | grass, ring mushrooms and pines |
| **Palmwater** | town, the main one | | the middle, by the oasis | grass and palms |
| **Amber Dunes** | hunting | 18–19 | east and south-east | sand, cacti, low dunes and two buttes |
| **Sandstone Cliffs** | hunting | 20–21 | the north, on a mesa 10 m up | sand, sandstone pillars and crags, red cliff faces |
| **Sunken Ruins** | hunting, below | 21–22 | under the mesa, down the Ruins Stair | a buried temple (section 6) |

- **The river** comes down from the western mountains, runs east along the foot of the ledges, then turns south through the gorge and out through the south mountains.
  - It's 12 m wide and deep, so it blocks.
  - One ford crosses it, on the road to the Hollow Mouth. The main road keeps to the east bank.
- **The oasis** is a lake by Palmwater, fed by a spring, not the river. The generator needs both ends of a river in the mountains, and a spring suits a desert.
- **Gentle banks** on the river and the lake, as in Willowmere: every bank can be walked back up.
- **The gorge's east wall** is a broad sandy rise, 14 m high, between the gorge and the south dunes.
- **The ledges** are a plateau 8 m up, with cliff edges all round.
- **The mesa** is 10 m up, with cliff edges coloured as sandstone. On its top:
  - a crag 8 m higher;
  - a low swell across the west.
- **The dunes** have four low ridges, up to 4 m. They also have two buttes, 12 and 14 m high, that can't be climbed.
- **Roads:**
  - **The main road** runs from the South Gate up the gorge's east bank to Palmwater.
  - **The west road** climbs onto the ledges, crosses them, and climbs the mesa at its west end.
  - **The east road** runs past the oasis into the dunes.
  - **The north road** climbs the mesa and runs to the East Pass, with a branch to the Ruins Stair.
  - **A short road** runs from the main road over the ford to the Hollow Mouth.
  - Where a road meets a cliff, it cuts a ramp, as in Willowmere.
- **Waypoints:** 14:
  - Gorge Mouth, Sporewood and Hollow Mouth in the gorge;
  - Palmwater;
  - Toadstool Ledges and Ledges North;
  - Oasis Shore, South Dunes and East Dunes;
  - Cliff Road, Mesa Top, West Mesa, Ruins Stair and East Pass.
- **Camps:**
  - 25: six in the gorge, six on the ledges, seven in the dunes and six on the mesa;
  - normal mobs come back after 30 s and elites after 40 s, as in Willowmere's new areas;
  - every camp keeps clear of the stones, the start, the gates and the NPC, by the loader's rule.
- **Bosses:** one per hunting area, two levels over its top (section 3).

### Nothing a player can stand on where mobs charge or spit

- Every hunting area here has a charger or a spitter. The region tests ask that each mob reach a player anywhere in its leash.
- A player on a 1.1 m rock is out of a charge and a low spit. So the hunting areas scatter no solid a player can stand on: no small rocks or stones, and no stumps.
- **What they scatter is either looks only or past a jump:**
  - cacti, palms, dry trees, shrubs and small mushrooms are looks only, as the old desert maps' cacti were;
  - sandstone boulders (2.31 m), ring mushrooms (2.45 m), giant mushrooms and pillars are past a jump.

### Gates

| Gate | Where | Leads to |
| --- | --- | --- |
| **South Gate** | the gorge's south edge | Willowmere's north pass, which led to Shroom Grove |
| **Hollow Mouth** | the gorge's west, at the mountains' foot | Spore Hollow |
| **Ruins Stair** | the mesa, between four broken columns | the Sunken Ruins |
| **East Pass** | the mesa's north-east corner | the Crossroads, until the snow region is built |

### NPCs

- **Palmwater:** a new Oasis Guide, who only talks, so the town has someone to visit:
  - "Welcome to Palmwater. The spring here has never run dry."
  - "South, the river cuts the Sporewood Gorge, and a hollow full of moths opens under it."
  - "The dunes run east past the buttes, and the mesa stands north. Something old sleeps in the ruins beneath it."
  - "If you fall out there, you'll wake at the nearest waypoint."
- **The Crossroads Guide** stays at the Crossroads, with lines for its new roads:
  - "Welcome to the Crossroads, where the desert road meets the snow."
  - "West through the pass lies Sunreach, and the oasis at Palmwater."
  - "The Frost Trail climbs to Glacier Peak. Past it stands the Fallen Keep, and few come back."
  - "If you fall out there, you'll wake up here."
- **Quests need no change:** none names a mob of the woods or the desert, or one of their maps.

## 3. Today's mobs move down

| Mob | Role | Level today | Level now | Where |
| --- | --- | --- | --- | --- |
| Spore Slime | normal | 15 | **12** | Sporewood Gorge |
| Mossback Snail | elite | 16 | **13** | Sporewood Gorge |
| Spore Mother | boss | 18 | **15** | Sporewood Gorge |
| Dust Moth | normal | 17 | **14** | Spore Hollow |
| Toadstool Rat | elite | 18 | **15** | Spore Hollow |
| Moth Empress | boss | 20 | **17** | Spore Hollow |
| Glow Slime | normal | 19 | **16** | Toadstool Ledges |
| Elder Snail | elite | 20 | **17** | Toadstool Ledges |
| Lantern Slime | boss | 22 | **19** | Toadstool Ledges |
| Sand Mouse | normal | 21 | **18** | Amber Dunes |
| Shellback Scarab | elite | 22 | **19** | Amber Dunes |
| Dune Baron | boss | 24 | **21** | Amber Dunes |
| Dust Vulture | normal | 23 | **20** | Sandstone Cliffs |
| Sun Slime | elite | 24 | **21** | Sandstone Cliffs |
| Cliff Reaper | boss | 26 | **23** | Sandstone Cliffs |
| Tomb Rat | normal | 25 | **21** | Sunken Ruins |
| Ruin Wisp | elite | 26 | **22** | Sunken Ruins |
| Crypt Warden | boss | 28 | **24** | Sunken Ruins |

- **Looks, sizes, attacks and statuses stay:** the dust moth still poisons and the scarab still makes you bleed.
- **Stats:**
  - each moved mob's HP, attack, defenses, accuracy, evasion and EXP are set again for its new level by `mobs.yaml`'s rule, as the Cave Bat's and Vampire Bat's were;
  - each boss is set by the field-boss rule from its area's top normal mob;
  - the balance test checks those of level 15 and up.
- **Drops:**
  - The gear in each moved mob's table follows its new level, by `mobs.yaml`'s rule: its area's band at normal rates and the next band's rarely. Potions follow the band too.
  - Its own material, such as Spore Jelly or a Tomb Fang, stays.
  - **Why, when the bats kept theirs:** the bats moved two levels. A move of three or four would leave a mob dropping gear its area's players can't wear yet.

## 4. The snow mobs move too

| Mob | Role | Level today | Level now | Map |
| --- | --- | --- | --- | --- |
| Frost Slime | normal | 26 | **23** | Frost Trail |
| Snow Weasel | elite | 27 | **24** | Frost Trail |
| Avalanche Slime | boss | 29 | **26** | Frost Trail |
| Icewing | normal | 28 | **26** | Icefall |
| Glacier Snail | elite | 29 | **27** | Icefall |
| Blizzard Wing | boss | 31 | **29** | Icefall |
| Crystal Slime | normal | 29 | 29 | Glacier Peak |
| Rime Rat | elite | 30 | 30 | Glacier Peak |
| Crystal Colossus | boss | 32 | 32 | Glacier Peak |

- **Why now:** the desert ends at 22. Without this, the old snow maps would start at 26 beyond the East Pass until the snow region is built, a four-level jump.
- **The maps' bands:** Frost Trail 23–24, Icefall 26–27, Glacier Peak 29–30.
  - Glacier Peak doesn't move: its top stays at the level cap, 30, for the climb before the keep.
  - The snow region's three areas can then cover 22–24, 25–27 and 28–30 with no gap.
  - The Fallen Keep's mobs stay at 31 to 35.
- **Stats and drops** follow the same rules as section 3.
- The snow region lays these mobs out again when it's designed.

## 5. Spore Hollow

- **The cave:**
  - 384 × 256 m, with Greyfang Cave's look: the cave sky, rock walls 4.5 m high and wall torches every 12 m;
  - small mushrooms over the floor, three ring mushrooms, and standing torches by the camps.
- **Its halls, as a loop:**
  - the mouth in the south, flat, with the gate and the waypoint;
  - the moths' gallery west, flat for their spit;
  - the rats' warren north;
  - the Moth Empress's chamber east, flat;
  - a tunnel from the chamber back to the mouth, so nobody walks back the way they came.
- **Camps:** two of dust moths, four each, back after 36 s; two of toadstool rats, three each, back after 45 s. These are Greyfang Cave's times.

## 6. The Sunken Ruins

- **The temple:** 384 × 256 m, with the cave sky, rock walls 5 m high, sand drifted over the floor and wall torches every 10 m.
- **Its halls:**
  - the stair in the south, flat, with the gate and the waypoint;
  - the nave north of it, flat, with four columns;
  - the west crypt, flat;
  - the Crypt Warden's tomb east, flat, with four columns;
  - a tunnel from the crypt back to the stair.
- **Camps:** tomb rats and ruin wisps in the nave and again in the crypt. Rats are four to a camp, back after 36 s; wisps are three, back after 45 s.
- **The Warden's room:**
  - His body is 5.25 m across, as the old map's rule noted.
  - Every gap inside his leash is wider than that: the columns stand 8 m from the walls, and the tunnels are 10 m wide.

## 7. Retiring six maps

- **The maps go:** `shroom_grove`, `spore_hollow`, `toadstool_heights`, `dune_road`, `sandstone_cliffs` and `sunken_ruins` leave `data/maps`, and their places leave the world chart.
- **The chart** gains Sunreach, north-east of Willowmere, where the woods stood.
- **The links:**
  - Willowmere's north pass leads to Sunreach's South Gate.
  - The Crossroads' portal to Toadstool Heights becomes `from_desert`, to the East Pass.
  - The Crossroads' portal to Dune Road goes. Its road to the Frost Trail stays.
- **Characters on a retired map** join at Willowmere's start, as large-regions.md section 12 says.
- **The places' ids are new,** `spore_caves` and `ruin_crypt`, so a character saved on the old Spore Hollow or Sunken Ruins never joins inside a new place's rock.
- **The test region** is a generator fixture with the woods' mobs. Its bands follow their new levels.
- **Bots and tests** that walk the old maps move to Sunreach, or to fixtures of their own where a test only needs a small map.

## 8. The build

Each step is its own PR:

1. **The snow mobs moved:** levels, stats and drops (section 4), and the Frost Trail's and Icefall's bands. Data only.
   - **As built:** the Frost Trail's Frost Slime 23, Snow Weasel 24 and Avalanche Slime 26, and the Icefall's Icewing 26, Glacier Snail 27 and Blizzard Wing 29, with bands 23–24 and 26–27; Glacier Peak and its mobs are unchanged.
   - Stats come from the balance test and drops from the tables of the same levels. Each boss has twice its top normal mob's attack, with its cooldowns lengthened by the smallest factor that keeps a melee player within 30 of the band's potion.
2. **The desert:**
   - the three plans in `data/regions`;
   - the eighteen mobs moved (section 3);
   - the six maps retired and the links changed (section 7);
   - the Oasis Guide, and the Crossroads Guide's lines;
   - the chart, the teleport list, bots and tests.
   - Every shipped region's hunting areas then pass the three design tests (large-regions.md section 14):
     - no mob sees an arrival;
     - each mob reaches a player anywhere in its leash;
     - no solid leaves a gap only a player fits.

     These may move a camp a few metres, as they did in Willowmere.

- **As built (desert PR 2):**
  - **The chart** places Sunreach at [38, 32], north-east of Willowmere.
  - **The plans** are in `data/regions` as drafted: Sunreach, `spore_caves` and `ruin_crypt`. The loader's checks passed with no camp moved; the nav design tests moved two:
    - the west slimes from (240, 316) to (234, 316), and the rim elders from (380, 560) to (372, 576), so no leash reaches a spot on a terrace's top or beyond a drop where no blow lands;
    - the Sunken Ruins' tunnel back to the stair bends through (36, 112) and (52, 84) in place of (36, 100), so no rock beside the bend can be walked onto.
  - **Mob stats** are set by the balance test's bands for each area, not scaled from the old levels:
    - a normal mob takes 2.9 hits at the reference pace in Sporewood Gorge, 3 in the four areas after it and 4 in the Sunken Ruins, with the bands' survival and hit-rate targets for every class;
    - an elite takes 2.5 times its normal mob's hits and gives 2.5 times its EXP, a boss 40 times its top normal mob's, and a boss kill takes about 3 minutes alone;
    - defenses, accuracy and evasion keep each mob's own share, moved along the bands' curves with its level.
  - **The balance tests now see the regions:** `TestBalance` and `TestFieldBosses` also take each hunting area of a region from level 12 up as a map of its own. Willowmere's lower areas stay set by hand, and the test region mixes mobs of other areas, so both are left out.
  - **Drops** follow the normal mob of each mob's area: its gear from two levels under to one over at normal rates (0.045 a weapon, 0.0375 other gear), the next five levels' at 0.003, twice that for an elite and ten times for a boss. Potions are Orange for all eighteen. Each mob keeps its own material.
  - **EXP rises with level** among normal mobs, elites and bosses, as `TestRealDataLoads` asks: the Spore Slime gives 80, the Mossback Snail 200 and the Spore Mother 3,200.
  - **The test region's** north area is levels 12–14 and its woods 16–17, to hold the moved mobs.
  - **The bots** leave a region to the later game when all its hunting areas are above the promotion level, as they do a hunting map, so they stop at Willowmere's two places.
  - **Stand-ins** of the three plans stand in the tests, with the same ids, gates, waypoints, NPC, bosses and camps' mobs on 256 m and 128 × 256 m.
  - **Web tests** that drew or walked a retired map use the frost trail, the icefall or a place below; the sandstone cliffs' crag-fade and south-bank cut tests went with the map, and the tower test serves its tower on the wide map for its own length.
  - **Characters** saved on any of the six maps start at Willowmere's start; the join test names each.

**Then:** the generator's previews of the built region, and a live walk from the South Gate through both places to the East Pass. Both are heavy runs, approved by the user first.

## 9. Decisions

The user's choice is in section 1. These calls were made in design and logged for the user to push back on:

- **Two places below, Spore Hollow and the Sunken Ruins,** rather than two more surface areas. The woods' middle map becomes a cave and the desert's last map a crypt. So the surface keeps four large areas, and the desert has two places as Willowmere does.
- **The woods first, then the desert,** by level, as today's maps run.
- **The snow mobs move with the desert** (section 4), so there's no jump at the East Pass.
- **Moved mobs drop their new level's gear** (section 3).
- **A new Oasis Guide** in Palmwater, who only talks.
- **No solid a player can stand on** in the hunting areas (section 2).
- **The East Pass leads to the Crossroads** until the snow region replaces it.

## 10. Alternatives considered

- **A desert-only desert with about twelve new mobs:** offered, and the user chose otherwise. The theme would be purer, but it would need another batch of art, and the woods' nine mobs would go unused.
- **Every mob keeping its level:** the desert's mobs would land in the snow region's range, and the woods' in the desert's.
- **A river out of the oasis lake:** the generator needs both ends of a river in the mountains, so the lake is spring-fed.
- **Leaving the snow mobs until the snow region:** players would face a four-level jump past the East Pass meanwhile.
