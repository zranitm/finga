# B9. Teleport

- **Feature:** B9. Teleport
- **Group:** B. World and movement
- **Priority:** P1
- **Status:** Agreed

**Story:** As a player, I pick a map on the world map and teleport there, so I can get to my grinding spot or my party without walking every map in between.

## Acceptance

- **Where from:** a map's layout in the World tab (B8) has a Teleport here button. It's not shown for the map the player is on, or while they're dead.
- **Where to:**
  - In this phase, any map, whether the player has been there or not.
  - Later, only towns and maps the player has visited. That needs the game to remember which maps each character has entered, which waits for a later phase.
- **The channel:**
  - Pressing the button closes the map window. The player channels for 3 seconds, with a bar that reads "Teleporting to <map>" and a glow around them.
  - Moving, jumping, attacking, using a skill, or being hit cancels it, with a short notice saying why.
  - A MISS doesn't cancel it.
  - Drinking a potion or talking to an NPC doesn't cancel it either.
  - So a teleport can't be used to escape a fight.
- **Arrival:** the player arrives at the map's start point, where a new arrival by portal would stand. The screen fades as through a portal.
- **Cost:** free.
- **Fairness:** the server runs the channel and decides what cancels it. The bar on the player's screen follows it.

## Not in this feature

- Limiting teleports to visited maps (later).
- A cost or teleport items.
- Teleporting to a party member, or taking others along.
- Others seeing the channel's glow: they see the player leave the map.

## Rules

World, and Screens and controls.
