# C9. Animation cancel

- **Feature:** C9. Animation cancel
- **Group:** C. Combat and progression
- **Priority:** P1
- **Status:** Draft

**Story:** As a player, I cut an attack or a skill short with my next move, so attacks and skills flow into one chain.

## Acceptance

- **One action at a time:** a basic attack or an active skill is an action. Each has a length, and the next action starts when it ends.
- **Cancels:**
  - From an action's cancel point to its end, the other kind of action may start at once and cut it off. A skill cuts off a basic attack, and a basic attack cuts off a skill.
  - Every skill can cut off a basic attack. Only the skills game data marks as cancelable can be cut off by one; the others run to their end.
  - A basic attack can't cut off a basic attack, and a skill can't cut off a skill.
- **No timing needed:** the next move pressed at any time during an action goes at the action's cancel point, or at once if the point has passed, when it may cut the action off.
  - A skill pressed at any time during a basic attack goes just after the attack's strike.
  - A basic attack pressed at any time during a cancelable skill goes just after the skill's strike. Holding the attack key does the same (C11).
  - After an attack, holding the attack key keeps attacking at each action's end.
- **The skill window** shows each active skill's length, and whether a basic attack can cut it short.
- **Pace:** attacks last about a second, and a skill can cut one from 0.15 to 0.35 seconds in, depending on the class. Skills last 1 second, and an attack can cut one from 0.45 seconds (Systems → Combat → Pace).
- **Chaining is how everyone fights,** not a bonus for timing. With skills, a chain deals about 25% more damage per second than one action at a time. This is logged for playtests, not capped.
- **Movement stays free** during basic attacks. While using a skill, the player can't move or turn (C11). Potions aren't actions and can be used at any time.
- **What players see:** an action's beats and shape play and stop short when it's cancelled (C11).
- **Fairness:** a cancel plays at once on the player's screen. A skill that needs the energy of the attack it cuts waits a moment for the server to count it, and is never refused for energy the player has earned (C14). The server checks its timing on the attack or skill it already receives, so a modified client can't chain faster. Every player sees the same results.

## Not in this feature

- Attack animation frames for characters, which come with an art pass.
- A skill cutting off another skill.

## Rules

Systems → Combat → Actions
