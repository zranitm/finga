# TD-23. Client stack and structure

- **Area:** Architecture
- **Decision:** TD-23. Client stack and structure
- **Default:** Phaser 4, React 19, Zustand, Vite 8, TypeScript 6
- **Outcome:** Phaser 4 for the game, React 19 for the HTML overlay (replacing the earlier Preact proposal), Zustand as the shared store, Vite 8, TypeScript 6.x until typescript-eslint supports 7, Vitest 4, ESLint 9 with typescript-eslint, Prettier, Node 24 LTS. Layered layout following bulletproof-react.
- **Status:** Decided

## Why React over Preact

- Phaser publishes an official React template, so the Phaser and React integration follows a maintained example.
- React has far more learning material, examples, and libraries, which matters with no frontend experience.
- Preact's size advantage doesn't matter next to the game's assets.

## Why Zustand instead of the template's event bus

- One typed store works from Phaser (`getState`, `subscribe`) and from React (`useStore` with selectors).
- Untyped string events are easy to mistype and hard to trace.

## Why TypeScript 6, not 7

TypeScript 7.0 ships without a programmatic API, which is expected in 7.1. Until then typescript-eslint can't run on it.

## Structure

Layered, one-way imports enforced by ESLint: `lib` and `config`, then `protocol` and `sim`, then `net`, `state`, `input`, and `components`, then `game` and `features`, then `app`.

## Details

Low-level design, page 7: Client.
