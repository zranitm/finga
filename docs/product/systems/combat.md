# Combat

## Basics

- Real-time combat against mobs in a 3D world, seen from above, looking north at a fixed angle. No player-versus-player combat in the MVP.
- **Target lock** (C15): the nearest living mob on screen, in any direction, is locked, and stays locked until it dies, leaves the screen, or the player presses Tab for the next closest. Attacks and skills aim at the lock, not the way the player faces; with nothing locked, they go the way the player faces. A lock out of reach is still aimed at, and the swing misses. A gold ring and outline mark it. An attack or skill turns the player to the lock for its strike only. A target window at the top of the screen shows the locked mob's name, level, and HP.
- A hit lands on what the attack's shape covers: an arc in front for melee, a lane for a shot, a circle around the player for an area skill (Classes and skills).
- Attack style and range depend on class: a Beginner's short swing, close melee, fast short-range strikes, long-range shots, or ranged magic (Classes and skills).
- **Every attack is physical or magic.**
  - Basic attacks: the Beginner's, Warrior's, Thief's, and Bowman's are physical; the Magician's is magic.
  - Each skill names its own type.
  - Each mob type names the type of damage its attacks deal.
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

  - Mobs' accuracy grows with their level. On-level, a mob lands about 93% of its attacks on a Warrior or a Magician, about 87% on a Bowman, and about two thirds on a Thief built on AGI (proposal).
  - **A mob's attack that misses** shows MISS and does nothing else: no knockback and no hurt blink, so the player keeps their place. The player is still safe from mobs' attacks for 0.5 seconds, as after a hit.
  - **A dodge** (C17): an escape skill gives a short dodge from its start. A mob's attack landing in it passes through and shows DODGE where MISS would. It does no damage, gives no knockback, and starts no 0.5-second guard, so the next attack can be dodged too.
- **Damage:**
  - A hit's damage comes from the attacker's physical or magic attack against the target's physical or magic defense, whichever matches the attack.
  - Penetration ignores part of that defense.
  - There is a small random variation, and a crit adds crit damage.
  - The exact formula is still to define.
- Damage numbers appear above the target. Every player sees the same results, and damage can't be faked.

## Actions

A basic attack or an active skill is an **action**. A player does one action at a time, and can cut one short with the next (C9).

- **Length:** each action has a length. Its hit lands as it starts, and the rest is recovery. The next action starts when it ends.
  - A basic attack's length is the class's attack time, such as a Warrior's 900 ms (Pace). Nothing shortens it (C14).
  - Each skill has its own length, set in game data.
  - Game data can also give an action a wind-up before its hit, for later skills such as a charged spell. Today's actions have none.
- **One at a time:** a key pressed during an action waits for it to end, then goes, unless it cancels. A skill still needs its cooldown to be over and enough energy (Energy).
- **Cancel point:** each action has a point in its recovery from which the other kind of action may start at once and cut off the rest.
  - The point is set per action in game data (proposal): 30% of its length, just after its strike, for basic attacks and cancelable skills alike.
  - Timing counts in the server's 50 ms ticks, so a cancel saves whole ticks.
- **What may cancel:**
  - Any skill may cut off a basic attack.
  - A basic attack may cut off a skill marked cancelable in game data. Other skills run to their end.
  - A basic attack doesn't cut off a basic attack, and a skill doesn't cut off a skill.
- **No timing needed:** a press at any time during an action it may cut off goes at the action's cancel point, or at once if the point has passed.
  - A skill pressed during a basic attack goes just after the attack's strike.
  - A basic attack pressed during a cancelable skill goes just after the skill's strike, cutting it short.
- **Holding the attack key** attacks again as soon as an attack may start: at a cancelable skill's cancel point, as a press would, and otherwise at each action's end. So holding attack never cuts an attack short.
- **Chaining is how everyone fights:** since cancels need no timing, a chain isn't a bonus for timing. With skills, it deals about 25% more damage per second than one action at a time, which the balance check logs for playtests. Skill powers are set with chains in mind.
- **Moving during an action:** walking, jumping, and turning are never blocked by a basic attack or a skill, and don't cancel either (C11).
  - **Moving skills** (C17) are the exception: a dash, leap or blink moves the player for the skill, in place of the walk while it runs. Plain movement still has no dash.
  - Using a skill only means another skill waits for its end, after 1 second, since one action goes at a time. A basic attack can still cut a cancelable skill from just after its strike.
