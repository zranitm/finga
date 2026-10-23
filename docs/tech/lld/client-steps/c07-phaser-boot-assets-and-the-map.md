# C07. Phaser boot, assets and the map

- **Depends on:** C03, C06
- **Features:** B1
- **Milestone:** M1. Walk together
- **Order:** 7
- **Status:** Done
- **Step:** C07. Phaser boot, assets and the map

**Goal:** the game canvas shows the current map with the local character standing at the server's position.

**Layer in this step:** `game/main.ts`, `game/keys.ts`, `game/scenes/boot`, `game/scenes/preloader`, `game/scenes/world` (first cut), `game/entities/player`, `app/PhaserGame.tsx` (page 7 layers 4 and 5).

## Scope

- `app/PhaserGame.tsx` mounts when the app starts, with the canvas behind the React screens, creates the game once and destroys it on unmount, guarded against React's development double mount.
- Boot and Preloader: load the Kenney CC0 images and spritesheets under `public/assets/` and the three maps from `public/maps/` (copied from `data/maps`) in the background while the player is on the React screens. No texture atlases in Stage 1 (page 7, Assets).
- Assets, decided (finga, 2026-10-22; decisions 119 and 120): Kenney "Platformer Pack Remastered" (the current version of Platformer Pack Redux, which [kenney.nl](https://kenney.nl) no longer hosts) for ground, platforms, ladders (ropes), doors (portals), backgrounds, the four mobs (slimeGreen, snail, mouse, fly for `green_slime`, `orange_mushroom`, `wild_boar`, `cave_bat`) and the two classes (alien Blue for Warrior, Green for Bowman), all 128 px art at scale 0.25 on the 32 px grid; Kenney "Generic Items" for the flask the two potions tint; the other ten item templates are original 32 px shapes drawn at runtime. `public/assets/LICENSES.md` lists packs, dates, URLs and the CC0 line, and the original placeholders.
- `game/keys.ts`: scene, asset and animation keys as constants.
- World scene: draws the map named in Welcome from its object layers, since the maps carry no tile art: solids and platforms as repeating textures, ropes, and portal markers. It builds the `sim/map` collision view from the same objects, places the local player at its entry in the first Snapshot after Welcome (Welcome carries no position), and the camera follows the player within map bounds.
- Player entity: a view that draws position, facing and the idle, walk, jump and climb animations from the state it is given.

## Design notes

- Entities own no rules. The scene passes them plain state every frame.
- The map is not validated here; the Go `gamedata` tests already did (page 7, Phaser conventions).

## Tests first

- `PhaserGame` (Testing Library with a stubbed Phaser): one game instance after a double mount, destroyed on unmount
- Preloader asset manifest: every key in `game/keys.ts` is in the manifest and every manifest entry has a file under `public/assets/` (a Node test over the filesystem)
- World layout as pure data: solids, platforms, ropes and portals become tiles with the map's look; poses from the movement state
- `GameHandle`: attach once, enter before or after attach, restart on a second enter, leave stops the World

## Done when

- [ ] Against `make up`: enter the world and see the town with the character on the spawn point; the camera stays inside the map. (Every manifest file and map was served and fetched from the dev server; the look itself is owed with the M1 browser hand check in C09, finga's ruling of 2026-10-22.)

## Review focus

- No game logic in scenes beyond calling systems.
- Asset licences recorded in `public/assets/LICENSES.md`.
