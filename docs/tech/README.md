# Tech

How the game is built. The full tech design was removed on 2026-11-05; pages come back one at a time, when a feature needs them. Each page describes the code as it stands and the change it plans, and is kept true to the code once built.

One rule runs through every page: **the client computes, the server validates.** Whatever is display-only, and derivable from game data and state the client already holds, the client works out itself, so the server can serve more players. Examples are markers, previews, and progress counts. Where a client copy of a rule could drift, shared test vectors pin both sides. The server checks every action, and computes whatever grants or changes state.

- [Equipment, rolled stats, and item details](./equipment-and-item-details.md): D3, D6, D7.
- [Auto pickup and the combat channel](./auto-pickup-and-combat-channel.md): D1, E3's party loot, E5.
- [Stat system: points, fifteen derived stats, damage types, and speed](./stat-system.md): C6, Progression, Combat.
- [NPCs and task quests](./npcs-and-quests.md): H1, H2.
- [Classes, promotion, and skills](./classes-and-skills.md): C5, C8, A2.
- [Animation cancel](./animation-cancel.md): C9, Combat → Actions.
- [Field bosses](./field-bosses.md): C10, Combat → Field bosses.
- [Combat feel](./combat-feel.md): C11, Combat → Actions and Pace.
- [Mobs fight back](./mobs-fight-back.md): C12, Combat → Mobs and Field bosses.
- [One binary: a modular monolith](./modular-monolith.md): the six services as modules in one process.
- [Combat effects](./combat-effects.md): C13, Combat → What players see.
- [Energy](./energy.md): C14, Combat → Energy, Progression.
