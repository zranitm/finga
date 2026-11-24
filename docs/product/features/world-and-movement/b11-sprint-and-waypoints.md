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
- **Walk mode:**
  - Left Shift (rebindable, B6) switches between sprint mode, the default, and walk mode.
  - In walk mode the character never breaks into a sprint, for a player who wants to walk out of combat.
  - Switching back to sprint mode while moving out of combat sprints at once, with no 2-second wait.
  - Each switch shows a notice, "Walk" or "Sprint". While walk mode is on, a walk icon sits first in the player's own frame row, and its hover names the key.
  - The mode is kept with the account's settings, as the zoom is.
- **Waypoints:**
  - A waypoint is a tall carved stone with a cyan crystal floating above it, in every town and at crossroads, camps and passes, about every 150 m along the roads.
  - Until it's found, its crystal glows a dim teal and its runes are dark, still easy to spot from afar and in a dark cave. Once found, the crystal shines near-white and the runes glow, so a player can see in the world which stones are left.
  - Walking within 12 m of one finds it. The crystal and runes light up, a beam rises and a ring spreads on the ground, all in under a second. A notice says "Waypoint found: <name>", and it's remembered for the character.
  - Unfound waypoints show grey on the map, and found ones lit.
  - Each underground or underwater place has one inside, by its entrance (B13).
- **Teleport** (B9) goes to a waypoint, in place of a map:
  - In the map window, picking a found waypoint, in any region, shows Teleport here.
  - The 3-second channel and what cancels it are as in B9.
  - Every region's main town can always be teleported to, found or not.
- **After dying,** the player gets back up at the nearest waypoint in their region, found or not, or the nearest town if that's closer. Inside an underground or underwater place, it's the waypoint inside (B13).

## Not in this feature

- Mounts and gliding.
- A key held to sprint, or a stamina bar.
- Teleport costs.

## Rules

B10 for regions; B9 for the teleport's channel.
