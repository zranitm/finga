# C21. Blocking

- **Feature:** C21. Blocking
- **Group:** C. Combat and progression
- **Priority:** P1
- **Status:** Agreed

**Story:** As a player, I read a mob's wind-up from its body and raise my guard in time. A block takes the blow, feeds my energy, and turns my key into a counter. Only field bosses mark the ground: yellow for an attack I can block, red for one I can only dodge or step out of.

## Acceptance

- **Warning areas:**
  - **Normal mobs and elites show none.** Their wind-up shows only in their body, as it does today: the crouch and the attack's motion. A spit's ball still flies, and a hop's or a charge's impact still shows where it lands.
  - **Field bosses' attacks show an area** on the ground that fills over the wind-up, full as the hit lands:
    - **yellow:** it can be blocked;
    - **red:** it can't be blocked; only a dodge, or stepping out of it, escapes it.
  - The first bosses' attacks: their family's attack is yellow, and their slam is red.
  - **The areas are light,** as in Blade & Soul: a pale coral or lemon glow that lightens the ground under it, a stronger fill growing over it, and a bright rim in its colour, so red and yellow read apart on every ground.
  - A boss attack that inflicts a status still edges its area in the status's colour, just outside the rim (C19).
  - **Every normal and elite attack can be blocked.** Only a boss's attack can be unblockable, since only a boss's attacks are marked.
- **Block skills:** each class has one, which comes with the class and takes no points, as break-free skills do (C19):

  | Class | Skill | From level | Guards for | Cooldown | Also | Counter |
  | --- | --- | --- | --- | --- | --- | --- |
  | Beginner | Guard | 1 | 0.6 s | 6 s | | Strike |
  | Warrior | Bastion | 10 | 0.6 s | 2 s | | Ground Slam |
  | Thief | Parry | 10 | 0.4 s | 4 s | | Flurry |
  | Bowman | Deflect | 10 | 0.6 s | 6 s | then leaps 2 m back | Power Shot |
  | Magician | Ward | 10 | 0.8 s | 7 s | | Frost Nova |

  - Each class's block has its own feel: the Warrior's comes back in 2 seconds so it can guard almost every blow, the Thief's is a short parry, the Bowman's a guard, then a spring back.
  - A promoted character has their class's block in place of Guard. A key holding Guard takes the class's block at the promotion.
- **Blocking:**
  - The key raises the player's guard for the skill's time, toward the locked mob or, with none, the way the player faces as they press it.
  - It covers the front half, 180°. The guard doesn't turn as the player moves; moving stays free, as with every skill.
  - **A blockable attack from the front** landing while the guard is up is blocked: no damage, no knockback, no status. BLOCK shows where MISS would. It starts no 0.5-second guard, so the next attack can be blocked too.
  - **An attack from behind, or a red one,** lands as usual. A dodge still escapes both.
  - **No energy cost.** The first attack each use blocks gives 2 energy, as a basic attack that lands does.
  - A block is an action: other skills wait for its end, and a basic attack can't cut it short. A stun or another hard status ends it, and Deflect's waiting leap with it.
- **Counters:** for 1.5 seconds after a block, the block's key turns into the class's counter, glowing, as Roll Back's turns into Lunge after a dodge (C18). The counter needs a point, as any follow-up does.
- **Fairness:** the server decides every block: the time, the side, and whether the attack can be blocked. It grants the energy and checks the counter's condition.

## Not in this feature

- Blocking other players' attacks outside PvP matches. In a match a block stops an enemy's attacks and skills from the front, and none of them is unblockable (I1).
- A bonus for blocking at the last moment.
- Blocks that turn a shot back, or stagger an elite or a boss.
- Mobs that block.
- Traits for block skills: like break-free skills, they take no points.

## Rules

Systems → Combat.
