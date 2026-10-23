# TD-18. Movement: does the server simulate from inputs or check client positions?

- **Area:** Architecture
- **Decision:** TD-18. Movement: does the server simulate from inputs or check client positions?
- **Default:** Server simulates from inputs
- **Status:** Open

## Options

- **A.** The client sends inputs; the server runs the same movement code and is the only source of positions. The client predicts and gets corrected.
- **B.** The client sends positions; the server checks them for plausibility.

## Why the default

A can't be cheated by sending fake positions, and the shared movement code is tested anyway (shared test vectors). B is simpler but leaves room for speed hacks and wall clipping.

## Affects

Low-level design: Game server runtime, Protocol, Gameplay rules in code, Client.
