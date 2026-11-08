# D7. Item details and comparison

- **Feature:** D7. Item details and comparison
- **Group:** D. Items and inventory
- **Priority:** P1
- **Status:** Draft

**Story:** As a player, I can hover an item to see what it is and whether it beats what I'm wearing.

## Acceptance

- Hovering an item in the inventory or the character window shows a details card beside the pointer, always fully on screen. It goes away when the pointer leaves the item or the item is dragged.
- The card shows:
  - the name, in the colour of its stars;
  - its stars, from 1★ to 7★;
  - its type: the slot for equipment (for example "Weapon" or "Ring"), otherwise Use or Etc;
  - its requirements, minimum level and class, each in red when the character doesn't meet it;
  - for equipment, every stat this copy rolled, with the item type's range beside it, for example "Attack 18 (16–20)" (proposal);
  - for Use items, the effect, for example "Restores 50 HP"; for a stack, the quantity;
  - a one-line description.
- Two copies of the same equipment can show different stats: the card always shows the hovered copy's own.
- Hovering equipment in the inventory also shows a second card for the item worn in that slot. The hovered card marks how each stat would change if the two were swapped: gains in green, losses in red. A stat only one of the two items has counts as a full gain or loss.
- When that slot is empty, the second card says nothing is worn there, and every stat shows as a gain.
- For a ring, the comparison is with the ring a double-click would replace: the first empty ring slot, otherwise the first ring.
- An item the character can't wear still shows the comparison, with the unmet requirement in red.
- Hovering a worn item in the character window shows its card alone, with no comparison.

## Not in this feature

- Details of drops on the ground or of items in chat.

## Rules

Systems → Items
