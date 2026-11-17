# C19. Abnormal statuses

- **Feature:** C19. Abnormal statuses
- **Group:** C. Combat and progression
- **Priority:** P1
- **Status:** Agreed

**Story:** As a player, I can stun, freeze, slow, poison, or weaken what I fight, and some mobs can do the same to me. Each status reads at a glance, and none can lock a fight forever. Fights have openers, finishers, and moments to escape.

## Acceptance

- **One set of statuses,** shared by skills and mobs:

  | Group | Status | Effect |
  | --- | --- | --- |
  | Hard control | Stun | Can't move or act |
  | | Freeze | As stun. The next hit ends it, and deals 50% more |
  | | Sleep | Can't move or act. Any damage wakes it |
  | | Knockdown | Down: can't move or act, then rises with brief armour, as today |
  | | Pin | Can't move, but still attacks what it reaches |
  | Soft control | Slow | Moves 40% slower |
  | | Silence | Can't use skills. Basic attacks still work |
  | | Blind | Half accuracy, so far more misses |
  | Damage over time | Poison | Steady damage once a second |
  | | Burn | Heavier damage twice a second, for a short time |
  | | Bleed | Physical damage once a second, doubled while the bleeding one moves |
  | Weakened | Armour Break | Defense 30% lower |
  | | Weaken | Attack 25% lower |
  | Aggro, on mobs only | Taunt | Attacks the taunter (C17) |
  | | Lose sight | Stops chasing for a moment (C17) |

- **Rules:**
  - **No stacking:** the same status again restarts its time.
    - Damage over time is the exception: each attacker's runs on its own.
    - Its damage counts as the attacker's, for kills, EXP, and a boss's damage share.
  - **Breaking free:** there's no immunity after a hard control. Each class has a break-free skill instead, with a 32 s cooldown and no energy cost:
    - Break Free (Beginner), Unyielding (Warrior), Vanish (Thief), Recoil (Bowman), and Arcane Release (Magician).
    - While the player is stunned, frozen, asleep, knocked down, or pinned, the key of their escape skill (Roll Back, Smoke Bomb, Disengage, or Blink) turns into it, glowing (C18).
    - Pressing it ends the control. The player then dodges and, by class, leaps back, blinks, shoves mobs away, or makes them lose sight.
    - It works only while held, through silence too, and comes with the class, with no points needed. Keeping the escape skill on the bar is what keeps it ready.
  - **Elites and bosses** shrug off hard control. Soft control, damage over time, and weakening last half as long on them.
- **Skills** inflict them as C17's table says, now in these terms:
  - Charge stuns, Frost Nova freezes, and Pinning Shot pins.
  - Ground Slam and Meteor knock down.
  - Taunt and Smoke Bomb work on aggro.
  - Poison Blade poisons.
  - A few skills gain one as they evolve: Fire Arrow's evolution burns, Double Stab's bleeds, Power Shot's blinds, and Slash Blast's breaks armour.
- **Mobs inflict a few on players:**
  - a boss's slam stuns for 1 s;
  - icy mobs' spit slows;
  - poisonous mobs' bite poisons;
  - the desert's charges bleed;
  - the keep's shades silence.
  - Each of these mobs' attacks says so in its look: the red area tinted by the status.
- **On a player:**
  - Hard control stops them moving, jumping, attacking, and using skills.
  - Silence stops their skills.
  - Slow slows their walking.
  - A player can't act while knocked down, and rises after 1 second.
- **Showing them:**
  - Each status has a mark on the unit: stars, ice, "Zzz", roots, a blue trail, a crossed-out mark, a dark haze, green, flames, red drips, a cracked shield, or a drooping sword.
  - It also has an icon with its time left, on the target window for the locked mob, on the player's own frame, and on the party's frames. Hard control comes first; six show, and the rest as "+N".
  - A party member's icons show only while they're on the player's map, since the game sends a player only what's around them.
- **Fairness:** the server applies every status and decides every effect. A player's own screen shows a status on them as soon as the server says so.

## Not in this feature

- Removing statuses with a skill or a potion (a cleanse).
- Statuses on other players: no player-versus-player.
- Escaping a hard control with a skill, as Blade & Soul's escapes do: a later chain condition.
- Airborne and juggles, which come with launchers.

## Rules

Systems → Combat.
