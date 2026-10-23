# C03. sim: movement mirror

- **Depends on:** C01
- **Features:** B2
- **Milestone:** Foundations
- **Order:** 3
- **Status:** Done
- **Step:** C03. sim: movement mirror

**Goal:** the movement simulation from page 5 in TypeScript, step for step the same as the Go `sim`, so prediction matches the server.

**Layer in this step:** `sim` (page 7 layer 2), and the generated `gamedata` (layer 1). Pure TypeScript: no Phaser, React or DOM.

## Scope

- `web/scripts/gen-gamedata.ts` turns `data/*.yaml` into `src/gamedata/` (git-ignored), run before dev, build, typecheck and test, so no value can drift:
  - movement: every key in `data/movement.yaml` (`dt`, `walk_speed` 200, `jump_velocity` 620, `gravity` 1900, `max_fall_speed` 900, `climb_speed` 140, `drop_through` 0.25, and the 24 x 56 box), which `sim` reads;
  - for display and send gates only (page 9, Rules): the EXP curve (`exp_curve.yaml`, for the EXP bar), item templates (`items.yaml`: names, tabs, stack sizes), class attack cooldowns (`classes.yaml`), and mob types (`mobs.yaml`).
- `sim/map.ts`: the collision view of a Tiled map (solids, one-way platforms, ropes and ladders, portals, spawn) parsed from the `.tmj` the server validates. The maps hold only these object layers, no tile art; C07 draws from the same objects.
- `sim/step.ts`: `step(state, input, map, dt)` for one 1/60 s sub-step: walk, jump, gravity and fall clamp, platform landing and drop-through, ledges and ceilings, rope entry by the feet and climbing in the held direction, exactly as page 5 describes.
- `sim/input.ts`: the held-keys input shape the server expects (left, right, up, down, jump), independent of key presets.

## Design notes

- The step function takes and returns plain objects and never mutates its input, so prediction can keep a history of states cheaply.
- Page 5's representation is shared between client and server; if a vector disagrees, page 5 or the Go code is wrong, not the vector.

## Tests first

- replay every vector found in `testdata/movement/` at test time, one row per file, so a new vector fails until it passes. Eleven today: `ceiling`, `climb-rope`, `drop-through`, `fall-clamp`, `jump-through-platform`, `jump`, `land-on-platform`, `ledge`, `platform-edge`, `walk-flat`, `wall`. Every intermediate state equals the recorded one exactly: the Go test compares with `==`, so there is no tolerance.
- `map.ts` parses the maps into these counts of solids, platforms, ropes and portals: town 4, 2, 1, 1; forest 4, 3, 2, 2; cave 6, 4, 3, 1
- the generated `gamedata` equals the YAML it came from (a test reads both)

## Done when

- [ ] Every vector passes unchanged.
- [ ] `make cover` holds 80% on `sim`.

## Review focus

- Floating-point operations run in the same order as the Go code, since the vectors compare exactly.
- Go may fuse `x*y+z` into one instruction on arm64 (Apple Silicon); JavaScript never does. Vectors are regenerated with `-update` on amd64 (CI) only, or the Go code wraps products in `float64()` to prevent fusing.
