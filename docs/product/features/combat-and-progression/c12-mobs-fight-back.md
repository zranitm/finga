# C12. Mobs fight back

- **Feature:** C12. Mobs fight back
- **Group:** C. Combat and progression
- **Priority:** P1
- **Status:** Draft

**Story:** As a player, I can't walk through mobs, and they fight me with attacks I can see coming and step out of, instead of hurting me by touch.

## Acceptance

- **Mobs block players:**
  - Walking into a mob from the side stops the player at its edge, like a wall.
  - A player can't stand on a mob. Landing on one, they drop through it to the ground.
  - So a jump gets a player past a mob when their feet are above its top as they reach it: they pass over it, or land on it and drop through. Every normal and elite mob is low enough. Field bosses are taller: a well-timed jump gets past a slime boss, only the very top of a jump past a flying one, and nothing past the others.
  - A player inside a mob, for whatever reason, can walk out either way, so nothing traps them.
  - A mob that walks into a player stops there. It doesn't push.
  - Mobs pass through each other, and players through players, as today.
  - On the player's screen, a mob blocks them where they see it. They aren't pulled back.
- **Touching a mob does no harm.** Mobs hurt players only with their attacks.
- **When a mob attacks:**
  - A mob patrols as today until a player is in its reach. Then it stops, turns to face them, and attacks.
  - A mob a player hits turns to them and follows them along its own ground for about 5 seconds, attacking when they're in reach. It never leaves the ground it patrols.
- **An attack** has a wind-up the player can see, about 0.6 seconds, then its hit, then a cooldown before the mob attacks again.
  - A player who steps out of the attack's area during the wind-up isn't hit.
  - In the area, evasion still decides whether it lands, as for every attack: a MISS does nothing.
  - A hit knocks the player back and makes them blink. For 1.5 seconds after any mob's hit or MISS, no mob's attack can hit them, so a crowd can't chain hits.
- **Each mob family has its attack:**

  | Family | Mobs | Attack |
  | --- | --- | --- |
  | Slimes | every slime | **Hop:** leaps toward the player and hits the ground around where it lands |
  | Snails | the mushroom, the snails, the scarab | **Charge:** draws back, then rushes a short way forward, hitting the first player it runs into |
  | Mice | the boar, the rats, the mouse, the weasel | **Bite:** a quick lunge at what's right in front of it |
  | Flyers | the bats, the moths, the vulture, the wisp, the icewing | **Spit:** a slow shot forward in a line, hitting the first player it reaches |

- **Field bosses** use their family's attack, bigger, and a **slam** that hits all around them after a longer wind-up. They take turns between the two.
- **Damage:** an attack hurts as much as a touch did, by the mob's attack and damage type. A player who never dodges takes a little less over a fight than from touches, and dodging takes less still.
- **Fairness:** every player sees the same mobs, wind-ups, and hits. The server decides every block, hit, and dodge.

## Not in this feature

- Mobs that fly, climb, or cross between platforms.
- Marks on the ground showing where an attack will hit.
- More than two attacks for a boss, or attacks that differ within a family.

## Rules

Systems → Combat → Mobs and Field bosses
