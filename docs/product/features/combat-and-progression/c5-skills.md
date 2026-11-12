# C5. Skills

- **Feature:** C5. Skills
- **Group:** C. Combat and progression
- **Priority:** P1
- **Status:** Draft

**Story:** As a player, I put skill points into the skills I want, watch them evolve, and use them in combat.

## Acceptance

- **Skill points:**
  - Each level-up gives 5 skill points. Points not spent are kept.
  - The HUD shows a badge while skill points are unspent.
- **Skill window:**
  - The skill window (K) lists every skill the character can learn: their class's, and every earlier class's, so Beginner skills always.
  - Each skill shows its points, what each point adds, its next evolution, and how many points that needs.
- **Spending and evolving:**
  - Points are staged with **+** and take effect on **Apply**; **Undo** returns them (proposal).
  - Reaching an evolution total turns the skill into its evolved form at once, keeping every point. The combat channel announces it.
  - Evolution totals and forms come from game data and can change without a code change (Systems → Classes and skills).
- **Using skills:**
  - Active skills go on the skill bar (keys 1 to 6) by dragging them from the skill window.
  - Pressing a skill's key uses it: it costs MP and starts its cooldown. It is refused, with a short notice, when MP is too low, during its cooldown, while dead, or for damaging skills in a town.
  - A skill hits or misses by the combat rules, and its damage or effect follows the character's stats.
  - Using a skill is an action: one at a time with basic attacks, and the two can cut each other short (C9).
- **Passive skills** work while they have at least one point.

## Not in this feature

- Skill resets (none for now).
- The skill sets of the first classes, which are content defined per class.

## Rules

Systems → Classes and skills and Systems → Combat
