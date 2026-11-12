# C11. Combat feel

- **Feature:** C11. Combat feel
- **Group:** C. Combat and progression
- **Priority:** P1
- **Status:** Draft

**Story:** As a player, I see my character's attacks and skills clearly enough to time my cancels by eye. My skills feel heavy, and using one commits me to it.

## Acceptance

- **A closer view:**
  - The game view shows about 30 by 17 tiles of the world (960 by 540 px) at any window size. Characters, mobs, and maps all draw bigger.
  - On a 1080p screen, that's twice today's size, and a character stands about a tenth of the screen's height.
  - A window wider or taller than that shape shows more of the map in that direction only. A bigger screen alone doesn't show more of the map.
  - Names, damage numbers, and NPC markers keep their size on screen and stay sharp. The HUD and windows don't change.
  - Hitboxes, reach, jumps, and maps stay as they are. Only the drawing changes.
- **Animations that span the action:**
  - Each attack and skill plays in three beats timed to its length: the strike, the follow-through, and back to ready exactly at its cancel point.
  - Melee basic attacks show a slash arc in front of the character. Ranged ones keep their arrow or orb.
  - A skill's shape stays up through its strike and fades out by its cancel point.
  - A cancel cuts the beats off, and the next action's strike starts at once.
- **The cancel cue:**
  - A short glint on the player's own character marks the moment a press of the other kind starts to cancel: 100 ms before the cancel point, with the early press.
  - It shows only when the other kind could cut the action off: after every basic attack, and after cancelable skills.
  - Only the player sees their own glint.
- **Other players' actions:** their basic attacks animate too, ranged shots included, as their skills do today.
- **Slower skills:**
  - Every active skill takes 1.5 seconds.
  - Its cancel window grows to 250 ms, plus the 100 ms early press.
  - Damage skills' cooldowns grow 1.5× with it, so basic attacks keep their place between skills and every class still has attacks to cancel with.
- **Moving while using a skill:**
  - While using any active skill, a player walks and climbs at half speed and can't turn around.
  - Pressing the other way walks them backward, still facing forward. Jumps keep their height.
  - This lasts until the skill ends, or until a basic attack cuts it off.
  - Basic attacks don't slow the player.
- **Fairness:** the slow plays at once on the player's own screen, and the server moves every player by the same rules.

## Not in this feature

- Attack animation frames for characters, which come with an art pass. The beats are built from today's frames.
- A zoom setting, or a timing bar under the character.
- New skill powers: the slower skills are tuned with cooldowns alone.

## Rules

Systems → Combat → Actions and Pace; Screens and controls → The view
