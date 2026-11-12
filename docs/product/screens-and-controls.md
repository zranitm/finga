# Screens and controls

The screens players see and the keys they use. This is the input for the design phase.

## Screens

| Screen | What the player does there | Priority |
| --- | --- | --- |
| **Sign up and log in** | Create an account, log in | P0 |
| **Character creation** | Pick a name; every character starts as a Beginner | P0 |
| **Game view and HUD** | Play: the map; HP, MP, and EXP bars and level at the top left; Back to characters and the party panel at the top right; the chat at the bottom left (see [Chat](#chat)); currency from P1 | P0 |
| **Inventory** | See items by tab; drink potions; discard them | P0 |
| **Party window** | Invite, accept, leave, kick, hand over leadership | P0 |
| **Marketplace** | Browse and search, buy, list an item, manage my listings | P1 |
| **Settings** | Choose a control preset; turn on spending stat points automatically at level-up (off by default) | P0 |
| **Character select** | Pick a character to play | P1 |
| **Channel select** | Pick or switch channel | P1 |
| **Character window** | See level, EXP, HP, MP, every stat, and the ten equipment slots; equip and unequip; spend, auto-assign, and reset stat points | P1 |
| **Item details** | Hover an item to see its stars, stats, and requirements, compared with what's worn | P1 |
| **NPC shop** | Buy and sell at fixed prices | P1 |
| **Skill window** | See every skill you can learn, spend skill points, follow evolutions; drag active skills to the skill bar | P1 |
| **Skill bar** | Assign skills to keys | P1 |
| **NPC dialog** | Talk to an NPC: promotions, quests to take and hand in, later shops | P1 |
| **Quest log** | See active quests and their progress; abandon one | P1 |

## The view

The game view shows about 30 by 17 tiles of the world, 960 by 540 px, whatever the window's size (C11).

- The world draws as big as the window allows while still showing that much. On a 1080p screen, that's twice the art's base size, and a character stands about a tenth of the screen's height.
- A window wider or taller than 16:9 shows more of the map in that direction. A bigger screen alone shows no more of it.
- Names, damage numbers, and NPC markers keep their size on screen. The HUD and windows don't scale with the view.

## Chat

The chat stays out of the way of play until the player uses it.

- **Idle:** only the last three lines of the chosen tab show, low in the bottom-left corner, below where a character stands. There are no tabs and no input.
- **Open:** Enter, or a click on the idle lines, opens it with the General, Party, and Combat tabs, the last eight lines, and the input. Esc or sending a line closes it again.
- **Pin:** the Pin button keeps it open while the player isn't typing. The choice is saved with the account's settings.
- **Newest line:** it always shows. A long line that wraps pushes the oldest line out of view.

## Controls

Keyboard only. Attacks go the way the character faces; the mouse is used only for menus such as inventory and the marketplace. Players choose one of two presets in Settings.

| Action | Classic | WASD |
| --- | --- | --- |
| Move left and right | Left and Right arrows | A and D |
| Climb ropes and ladders | Up and Down arrows | W and S |
| Jump | Space | Space |
| Drop through a platform | Down + Space | S + Space |
| Enter a portal | Up, while standing on it | W, while standing on it |
| Attack | X | J |
| Inventory | I | I |
| Party | P | P |
| Character window | C | C |
| Talk to an NPC | Z, while in reach | E, while in reach |
| Drink an HP potion | A | R |
| Drink an MP potion | S | F |
| Skill window | K | K |
| Quest log | Q | Q |
| Open the chat; close it while typing | Enter; Esc | Enter; Esc |
| Close the top window | Esc | Esc |
| Use the skill on the skill bar's key | 1 to 6 | 1 to 6 |

There is no pickup key: drops are collected automatically (D1). The old pickup keys now talk to NPCs (Z, Classic) and open the skill window (K) (proposal).

Remappable keys: later (P2).
