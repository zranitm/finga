# 20. Shared kernel

- **Depends on:** 19
- **Milestone:** Services
- **Order:** 20
- **Status:** Ready for review
- **Step:** 20. Shared kernel

**Goal:** everything more than one service needs sits where every service can import it, before any service leaves: generic code in `pkg/`, Finga's shared knowledge in the root `internal/` (page 10, Layout rules). One binary still; no behaviour changes.

**Layers in this step:** pkg, model, and the shared rules. A refactor: the tests that exist keep passing with their imports changed.

## Scope

**pkg**

- `pkg/typedid`: typed UUIDv7 identifiers, `ID[K]` over a kind type, so two kinds are different types. `New`, `Parse` (bad text is `ErrMalformed`), `String`, `IsZero`, `MarshalText`, `UnmarshalText`. No game knowledge.
- `pkg/tiled`: Tiled's JSON map format (`.tmj`): the map, its layers, objects and custom properties, and typed property lookups (`String`, `Int`). It checks the format only; which layers and properties a map must have is the reader's business.

**Root internal**

- `internal/model`: `AccountID`, `CharacterID`, `ItemID` and `SessionID` become `typedid.ID` of their own kind. `NewAccountID`, `ParseAccountID` and the rest keep their names, and `ErrBadID` is `typedid.ErrMalformed`, so no caller changes.
- `internal/gamedata`: moved from `internal/application/gamedata`; the map loader reads files with `pkg/tiled` and keeps Finga's rules (the six layers, `kind`, `town`, portals, spawns).
- `internal/sim`: moved from `internal/application/world/sim`. Game data's map geometry and movement parameters are the simulation's types, and game data is shared, so the simulation is too.
- `internal/progression`: the character module's pure rules (`DeriveStats`, `AddStats`, `AwardExp`, `DeathPenalty`, `PresetPlan`, and the `Derived`, `Equipment`, `Progress`, `Award`, `ExpCurve` types) move out of `character/rules.go`. The character module and the world's tick both run them; after the split the world cannot import the character service's code.

**Lint**

- depguard: `pkg` imports nothing of Finga; `internal/model`, `internal/gamedata`, `internal/sim` and `internal/progression` import no application module, handler, protocol, telemetry or config.

## Design notes

- The vectors in `testdata/movement/` keep their path; the generator's relative path changes with the package.
- Page 1 (tree, packages), page 2 (game data files), and page 5 (where the rules live) are updated in this step.

## Tests first

- typedid: an ID prints and parses back equal; bad text, the empty string and a wrong length fail with `ErrMalformed` (table); the zero ID is zero and a new one is not; two new IDs are version 7 and sort in creation order; text marshalling round-trips through JSON as a string
- typedid: `model.AccountID` and `model.CharacterID` are distinct types (a compile-time check in the test file: assigning one to the other is commented with why it does not compile, and a type switch tells them apart)
- tiled: a minimal map parses with its size, tile size, layers and objects; a file that is not a map, has no tile size or is not JSON fails with a message naming the file (table); `String` and `Int` find a property, report a missing one, refuse a wrong type and a fractional number (table)
- every existing test of gamedata, sim, character and world passes from its new place

## Done when

- [ ] `make test`, `make lint` and `make cover` are green, and the game runs as before (`make up`, `make smoke`).

## Review focus

- Nothing generic kept game knowledge (`pkg/tiled` knows no layer names).
- No behaviour changed: the movement vectors and the game data tests are byte-for-byte the same.
