# C12. Portals and map change

- **Depends on:** C11
- **Features:** B3, B5
- **Milestone:** M4. Travel
- **Order:** 12
- **Status:** Done
- **Step:** C12. Portals and map change

**Goal:** walk from the town to the forest to the cave and back through portals.

**Layer in this step:** `game/systems/portals`, `game/systems/map-transition` (reuse), `game/scenes/world` (page 7 layer 4).

## Scope

- Portals from the map's object layer drawn as animated markers; a fresh press of the up key (Classic) or W (WASD) while the feet are inside a portal rectangle sends `EnterPortal` with the current sequence number. Holding the key sends nothing more, and after a `MapChange` the key must be released before it counts again: you arrive standing in the target portal, so a held key would bounce you straight back.
- `MapChange`: fade out, swap the tile layers and collision view for the named map, place the player at MapChange's position, which the following Snapshot confirms, reset prediction and interpolation, fade in.
- Towns show no mobs and the attack key does nothing there, as the server runs no combat on town maps.
- A small map name banner on arrival.

## Design notes

- The portal entry rule matches page 5 and the server's `standsIn`: bottom edge included, so a fresh press on arrival goes straight back, by design.
- Everything cross-map goes through `map-transition` from C10; this step must not add a second path.

## Tests first

- portals system over plain data: a press with the feet inside the rectangle sends EnterPortal; outside sends nothing; holding the key sends exactly one; after a MapChange, a key still held sends nothing until it is released and pressed again
- map-transition on MapChange to a different map rebuilds the collision view (the `sim/map` for the target) and clears buffers; a same-map Respawned (C10) only resets position, since the server never sends a MapChange to the map you're on. (Built as a scene restart with the arrival position: the restart reparses the target map and recreates every buffer and view, decision 142; the reducer's change-map case is tested in map-transition, the rebuild is by construction.)
- a press outside the portal uses up the press, so walking into it with the key held sends nothing

## Done when

- [ ] Against `make up`: town to forest to cave and back, each hop a clean fade with HP intact; a second browser on the forest sees the traveller leave and arrive. (Not run: no browser is attached to the implementation session; owed to the author with the pooled play-through.)
- [ ] M4 milestone recorded.

## Review focus

- No stale mobs, drops or players after a hop.
