# C6. Stat allocation

- **Decisions:** OD-16, OD-17, OD-18
- **Feature:** C6. Stat allocation
- **Group:** C. Combat and progression
- **Priority:** P1
- **Status:** Draft

**Story:** As a player, I spend points at each level-up on the stats I choose, to build the character I want.

## Acceptance

- **Points:**
  - Each level-up gives 5 stat points.
  - Points not spent are kept, across maps, deaths, and logouts.
  - While points are unspent, the HUD shows a badge with the count, and the character window shows a **+** beside each primary stat.
  - Points can go into any of the five stats, with no limit per stat and no class restriction.
- **Staging:**
  - Pressing **+** stages a point. Nothing changes until **Apply**; **Undo** returns every staged point.
  - Before Apply, the window shows what the staged points would change in the derived stats (proposal).
  - Applied points take effect at once, in combat and in the window, and stay until a reset.
- **Auto-assign:**
  - A button in the window spends the unspent points by the class's recommended split.
  - A Settings option, off by default, does the same at every level-up.
- **Resets:**
  - A reset returns every spent point to unspent.
  - Each character has one free reset.
  - Once currency exists (F1), later resets cost currency, more at higher levels.
- **A fresh start:** this feature ships with an empty game. Existing accounts and characters are removed rather than converted, since the game is still in private testing.

## Not in this feature

- Skill points (C5).
- Stat requirements on equipment are in Items and D3.

## Rules

Systems → Progression
