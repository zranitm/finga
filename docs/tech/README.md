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
- [3D world](./3d-world.md): B7; the 3D rebuild, its `3d` branch, and the slice.
- [Target lock](./target-lock.md): C15, Combat → Aiming; replaces the 3D world's auto-aim.
- [Minimap and world map](./map-views.md): B8.
- [Skill icons and cooldowns](./skill-icons.md): C16.
- [Teleport](./teleport.md): B9; with the map layout of Minimap and world map, section 12.
- [A fuller skill roster](./skill-roster.md): C17; moving skills, dodges, control, poison, shields, and 22 skills.
- [Skill chains](./skill-chains.md): C18; a key turning into a follow-up while its condition holds.
- [Abnormal statuses](./statuses.md): C19; one status system for mobs and players.
- [Key bindings](./key-bindings.md): B6; one default layout with skills on Q W E R A S D F, every key rebindable in Settings, saved with the account.
- [Skill traits](./skill-traits.md): C20; a default and an extra trait per active skill, and the skill window as a grid with cards on hover.
- [Blocking](./blocking.md): C21; a block skill per class with counters, and warning areas for field bosses only, yellow or red.
- [Buffs and hidden cooldowns on the frame](./frame-icons.md): C22; the player's buffs and the cooldowns of follow-ups no key shows, beside their statuses.
- [Large regions](./large-regions.md): B10 to B12; three regions about 1 km across, generated from a plan, with only what's near sent and drawn, region channels, sprint, waypoints and camera zoom.
- [PvP](./pvp.md): I1 to I4; matchmaking with bots, match instances, combat between players, 1v1 and 3v3 rounds, 15v15 capture points, and ratings.
- [Loot pouches and timed bosses](./loot-pouches.md): D1, C10; pouches and bosses' treasure boxes opened with T, and bosses back 5 minutes after they're beaten.
- [Outfits](./outfits.md): D8, D9; a wardrobe per account with outfit, headwear, face, weapon looks and wings on KayKit's rig, from boss drops, PvP ranks, quests and feats.
- [Underground and underwater](./underground-and-underwater.md): B13; caves lit by torches and a sunken shrine, small regions entered through a gate, with a baked light map and a sea look.
- [The green region](./green-region.md): B10 and B13; Willowmere, levels 1 to 12, with twelve new mobs, Greyfang Cave and the Sunken Shrine, replacing Town, Forest and Cave.
