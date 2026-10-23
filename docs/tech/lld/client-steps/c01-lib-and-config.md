# C01. lib and config

- **Depends on:** C00
- **Milestone:** Foundations
- **Order:** 1
- **Status:** Done
- **Step:** C01. lib and config

**Goal:** the generic helpers everything above relies on, with no game knowledge.

**Layer in this step:** `lib`, `config` (page 7 layer 1).

## Scope

- `lib/vec2.ts`: add, scale, length, distance.
- `lib/ring-buffer.ts`: fixed capacity, push, iterate from oldest, find by key (for inputs by sequence number).
- `lib/backoff.ts`: exponential with full jitter, capped (for reconnect).
- `lib/fixed-step.ts`: an accumulator that turns variable frame time into whole 1/60 s steps with a cap on steps per frame, and flags every third step as the start of a 50 ms tick (C08 samples keys there). It counts time in whole microseconds: in floating-point milliseconds, subtracting 1000/60 from 50 leaves less than one step after two, so a 50 ms frame gives 2 steps instead of 3.
- `lib/id.ts`: idempotency and request IDs (UUID v4 built from `crypto.getRandomValues`, prefixed `r-` and `k-` as the golden files show). Not `crypto.randomUUID`: it exists only in secure contexts (HTTPS or localhost), so the LAN session in C14, served over plain http, would crash. Idempotency keys must be random, not counters: the server keeps them for 24 hours per account, so a counter restarting after a reload would collide.
- `config/env.ts`: typed access to `import.meta.env` with `VITE_API_ORIGIN` (empty means same origin) and a `isDev` flag; fails fast on an unknown value.

## Design notes

- `lib` imports nothing from the project. Any helper that needs a protocol or game type belongs in a higher layer.

## Tests first

- `vec2`: table of operations with expected results, including zero length
- `ring-buffer`: overwrite past capacity keeps the newest, iteration order, find hit and miss
- `backoff`: delays stay within the jitter window and never exceed the cap; a seeded random makes the cases exact
- `fixed-step`: 16.7 ms frames yield one step; a 50 ms frame yields three (the floating-point trap above); a 1 s frame is capped and the remainder dropped; every third step is flagged as a tick start, across frames of any length
- `id`: works with `crypto.randomUUID` removed (as in an insecure context); IDs have the v4 version and variant bits
- `env`: an empty origin resolves to same origin; a malformed one throws

## Done when

- [ ] Tests pass with 80% coverage on `lib`.

## Review focus

- No `any`; every helper is pure and synchronous.
