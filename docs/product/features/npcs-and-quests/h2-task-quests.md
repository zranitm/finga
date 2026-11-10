# H2. Task quests

- **Feature:** H2. Task quests
- **Group:** H. NPCs and quests
- **Priority:** P1
- **Status:** Draft

**Story:** As a player, I take tasks from NPCs, complete them while hunting, and hand them in for rewards.

## Acceptance

- **Accepting:** take a quest at its NPC when its conditions are met. The NPC shows what the quest asks and what it gives.
- **The quest log** (Q) lists active quests with each objective's progress, and lets the player abandon one.
- **Progress:**
  - It counts as it happens and survives map changes, deaths, and logouts.
  - The combat channel shows each step.
  - Kills count for everyone who gets EXP from them.
- **Handing in:**
  - At the named NPC, once every objective is done.
  - Items the quest collects are taken, and the rewards arrive at once.
  - An item reward that doesn't fit refuses the hand-in, with nothing taken.
- **Repeating:** a one-time quest can't be taken again once finished; a repeatable one can.
- **Limit:** at most 20 quests are active at once.
- **Finished quests are kept,** since promotions and later quests can require them.

## Not in this feature

- Story, dialogue trees, or cutscenes.
- Daily quests (later).

## Rules

Systems → NPCs and quests
