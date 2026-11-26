# The green region: Willowmere

> **Status: approved · 2026-11-26.** The design of the first large region, levels 1 to 12, and the two places below it. It's step 8 of [Large regions](./large-regions.md) and step 6 of [Underground and underwater](./underground-and-underwater.md). It replaces Town, Forest and Cave.

## 1. What was picked

- **The layout** was drawn by the region generator from a draft plan, then picked by the user on 2026-11-25 and 2026-11-26. They made these six calls:
  - the layout as sketched;
  - gentle riverbanks everywhere;
  - new monsters for levels 5 to 14, not recolours;
  - the shrine at levels 9 to 11, with the fish keeping their names;
  - the Bowman Trainer in Fernbridge;
  - the names as placeholders for now.
- **The new monsters** were picked from a sheet of the 26 monsters in Quaternius's Ultimate Monsters pack that the game didn't yet use (section 4).
- **The plans** are the drafts the previews were drawn from:
  - `willowmere.yaml`, the region;
  - `greyfang_cave.yaml` and `sunken_shrine.yaml`, the places below.
  - They move into `server/data/regions/` with the build.
- **Names are placeholders:** Willowmere, its areas, towns and new mobs can be renamed later without moving anything.

## 2. The layout

The region is 1024 m square, with mountains 72 m wide round its edge. You start in Willowbrook in the south. Levels rise in a loop round the middle, so each next area is a short walk from the last.

| Area | Kind | Levels | Where | Ground |
| --- | --- | --- | --- | --- |
| **Willowbrook** | town, the main one | | south middle | meadow |
| **Sunmeadow** | hunting | 1–4 | south and east, an L round the town | meadow |
| **Oakshade Woods** | hunting | 4–7 | south-west | dense oaks |
| **Toadcap Glade** | hunting | 6–9 | west and north-west, along the river | giant mushrooms |
| **Fernbridge** | town | | the middle, where the roads meet | meadow |
| **Mirrorlake Shore** | hunting | 8–11 | east, round the lake | shore |
| **Greyfang Cliffs** | hunting | 10–12 | the north: a strip of rough ground, then a 10 m cliff plateau | pines and crags |

- **The river,** Willow Run, comes down from the north-west mountains, past Fernbridge and out through the east mountains. It's 14 m wide and deep, so it blocks. You cross at two fords and on the two roads that run over it as causeways.
- **The lake,** Mirrorlake, lies east of Fernbridge. The Sunken Shrine is below it.
- **The plateau** of Greyfang Cliffs has cliff edges all round. The main road is the only way up, which makes the top a place you reach rather than wander onto.
- **Gentle banks:** every bank of the river and the lake can be walked back up (section 7). Deep water still blocks.
- **Roads:**
  - the main road runs north from Willowbrook through Fernbridge, up the ramp to the cliff top and on to the north pass;
  - a west road goes through Oakshade Woods and Toadcap Glade;
  - an east road runs round Sunmeadow and the lake;
  - two short roads link Fernbridge to the west road and to the lake's north shore;
  - one road leads to the cave mouth.
- **Waypoints:** 13. There's one in each town, at each area's way in, at the cliff foot and top, and at the north pass.
- **Camps:**
  - 31, six or seven per area;
  - the new areas' normal mobs come back after 30 s and their elites after 40 s; Sunmeadow's green slimes keep the Forest's 24 s and its orange mushrooms 36 s (the Forest's 30 s mesa slimes aren't carried over);
  - every camp keeps clear of the stones, the start, the gates and the NPCs, by the loader's rule.
- **Bosses:** one per hunting area, in its far corner from the way in, two levels over the area's top (section 4).

### Gates

| Gate | Where | Leads to |
| --- | --- | --- |
| **North pass** | the cliff top's north-east corner | Shroom Grove, the mushroom woods' first map |
| **Cave mouth** | the foot of the cliffs, north of Fernbridge | Greyfang Cave |
| **Dive spot** | the lake's west shore | the Sunken Shrine |

- **Why Shroom Grove, not the Crossroads:**
  - The mushroom woods (levels 15 to 20) come after the Cave today. The Crossroads comes after them, and its only way back leads into their top map, Toadstool Heights (19–20).
  - A level-12 player sent to the Crossroads would skip the woods and land among levels 21 and up.
  - So the north pass takes the Cave's place. Shroom Grove's `from_cave` portal now leads to the north pass, and the Crossroads is unchanged.
  - Large-regions.md section 12 said the gate would lead to the Crossroads; that's corrected there.

### NPCs

- **Willowbrook:** the Town Guide, the Warrior Trainer and the Magician Trainer, near the well. Their quests are unchanged.
- **Fernbridge:** the Bowman Trainer, so the middle town has someone to visit. Its trial, 30 Orange Mushrooms, is done in Sunmeadow, a road away.
- **Greyfang Cave:** the Thief Trainer, in the mouth hall by the waypoint, as in the Cave today.
- **Quests need no change:** their objectives name mobs, items and NPCs, never a map.

