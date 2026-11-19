# Screens and controls

The screens players see and the keys they use. This is the input for the design phase.

## Screens

| Screen | What the player does there | Priority |
| --- | --- | --- |
| **Sign up and log in** | Create an account, log in | P0 |
| **Character creation** | Pick a name; every character starts as a Beginner | P0 |
| **Game view and HUD** | Play: the map, seen from above, looking north at a fixed angle, the camera easing after the player; HP and EXP bars, the energy pips, and level at the top left; the field boss's bar and, smaller under it, the locked mob's target window with its stats, at the top centre (C15). The player's frame, the party's, and the target window share one look: a green edge and dot for the player and allies, a red edge and ✕ for enemies, and the unit's statuses as icons under its HP bar (C19), with, on the player's own frame, their buffs before them and the cooldowns of the follow-ups no key shows after (C22), and a walk icon first while walk mode is on (B11); Back to characters and the party panel at the top right; the chat at the bottom left (see [Chat](#chat)); the minimap at the bottom right (B8); currency from P1 | P0 |
| **Inventory** | See items by tab; drink potions; discard them, one at a time or several at once | P0 |
| **Party window** | Invite, accept, leave, kick, hand over leadership | P0 |
| **Marketplace** | Browse and search, buy, list an item, manage my listings | P1 |
| **Settings** | Rebind keys in the Key bindings window (B6); turn on spending stat points automatically at level-up (off by default); show the minimap (on by default); Credits, naming the icons' and art's authors (C16) | P0 |
| **Map window** | M: This region, the current region with pan and zoom: its areas with their levels, towns, camps, found and unfound waypoints, gates, NPCs and party, and the channel to switch (B10, B11); on an old map still in use, This map as before. World, the regions and old maps as a chart, with where the player and their party are. A found waypoint, or a region's main town, has a Teleport here button (B8, B9, B11) | P1 |
| **Character select** | Pick a character to play | P1 |
| **Channel select** | Pick or switch channel | P1 |
| **Character window** | See level, EXP, HP, every stat, and the ten equipment slots; equip and unequip; spend, auto-assign, and reset stat points | P1 |
| **Item details** | Hover an item to see its stars, stats, and requirements, compared with what's worn | P1 |
| **NPC shop** | Buy and sell at fixed prices | P1 |
| **Skill window** | See every skill you can learn as a grid of icons, by class, with each skill's details on a card when hovered; spend skill points, follow evolutions, choose each active skill's trait out of combat (C20); drag active skills to the skill bar by their icons | P1 |
| **Skill bar** | Assign skills to keys; each key shows the skill's icon, its energy cost, and a sweep with the seconds left while it cools down (C16), and a small diamond when the skill uses its extra trait (C20) | P1 |
| **NPC dialog** | Talk to an NPC: promotions, quests to take and hand in, later shops | P1 |
| **Quest log** | See active quests and their progress; abandon one | P1 |
| **Arena window** | V: queue for 1v1, 3v3 or 15v15 and see each mode's rating, rank and record; the 1v1 and 3v3 leaderboards (I1, I4). In a match, the match bar at the top centre shows rounds or scores and the time | P1 |

## Chat

The chat stays out of the way of play until the player uses it.

- **Idle:** only the last three lines of the chosen tab show, low in the bottom-left corner. There are no tabs and no input.
- **Open:** Enter, or a click on the idle lines, opens it with the General, Party, and Combat tabs, the last eight lines, and the input. Esc or sending a line closes it again.
- **Pin:** the Pin button keeps it open while the player isn't typing. The choice is saved with the account's settings.
- **Newest line:** it always shows. A long line that wraps pushes the oldest line out of view.

## Controls

Keyboard only. Attacks and skills go to the locked mob, or in a PvP match the locked enemy (C15, I1): the nearest on screen is locked, a gold ring marks it, a target window at the top shows its name, level, and HP, and the lock key switches to the next closest. With nothing locked, they go the way the character faces. The mouse is used only for menus such as inventory and the marketplace.

Every key but Enter and Esc can be rebound in Settings → Key bindings (B6). The defaults put the right hand on the arrows and the left hand on the skills:

| Action | Default key |
| --- | --- |
| Walk up, down, left, and right on screen; two together walk diagonally | Arrow keys |
| Jump | Space |
| Switch between walking and sprinting out of combat (B11) | Left Shift |
| Enter a portal | Z, while standing in it |
| Attack | X |
| Lock the next closest mob | Tab |
| Inventory | I |
| Map | M |
| Zoom the camera in and out, still facing north (B12) | Mouse wheel; = and − |
| Arena window | V |
| Party | P |
| Character window | C |
| Talk to an NPC | Z, while in reach |
| Drink an HP potion | 1 |
| Skill window | K |
| Quest log | J |
| Open the chat; close it while typing | Enter; Esc (fixed) |
| Close the top window | Esc (fixed) |
| Use the skill on the skill bar's key | Q, W, E, R, A, S, D, F |

There is no pickup key: drops are collected automatically (D1).
