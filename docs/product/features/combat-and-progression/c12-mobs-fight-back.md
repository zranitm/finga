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
  - A mob that walks into a player stops there. It doesn't push. If it ends up inside them, landing a hop say, it steps out to their side.
  - Mobs going after one player spread round them, each to its own place, and those with no room wait a step further out. An idle mob makes way for a busy one, and two walking into each other drift apart, so a pile following a player keeps attacking. A charge stops where it meets another mob. Players pass through players.
  - On the player's screen, a mob blocks them where they see it. They aren't pulled back.
- **The jump:** a running jump rises about 1.9 metres and lands after about 0.65 seconds.
  - It gets a player past one mob at a time, never a group: any normal mob or elite. The field bosses can't be jumped past.
  - Every portal, NPC, and spawn on a map can be reached by walking and jumping.
- **Touching a mob does no harm.** Mobs hurt players only with their attacks.
- **Mobs come after players:**
  - A mob that sees a player within about 9 metres in any direction, with their feet from 1 metre below its own to 3 metres above, walks over and attacks when they're in reach. A ranged mob stops at its range, where no pillar or rise stands between them; otherwise it walks round. One that can't get any closer, with no way on or with others in its way, attacks whoever it can reach.
  - A mob going after a player walks round whatever is in the way, past its own area. It gives up once the player is more than 16 metres past the edge of its area, out of its sight for a second, or after about 3 seconds of getting nowhere. Players walk faster than every mob, so walking away still gets them free.
  - A mob never steps into a portal, a boss included, so a portal is always a way out. A player chased past one, back to the wall, can walk into it and leave; in the 2D game, mobs on the portal pinned them in the strip by the wall, and nobody can jump a boss.
  - Its hop or charge stops at the portal's edge too. Mobs still reach a player standing in the portal from its edge, so a portal isn't a safe spot to fight from.
  - A mob a player hits goes after them for about 5 seconds, whoever else is near, even out of its sight.
  - Mobs that reach a player at the same moment don't attack as one. Each time a player comes into a mob's reach, with nobody in it before, the mob waits a random moment, up to about 0.4 seconds, before it attacks, so their attacks come one after another, and so do the next ones. Waiting out a cooldown, or turning to another player in reach, adds no wait.
  - A mob that sees nobody walks back to its area and wanders there, each at its own speed and with its own pauses, so mobs a player gathered into a pile drift apart again.
- **An attack** has a wind-up of 0.6 seconds, a charge's 0.8, then its hit, then a cooldown before the mob attacks again.
  - Through the wind-up, the area it will hit shows red on the ground and fills, full as the hit lands.
  - A player who steps out of the attack's area during the wind-up isn't hit. The area always includes the mob itself, so standing inside a mob is never safe.
  - In the area, evasion still decides whether it lands, as for every attack: a MISS does nothing.
  - A hit knocks the player back half a metre and makes them blink. For 0.5 seconds after any mob's hit or MISS, no mob's attack can hit them. The knockback's hop lasts about a third of a second, so they land, with a moment to jump, before the next blow. A crowd can land up to a hit every half second, so jumping over mobs is the way out. It was 1.5 seconds until 2026-11-16, when crowds were found too safe: 4 or 5 mobs landed no more often than one.
- **Hits stagger mobs:**
  - Each hit that lands on a normal mob makes it flinch: it stops for a moment and is pushed back a little. A wind-up it was in is broken off, and that attack waits for its cooldown. A leap or charge already on its way finishes.
  - A quick burst of hits, about 3 within 1.5 seconds, knocks a normal mob down for about a second. It can't move or attack while down, so a leap or charge on its way stops there, though it still takes damage. Then it gets up, unstoppable for a second.
  - Elites and field bosses have super armor: they never flinch, are never pushed, and are never knocked down, so their attacks must be dodged.
- **Each mob family has its attack:**

  | Family | Mobs | Attack |
  | --- | --- | --- |
  | Slimes | every slime | **Hop:** leaps up to 2¼ metres toward the player and hits a circle where it lands |
  | Snails | the mushroom, the snails, the scarab | **Charge:** draws back, then rushes 4 metres along a lane as wide as itself, hitting the first player it runs into |
  | Mice | the boar, the rats, the mouse, the weasel | **Bite:** a quick lunge at a 90° fan in front, reaching ¾ metre past its body |
  | Flyers | the bats, the moths, the vulture, the wisp, the icewing | **Spit:** a ball flying 7½ metres in a line, hitting the first player it reaches |

  An elite's bite, hop, and charge reach 1.5 times as far, and a field boss's about twice. Their spits fly as far, with a bigger ball.

- **Field bosses** use their family's attack, bigger, and a **slam** that hits all around them after a longer wind-up. They take turns between the two.
- **Damage:** an attack hurts as much as a touch did, by the mob's attack and damage type. A player who never dodges takes a little less over a fight than from touches, and dodging takes less still.
- **Fairness:** every player sees the same mobs, wind-ups, and hits. The server decides every block, hit, and dodge.

## Not in this feature

- Mobs that fly, climb, or jump onto raised ground. Until they do, a Bowman's or Magician's shot, or an area skill, reaches mobs from raised ground they can't climb, such as the forest's mesa: shots and areas don't check height, so mobs below that can't reach up can't answer. Melee arcs check height.
- Later, in this order: attack combos; mobs jumping onto raised ground to reach a player; launchers and air juggles; players knocked down by heavy hits.
- More than two attacks for a boss, or attacks that differ within a family.

## Rules

Systems → Combat → Mobs and Field bosses
