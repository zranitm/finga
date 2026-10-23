# Combat

## Basics

- Real-time, side-on combat against mobs. No player-versus-player combat in the MVP.
- Players attack in the direction they face. A hit lands if the target is within range.
- Attack style and range depend on class: close melee, fast short-range strikes, long-range shots, or ranged magic (OD-15).
- Damage comes from the attacker's Attack against the target's Defense, with a small random variation. The exact formula is still to define.
- Damage numbers appear above the target. Every player sees the same results, and damage can't be faked.

## Mobs

- Each mob type has: a level, HP, Attack, Defense, an EXP reward, a drop list, and a movement pattern.
- Mobs appear at set spots up to a maximum per map, wander, and hurt players on contact.
- Defeated mobs come back after a respawn delay.
- Number of mob types in the MVP: OD-12.

## HP and MP

- HP drops when hit. At zero, the character dies.
- HP and MP regenerate slowly over time. Potions restore them instantly (P1).

## Death and respawn

- A character who dies respawns in the town linked to their map, with half HP (proposal).
- Penalty: see Progression (OD-06).

## Later

- Skills that cost MP and have cooldowns (P1).
- Bosses in party dungeons (P2).
