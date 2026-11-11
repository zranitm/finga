# Progression

## Characters

- Every character starts as a **Beginner** and is promoted later to a class: **Warrior**, **Thief**, **Bowman**, or **Magician**, with more to come (Classes and skills).
- A class decides how the character fights: its basic attack's shape and range, whether that attack deals physical or magic damage (Combat), and which skills it can learn.
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

Every character starts with the Beginner's stats: STR 7, DEX 7, INT 7, AGI 7, VIT 7 (proposal, to tune). A promotion doesn't change them.

## Spending points

Players spend their own points (C6).

- **Points:** each level-up gives 5 stat points, so 145 by the level cap. Points not spent are kept.
- **No restrictions:** points can go into any primary stat, with no limit per stat and no class restriction. A Thief may put everything into INT.
- **Staging:** points are staged in the character window and take effect only on Apply. Undo returns staged points. Applied points stay until a reset.
- **Auto-assign** spends points by a recommended split per class. It's a button in the character window, and a Settings option to do it at every level-up, off by default. It works only once a character has a class: a Beginner spends their own points. Recommended splits (proposal):

  | Class | Split |
  | --- | --- |
  | Warrior | STR 3, VIT 2 |
  | Bowman | STR 2, DEX 2, AGI 1 |
  | Thief | STR 2, AGI 2, DEX 1 |
  | Magician | INT 3, DEX 1, VIT 1 |

- **Resets**: a reset returns every spent point. Each character has one free reset. Once currency exists (F1), further resets cost currency, more at higher levels.
- **Skill points are separate** and belong to the skill system (Classes and skills, C5).

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

Rates have diminishing returns, with the old caps as the limits.

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
- Each level-up gives 5 stat points and 5 skill points, raises the class's base values (max HP and MP grow per level), and fully restores HP and MP (proposal).
- The EXP needed per level grows with each level: round(20 × level^1.7) from each level to the next. Target (proposal): about 10 hours of play to reach the level cap.
- Level cap in the MVP: 30. A hunting map's normal mob gives about a thirtieth of the EXP its level needs.

## Death

- Death penalty, outside towns only: lose 5% of the EXP the current level needs to reach the next, rounded down. A level-5 character, needing 309, loses 15. Nothing is lost at the level cap.
- EXP never drops below the start of the current level, so a character can never lose a level.

## Later

- Second promotions and more classes (Classes and skills).
