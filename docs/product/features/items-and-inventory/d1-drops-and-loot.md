# D1. Drops and loot

- **Decisions:** OD-05
- **Feature:** D1. Drops and loot
- **Group:** D. Items and inventory
- **Priority:** P0
- **Status:** Draft

**Story:** As a player, I get the items and currency that defeated mobs drop, without stopping the fight to collect them.

## Acceptance

- Defeated mobs drop items based on their drop list, and currency once F1 exists.
- Each item drop belongs to one player from the moment it falls: the killer when hunting alone, or in a party the living member whose turn it is (E3). A currency drop in a party is split evenly instead (F1).
- Everyone on the map sees the drop fall. After a short delay (1 second to start, tunable) it flies to its owner and goes into their inventory. There is no pickup key.
- The owner gets it wherever they are on the map, and also if in the meantime they died, left the map, or logged out.
- No other player can take a drop that isn't theirs.
- With a full inventory, the drop stays on the ground, reserved for its owner, and is collected within a few seconds of a slot freeing up. The owner sees what is waiting. A drop still waiting when its life ends (60 seconds) disappears.
- Every drop collected shows in the owner's combat channel (E5). A rare find (4★ and up) also shows a toast (proposal).

## Not in this feature

- Choosing which drops to collect (a loot filter): later.
- Bidding on party loot: later (E3).

## Rules

Systems → Items and Systems → Social
