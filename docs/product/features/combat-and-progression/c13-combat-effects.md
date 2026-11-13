# C13. Combat effects

- **Feature:** C13. Combat effects
- **Group:** C. Combat and progression
- **Priority:** P1
- **Status:** Draft

**Story:** As a player, every hit I land feels solid and every skill looks like itself, so chaining attacks, skills, and cancels for the most damage is satisfying. Grinding and that combat are the game's main experience.

## Acceptance

- **Hits land hard:**
  - **Hit-stop:** when the player's own hit lands, their pose and the mob freeze for a moment, about 0.05 seconds. It's drawing only: the timing of actions and cancels never shifts.
  - **Impact sparks** burst where a hit lands, on mobs and players alike. Physical hits spark white and magic hits violet. A crit bursts bigger, and a MISS shows a small grey puff.
  - **Damage numbers** pop in with a bounce. Crits are bigger, and quick hits in one spot stack upward instead of overlapping, whether they land on one foe or several piled together.
- **Every attack looks like itself:**
  - Each class's basic attack has its own look:
    - the Beginner's short swing;
    - the Warrior's heavy, wide arc;
    - the Thief's quick, thin slash;
    - the Bowman's arrow with a trail;
    - the Magician's glowing orb.
  - Each skill has its own look:
    - Strike: a heavy overhead arc.
    - Throw: a spinning stone that can be seen flying.
    - Spark: a zig-zag bolt.
    - Slash Blast: a wide sweep.
    - Double Stab: two quick flashes.
    - Power Shot: a thick arrow with a trail.
    - Fire Arrow: a burning arrow that bursts.
    - Heal: rising sparkles.
    - The buffs: an aura in the buff's colour.
  - Each evolution looks stronger than the last. Crushing Blow cracks the ground, Volley throws three stones, and Arc's lightning leaps between foes.
  - Effects play within the action's beats: strong through the strike, gone by the cancel point (C11).
- **The cancel hint:** right after the player cuts an action short with a cancel, a faint afterimage of the pose they cut off fades away. It shows only after a cancel, never before one, so it confirms a cancel without hinting at when to press. Only that player sees it.
- **Art and sounds can be added later:**
  - Every effect has a name.
  - Sprite animations, and sounds, can later be dropped in for any effect without a code change.
  - Until then the drawn version shows, and nothing plays a sound.
- **Fairness:** effects are drawing only and change no hit, damage, or timing.

## Not in this feature

- Screen shake.
- A combo counter.
- Attack animation frames for characters, which come with an art pass.
- Sounds themselves, and a volume setting, which come with the first sounds.

## Rules

Systems → Combat → What players see
