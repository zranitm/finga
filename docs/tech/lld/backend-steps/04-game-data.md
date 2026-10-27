# 04. Game data

- **Depends on:** 01, 03
- **Features:** B1
- **Milestone:** Foundations
- **Order:** 4
- **Status:** Done
- **Step:** 04. Game data

> **Changed by step 20 (2026-10-27):** game data lives in `internal/gamedata` and reads maps with `pkg/tiled`.

**Goal:** load and validate all static content. The server refuses to start on bad data.

## Scope

- Stage 1 content in `data/`:
  - `classes.yaml`: Warrior and Bowman.
  - `stats.yaml`: the coefficients and caps from page 5.
  - `exp_curve.yaml`: the EXP needed for each level up to 30.
  - `mobs.yaml`: 3 to 4 mobs with stats, behavior, and drop tables.
  - `items.yaml`: about 10 items across the equip, use, and etc tabs.
  - `maps/*.tmj`: the town and 2 hunting maps, made in Tiled with the layers from page 5.
- `internal/application/gamedata`: typed structs, loading, validation, lookups by ID, and map geometry converted into `sim.Geometry`.

## Design notes

- Tiled puts the origin top-left with y growing downward, the same as `sim`, so no flipping is needed. The conversion still has its own tests.
- Custom Tiled properties (`mob`, `max`, `target_map`, and so on) are parsed into typed fields, never read as loose strings later.

## Tests first

- valid data loads, and every class has a coefficient for every derived stat
- a spawn naming an unknown mob fails, naming the map and the object
- a portal whose target map or portal doesn't exist fails
- a drop table naming an unknown item fails
- duplicate IDs fail
- a map missing a required layer fails
- the EXP curve rises strictly and covers every level up to the cap
- the real `data/` folder loads, so every content change is checked in CI

## Done when

- [ ] Each error names the file and the object.
- [ ] The real data passes.

## Review focus

- Map rectangles land in the same coordinates `sim` expects. Test one known platform end to end.
