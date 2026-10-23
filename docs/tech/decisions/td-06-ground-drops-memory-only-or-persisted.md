# TD-06. Ground drops: memory only, or persisted?

- **Area:** Data
- **Decision:** TD-06. Ground drops: memory only, or persisted?
- **Default:** Memory only
- **Status:** Open

## Options

- **A.** Memory only: a drop becomes a real item when picked up.
- **B.** Persist drops so they survive a crash.

## Why the default

A drop nobody owns yet can be lost in a crash without harming any player, and it keeps the item history clean.

## Affects

HLD sections 5, 6, and 9.
