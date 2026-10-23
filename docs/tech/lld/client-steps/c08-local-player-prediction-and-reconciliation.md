# C08. Local player: prediction and reconciliation

- **Depends on:** C07, B18
- **Features:** B2
- **Milestone:** M1. Walk together
- **Order:** 8
- **Status:** Done
- **Step:** C08. Local player: prediction and reconciliation

**Goal:** the local character moves within one tick of a key press, exactly as the server will move it, so it never snaps away from where the server says it is.

**Layer in this step:** `game/systems/input-sender`, `game/systems/prediction`, `game/scenes/world` (page 7 layer 4).

## Scope

- Input sender: samples the held keys through `input/actions` once per 50 ms tick (`lib/fixed-step` flags each tick's first sub-step), latching a jump pressed and released inside the tick, and sends that sample as the tick's Input with the next sequence number, every tick, even when nothing changed. When `ui.captureKeyboard` becomes set it sends an all-released Input at once, then samples no keys until it clears.
- Prediction: a fixed 1/60 s accumulator runs `sim/step` for the local player, and each sub-step uses the Input sent for its tick. That is exactly what the server runs once backend step 18 makes it apply one Input per tick in sequence order. The last 2 s of inputs and predicted states (40 ticks) are kept by sequence number in a ring buffer.
- Development only: a send and receive delay in `GameSocket` (`VITE_NET_DELAY_MS`, read through `config/env`, ignored in builds) for the latency check below. Chrome's network throttling has historically not slowed WebSocket frames.
- Reconciliation: on each Delta, the local player's entry is compared with the predicted state at `ack_seq`; if they differ by more than 4 px, or a Correction arrives, reset to the server state and replay the unacknowledged inputs.
- The scene draws the predicted state, and pushes HP and MP to the `character` store only when they change.

## Design notes

- Prediction and reconciliation are functions over plain data (`predict(history, input)`, `reconcile(history, serverState, ackSeq)`), so they run under Vitest without Phaser.
- Correction is reserved on page 4; the code path exists but Stage 1 traffic never exercises it.
- Keys take effect at the next tick boundary, up to 50 ms later and 25 ms on average: the price of matching the server exactly. A match should leave reconciliation idle; a reset means a real disagreement, such as contact-damage knockback, which the server applies and the next Delta reports.

## Tests first

- prediction over a scripted input sequence equals running `sim/step` directly
- reconcile with a server state equal to the prediction leaves the history untouched
- reconcile with a 5 px difference resets and replays, and the replayed state equals a fresh simulation from the server state
- reconcile with an `ack_seq` older than the buffer drops the stale entries without throwing
- when `captureKeyboard` becomes set, the input sender sends one all-released Input at once, then nothing until it clears
- the input sender sends exactly one Input per tick, with consecutive sequence numbers, including ticks where no key changed
- a jump pressed and released between two tick boundaries goes out as `jump: true` for that tick
- server states computed by running `sim/step` 3 times per tick over the sent inputs never trigger a reset
- reconcile part way through a tick finishes the tick for the comparison (a localhost Delta lands before the client's three sub-steps are done); a Correction resets and replays after its seq
- keyboard: each movement key under both presets presses and releases its action and the page does not scroll; while captured, key downs are ignored but key ups still release; losing focus releases every key
- opt-in live check (`FINGA_LIVE_URL`, `FINGA_LIVE_DELAY_MS`): a scripted hundred ticks through the real socket, counting resets

## Done when

- [ ] Against `make up`: walk, jump, drop through a platform and climb a rope in the town with no visible snapping; with `VITE_NET_DELAY_MS=100` (200 ms round trip), movement still feels immediate and settles without rubber-banding. (Checked through the real socket by the opt-in live test at 0 and 100 ms one-way delay: a hundred ticks with no reset, a 3-tick stall held by the server, an 8-tick stall exactly one reset; backend step 18's hold rule, decision 127. The in-browser feel is owed with the M1 hand check in C09.)

## Review focus

- The history never grows past 2 s.
- No `state` writes inside the frame loop except the HP and MP change check.
