# C9. Animation cancel

- **Feature:** C9. Animation cancel
- **Group:** C. Combat and progression
- **Priority:** P1
- **Status:** Draft

**Story:** As a player, I cut an attack or a skill short with my next move, so a well-timed chain deals more damage.

## Acceptance

- **One action at a time:** a basic attack or an active skill is an action. Each has a length, and the next action starts when it ends.
- **Cancels:**
  - From an action's cancel point to its end, the other kind of action may start at once and cut it off. A skill cuts off a basic attack, and a basic attack cuts off a skill.
  - Every skill can cut off a basic attack. Only the skills game data marks as cancelable can be cut off by one; the others run to their end.
  - A basic attack can't cut off a basic attack, and a skill can't cut off a skill.
- **Timing:**
  - A skill pressed inside an attack's window cancels it. So does one up to 100 ms before the cancel point (proposal, to revisit after playing). One pressed earlier waits for the attack to end.
  - A basic attack pressed at any time during a cancelable skill goes at the skill's cancel point, just after its strike. Holding the attack key does the same (C11).
  - After an attack, holding the attack key keeps attacking at each action's end.
- **The skill window** shows each active skill's length, and whether a basic attack can cut it short.
- **Pace:** attacks last about a second, so a skill's window to cut one can be felt: 150 to 200 ms, plus the early margin. Skills last 1.5 seconds, and an attack can cut one from 0.45 seconds (Systems → Combat → Pace).
- **Reward:** a well-timed chain of attacks and skills deals up to about 20% more damage per second than one action at a time.
- **Movement stays free** during basic attacks. While using a skill, the player is slowed and can't turn (C11). Potions aren't actions and can be used at any time.
- **What players see:** an action's beats and shape play and stop short when it's cancelled (C11).
- **Fairness:** a cancel plays at once on the player's screen. The server checks its timing on the attack or skill it already receives, so a modified client can't chain faster. Every player sees the same results.

## Not in this feature

- Attack animation frames for characters, which come with an art pass.
- A skill cutting off another skill.

## Rules

Systems → Combat → Actions
