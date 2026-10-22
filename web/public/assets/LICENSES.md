# Asset licences

Everything under `public/assets/` is CC0 (Creative Commons Zero 1.0,
https://creativecommons.org/publicdomain/zero/1.0/) unless a section below
says otherwise. Credit to Kenney is appreciated but not required by the
licence; it is given here.

## Kenney, Platformer Pack Remastered (`remastered/`)

- Author: Kenney Vleugels, https://kenney.nl
- Page: https://kenney.nl/assets/platformer-pack-remastered
- File: `kenney_platformer-pack-remastered.zip`, release of 2026-03-29
  (the pack carries no version number; this is the file date on kenney.nl)
- Licence: CC0 ("You may use these graphics in personal and commercial
  projects. Credit (Kenney or www.kenney.nl) would be nice but is not
  mandatory.")
- Used: ground tiles (Dirt, Grass, Stone: `*Mid`, `*Center`, `*Half_mid`),
  tiles (`ladderMid`, `ladderTop`, `doorClosed_*`, `doorOpen_*`), enemies
  (`slimeGreen*`, `snail*`, `mouse*`, `fly*`), players (`alienBlue_*`,
  `alienGreen_*`), backgrounds (`colored_grass`, `colored_land`,
  `colored_shroom`, `blue_land`). All 128 px art drawn at scale 0.25 so one
  tile is one 32 px grid cell.

## Kenney, Generic Items (`generic-items/`)

- Author: Kenney Vleugels, https://kenney.nl
- Page: https://kenney.nl/assets/generic-items
- File: `kenney_generic-items.zip`, "Generic Items #1", release of 2023-03-01
- Licence: CC0 (same wording as above)
- Used: `genericItem_white_105.png` as `flask.png`, tinted red and blue for
  the two potions.

## Original placeholders (no files)

The item templates with no match in the packs above (`wooden_sword`,
`short_bow`, `iron_sword`, `hunting_bow`, `leather_cap`, `leather_vest`,
`slime_gel`, `mushroom_cap`, `boar_tusk`, `bat_wing`) are 32 px shapes drawn
at runtime by `src/game/scenes/preloader.ts` in the Kenney palette. They are
original to this project and released under the same CC0 terms.
