# TD-10. Client framework

- **Area:** Architecture
- **Decision:** TD-10. Client framework
- **Default:** Phaser with TypeScript
- **Outcome:** Phaser with TypeScript, over Ebitengine compiled to WASM. Menus like inventory and market are much easier as HTML overlays, and the ecosystem is bigger.
- **Status:** Decided

## Options considered

- **Phaser with TypeScript.**
- **Ebitengine compiled to WASM:** one language and shared physics, but painful UI and a heavier download.

## Cost accepted

Movement rules exist in both TypeScript and Go; shared test vectors keep them in sync (HLD section 16).
