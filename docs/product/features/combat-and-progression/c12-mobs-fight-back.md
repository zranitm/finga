# C12. Mobs fight back

- **Feature:** C12. Mobs fight back
- **Group:** C. Combat and progression
- **Priority:** P1
- **Status:** Draft

**Story:** As a player, I can't walk through mobs. They come after me and fight with attacks I can see coming and step out of, instead of hurting me by touch, and my hits stagger and knock them down. The fights feel like Elsword more than MapleStory.

## Acceptance

- **Mobs block players:**
  - Walking into a mob from the side stops the player at its edge, like a wall.
  - A player can't stand on a mob. Landing on one, they drop through it to the ground.
  - So a jump gets a player past a mob when their feet are above its top as they reach it: they pass over it, or come down inside it and walk out the far side.
  - Neither a player nor a mob ever moves further into the other, and both can always move apart.
    - A player wholly inside a mob, after coming down on it or a knockback, walks out either way, so nothing traps them.
    - One only partly in can back out or jump, but can't push through.
  - A mob that walks into a player stops there. It doesn't push. With lag, it may end a few pixels into them.
  - Mobs pass through each other, and players through players, as today.
  - On the player's screen, a mob blocks them where they see it. They aren't pulled back.
- **A lower jump:** a running jump rises about 61 px, down from about 100, and lands after 0.63 seconds.
  - It gets a player past one mob at a time, never a group: any normal mob, and the 42 and 48 px elites about as easily. The 60 px elites and the field bosses can't be jumped past.
  - The maps are reworked so the jump alone still reaches every platform or block with no rope up to it, at most 48 px up, and every portal and spawn can still be reached.
- **Touching a mob does no harm.** Mobs hurt players only with their attacks.
- **Mobs come after players:**
  - A mob that sees a player within about 300 px, level with it or up to about 96 px above, walks over and attacks when they're in reach. A ranged mob stops at its range. One that can't get any closer, at the end of its ground or with a player in its way, attacks whoever it can reach.
  - A mob going after a player follows them anywhere along the ground it stands on, not only its patrol area, until a wall, a block, a portal, or the ground's edge stops it. Otherwise a player standing just past a patrol area's end could hit a mob that couldn't reach back. Players walk faster than every mob, so walking away still gets them free.
  - A mob never steps onto a portal, a boss included, so a portal is always a way out. A player chased past one, back to the wall, can walk onto it and leave; before this, mobs on the portal pinned them in the strip by the wall, and nobody can jump a boss.
  - Its hop or charge still reaches across a portal, as far as it would go anyway, so a player standing on one isn't out of reach. The mob itself stops at the portal's edge.
  - A mob a player hits goes after them for about 5 seconds, whoever else is near.
  - Mobs that reach a player at the same moment don't attack as one. Each time a player comes into a mob's reach, with nobody in it before, the mob waits a random moment, up to about 0.4 seconds, before it attacks, so their attacks come one after another, and so do the next ones. Waiting out a cooldown, or turning to another player in reach, adds no wait.
  - A mob that sees nobody walks back to its patrol area and patrols as today, each at its own speed and with its own pauses, so mobs a player gathered into a pile drift apart again. It never leaves the ground it stands on.
- **An attack** has a wind-up the player can see, about 0.6 seconds, then its hit, then a cooldown before the mob attacks again.
  - A player who steps out of the attack's area during the wind-up isn't hit. The area always includes the mob itself, so standing inside a mob is never safe.
  - In the area, evasion still decides whether it lands, as for every attack: a MISS does nothing.
  - A hit knocks the player back and makes them blink. For 1.5 seconds after any mob's hit or MISS, no mob's attack can hit them, so a crowd can't chain hits.
- **Hits stagger mobs:**
  - Each hit that lands on a normal mob makes it flinch: it stops for a moment and is pushed back a little. A wind-up it was in is broken off, and that attack waits for its cooldown. A leap or charge already on its way finishes.
  - A quick burst of hits, about 3 within 1.5 seconds, knocks a normal mob down for about a second. It can't move or attack while down, so a leap or charge on its way stops there, though it still takes damage. Then it gets up, unstoppable for a second.
  - Elites and field bosses have super armor: they never flinch, are never pushed, and are never knocked down, so their attacks must be dodged.
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

- Mobs that fly, climb, or cross between platforms. Until they do, a player on a platform or a block above a mob can hit it from where it can't reach.
- Later, in this order: attack combos; mobs crossing platforms to reach a player; launchers and air juggles; players knocked down by heavy hits.
- Marks on the ground showing where an attack will hit.
- More than two attacks for a boss, or attacks that differ within a family.

## Rules

Systems → Combat → Mobs and Field bosses