## 3. Today's mobs in their new places

| Mob | Level today | Level now | Where |
| --- | --- | --- | --- |
| Green Slime | 1 | 1 | Sunmeadow |
| Orange Mushroom | 4 | 4 | Sunmeadow |
| King Slime, boss | 6 | 6 | Sunmeadow |
| Wild Boar | 10 | 10 | Greyfang Cave |
| Cave Bat | 14 | **12** | Greyfang Cave |
| Vampire Bat, boss | 16 | **14** | Greyfang Cave |
| Piranha, Lionfish, Swordfish | 9, 10, 10 | same | the Sunken Shrine |
| Goblin Shark, elite | 11 | 11 | the Sunken Shrine |
| Anglerfish, boss | | 13 | the Sunken Shrine; its entry, tuned in #333, is added now |

- **The Cave Bat and the Vampire Bat move down two levels** so the cave fits its 10–12 band, under the cliffs' own 10–12. Their HP, attack and EXP are set again for the new level by `mobs.yaml`'s rule. Their drops stay as they are.
- **The mushroom woods' mobs don't move.** Their old maps stay until the desert is built.

## 4. The new mobs

Each mob type has one level, and nothing on the land sat between level 4 and level 10. So four areas get new mobs: a normal mob, its elite and a boss each. All come from the Ultimate Monsters pack the user downloaded (CC0), the same pack as today's mobs.

| Area | Mob | Level | Role | Model | Attack |
| --- | --- | --- | --- | --- | --- |
| Oakshade Woods | **Oak Birb** | 5 | normal | Birb (Blob) | hop |
| | **Great Birb** | 6 | elite | Birb (Big) | bite |
| | **Oakshade Chief** | 9 | boss | Tribal (Big) | charge and slam |
| Toadcap Glade | **Puffcap** | 7 | normal | Alien (Blob) | spit that poisons, magic damage |
| | **Toadcap Toad** | 8 | elite | Frog (Big) | hop |
| | **Mushroom King** | 11 | boss | MushroomKing (Big) | spit that poisons, and slam; magic damage |
| Mirrorlake Shore | **Glub** | 9 | normal, flies | Glub (Flying) | spit |
| | **Twin Glub** | 10 | elite, flies | Glub Evolved (Flying) | spit that slows |
| | **Lake Lurker** | 13 | boss | Fish (Big) | bite and slam |
| Greyfang Cliffs | **Cliff Orc** | 11 | normal | Orc (Big) | charge |
| | **Greyfang Raptor** | 12 | elite | Dino (Big) | bite that bleeds |
| | **Crag King** | 14 | boss, flies | Alpaking Evolved (Flying) | spit, and slam that stuns |

- **Every attack is one mobs already have:** hop, bite, charge, spit and the bosses' slam. Each status is one mobs already give: poison, slow, bleed and stun.
  - Each area mixes reach: the woods are melee, the glade spits and hops, the shore spits from the air, and the cliffs charge and bite under a flying boss.
- **Stats:** by `mobs.yaml`'s rule for their level and role:
  - an elite takes 2 to 3 times its normal mob's hits and gives 2 to 3 times its EXP;
  - a boss is 3 times the size, with twice the attack and 40 times the EXP of the area's top normal mob, and about 3 minutes' fight alone;
  - below level 15 the balance harness only logs, as for the Forest and the Cave, so these are set by hand to the rule and checked against its log.
- **Sizes and speeds** follow their model, as for today's mobs: blobs about 0.5 m wide and slow, the big ones about 0.9–1.3 m, flyers like the bats. Flyers are centred in their cylinder's height.
- **Drops:**
  - each normal mob and elite drops a material of its own: Birb Feather, Great Plume, Puff Spore, Toadskin, Glub Jelly, Glub Eye, Orc Tooth and Grey Fang;
  - each drops red potions and its area's band of gear, with the next band rarely;
  - a boss drops its area's gear at 10 times the chance, with potions, as every boss does.

| Area | Its gear (normal rate) | The next band (rarely) |
| --- | --- | --- |
| Oakshade Woods | the level 5–7 pieces: Wooden Pendant, Leather Vest, Copper Ring, Copper Earrings | the level 8 weapons |
| Toadcap Glade | Copper Earrings, the level 8 weapons | Iron Helm, Traveler's Boots |
| Mirrorlake Shore | Iron Helm, Traveler's Boots, Chain Leggings | Hunter's Gloves, Chain Mail |
| Greyfang Cliffs | Chain Leggings, Hunter's Gloves, Chain Mail | the level 14 weapons |

