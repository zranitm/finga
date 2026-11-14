# C11. Combat feel

- **Feature:** C11. Combat feel
- **Group:** C. Combat and progression
- **Priority:** P1
- **Status:** Draft

**Story:** As a player, I see my character's attacks and skills clearly enough to time my cancels by eye. My skills feel heavy, and using one commits me to it.

The clearer animations come first, on their own, so they can be played before anything else changes.

## Acceptance

- **Animations that span the action,** clear at today's character size:
  - Each attack and skill plays in three beats timed to its length: the strike, the follow-through, and back to ready: at 85% of a basic attack, and at a skill's end, so a skill's animation fills it.
  - The strike is bold: the character leans into it and lunges forward a little, in the drawing only.
  - Melee basic attacks show a bright slash arc over their reach. Ranged ones keep their arrow or orb.
  - Using a skill flashes the character in the skill's colour. The skill's shape draws strongly through its strike and fades out as the pose returns to ready.
  - A cancel cuts the beats off, and the next action's strike starts at once.
- **No cue for the cancel point:** none is needed, since a press at any time during an action goes at it (C9).
- **Other players' actions:** their basic attacks animate too, ranged shots included, as their skills do today.
- **Skills take their time:**
  - Every active skill takes 1 second, and its animation plays slowly enough to fill it.
  - Damage skills' cooldowns grew 1.5× when skills took 1.5 seconds, so basic attacks keep their place between skills and every class still has attacks to cancel with.
- **Cutting a skill with an attack:**
  - A basic attack can cut a cancelable skill from 0.45 seconds, just after its strike, instead of near its end.
  - An attack pressed at any time during the skill goes then, and so does a held attack key, so no timing is needed.
  - An attack that cuts the skill cuts its animation short and goes straight into its strike.
- **Moving while using a skill:**
  - Players move freely while using a skill, as during a basic attack: walking, jumping, climbing, and turning.
  - Another skill waits until the current one ends, since one action goes at a time. A basic attack can still cut a cancelable one.
- **Fairness:** a skill plays at once on the player's own screen, and the server times every action by the same rules.

## Not in this feature

- Attack animation frames for characters, which come with an art pass. The beats are built from today's frames.
- A closer view (a camera zoom), on hold until the clearer animations have been played.
- A cue or a timing bar that shows when a cancel opens.
- New skill powers: the slower skills are tuned with cooldowns alone.

## Rules

Systems → Combat → Actions and Pace
