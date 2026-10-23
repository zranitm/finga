# 03. Movement simulation

- **Depends on:** 00
- **Features:** B2
- **Milestone:** Foundations
- **Order:** 3
- **Status:** Done
- **Step:** 03. Movement simulation

**Goal:** the deterministic movement code that the server runs, and that the client mirrors in TypeScript.

## Scope

- `internal/application/world/sim`, with no dependencies outside the standard library:
  - `State`: position, velocity, on ground, climbing, facing, and the drop-through timer.
  - `Input`: held left, right, up, down, jump.
  - `Geometry`: solids, one-way platforms, ropes.
  - `Params`: the movement constants from page 5.
  - `Step(state, input, geometry, params) State`: one fixed sub-step of 1/60 s.
- Test vectors in `testdata/movement/*.json`: a starting state, inputs per sub-step, and the expected state after each one.

## Design notes

- `float64` everywhere, because JavaScript numbers are 64-bit floats. Float32 on one side would make the two drift.
- Collision is axis-separated: move on X and resolve, then on Y.
- The order of operations inside `Step` is fixed and documented, because the TypeScript port must follow it exactly.

## Tests first

Each row is one vector file, also run later by the client:

- walk left and right on flat ground
- a jump's peak height and landing position
- walking off a ledge
- landing on a one-way platform from above
- jumping up through a one-way platform from below
- dropping through with Down + Jump
- climbing a rope up and down, and jumping off it
- walking into a wall; hitting a ceiling
- standing on the very edge of a platform
- falling speed clamped at the maximum

## Done when

- [ ] Every vector passes.

## Review focus

- No map iteration (its order is random in Go), no clock, and no randomness inside `Step`.
