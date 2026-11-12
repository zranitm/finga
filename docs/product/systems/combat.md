# Combat

## Basics

- Real-time, side-on combat against mobs. No player-versus-player combat in the MVP.
- Players attack in the direction they face. A hit lands if the target is within range.
- Attack style and range depend on class: a Beginner's short swing, close melee, fast short-range strikes, long-range shots, or ranged magic (Classes and skills).
- **Every attack is physical or magic.**
  - Basic attacks: the Beginner's, Warrior's, Thief's, and Bowman's are physical; the Magician's is magic.
  - Each skill names its own type.
  - Each mob type names the type of damage its touch deals.
- **Hitting:** the attacker's accuracy against the target's evasion decides whether an attack lands. This holds for every attack, physical or magic, players and mobs alike.
  - What counts is evasion as a share of the attacker's accuracy.
    - Evasion well under the attacker's accuracy barely matters.
    - Around and past it, evasion starts to tell.
    - Far past it, each extra point counts for less, so stacking evasion never makes anyone untouchable.
  - The chance is 94% ÷ (1 + 0.175 × (evasion ÷ accuracy)²), held between 20% and 99% (proposal):

    | Evasion against accuracy | Lands |
    | --- | --- |
    | None | 94% |
    | Half | 90% |
    | Equal | 80% |
    | One and a half times | 67% |
    | Twice | 55% |
    | Three times | 37% |

  - Mobs' accuracy grows with their level. On-level, a mob lands about 93% of its touches on a Warrior or a Magician, about 87% on a Bowman, and about two thirds on a Thief built on AGI (proposal).
  - **A dodged touch** shows MISS and does nothing else: no knockback and no hurt blink, so the player keeps their place. The mob still waits a second before its next touch, as it does after a hit.
- **Damage:**
  - A hit's damage comes from the attacker's physical or magic attack against the target's physical or magic defense, whichever matches the attack.
  - Penetration ignores part of that defense.
  - There is a small random variation, and a crit adds crit damage.
  - The exact formula is still to define.
- Damage numbers appear above the target. Every player sees the same results, and damage can't be faked.

## Mobs

- Each mob type has: a level, HP, an attack and the type of damage it deals (physical or magic), physical and magic defense, accuracy, evasion, an EXP reward, a drop list, and a movement pattern.
- Some mobs deal magic damage: the cave bat, and one of the two mobs on every map from the woods on. Magic defense matters from the first maps.
- Mobs appear at set spots up to a maximum per map, wander, and hurt players on contact.
- Defeated mobs come back after a respawn delay.
- Number of mob types in the MVP: see World → MVP world.

## HP and MP

- HP drops when hit. At zero, the character dies.
- HP and MP regenerate over time, at the character's HP and MP regen (Progression). Potions restore them instantly (P1).

## Death and respawn

- A character who dies respawns in the town linked to their map, with half HP (proposal).
- Penalty: see Progression.

## Skills

- Active skills cost MP and have cooldowns, and follow the same hit and damage rules as attacks (Classes and skills).

## Later

- Bosses in party dungeons (P2).
