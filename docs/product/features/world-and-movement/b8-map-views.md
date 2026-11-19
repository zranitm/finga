# B8. Minimap and world map

- **Feature:** B8. Minimap and world map
- **Group:** B. World and movement
- **Priority:** P1
- **Status:** Agreed

**Story:** As a player, I see where I am on the map, where its portals and mobs are, and how the maps link up. I can find my way to the next place to grind, and to my party.

Changed by [B10. Large regions](./b10-large-regions.md): This map becomes This region, with pan and zoom, showing areas with their levels, towns, camps, gates and waypoints, lit once found ([B11](./b11-sprint-and-waypoints.md)). The minimap stays at about 40 m round the player. The World tab charts the regions and any old maps still in use.

Changed by [B13. Underground and underwater](./b13-underground-and-underwater.md): while a player is in a cave or underwater, the minimap and the map window show it. The region's map marks each entrance with its name and levels. A party member inside shows at its entrance, and one outside at the way back.

## Acceptance

- **The minimap:**
  - A small square map sits in the bottom-right corner, always on.
  - It's north up, as the camera looks.
  - It's centred on the player and shows about 40 m around them.
  - Settings can hide it, and the choice is saved with the account's settings.
- **The map window:** M, by default (B6), opens a window with two tabs, This map and World. M or Esc closes it. The game keeps running behind it.
- **What the current map shows**, on the minimap and on the This map tab:
  - the ground, shaded by height, with walls, rocks, and trees as dark shapes;
  - portals;
  - NPCs, with their quest marks;
  - the player, as an arrow pointing the way they face;
  - party members on the same map, as dots;
  - the player's locked mob (C15), as a gold dot;
  - the field boss, as a mark where it stands, while it's up.
- **Mob spawn areas** show as tinted circles.
  - On the This map tab, each is labelled with its mob and level, such as "Green Slime Lv 1".
  - The minimap shows only the circles.
  - No live mob dots: mobs are found in the world.
- **The This map tab** shows the whole map, sized to the window, with labels:
  - each portal's destination, such as "To Cave";
  - NPC names;
  - the spawn labels;
  - "You".
- **The World tab:**
  - It shows every map as a chart, linked by its portals.
  - Each map shows its name, its level range, and a town icon for a town.
  - The player's map is marked "You are here", and each party member's map shows their name.
  - Pointing at a map shows the mobs found there and their levels.
  - Every map shows from the start, visited or not.
  - **Picking a map** shows its layout in the World tab, as This map shows the current one: its ground, its portals with their destinations, its NPCs, and its spawn areas with their mobs and levels. Party members on it are named over it. Back returns to the chart.
- **Travel:** the picked map's layout has a Teleport here button (B9).

## Not in this feature

- Live mob dots.
- Hiding maps not yet visited.
- Other players outside the party on the map.
- Rotating or zooming the minimap.

## Rules

World, and Screens and controls.
