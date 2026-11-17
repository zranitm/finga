# Skill icons and cooldowns

> **Status: built · 2026-11-17.** The high-level design for [C16. Skill icons and cooldowns](../product/features/combat-and-progression/c16-skill-icons.md). Client-only, apart from one data key, `icon`, which the loader carries. As built, the tile is in `components/`, and `colorOf` moved to `progression/` (section 4).

## 1. What has to hold

- **The user's words** (2026-11-17):
  - "we should setup the icon for the skills, we dont use bare texts when moving skill from skill UI to button";
  - "we also display skill icon with times until it cooldown".
- **Their choice:** game-icons.net, which needs a credits line, over CC0-only icons or drawing our own.
- **The client computes:** cooldowns are already timed on the client (`skillBar.cooldowns`: `from` and `until` per skill). Nothing new crosses the wire.

## 2. The change at a glance

| Part | Today | After |
| --- | --- | --- |
| `server/data/skills.yaml` | — | `icon` on every skill, and optionally on a stage: a game-icons.net path, such as `lorc/broadsword` |
| `load/` | — | Requires `icon` on every skill. A stage's overrides the skill's, as other stage fields do |
| `web/public/icons/skills/` | — | The SVGs used, copied from game-icons.net, each at `<author>/<name>.svg` |
| `web/public/icons/LICENSES.md` | — | Each author, the source, and CC BY 3.0 |
| `features/skills/SkillIcon.tsx` | — | New: an icon tile, with an optional cooldown sweep and count. As built, `components/SkillIcon.tsx` (section 4) |
| `SkillBar.tsx` | Number, cost, name, a CSS shade | Number, cost, and the icon tile with its cooldown. The name goes to the tooltip and `aria-label` |
| `SkillWindow.tsx` | Rows of text | Each row leads with the icon tile |
| Dragging | The browser's default ghost: the text | `setDragImage` with the icon tile |
| Settings | — | A Credits section |

## 3. Data and files

- **`icon` is a path under game-icons.net,** author then name, as its site lists them, such as `lorc/broadsword` or `delapouite/fire-shield`.
  - Every skill names one. A stage may name its own, so Strike, Heavy Strike, and Crushing Blow can each look stronger.
  - The build picks them: 12 skills, and their evolutions where a stronger-looking icon fits.
- **The SVGs** are copied once into `web/public/icons/skills/<author>/<name>.svg`. They're single-colour paths on a transparent ground, as game-icons.net serves them. Only the used ones are kept.
- **`gen-gamedata.ts`** carries `icon` into the generated skills.
- **A client test** fails if any skill's or stage's icon has no file, or if a file is unused.
- **As built:**
  - **The loader** also refuses an icon that isn't author/name, which stops `lorc/x.svg` and `../x`. The resolved stage carries the icon beside its name and description.
  - **The repo's files** (game-icons/icons on GitHub) carry a black 512×512 square under the glyph, which a mask would draw as a solid tile. Each file is rewritten as its glyph's paths only, and the rewrite refuses anything but path data. `LICENSES.md` says so.
  - **`LICENSES.md`** has a row per file, and the client test checks that too.
  - **The icons,** chosen by eye from contact sheets, then checked at 40 px:

| Skill | Its icon | Evolutions |
| --- | --- | --- |
| Strike | `lorc/sword-wound` | Heavy Strike `lorc/sword-slice`, Crushing Blow `lorc/hammer-drop` |
| Throw | `delapouite/throwing-ball` | Sling `delapouite/slingshot`, Volley `lorc/thrown-charcoal` |
| Spark | `lorc/circle-sparks` | Bolt `lorc/focused-lightning`, Arc `lorc/lightning-branches` |
| Second Wind | `lorc/wind-hole` | Deep Breath `delapouite/lungs` |
| Slash Blast | `lorc/sword-spin` | — |
| Iron Body | `lorc/breastplate` | — |
| Double Stab | `lorc/knife-thrust` | — |
| Haste | `lorc/sprint` | — |
| Power Shot | `lorc/heavy-arrow` | — |
| Focus | `delapouite/eye-target` | — |
| Fire Arrow | `lorc/flaming-arrow` | — |
| Heal | `zeromancer/heart-plus` | — |

## 4. The tile

`features/skills/SkillIcon.tsx` draws a square tile, 40 px on the bar and 32 px in the skill window:
- **The ground** is the skill's colour (`colorOf` in `game/effects/colors.ts`), darkened to 35%, with a 1 px border in the full colour.
- **The glyph** is the SVG as a white mask (`mask-image`), so every icon takes the same look whatever its author drew.
- **The stage** shows as one, two, or three small pips along the bottom edge.
- **Off** (short of energy or points): the tile is greyscale at 60% opacity, as the key's `skill-key-off` greys it today.
- **As built:**
  - **Where it lives:** features may not import each other, and both the bar (`features/hud`) and the window (`features/skills`) draw the tile. So it's `components/SkillIcon.tsx`. `colorOf` moved from `game/effects/colors.ts` to `progression/skills.ts`, which `components/` may import; the world's looks import it from there.
  - **A passive's colour:** Second Wind has no damage type, so `colorOf` gives it physical's orange.
  - **Pips:** one per stage reached, only on a skill that evolves. A one-stage skill shows none, and so does an evolving one with no points. The tile takes the points and works out its stage itself.
  - **The bar's keys** are the tiles, six 40 px keys 1 px apart, so the bar fits under the 246 px status frame. The key's number and the energy cost sit over the tile's top corners. An empty key is an outlined slot.
  - **The window greys a tile** only for a skill without points. Being short of energy is the bar's to show.

