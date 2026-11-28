# Minimap and world map

> **Changed by [Underground and underwater](./underground-and-underwater.md):** inside a place below, the minimap and This region draw its tiles. The region's map marks each gate below with the place's name and levels. A party member below shows at its gate.

> **Changed by [Loot pouches and timed bosses](./loot-pouches.md):** the client's map copy carries the boss's circle. While the boss is down, the map window marks its spot, greyed, with when it's back.

> **Changed by [Large regions](./large-regions.md) section 10:** the minimap and This region draw baked tiles, and the World tab charts regions.

> **Status: built · 2026-11-17.** The high-level design for [B8. Minimap and world map](../product/features/world-and-movement/b8-map-views.md). All of it is the client's. The only server-side change is one new data file, `world.yaml`, which the loader checks. As built, the account settings also carry the minimap's flag (section 5).

## 1. What has to hold

- **The user's words** (2026-11-17): "we should also have maps, a map of the current instance and a world map". They chose:
  - a minimap always on, plus a full map on M;
  - spawn areas named with their mob and level, and no live mob dots;
  - every map on the world map, visited or not.
- **The client computes.** The ground, solids, and portals are already in each map's JSON (`npm run maps`). NPCs and map properties are already in the generated game data. Positions of the player, the party, and the mobs are already in the frames. Nothing new crosses the wire.
- **Cheap to draw.** The ground is drawn once per map into an image. Only the marks move.

## 2. The change at a glance

| Part | Today | After |
| --- | --- | --- |
| `server/data/world.yaml` | — | New: each map's place on the world chart |
| `load/` | — | Checks `world.yaml`: every map placed once, and no two maps on the same spot |
| `web/scripts/gen-gamedata.ts` | `maps.ts`: banner, kind, town, levels, art keys, NPCs | Adds each map's spawns (mob, centre, radius), its portals' destinations, and its chart place. As built, portals carry their radius too, for the ring, and the links go to their own file, `map-links.ts` |
| Client | — | `features/map/`: the minimap, the map window and its two tabs; `game/map-image.ts`: the ground image. As built, `state/map-view.ts` too (section 5) |
| Input | — | `map`: M in both presets |
| Settings | — | `showMinimap`, on by default, saved with the account's settings. As built, it's `hide_minimap` on the wire and in the stored JSON (section 5) |

## 3. Data

`server/data/world.yaml`:

```yaml
# Each map's place on the world chart: x across and y down, 0 to 100. Links are drawn from the maps' portals.
places:
  town:        [50, 50]
  forest:      [62, 50]
  cave:        [74, 44]
  crossroads:  [38, 50]
  # ...every map
```

- **The loader** refuses a map with no place, a place for a map that doesn't exist, two maps closer than 6 apart, or a place outside 0 to 100. As built, it also refuses a map placed twice, so its tests cover five cases.
- **The places** are set by hand when building, laid out by region: the towns at the centre, each theme in its own direction, and the levels rising outward. As built, they're laid as an arc rather than round a centre: the forest and cave to the south west, the mushroom maps north over the top, the crossroads in the middle, the desert to the south east and the snow to the north east. The levels rise along the arc, and each line on the chart is short.
- **`gen-gamedata.ts`** adds to each map in `maps.ts`:
  - `spawns`: mob, centre, and radius, from the map's YAML;
  - `portals`: name, centre, and destination map, deduplicated into links for the chart;
  - `place`: from `world.yaml`.
- **As built,** the links are a sibling file, `map-links.ts` (`mapLinks`): each pair of maps a portal joins, once, sorted. Only the World tab reads them, so `maps.ts` stays one entry per map.
- **The boss's area isn't exported,** as it isn't today. The field boss shows only while it's up, at the position the frames give.

## 4. The ground image

`game/map-image.ts` draws one map into an offscreen canvas, at 4 pixels a metre, when the map loads, from the map JSON the 3D scene already reads:
- **The ground's colour** is `look.ground`'s, shaded lighter with height across the map's range. Steep cells are darker, as the 3D ground draws them.
- **Solids** are drawn as dark footprints: boxes rotated by their yaw, and cylinders as discs.
- **Props aren't drawn,** since nothing collides with them.
- **As built:**
  - the pixels are a pure function (`groundPixels`), tested without a canvas, which the test runner lacks; the page puts them on a canvas and keeps its data URL;
  - a footprint is 6% of the ground's colour, and steep ground keeps the 3D ground's 60% (`CLIFF`); the ground colours moved to `world3d/grounds.ts` for both to share.

The minimap and the This map tab both draw from this one image. Neither redraws the ground. A region's ground is its bake's tiles instead (large-regions.md step 5c-2a).

## 5. The minimap

