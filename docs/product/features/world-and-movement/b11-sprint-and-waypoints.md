# B11. Sprint and waypoints

- **Feature:** B11. Sprint and waypoints
- **Group:** B. World and movement
- **Priority:** P0
- **Status:** Agreed

**Story:** As a player, I cross a big region quickly: out of combat I break into a sprint, and I teleport between the waypoints I've found.

## Acceptance

- **Sprint:**
  - Walking on the ground out of combat for 2 seconds breaks into a sprint, 1.6 times as fast. No key is needed.
  - It stops when the player stops, attacks, uses a skill, is hit or hits, or enters combat.
  - Jumping keeps it.
  - The character runs with a longer stride and leans in.
  - Move speed from stats and buffs (C6) counts as before, and the sprint multiplies it.
  - Sprint is not a dash: it never happens in a fight.
- **Waypoints:**
  - A waypoint is a tall stone with a glow at the top, in every town and at crossroads, camps and passes, about every 150 m along the roads.
  - Walking within 12 m of one finds it. A notice says "Waypoint found: <name>", and it's remembered for the character.
  - Unfound waypoints show grey on the map, and found ones lit.
- **Teleport** (B9) goes to a waypoint, in place of a map:
  - In the map window, picking a found waypoint, in any region, shows Teleport here.
  - The 3-second channel and what cancels it are as in B9.
  - Every region's main town can always be teleported to, found or not.
- **After dying,** the player gets back up at the nearest waypoint in their region, found or not, or the nearest town if that's closer.

## Not in this feature

- Mounts and gliding.
- A sprint key or a stamina bar.
- Teleport costs.

## Rules

B10 for regions; B9 for the teleport's channel.
