# 14. Portals and towns

- **Depends on:** 09, 10, 11
- **Features:** B3, B5
- **Milestone:** M4. Travel
- **Order:** 14
- **Status:** Done
- **Step:** 14. Portals and towns

**Goal:** a connected world: a safe town and two hunting maps joined by portals. This completes M4, Travel.

## Scope

- One instance per map: the town and both hunting maps.
- EnterPortal (Up or W while on a portal): leave the instance, save a checkpoint, join the target instance at the linked portal, and send MapChange.
- Towns: no mobs, no combat, no death penalty.
- Respawn at the town linked to the map, as a map property in game data.

## Tests first

- a portal takes the player to the linked spot on the target map
- HP and inventory are the same after the trip
- spamming EnterPortal never leaves a player in two instances, or in none for more than one tick
- death respawns at the linked town
- no damage in town
- end-to-end rows for B3 and B5

## Done when

- [ ] A test client can walk from the town to the second hunting map and back.

## Review focus

- The handoff order between instances.
