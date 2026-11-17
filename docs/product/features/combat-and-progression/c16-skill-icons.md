# C16. Skill icons and cooldowns

- **Feature:** C16. Skill icons and cooldowns
- **Group:** C. Combat and progression
- **Priority:** P1
- **Status:** Agreed

**Story:** As a player, I recognise each skill by its icon, on the skill bar, in the skill window, and while I drag it between them. I see at a glance how long each one has left before I can use it again.

## Acceptance

- **Every skill has an icon,** passives included.
  - An evolution may have its own icon. One that doesn't keeps its skill's.
  - Icons come from game-icons.net, drawn white on a tile in the skill's colour: orange for physical, violet for magic, cyan for heals, and gold for buffs.
- **The skill bar's keys show icons, not names.**
  - The key's number sits in one corner, and the energy cost in another.
  - The name shows on pointing, and to screen readers.
  - A key the player can't use now, for want of energy or points, shows its icon greyed.
- **The skill window shows each skill's icon** on its tile, with its name under it; its card shows the description too (C20).
- **Dragging a skill** from the skill window to the bar, or from one key to another, carries its icon under the pointer, not its name.
- **Cooldowns:** while a skill cools down, its icon darkens under a shade that sweeps away clockwise from the top as the time runs out. The seconds left are counted on it:
  - in whole seconds, rounded up, as "5", "4", and so on;
  - in tenths below one second, as "0.7".
  - When it's ready again, the icon flashes once.
  - The skill window's icons show the same sweep and count.
- **Credits:** Settings gains a Credits section naming the icons' authors from game-icons.net under CC BY 3.0, beside the free art packs the game uses.

## Not in this feature

- Icons for items or buffs.
- Timers for buffs that are running.
- Drawing icons of our own.

## Rules

Screens and controls, and Systems → Classes and skills.