- **Potions aren't actions,** and can be used at any time (D4).
- **The client plays a cancel at once; the server checks its timing.** A cancel is just the next attack or skill sent early, so the check rides on the action the server already validates, with no extra messages. A cancel it doesn't allow waits for the action's end. This keeps a modified client from chaining faster than the rules allow.
- **What players see** (C11):
  - Each action plays in three beats timed to its length: the strike, the follow-through, and back to ready. A cancel cuts them off, and the next action's strike starts at once.
  - A basic attack is back to ready at 85% of its length. So a skill that cuts into an attack visibly cuts its follow-through, and holding attack reads as one swing after another.
  - A skill's animation plays slowly enough to fill its whole second, so it shows when another skill can go. An attack that cuts the skill visibly cuts its follow-through.
  - The strike is bold enough to read from the camera: the character's model plays its action's animation, in the drawing only.
  - Melee basic attacks leave a bright trail behind the weapon, and ranged ones fire an arrow or an orb. Using a skill flashes the character in the skill's colour, and its shape draws strongly through its strike and fades out as the pose returns to ready.
  - **No cue marks the cancel point,** and none is needed, since a press at any time during an action goes at it.
  - Every player sees everyone's attacks and skills animate.
  - The beats are timed on each model's own animations, every mob's too.
- **Combat effects** (C13):
  - **Hit-stop:** the player's own landed hit freezes their pose and the mob for about 0.05 seconds, in the drawing only.
  - **Impact sparks:** white for physical hits, violet for magic, bigger for a crit, and a grey puff for a MISS.
  - **Damage numbers:** they pop with a bounce, and quick hits stack.
  - **Looks:** every class's basic attack and every skill looks like itself, and each evolution looks stronger.
  - **The cancel hint:** after the player's own cancel, an afterimage of the pose they cut off drifts back from behind the new one and fades. Nothing marks a cancel before it happens.
  - **No screen shake and no combo counter.**
  - **Art and sounds:** every effect has a name, so they can be added later without code changes.

## Pace

The game moves at a deliberate pace, so a fight leaves room for timing and a cancel can be felt (proposal, all in game data):

- **Basic attacks:**

  | Class | Length | A skill cuts in at |
  | --- | --- | --- |
  | Beginner | 1050 ms | 300 ms |
  | Warrior | 900 ms | 250 ms |
  | Thief | 650 ms | 150 ms |
  | Bowman | 1050 ms | 300 ms |
  | Magician | 1250 ms | 350 ms |

  A skill cuts in at the attack's cancel point, 30% of its length rounded down to the server's 50 ms ticks. One pressed earlier in the attack goes then (Actions).
- **Skills** take 1 second each, and a basic attack can cut a cancelable one from 0.45 seconds, just after its strike. Damage skills come back in about 3.5 to 7 seconds, Heal in 4.5, and buffs in 40 to 60, each set per skill. The damage skills' cooldowns are long enough that basic attacks keep a place between skills for every class.
- **Movement:** walking at 5 metres a second, in towns too and while using a skill. A jump rises about 1.9 metres: over any mob but a boss, and onto the low rocks and ledges the maps have.
- **Mobs** walk slower than players. Their attacks wind up for 0.6 seconds, a charge for 0.8, and come back after 2 to 3. A field boss's slam winds up for a second.
- **Fights** take the hits their region's balance targets set, so they last as long as the pace makes them. EXP per kill and drop chances are set so the level cap still takes about 10 hours (Progression).

## Mobs

