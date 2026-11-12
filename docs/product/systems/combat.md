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
  - **A dodged touch** shows MISS and does nothing else: no knockback and no hurt blink, so the player keeps their place. The mob still waits 1.5 seconds before its next touch, as it does after a hit.
- **Damage:**
  - A hit's damage comes from the attacker's physical or magic attack against the target's physical or magic defense, whichever matches the attack.
  - Penetration ignores part of that defense.
  - There is a small random variation, and a crit adds crit damage.
  - The exact formula is still to define.
- Damage numbers appear above the target. Every player sees the same results, and damage can't be faked.

## Actions

A basic attack or an active skill is an **action**. A player does one action at a time, and can cut one short with the next (C9).

- **Length:** each action has a length. Its hit lands as it starts, and the rest is recovery. The next action starts when it ends.
  - A basic attack's length is the class's attack time, such as a Warrior's 900 ms (Pace), shortened by attack speed (Progression).
  - Each skill has its own length, set in game data. Attack speed doesn't change it.
  - Game data can also give an action a wind-up before its hit, for later skills such as a charged spell. Today's actions have none.
- **One at a time:** a key pressed during an action waits for it to end, then goes, unless it cancels. A skill still needs its cooldown to be over.
- **Cancel point:** each action has a point in its recovery from which the other kind of action may start at once and cut off the rest.
  - The point is set per action in game data (proposal: at 85% of the action's length).
  - Timing counts in the server's 50 ms ticks, so a cancel saves whole ticks.
- **What may cancel:**
  - Any skill may cut off a basic attack.
  - A basic attack may cut off a skill marked cancelable in game data. Other skills run to their end.
  - A basic attack doesn't cut off a basic attack, and a skill doesn't cut off a skill.
- **Timing:** a press inside the window cancels. So does one up to 100 ms before the cancel point, which then goes at the cancel point.
  - The 100 ms margin is one setting; 0 makes cancels strict (proposal, to revisit after playing).
  - A press earlier than the margin waits for the action's end.
  - Holding the attack key keeps attacking at each action's end. Only a fresh press cancels.
- **Reward:** cancel points are tuned so that a well-timed chain of attacks and skills deals up to about 20% more damage per second than one action at a time. Skill powers are set with chains in mind.
- **Movement is free:** walking, jumping, and climbing are never blocked by an action, and don't cancel one.
- **Potions aren't actions,** and can be used at any time (D4).
- **The client plays a cancel at once; the server checks its timing.** A cancel is just the next attack or skill sent early, so the check rides on the action the server already validates, with no extra messages. A cancel it doesn't allow waits for the action's end. This keeps a modified client from chaining faster than the rules allow.
- **What players see:** an action's shape plays and stops short when it's cancelled. Characters have no attack animation frames yet; those come with an art pass.

## Pace

The game moves at a deliberate pace, so a fight leaves room for timing and a cancel can be felt (proposal, all in game data):

- **Basic attacks,** before attack speed:

  | Class | Length | Cancel window |
  | --- | --- | --- |
  | Beginner | 1050 ms | 200 ms |
  | Warrior | 900 ms | 150 ms |
  | Thief | 750 ms | 150 ms |
  | Bowman | 1125 ms | 200 ms |
  | Magician | 1275 ms | 200 ms |

  The window is the time from the cancel point to the end. A press up to 100 ms before it counts too (Actions).
- **Skills** take about a second each. Damage and heal skills come back in 2 to 5 seconds, and buffs in 40 to 60, each set per skill.
- **Movement:** walking at 160 px a second and climbing at 112, in towns too. Jumps reach every platform the maps have, and take a little longer in the air.
- **Mobs** walk slowly, and touch a player at most every 1.5 seconds.
- **Fights** take the hits their region's balance targets set, so they last as long as the pace makes them. EXP per kill and drop chances are set so the level cap still takes about 10 hours (Progression).

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
- Using one is an action, like a basic attack (Actions).

## Later

- Bosses in party dungeons (P2).
