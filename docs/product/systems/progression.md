# Progression

## Characters

- Four classes at the start: **Warrior**, **Thief**, **Bowman**, **Magician**. A class is chosen at character creation and doesn't change.
- A class decides how the character fights: its basic attack's shape and range, and whether that attack deals physical or magic damage (Combat).
- A class doesn't decide which stats are worth raising: every stat does the same thing for every class.
- A character has a level, EXP, HP, MP, five primary stats, and the derived stats they give.

## Primary stats

Five, which players raise by spending points:

| Stat | Role |
| --- | --- |
| **STR** | Physical power |
| **DEX** | Precision: hitting, crits, and a little of both attacks |
| **INT** | Magic and skills |
| **AGI** | Speed and dodging |
| **VIT** | Toughness |

Starting stats at level 1, the same for every class:

| Class | STR | DEX | INT | AGI | VIT |
| --- | --- | --- | --- | --- | --- |
| Warrior | 7 | 7 | 7 | 7 | 7 |
| Bowman | 7 | 7 | 7 | 7 | 7 |

Thief and Magician start the same when they arrive. The class shows in how points are spent, not in where a character starts, so the later Beginner (who starts with 7 in each stat before choosing a class) needs no rebalancing.

## Spending points

OD-16 is decided: players spend their own points (C6).

- **Points:** each level-up gives 5 stat points, so 145 by the level cap. Points not spent are kept.
- **No restrictions:** points can go into any primary stat, with no limit per stat and no class restriction. A Thief may put everything into INT.
- **Staging:** points are staged in the character window and take effect only on Apply. Undo returns staged points. Applied points stay until a reset.
- **Auto-assign** spends points by a recommended split per class. It's a button in the character window, and a Settings option to do it at every level-up, off by default. Recommended splits (proposal):

  | Class | Split |
  | --- | --- |
  | Warrior | STR 3, VIT 2 |
  | Bowman | STR 2, DEX 2, AGI 1 |
  | Thief | STR 2, AGI 2, DEX 1 |
  | Magician | INT 3, DEX 1, VIT 1 |

- **Resets** (OD-17 decided): a reset returns every spent point. Each character has one free reset. Once currency exists (F1), further resets cost currency, more at higher levels.
- **Skill points are separate** and belong to the skill system (C5).

## Derived stats

Sixteen, computed from the primary stats, the class's base values (which grow with level), and equipment. The character window shows each one with equipment's share.

| Group | Stat | What it does | From each point | Limit |
| --- | --- | --- | --- | --- |
| Offense | Physical attack | Damage of physical attacks | STR +1.5, DEX +0.5 | none |
| | Magic attack | Damage of magic attacks | INT +1.5, DEX +0.5 | none |
| | Accuracy | Chance to hit with any attack, against the target's evasion | DEX +1 | hit chance 20–99% |
| | Crit rate | Chance of a critical hit, from 5% | DEX, diminishing | approaches 60% |
| | Crit damage | Extra damage on a crit, from +50% | DEX +0.5% | none |
| | Penetration | Ignores part of the target's physical and magic defense | equipment and skills only | approaches 50% |
| Defense | Max HP | | VIT +10, STR +2 | none |
| | Physical defense | Less physical damage taken | VIT +1 | none |
| | Magic defense | Less magic damage taken | VIT +0.5, INT +0.5 | none |
| | Evasion | Chance to dodge any attack, against the attacker's accuracy | AGI +1 | hit chance 20–99% |
| | HP regen | HP back over time | VIT +0.05 per second | none |
| Resources | Max MP | | INT +3 | none |
| | MP regen | MP back over time | INT +0.05 per second | none |
| Speed | Attack speed | Basic attacks come faster; skills don't | AGI, diminishing | approaches +50% |
| | Cooldown speed | Skills come back faster | INT, diminishing | approaches 40% |
| | Move speed | Faster walking, climbing, and jumping in combat maps. Jumps keep their height and length and just take less time. In towns everyone moves at the same speed. | AGI, diminishing | approaches +30% |

- Equipment can add to every primary and derived stat (Items). It adds on top of what points give, and a stat's limit is the ceiling for the total.
- Attack speed covers basic attacks only, so that no single stat is worth buying for every build: AGI sets the pace of basic attacks, and INT the pace of skills.

## Diminishing returns

OD-18 is decided: diminishing returns, with the old caps as the limits.

- **Rates give less per point the more a character has.** That covers crit rate, attack speed, cooldown speed, and move speed. A stat gives *limit × points ÷ (points + k)*, where *k* is the number of points that reaches half the limit. Each point always helps, but no rate reaches its limit from points alone.
- **k values** (proposal): crit rate 200 DEX, attack speed 150 AGI, cooldown speed 200 INT, move speed 200 AGI.
- **Flat stats stay linear:** attacks, defenses, HP, MP, accuracy, evasion, and regen.

What 100 points buy, with no equipment, on top of the class's base values:

| Into | Result |
| --- | --- |
| 100 STR | +150 physical attack, +200 max HP |
| 100 DEX | +100 accuracy, +50 physical and +50 magic attack, +18% crit rate, +50% crit damage |
| 100 INT | +150 magic attack, +300 max MP, +5 MP a second, +13% cooldown speed, +50 magic defense |
| 100 AGI | +100 evasion, +20% attack speed, +10% move speed |
| 100 VIT | +1,000 max HP, +100 physical defense, +50 magic defense, +5 HP a second |

## Leveling

- EXP comes from defeating mobs. In a party, EXP is shared (see Social).
- Reaching the EXP threshold raises the level by one. Extra EXP carries over to the next level.
- Each level-up gives 5 stat points, raises the class's base values (max HP and MP grow per level), and fully restores HP and MP (proposal).
- The EXP needed per level grows with each level. The exact curve is still to define; target (proposal): about 10 hours of play to reach the MVP level cap.
- Level cap in the MVP: OD-13 (default 30).

## Death

- Death penalty: OD-06 (default: lose a small percentage of current EXP, outside towns only).
- EXP never drops below the start of the current level, so a character can never lose a level.

## Later

- Skills: see Combat (P1), with their own skill points.
