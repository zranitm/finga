# C05. state and input

- **Depends on:** C02
- **Features:** B6
- **Milestone:** Foundations
- **Order:** 5
- **Status:** Done
- **Step:** C05. state and input

**Goal:** the stores that menus and the HUD read, and the key presets players choose.

**Layer in this step:** `state`, `input`, `components` (page 7 layer 3).

## Scope

- Zustand 5 stores, one file each: `session` (the account and whether it is logged in; the token itself stays in `net/token.ts`, C04), `character` (the Welcome character: level, exp, stats, hp, mp and maxima, plus the EXP the next level needs, from the generated EXP curve), `inventory` (3 tabs x 24 slots, stacks to 100), `party`, `chat` (last 100 lines, map and party), `connection` (connecting, connected, reconnecting, kicked), `settings` (preset, cached in `localStorage`), `ui` (open windows, `captureKeyboard`).
- Each store exposes plain actions that take protocol payloads: `applyWelcome`, `applyInventoryChanged`, `applyPartyState`, `applyChatMessage`, `applyLevelUp`, `applyExpGained`, `setConnection`.
- `input/presets.ts`: the two presets from Screens and controls (Classic: arrows, Space, X attack, Z pick up; WASD: WASD, Space, J attack, K pick up; I, P and Enter shared), and `input/actions.ts` mapping key codes to actions through the chosen preset.
- `components/`: Button, Window (draggable title bar, close on Escape), Bar (for HP, MP, EXP), Modal.

## Design notes

- Stores never hold per-tick world data (positions, mobs, drops); that stays in `game/systems` (C08).
- `settings` caches the preset locally so the first frame uses it before `GET /api/settings` answers.

## Tests first

- `inventory`: `applyInventoryChanged` places, moves, stacks and removes by the server's payload; a slot never holds two items; a refused move leaves the store unchanged
- `party`: a PartyState with `party: null` clears members; leader and member order follow the payload
- `chat`: the buffer keeps the last 100 lines and tags map and party lines
- `character`: the EXP needed for the next level comes from the generated curve; a negative `ExpGained` (the death penalty, backend step 19) lowers `exp` to the payload's total
- `settings`: preset round-trips through `localStorage`, and a bad cached value falls back to Classic
- `input`: each preset maps every action in the Screens and controls table; a key not in the preset maps to nothing
- `components`: Window closes on Escape and sets `ui.captureKeyboard` while focused (Testing Library)

## Done when

- [ ] Tests pass with 80% coverage on `state`.

## Review focus

- A store action is the only way state changes; no component mutates a store object.