## 5. Cooldowns

- **The sweep:** a dark shade at 60% opacity over the tile. It's a `conic-gradient` whose angle runs from 360° to 0° clockwise from the top, as the cooldown passes from `from` to `until`.
- **The count:** centred, bold, white with a dark outline.
  - At or above one second: the seconds left, rounded up (4.2 s shows "5").
  - Below one second: tenths, rounded up ("0.7").
- **Updating:** one `requestAnimationFrame` loop per cooling icon sets the angle every frame and the count when its text changes. It stops when the cooldown ends.
- **Ready:** when `until` passes, the tile flashes white for 150 ms.
- **Restored mid-way:** a cooldown restored part-way at a Welcome starts part-way, as the shade does today.
- **The skill window** shows the same sweep and count on its tiles.
- **As built:**
  - **The sweep** is the shade's edge moving clockwise from the top: the clear wedge grows as the shade shrinks.
  - **The flash:** the store ends a cooldown by its own timer, which can beat the last frame. So a cooldown leaving within 50 ms of its end flashes too, and one taken away earlier doesn't.
  - **Off greys only the face:** the ground, the glyph and the pips. A cooling key short of energy keeps its count and sweep at full strength over it, after finga-player saw Iron Body's "58" go pale over grass.
  - **The CSS-animated shade** it replaces is gone.

## 6. Dragging

- **From the skill window and from a key alike,** `onDragStart` calls `dataTransfer.setDragImage` with the tile's element, offset to its centre. What's dragged is the icon.
- **Drop rules don't change:** a skill is copied onto a key, two keys swap, and a key dropped off the bar empties.
- **As built:** `dragTile` in `components/SkillIcon.tsx` does it for both, finding the tile inside the dragged row or key.

## 7. Credits

- **Settings** gains a Credits section that lists:
  - "Skill icons from game-icons.net by" each author whose icon is used, worked out from the `icon` paths, "under CC BY 3.0", with a link;
  - the 3D packs used (KayKit, Quaternius, and Kenney), CC0, as a courtesy.
- **`web/public/icons/LICENSES.md`** lists the same, as the model packs' `LICENSES.md` does.
- **As built:**
  - **Icon authors:** `features/settings/Credits.tsx` names each one from the icon paths' first part, capitalised: Delapouite, Lorc and Zeromancer, as the site names them.
  - **The 3D packs:** no Kenney model ships (`public/models/LICENSES.md` lists none), so the line thanks Kay Lousberg (KayKit) and Quaternius. A test fails if that file names a maker the credits don't.

## 8. Testing

- **Loader:** a skill without `icon` is refused; a stage's icon overrides the skill's.
- **Files:** every icon has its SVG, and no SVG is unused.
- **`SkillIcon`:**
  - the ground in the skill's colour, and the pips by stage;
  - greyed when off;
  - the sweep's angle at a cooldown's half-way point;
  - the count at 4.2 s ("5"), 1.0 s ("1"), and 0.66 s ("0.7");
  - the flash at the end;
  - a part-way start.
- **`SkillBar`:** the icon in place of the name; the number and the cost; the name in `aria-label` and the tooltip.
- **`SkillWindow`:** each row's icon.
- **Dragging:** `setDragImage` gets the tile, from the window and from a key; the drop rules are as before.
- **Credits:** each used author is listed once.
- **Live:**
  - every class's skills have icons that read at 40 px;
  - drags show the icon;
  - the count reads against every colour of tile.

## 9. Build

One PR, each step one commit:
1. **Data:** `icon` in `skills.yaml` and the loader, the SVGs and `LICENSES.md`, and the generator.
2. **`SkillIcon`,** its cooldown sweep and count, and the skill bar and window on it.
3. **Drag images and Credits.**

## 10. Decisions

The user's choice of game-icons.net is in section 1. These calls were made in planning and logged for the user to push back on:

1. **One icon per skill,** and a stage may name its own, so evolutions can look stronger.
2. **White glyphs on a tile in the skill's colour,** the colours the skill's flash already uses, so the bar reads by damage type.
3. **The count:** whole seconds rounded up, tenths below one second. A clockwise sweep from the top. One flash when ready.
4. **The bar's keys show only the icon,** with the name on pointing.
5. **Credits in Settings,** for CC BY 3.0's attribution.

## 11. Alternatives considered

- **CC0 icons only:** fewer that fit, and mixed styles across 12 skills. The user chose game-icons.net.
- **Icons drawn in-house:** plainer than a pro set. The user chose game-icons.net.
- **Keeping each SVG's own colours:** game-icons.net's icons are one colour by design. Masking them white on the skill's colour keeps the set consistent.
- **Today's draining shade, without a count:** a clockwise sweep with the seconds left is the convention most players read without thinking.
