# C11. Combat feel

- **Feature:** C11. Combat feel
- **Group:** C. Combat and progression
- **Priority:** P1
- **Status:** Draft

**Story:** As a player, I see my character's attacks and skills clearly enough to time my cancels by eye. My skills feel heavy, and using one commits me to it.

The clearer animations come first, on their own, so they can be played before anything else changes.

## Acceptance

- **Animations that span the action,** clear at today's character size:
  - Each attack and skill plays in three beats timed to its length: the strike, the follow-through, and back to ready: at 85% of a basic attack, Heal, or a buff, and at a cancelable skill's cancel point.
  - The strike is bold: the character leans into it and lunges forward a little, in the drawing only.
  - Melee basic attacks show a bright slash arc over their reach. Ranged ones keep their arrow or orb.
  - Using a skill flashes the character in the skill's colour. The skill's shape draws strongly through its strike and fades out by its cancel point.
  - A cancel cuts the beats off, and the next action's strike starts at once.
- **No cue for the cancel point:** none is needed, since a press at any time during an action goes at it (C9). The follow-through easing back into ready still shows where it is.
- **Other players' actions:** their basic attacks animate too, ranged shots included, as their skills do today.
- **Slower skills:**
  - Every active skill takes 1.5 seconds.
  - Damage skills' cooldowns grow 1.5× with it, so basic attacks keep their place between skills and every class still has attacks to cancel with.
- **Getting free of a skill with an attack:**
  - A basic attack can cut a cancelable skill from 0.45 seconds, just after its strike, instead of near its end.
  - An attack pressed at any time during the skill goes then, and so does a held attack key, so no timing is needed. Attacking is how a player gets free of a skill's slow.
  - A skill that isn't cut slows the player for its full 1.5 seconds.
  - From the skill's cancel point until it ends, the character holds a recovery lean and walks slowly. An attack that cuts the skill drops both at once and goes straight into its strike, at full speed.
  - It changes damage little, since the next skill still waits for its cooldown. Mostly it gives the player their movement back.
- **Moving while using a skill:**
  - While using any active skill, a player walks and climbs at half speed and can't turn around.
  - Pressing the other way walks them backward, still facing forward. Jumps keep their height.
  - This lasts until the skill ends, or until a basic attack cuts it off.
  - Basic attacks don't slow the player.
- **Fairness:** the slow plays at once on the player's own screen, and the server moves every player by the same rules.

## Not in this feature

- Attack animation frames for characters, which come with an art pass. The beats are built from today's frames.
- A closer view (a camera zoom), on hold until the clearer animations have been played.
- A cue or a timing bar that shows when a cancel opens.
- New skill powers: the slower skills are tuned with cooldowns alone.

## Rules

Systems → Combat → Actions and Pace
