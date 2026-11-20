# D1. Drops and loot

- **Feature:** D1. Drops and loot
- **Group:** D. Items and inventory
- **Priority:** P0
- **Status:** Draft

**Story:** As a player, I get what defeated mobs and bosses drop: a pouch, or a boss's treasure box, that I walk up to and open with T, taking what I want.

## Acceptance

- **Pouches:**
  - A defeated mob drops a pouch about 1 time in 6, and an elite about 1 time in 3. A pouch holds what that kill rolled from its drop list, and currency once F1 exists.
  - Mobs come back slowly enough (C1) that a player can take the pouch before the camp refills.
  - An item one of the owner's active quests still needs is rolled at its own chance even when no pouch falls, and comes in a pouch.
- **A field boss's treasure box:** each player who earned a share of its rewards (C10) gets their own box where it fell, holding their own roll.
- **Owners:** a pouch belongs to one player from the moment it falls: the killer when hunting alone, or in a party the living member whose turn it is, one pouch each (E3).
- **Only the owner sees it.** Nobody else can see or open it.
- **Opening:**
  - Within about 2 metres of a pouch or box, its key (T, rebindable in B6) shows over it.
  - Pressing T opens one list of everything in the player's pouches and boxes within reach.
  - Clicking an item takes it. Take all, or T again, takes everything.
  - Hovering shows an item's card, with its stats and how it compares (D7).
  - Walking away or Esc closes the list.
- **Taking** works in combat, but not while dead. A taken item goes to the inventory and shows in the combat channel (E5).
- **A full tab:** an item whose tab is full stays in the pouch, with the reason shown.
- **Lasting:** a pouch is gone 60 seconds after it falls, and a boss's box after 2 minutes, with whatever is left in it. The combat channel says what was left behind.
- **Nothing follows the player:** a pouch waits where it fell.

## Not in this feature

- A loot filter.
- Bidding on party loot (E3).

## Rules

Systems → Items and Systems → Social