- **What it is:** `features/map/Minimap.tsx`, a 180 px square canvas in the bottom-right corner, which is free today. It sits behind the windows, and it ignores the pointer.
- **Its view:** 40 m across, centred on the player's drawn position, north up. At a map's edge it stops, so the map never slides out of the square.
- **Marks,** redrawn ten times a second:
  - spawn circles, tinted by mob;
  - portals, as a ring;
  - NPCs, as dots, with the quest mark the NPC shows in the world;
  - party members on this map, as green dots;
  - the lock, as a gold dot;
  - the field boss while it's up, as a red crown; in a region, every area's boss at its spot on This region (large-regions.md step 5c-3b);
  - the player, as a white arrow along the heading.
- **Its name** (from large-regions.md step 4b-3b): over the square, the map's name, and in a region its channel, "Test Region · Ch. 2".
- **Settings:** `showMinimap` (on by default) hides it. It's saved with the account's settings, as chat's pin is.
- **As built:**
  - **SVG, not a canvas.** The marks are SVG in metres, so the This map tab draws them with the same component (`MapMarks`), and tests read them without a canvas. The browser redraws only the marks that moved.
  - **Where the marks come from:** features can't import the game layer, so the world scene publishes to `state/map-view.ts`: the ground's image as a map loads, and the player, the party members drawn on this map, the lock and the field boss every 100 ms. Spawns, portals and NPCs come from the game data.
  - **The lock** is a gold dot in a gold ring, so it doesn't read as an NPC's dot. The quest marks' text moved to `progression/quests.ts` for the world and the map to share.
  - **The setting** is `hide_minimap` on the wire and in the stored settings, so an account saved before it, or a PUT that leaves it out, keeps the minimap shown. The client's store keeps `showMinimap`. It's a checkbox under Map in Settings.

## 6. The map window

`features/map/MapWindow.tsx`, opened and closed with M, and closed with Esc, as the other windows are (`state/ui.ts`). The game keeps running behind it, and movement keys work as they do with other windows open.

### This map

- **The view:** the whole map, sized to fit the window, from the ground image. A region's plan pans and zooms, under a tab reading "This region" (large-regions.md step 5c-2b).
- **Marks:** the minimap's marks, plus labels:
  - "To <map name>" by each portal;
  - NPC names;
  - "<Mob> Lv <level>" by each spawn circle;
  - "You" by the player.
- **Labels** keep clear of each other with the name-tag rule the world's labels use.
- **In a region:** its channels (large-regions.md section 4) and its waypoints, marked grey till found and picked for a teleport (section 8, step 6c-1). Its areas are outlined, on the minimap too, and named here with a hunting area's levels (step 5c-3a).
- **As built:**
  - the rule (`stackTags`) moved to `lib/tags.ts` for both to use;
  - labels go in order: spawns, portals, NPCs, then "You", each lifted clear of the NPCs' quest marks and the labels before it, so only "You" moves as the player walks;
  - spawn circles of one mob that meet or come within 6 m of another, edge to edge, share one label. Separate groups keep their own. After the live check, the cliffs had shown "Dust Vulture Lv 23" six times over the middle. The rule halves the labels on the busiest maps (the cliffs 10 → 5, the keep 11 → 7);
  - a group's label sits on one of its own circles. At first it went at the middle of the group's centres, but a chain's middle could fall on another mob's circle: spore hollow's rats' label sat on the east moth circle, and the keep's west bats' on the gloom slimes. Now it tries the circle nearest the middle first, then the others, each with steps up and down inside the circle, and takes the first spot that covers no quest mark or label already placed. The groups with the fewest spots go first, so a small lone circle isn't crowded out by a chain. Failing every spot, it's lifted as other labels are. On every map, each spawn label now sits on its own mob's circle;
  - a chain is cut so every spawn circle has a label of its own mob on it, or within 8 m of its edge. A chain's one label had left far circles bare: spore hollow's east moth circle sat 22.5 m from the moths' label, and the keep's west bats 12–17 m from theirs.
    - **The cut:** a chain takes the circle nearest its middle, and keeps those its label reaches from there; the rest are cut again.
    - **Where the label goes:** each circle offers a spot moved from its centre toward the group's middle, by up to three quarters of its radius, as well as its centre. A label takes only spots within 8 m of all its group's circles, so close pairs still share one.
    - **The counts:** the busiest maps gain two or three labels (spore hollow 3 → 6, the keep 7 → 9, the cliffs 5 → 7, dune road 4 → 6, the ruins 6 → 8); the other seven are unchanged;
  - a label by the map's edge is held inside the view;
  - labels draw over the ground and the fixed marks, and under the party, lock, boss and player marks.

### World

- **The chart:** an SVG.
  - Each map is a node at its `place`, showing its name and "Lv a–b".
  - A town shows a house icon and no levels.
  - Each pair of maps joined by a portal is linked with a line.