- **The models** are converted from the pack as #187 converted today's: in Blender, each face takes its palette colour as one flat material per colour, the largest named `Main`. Each gets a row in `web/public/models/LICENSES.md`. The pack stays outside the repo; only the converted models are committed.

## 5. Greyfang Cave

A place below the cliffs, 384 × 256 m, entered at the cave mouth. One hunting area, levels 10 to 12.

- **The way through:**
  - the mouth hall in the south, with the gate, the waypoint and the Thief Trainer;
  - the west warren, with two boar camps;
  - the gallery in the north, flat so the bats' shots fly true, with two bat camps;
  - the roost in the north-east, with the Vampire Bat;
  - the east warren, with one boar camp, on a tunnel back to the mouth.
  - It's a loop, so nobody walks back the way they came.
- **Camps:** three of Wild Boars (10 in all) and two of Cave Bats (8), against the old Cave's three camps of 10 mobs.
- **Torches** hang along every wall, 10 m apart. Standing torches ring each camp and the roost, outside their circles.
- **Walls** are 4.5 m of rock that wanders up to 1.5 m, as the generator's caves are. Tunnels are 10 m wide, so a metre stays open past the wander and the widest body.

## 6. The Sunken Shrine

A sea floor below the lake, 256 m square, entered at the dive spot. One hunting area, levels 9 to 11.

- **The way through:**
  - a column of bubbles on the west sand, with the waypoint beside it;
  - two piranha camps near the way in;
  - a reef north and a reef south, rounded, 2 m high with ramped edges, each with a lionfish camp on top;
  - swordfish in the open middle lane and in the north-east;
  - the Goblin Sharks in the south-east;
  - the shrine's court in the east: raised 3 m with ramped edges, six pillars round it, and the west and east sides open, with the Anglerfish in the middle.
- **The floor** is scattered with kelp, sea grass, coral, shells, vents and rocks, kept off the camps and the waypoint.

## 7. Gentle banks

- **Built in #442** (large-regions.md section 3): a river or a lake takes `banks: gentle`, which holds the ground beside the water under a ford's 0.5 ramp all the way round. Willowmere's river and lake both use it.
- **Why:** with steep banks, a player who walks down to the water sometimes can't climb back and has to walk along to a ford. New players shouldn't get stuck there, and the cliffs already give the region its drama.
- **The cost:** where the ground stands high over the water, the river runs in a wider valley. Its banks show as earth by slope.

## 8. Retiring Town, Forest and Cave

- **The maps go:** `town`, `forest` and `cave` leave `data/maps`, and their places leave the world chart.
- **The chart** gains Willowmere, at about where the three stood. The old maps from Shroom Grove on keep their places until their regions come.
- **Characters on a retired map** join at Willowmere's start, as large-regions.md section 12 already says. New characters begin there too.
- **Teleport:** the old maps' list loses the three. Willowmere is reached by its waypoints, as the test region is.
- **Bots and tests** that start in Town or walk the Forest move to Willowmere, or to fixtures of their own where a test only needs a small map.

## 9. The build

Gentle banks are built (#442). The rest goes in this order, each its own PR:

1. **The new mobs:**
   - the twelve models converted, with their looks and licence rows;
   - their entries and materials;
   - the Cave Bat and Vampire Bat moved down, and the Anglerfish's entry.
   - The models need Blender, a heavy run, so the user approves it first. So does a lineup picture of the twelve in the game's renderer.
2. **The region and its places:**
   - the three plans in `data/regions`, with `banks: gentle` on the river and lake;
   - the three old maps retired, and Shroom Grove's portal relinked;
   - new and stranded characters starting in Willowbrook;
   - the chart, the teleport list, bots and tests.
   - Every shipped region's hunting areas then pass the maps' design tests: no mob sees an arrival, each mob reaches anywhere in its leash, and no solid leaves a gap only a player fits (large-regions.md section 14). If the giant mushrooms or pines by the cliffs fail the last one, the scatter keeps solids off cliff edges.

**Then:** the generator's previews of the built region, and a live walk from Willowbrook to the north pass, the cave and the shrine. The bot budget run (large-regions.md section 11) can then run on a region of full size.

## 10. Decisions

These calls were made in design and logged for the user to push back on:

- **The north pass leads to Shroom Grove,** not the Crossroads (section 2): the level path stays region, then mushroom woods, then Crossroads.
- **The Cave Bat and Vampire Bat move down to 12 and 14** to fit the cave under the cliffs, as the old Cave map retires.
- **Twelve new mobs,** where the earlier plan only moved today's mobs into area bands. Mob levels are fixed per type, and the mushroom woods' mobs can't move while their maps stay.
- **Camps keep clear of stones** (large-regions.md section 3): every plan here passes the loader's rule.
