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
  tile is one 32 px grid cell. Until the pack's sand, snow and castle art is
  imported, the desert, snow and keep maps draw the dirt and stone tiles and
  the `colored_land` and `blue_land` backgrounds tinted, and the new mobs are
  the four enemies above, tinted and, for elites, drawn 1.5 times as big.

## Kenney, Generic Items (`generic-items/`)

- Author: Kenney Vleugels, https://kenney.nl
- Page: https://kenney.nl/assets/generic-items
- File: `kenney_generic-items.zip`, "Generic Items #1", release of 2023-03-01
- Licence: CC0 (same wording as above)
- Used: `genericItem_white_105.png` as `flask.png`, tinted for the five
  potions.

## Original placeholders (no files)

Every item template with no match in the packs above is a 32 px shape drawn
at runtime by `src/game/scenes/placeholders.ts`: equipment by its slot kind in
its stars' colour, and the drop materials (`slime_gel`, `bat_wing` and the
rest of the `etc` tab) from a few shapes (gel, shell, wing, fang, feather and
so on) in their mob's colours. They are original to this project and released
under the same CC0 terms.
