# Client steps

| Step | Depends on | Features | Milestone | Order | Status |
| --- | --- | --- | --- | --- | --- |
| [C00. Client skeleton](./c00-client-skeleton.md) | B17 (published backend) |  | Foundations | 0 | Done |
| [C01. lib and config](./c01-lib-and-config.md) | C00 |  | Foundations | 1 | Done |
| [C02. protocol: types and codec](./c02-protocol-types-and-codec.md) | C01 |  | Foundations | 2 | Done |
| [C03. sim: movement mirror](./c03-sim-movement-mirror.md) | C01 | B2 | Foundations | 3 | Done |
| [C04. net: REST and WebSocket clients](./c04-net-rest-and-websocket-clients.md) | C01, C02 | A1 | Foundations | 4 | Done |
| [C05. state and input](./c05-state-and-input.md) | C02 | B6 | Foundations | 5 | Done |
| [C06. app shell: sign up, log in, characters, settings](./c06-app-shell-sign-up-log-in-characters-settings.md) | C04, C05 | A1, A2, B6 | Foundations | 6 | Done |
| [C07. Phaser boot, assets and the map](./c07-phaser-boot-assets-and-the-map.md) | C03, C06 | B1 | M1. Walk together | 7 | Done |
| [C08. Local player: prediction and reconciliation](./c08-local-player-prediction-and-reconciliation.md) | C07, B18 | B2 | M1. Walk together | 8 | Done |
| [C09. Other players, HUD basics and connection loss](./c09-other-players-hud-basics-and-connection-loss.md) | C08 | B2, A1 | M1. Walk together | 9 | Done |
| [C10. Mobs, attack, damage and death](./c10-mobs-attack-damage-and-death.md) | C09, B19 | C1, C2, C3, C4 | M2. Fight and grow | 10 | Done |
| [C11. Drops, pickup and the inventory window](./c11-drops-pickup-and-the-inventory-window.md) | C10 | D1, D2, D5 | M3. Loot and keep | 11 | Done |
| [C12. Portals and map change](./c12-portals-and-map-change.md) | C11 | B3, B5 | M4. Travel | 12 | Done |
| [C13. Chat and party](./c13-chat-and-party.md) | C12 | E1, E3 | M5. Play together | 13 | Done |
| [C14. Polish and the proof session](./c14-polish-and-the-proof-session.md) | C13 |  | M6. Proof session | 14 | Ready for review |