- Each mob type has: a level, HP, an attack and the type of damage it deals (physical or magic), physical and magic defense, accuracy, evasion, an EXP reward, a drop list, a movement pattern, and its attacks.
- Some mobs deal magic damage: the cave bat, and one of the two mobs on every map from the woods on. Magic defense matters from the first maps.
- Mobs appear at set spots up to a maximum per map and wander a circle round where they appear.
- **Mobs block players** (C12):
  - A player walking into a mob from the side stops at its edge.
  - Nobody stands on a mob: landing on one, a player drops through it to the ground.
  - So a jump, which rises about 1.9 metres, gets a player past a mob whose top their feet are above as they reach it. That's one mob at a time: any normal mob or elite, but no boss.
  - Neither a player nor a mob ever moves further into the other, and both can always move apart. A player wholly inside a mob walks out either way, and one only partly in can back out or jump but not push through.
  - A mob that walks into a player stops there, and doesn't push. One that ends up inside a player, landing a hop say, steps out to their side.
  - Mobs going after one player spread round them, each to its own place, and those with no room wait a step further out. An idle mob makes way for a busy one, and two walking into each other drift apart, so a pile following a player keeps attacking.
  - A charge stops where it meets another mob. Players pass through players.
- **Touching a mob does no harm.** Mobs hurt players with their attacks:
  - A mob that sees a player within about 9 metres in any direction, with their feet from 1 metre below its own to 3 metres above, walks over and attacks when they're in reach. A ranged mob stops at its range, where no pillar or rise stands between them; otherwise it walks round.
  - A mob that's hit goes after its attacker for about 5 seconds, whoever else is near, even out of its sight.
  - A mob going after a player walks round whatever is in the way. It gives up once the player is more than 16 metres past the edge of its area, out of its sight for a second, or after about 3 seconds of getting nowhere, then walks back and wanders there.
  - A mob never steps into a portal, a boss included, and a hop or a charge stops at its edge, so a player it chases can always reach a portal and leave.
  - On the maps designed in 3D, no mob sees a player come in or stand at an NPC: every mob's home, a boss's included, lies out of sight of the map's start, its portals and its NPCs, so a fight is one the player walks into.
  - A mob still reaches a player standing in a portal from its edge. A portal is a way out, not a safe spot to fight from.
  - A mob that can't get any closer to its target, with no way on or with others in its way, attacks whoever it can reach.
  - Each time a player comes into a mob's reach, with nobody in it before, the mob waits a random moment, up to about 0.4 seconds, before it attacks. So mobs that reach a player together don't attack as one. Waiting out a cooldown, or turning to another player in reach, adds no wait.
  - Each attack winds up for 0.6 seconds, a charge for 0.8, then hits its area, then cools down. Through the wind-up the area shows red on the ground and fills, full as the hit lands. Stepping out of it during the wind-up dodges it, and evasion still rolls for a player in it.
  - A hit knocks the player back half a metre. For 0.5 seconds after any mob's hit or MISS, no mob's attack hits them: long enough to land from the knockback's hop, about a third of a second, before the next blow, and short enough that a crowd is dangerous.
- **Each mob family has its attack** (proposal, to tune):

  | Family | Attack |
  | --- | --- |
  | Slimes | **Hop:** a leap of up to 2¼ metres toward the player, hitting a circle where it lands |
  | Snails | **Charge:** a 4-metre rush along a lane as wide as the snail, hitting the first player it meets |
  | Mice | **Bite:** a lunge at a 90° fan in front, reaching ¾ metre past the mouse |
  | Flyers | **Spit:** a ball flying 7½ metres in a line at about 9 metres a second, hitting the first player it reaches |

- An elite's bite, hop, and charge reach 1.5 times as far, and a field boss's about twice. Their spits fly as far, with a bigger ball.

- An attack hits as hard as a touch did: the mob's attack, with its damage type.
- **Hits stagger mobs** (proposal, to tune):
  - A hit that lands makes a normal mob flinch for 0.3 seconds and pushes it back a little, breaking off any wind-up. The broken-off attack waits for its cooldown. A leap or charge already on its way finishes.
  - 3 hits within 1.5 seconds knock it down for a second, stopping even a leap or charge on its way. It still takes damage while down, then gets up with a second of super armor.
  - Elites and field bosses always have super armor: no flinch, no push, no knockdown.
- **Until mobs can climb,** a Bowman's or Magician's shot, or an area skill, reaches mobs from raised ground they can't climb, such as the forest's mesa: shots and areas don't check height, so mobs below that can't reach up can't answer. Melee arcs check height.
- **Later**, in this order: attack combos; mobs jumping onto raised ground; launchers and air juggles; players knocked down by heavy hits. Plain movement has no dash; only skills dash (C17).
- Defeated mobs come back after a respawn delay.
- Number of mob types in the MVP: see World → MVP world.

