# C02. protocol: types and codec

- **Depends on:** C01
- **Milestone:** Foundations
- **Order:** 2
- **Status:** Done
- **Step:** C02. protocol: types and codec

**Goal:** one typed definition of every Stage 1 message, proven against the server's golden files.

**Layer in this step:** `protocol` (page 7 layer 2).

## Scope

- `protocol/messages.ts`: the envelope `{ t, req?, seq?, tick?, d }` and a TypeScript type per message in `testdata/contracts/` (31 files: Hello, Welcome, Input, Snapshot, Delta, Correction, Attack, Damage, Died, Respawned, ExpGained, LevelUp, PickUp, InventoryChanged, InventoryMove, InventoryDiscard, EnterPortal, MapChange, ChatSend, ChatMessage, PartyInvite, PartyInvited, PartyRespond, PartyLeave, PartyKick, PartyPromote, PartyState, Response, Kicked, Ping, Pong), as a discriminated union on `t`.
- `protocol/codec.ts`: a `Codec` interface (`encode(msg): string | ArrayBuffer`, `decode(raw): Message`) and `JsonCodec`. Decoding validates shape and rejects unknown `t`; Stage 2 adds a Protobuf codec behind the same interface. Open for review: shapes are checked with valibot 1.x schemas, and the TypeScript types are inferred from them (`v.InferOutput`), so a type and its check can't drift; hand-written guards for 31 messages are the alternative.
- `protocol/errors.ts`: the `Response` error codes exactly as page 4 lists them, as a string union: the generic ones from `pkg/errs` (`invalid_argument`, `unauthenticated`, `not_found`, `conflict`, `rate_limited`, `unavailable`, `internal`) and the Stage 1 domain codes (`inventory_full`, `idempotency_key_reused`). Also the Kicked reasons: `logged_in_elsewhere`, `outdated_protocol`, `server_restart`, `abuse`, `unauthenticated`, `session_expired`.

## Design notes

- Entity shapes in Snapshot and Delta (player, mob, drop) are shared types; Delta adds `removed_players`, `removed_mobs`, `removed_drops` and `ack_seq`, exactly as the golden Delta shows. Players carry `dead` and `invulnerable` (backend step 19), both omitted when false.
- `req` is optional on Response too: a failed pickup and a refused chat line arrive without one (page 4, Wire shapes).
- The client never depends on message field order.

## Tests first

- golden round trip: for every file in `testdata/contracts/`, decode then encode gives JSON deep-equal to the file (one `test.each` row per file, discovered at test time so a new golden file fails until typed)
- decode rejects: unknown `t`, missing `d`, a `req` that is not a string
- Delta with empty `removed_*` lists decodes to empty arrays, never `undefined`
- a Response with no `req` decodes; a player without `dead` or `invulnerable` decodes them as false

## Done when

- [ ] All 31 golden files round-trip.
- [ ] `make cover` holds 80% on `protocol`.

## Review focus

- The union is exhaustive: a `switch` on `t` with `never` in the default compiles.
