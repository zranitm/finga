# C22. Buffs and hidden cooldowns on the frame

- **Feature:** C22. Buffs and hidden cooldowns on the frame
- **Group:** C. Combat and progression
- **Priority:** P1
- **Status:** Agreed

**Story:** As a player, I see at a glance everything running on me: my buffs, my statuses, and the cooldowns of the follow-ups my keys hide. So I know when Ground Slam or Break Free is ready before my key offers it.

## Acceptance

- **One row of icons under the player's own HP bar,** in this order:
  1. **Buffs:** each buff on the player, their own or a party member's, such as Iron Body, Haste, War Cry or Barrier.
  2. **Statuses** (C19), as today.
  3. **Hidden cooldowns:** each follow-up that is cooling down and that no key shows now.
- **Each kind has its own look,** so they can't be confused:
  - **a buff:** the skill's icon in full colour, with a green edge, as allies have;
  - **a status:** as today, in its status's colour;
  - **a cooldown:** the skill's icon dimmed under the skill bar's dark sweep, with a grey edge.
  - Each shows its seconds left under it, as statuses do.
- **Which cooldowns show:**
  - The follow-ups of the chains whose roots sit on the player's six keys (C18), while they cool down. That includes the break-free skill (C19) and the block's counter (C21).
  - A skill that shows on a key, as a root does, keeps its cooldown on its key and isn't repeated in the row.
  - A cooldown leaves the row as it ends, and the follow-up can come up on its key again.
- **Order within a kind:** what ends soonest first. Statuses keep hard control first, as today.
- **Room:** up to 12 of each kind, then "+N". The row wraps onto more lines when full. Party frames and the target window, which are smaller, keep statuses to six.
- **Hovering an icon** names it:
  - a buff: its name and time left, and who it came from if not the player;
  - a cooldown: its name, when it's ready, and on whose key it comes up and when, such as "Ground Slam: ready in 7 s. On Strike's key while the target is stunned or frozen."
- **Fairness:** the server tells the player which buffs are on them and for how long. The client counts cooldowns itself, as it does for its keys.

## Not in this feature

- Buffs on party members' frames and on the target window.
- Cooldowns of skills in no chain that aren't on a key.
- Cancelling a buff by clicking its icon.

## Rules

Systems → Combat and Systems → Classes and skills.