- **Marks:**
  - the player's map is ringed and labelled "You are here";
  - each party member's map (`PartyMember.map_id`) shows their name under it, and an offline member shows none.
- **Pointing at a node** shows a small card: the map's name, its level range, and its mobs with their levels, from the map's `spawns`.
- **A region** reads "Region" in place of a level range, and is reached by its waypoints when picked (large-regions.md section 8).
- **Every map shows from the start.**
- **Clicking a node** shows that map's layout (section 12).
- **As built:** the card opens toward the chart's middle, so it stays inside the window; focusing a node with Tab shows it too.

## 7. Input

- **`map`** is a new action in `input/actions.ts`, bound to M in both presets. M is free in both today.
- It's ignored while typing, as the other window keys are.
- The controls table (Screens and controls) gains the row. As built, the row was already there from the product docs.

## 8. Testing

- **Loader:** `world.yaml`'s refusals.
- **Generator:** spawns, portal links (each pair once), and places in `maps.ts`.
- **The ground image:** heights shade, steep cells are darker, and a rotated box's footprint lands where its 3D model stands.
- **The minimap:**
  - centred on the player, and held inside the map at its edges;
  - marks placed by world x and z, with north up;
  - party dots only for members on this map;
  - the lock's dot moves with Tab;
  - the boss shows only while it's up;
  - hidden by the setting.
- **The map window:**
  - M opens and closes it, Esc closes it, and typing doesn't open it;
  - This map's labels, portal destinations, and spawn labels;
  - World's nodes, links, town icons, "You are here", party names, and a node's card.
- **Live:** a playtest on every map checks that the minimap matches the world around the player, that portals lead where their labels say, and that the chart's links match the portals.

## 9. Build

One PR, each step one commit, green on `make test`, `make lint`, `make cover`, and `make breaking`:

1. **Data:** `world.yaml` with every map placed, the loader's checks, and the generator's additions.
2. **The ground image and the minimap,** with the setting.
3. **The map window:** This map and World, M, and the controls row.

## 10. Decisions

The user's three choices are in section 1. These calls were made in planning and logged for the user to push back on:

1. **The minimap goes in the bottom-right corner,** the one free corner. The top-right holds Back to characters, the boss bar and the enemies' frames, the top left the party panel, and the top centre the match bar and the target window.
2. **The minimap shows 40 m around the player,** north up, and stops at the map's edge. Maps are 48 to 84 m wide, so it shows about half of one.
3. **The world chart is laid out by hand in `world.yaml`.** An automatic layout of the portal graph would put maps anywhere. A hand layout can put regions in a sensible order.
4. **Marks:** party members, the lock, and the field boss are shown; other players aren't.
5. **The ground is drawn once into an image,** and only the marks move.

## 11. Alternatives considered

- **Rendering the minimap from the 3D scene with a second camera:** it costs a second render every frame, and props and effects would clutter it. A 2D image of the ground is cheaper and clearer.
- **Exporting the spawns in the map JSON:** the World tab needs every map's mobs without loading every map's geometry, so the generated game data carries them.
- **A top-right minimap that pushes the party panel down:** it would move a layout the user already has, for no gain over the free corner.

## 12. A map's layout from the World tab

Added on 2026-11-17. The user: "picking a map in world should show that map's layout".

- **Clicking a node** on the chart replaces the chart with that map's layout, inside the World tab. A header carries:
  - the map's name and level range;
  - Back, which returns to the chart;
  - Teleport here ([Teleport](./teleport.md)), which isn't shown for the player's own map.
- **The layout** is drawn as This map draws the current one:
  - the ground image (`game/map-image.ts`), from `maps/<id>.json`, which is fetched and kept for the rest of the session;
  - the spawn circles and their labels, from `maps.ts`;
  - the portals, with "To <map name>";
  - the NPCs, with their names.
- **Live marks** (the player, party dots, the lock, and the boss) show only on the player's own map, since the client tracks positions only there.
- **Party members on another map** are named under its header, from `PartyMember.map_id`, without positions.
- **Testing:**
  - a click shows the layout, and Back returns to the chart;
  - a map's JSON is fetched once;
  - labels and spawns come from that map;
  - live marks show only on the player's own map;
  - party names show under the header;
  - Teleport here is hidden on the player's own map.
- **Build:** one client PR, with [Teleport](./teleport.md)'s client step or before it.
- **As built:**
  - The chart is a group of buttons, so a node opens with a click, Enter or Space.
  - Another map's layout comes from `app/layouts.ts`, which keeps each map's size and ground image for the session. A fetch that fails says "The map didn't load." in the window, and is tried again the next time the map is picked.
  - Teleport here is hidden while the player is dead too, as B9 says.
