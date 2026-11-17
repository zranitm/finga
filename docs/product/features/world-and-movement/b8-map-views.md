# B8. Minimap and world map

- **Feature:** B8. Minimap and world map
- **Group:** B. World and movement
- **Priority:** P1
- **Status:** Agreed

**Story:** As a player, I see where I am on the map, where its portals and mobs are, and how the maps link up. I can find my way to the next place to grind, and to my party.

## Acceptance

- **The minimap:**
  - A small square map sits in the bottom-right corner, always on.
  - It's north up, as the camera looks.
  - It's centred on the player and shows about 40 m around them.
  - Settings can hide it, and the choice is saved with the account's settings.
- **The map window:** M, in both presets, opens a window with two tabs, This map and World. M or Esc closes it. The game keeps running behind it.
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
- **No fast travel:** the maps show the way, and the player walks it.

## Not in this feature

- Live mob dots.
- Hiding maps not yet visited.
- Travelling by clicking the map.
- Other players outside the party on the map.
- Rotating or zooming the minimap.

## Rules

World, and Screens and controls.