## Field bosses

Each hunting map has a field boss: a much bigger, stronger, and tougher version of its map's mob, with its own name and a look a glance tells from its mob's (C10). Towns have none.

- **Appearing:**
  - Each map counts the mobs killed on it, by anyone, and the boss appears at its set spot when the count reaches the map's number (proposal: 300; set per map in game data).
  - The count is hidden from players.
  - When a boss appears, every player in the game gets a notice, and a line in the General tab: "King Slime has appeared in the Forest!"
  - Each channel counts and spawns its own, once there are several (B4).
- **One at a time:** kills don't count while a boss is up, and its own kill doesn't count. After it's beaten or leaves, the count starts again from 0.
- **Leaving:** a boss nobody beats leaves 10 minutes after it appears, drops nothing, and players on the map are told.
- **Strength** (proposal, to tune):
  - Size: about 3 times its map's mob, its model scaled with a tint until it has its own art.
  - Level: the map's top mob level + 2.
  - Attack: about twice its map's mob's, with the same damage type, and higher defenses.
  - Attacks: its family's attack, bigger, and a slam that hits all around it after a one-second wind-up, taking turns. Their cooldowns are set so a lone melee player who never dodges needs at most 30 of their band's potions for the fight.
  - HP: enough that one on-level player beats it alone in about 3 minutes; a group does it faster.
- **Fighting:** it blocks and attacks as other mobs do (Mobs), with its two attacks. More special attacks come later.
- **The boss HP bar:** while it's up, everyone on the map sees a bar at the top of the screen with its name, its HP, and the time it has left.
- **Rewards** go to the players alive on the map when it falls. Anyone dead or off the map then gets nothing, whatever they dealt.
  - **EXP:** the boss's EXP is split by each player's share of the damage dealt to it. A party's damage adds up, and its share is split among its members as for any kill (Social → Party).
  - **Drops:** each player whose own damage, or whose party's, reached 1% of the boss's HP gets their own roll on its drop list. Drops are theirs alone, so party turns don't apply (Items → Drops).
  - The drop list holds its map's band gear at better odds than its mobs give (proposal, to tune).

## HP

- HP drops when hit. At zero, the character dies.
- HP regenerates over time, at the character's HP regen (Progression). Potions restore it instantly (P1).

## Energy

Energy is what skills cost (C14). It replaces MP, so a fight is a rhythm of attacks feeding skills rather than a pool refilled with potions.

- Every character has energy from 0 to 10. Nothing raises the 10.
- **Gaining it:** each basic attack that reaches a mob gives 2 energy (proposal, in game data), hit or MISS, however many mobs it reaches. A swing that reaches nothing gives none, and skills give none.
- **Spending it:** each active skill costs 2 or 3, set per skill in game data. The cost doesn't grow with points or change when the skill evolves (Classes and skills).
  - 2: the quick single-target skills, Strike, Throw, Double Stab, and Power Shot, and the Magician's Spark and Fire Arrow.
  - 3: Slash Blast, Heal, and the buffs.
- A skill without enough energy is refused with a notice, and nothing is spent.
- **Keeping it:** energy stays between fights, across maps, and over logging out. It drops to 0 on death, and a new character starts at 0. Level-ups and promotions don't change it.
- **What players see:** 10 pips under the HP bar, filling as attacks land. Each skill on the skill bar shows its cost and dims when there isn't enough.
- The server counts every gain and cost. The player's own screen shows a spend the moment they press, and a gain as soon as the server counts it. A skill that needs the energy of the attack it cuts waits for that count, a moment at most, so it's never refused for energy the player has earned.

## Death and respawn

- A character who dies respawns in the town linked to their map, with half HP (proposal).
- Penalty: see Progression.

## Skills

- Active skills cost energy and have cooldowns, and follow the same hit and damage rules as attacks (Classes and skills).
- Using one is an action, like a basic attack (Actions).

## Later

- Bosses in party dungeons (P2).
