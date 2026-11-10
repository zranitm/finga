# E5. Combat channel

- **Feature:** E5. Combat channel
- **Group:** E. Social
- **Priority:** P1
- **Status:** Draft

**Story:** As a player, I can follow what I earn while I fight, without watching every drop.

## Acceptance

- The chat box has three tabs:
  - **General:** everything players say, map and party lines, each tagged as today;
  - **Party:** party lines only;
  - **Combat:** the lines below.
- What the player types goes where the open tab points: map chat from General, party chat from Party (proposal). "/p" sends to the party from any tab, as today. Typing from Combat sends map chat (proposal).
- New combat lines never switch the tab, and never push what players say out of General or Party.
- The Combat tab shows, as it happens:
  - EXP gained from a kill, and EXP lost on death;
  - each item collected, with its quantity and stars, the name in its star colour;
  - currency collected, once F1 exists;
  - level-ups;
  - a drop waiting on the ground because the inventory is full, and when it disappears uncollected;
  - in a party, which member got each drop, for example "Ayla got Iron Sword ★★".
- The lines are for the player alone: nobody else sees them, and they aren't kept after logging out. The tab keeps the most recent 200 lines (proposal).

## Not in this feature

- Filtering which lines show.
- Lines about other players outside the party.

## Rules

Systems → Social
