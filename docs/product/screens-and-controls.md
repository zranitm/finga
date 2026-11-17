# Screens and controls

The screens players see and the keys they use. This is the input for the design phase.

## Screens

| Screen | What the player does there | Priority |
| --- | --- | --- |
| **Sign up and log in** | Create an account, log in | P0 |
| **Character creation** | Pick a name; every character starts as a Beginner | P0 |
| **Game view and HUD** | Play: the map, seen from above, looking north at a fixed angle, the camera easing after the player; HP and EXP bars, the energy pips, and level at the top left; the field boss's bar and, smaller under it, the locked mob's target window with its stats, at the top centre (C15). The player's frame, the party's, and the target window share one look: a green edge and dot for the player and allies, a red edge and ✕ for enemies; Back to characters and the party panel at the top right; the chat at the bottom left (see [Chat](#chat)); the minimap at the bottom right (B8); currency from P1 | P0 |
| **Inventory** | See items by tab; drink potions; discard them, one at a time or several at once | P0 |
| **Party window** | Invite, accept, leave, kick, hand over leadership | P0 |
| **Marketplace** | Browse and search, buy, list an item, manage my listings | P1 |
| **Settings** | Choose a control preset; turn on spending stat points automatically at level-up (off by default); show the minimap (on by default); Credits, naming the icons' and art's authors (C16) | P0 |
| **Map window** | M: This map, the whole current map with its portals, NPCs, and named spawn areas; World, every map as a chart linked by portals, with where the player and their party are; picking a map shows its layout and a Teleport here button (B8, B9) | P1 |
| **Character select** | Pick a character to play | P1 |
| **Channel select** | Pick or switch channel | P1 |
| **Character window** | See level, EXP, HP, every stat, and the ten equipment slots; equip and unequip; spend, auto-assign, and reset stat points | P1 |
| **Item details** | Hover an item to see its stars, stats, and requirements, compared with what's worn | P1 |
| **NPC shop** | Buy and sell at fixed prices | P1 |
| **Skill window** | See every skill you can learn, each with its icon, spend skill points, follow evolutions; drag active skills to the skill bar by their icons | P1 |
| **Skill bar** | Assign skills to keys; each key shows the skill's icon, its energy cost, and a sweep with the seconds left while it cools down (C16) | P1 |
| **NPC dialog** | Talk to an NPC: promotions, quests to take and hand in, later shops | P1 |
| **Quest log** | See active quests and their progress; abandon one | P1 |

## Chat

The chat stays out of the way of play until the player uses it.

- **Idle:** only the last three lines of the chosen tab show, low in the bottom-left corner. There are no tabs and no input.
- **Open:** Enter, or a click on the idle lines, opens it with the General, Party, and Combat tabs, the last eight lines, and the input. Esc or sending a line closes it again.
- **Pin:** the Pin button keeps it open while the player isn't typing. The choice is saved with the account's settings.
- **Newest line:** it always shows. A long line that wraps pushes the oldest line out of view.

## Controls

Keyboard only. Attacks and skills go to the locked mob (C15): the nearest on screen is locked, a gold ring marks it, a target window at the top shows its name, level, and HP, and Tab switches to the next closest. With nothing locked, they go the way the character faces. The mouse is used only for menus such as inventory and the marketplace. Players choose one of two presets in Settings.

| Action | Classic | WASD |
| --- | --- | --- |
| Walk up, down, left, and right on screen; two together walk diagonally | Arrow keys | W, A, S, and D |
| Jump | Space | Space |
| Enter a portal | Z, while standing in it | E, while standing in it |
| Attack | X | J |
| Lock the next closest mob | Tab | Tab |
| Inventory | I | I |
| Map | M | M |
| Party | P | P |
| Character window | C | C |
| Talk to an NPC | Z, while in reach | E, while in reach |
| Drink an HP potion | A | R |
| Skill window | K | K |
| Quest log | Q | Q |
| Open the chat; close it while typing | Enter; Esc | Enter; Esc |
| Close the top window | Esc | Esc |
| Use the skill on the skill bar's key | 1 to 6 | 1 to 6 |

There is no pickup key: drops are collected automatically (D1). The old pickup keys now talk to NPCs (Z, Classic) and open the skill window (K) (proposal).

Remappable keys: later (P2).
