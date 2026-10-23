# C09. Other players, HUD basics and connection loss

- **Depends on:** C08
- **Features:** B2, A1
- **Milestone:** M1. Walk together
- **Order:** 9
- **Status:** Done
- **Step:** C09. Other players, HUD basics and connection loss

**Goal:** two people see each other move smoothly, and the screen says when the connection is in trouble.

**Layer in this step:** `game/systems/interpolation`, `game/entities/player` (remote), `features/hud` (first cut), `app` (page 7 layers 4 and 5).

## Scope

- Interpolation: buffer Snapshot and Delta states for remote players, render 100 ms in the past between the two nearest states; a player in `removed_players` is removed after the buffer drains.
- Remote player entities with name labels, facing and animations from velocity and `on_ground` and `climbing`.
- HUD first cut: HP, MP and EXP bars and level from the `character` store (`features/hud`); the EXP bar's maximum is the generated EXP curve's value for the level (C03), since no message carries it.
- Connection loss: a reconnecting notice from the `connection` store; on Welcome after a reconnect the scene reloads the map if `map_id` changed and resets prediction; a Kicked returns to the login screen with the reason, except `server_restart`, which shows the reconnecting notice and reconnects (C04).

## Design notes

- Interpolation is a function over plain data (`sample(buffer, renderTime)`), tested without Phaser.
- Remote state never goes through the Zustand store (page 7, How the pieces talk).

## Tests first

- sample at a time between two states returns the linear blend; before the first state returns the first; after the last extrapolates no further than one tick
- a removed player stays visible until the buffer has drained past the removal tick
- HUD bars render the store's values and update on `applyLevelUp` and `applyExpGained` (Testing Library)
- the reconnecting notice shows on `connection: reconnecting` and hides on `connected`
- the remote buffer keeps a second of states with one older state as the floor; a Snapshot replaces the set of remote players and skips the local one; the render clock sits 100 ms behind the newest tick and advances with local time between messages
- App: the HUD shows the Welcome character over the canvas and the World entry carries the name for its label

## Done when

- [ ] Against `make up` with two browsers: each sees the other walk and jump without stutter at 20 Hz; killing the server for 5 s shows the notice and both come back at their last saved position; logging in on a second tab kicks the first with a message. (Not run: no browser was attached to the implementation session; the M1 browser hand check is owed to the author, see the C09 log row.)
- [ ] M1 milestone: played and recorded in the log row.

## Review focus

- Nothing per-tick re-renders React.
