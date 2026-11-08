# D3. Equipment

- **Feature:** D3. Equipment
- **Group:** D. Items and inventory
- **Priority:** P1
- **Status:** Draft

**Story:** As a player, I can equip weapons, armor, and accessories to get stronger.

## Acceptance

- A character has ten equipment slots: a weapon, five armor slots, and four accessory slots (Systems → Items).
- An item goes only into the slot its type names. A ring fits either ring slot.
- Equip an item from the inventory with a double-click, or by dragging it onto its slot in the character window (D6).
- Equipping into a filled slot swaps the two: the worn item goes back to the inventory, into the slot the new one left.
- Unequip with a double-click on the worn item, or by dragging it back to the inventory. With a full Equip tab, nothing moves and the player is told why.
- An item above the character's level, or for another class, can't be equipped. Nothing moves and the player is told why.
- Stats change right away, in combat and in the character window. Every player sees the same results in combat.
- What a character wears is unchanged after a map change, a death, a disconnect, or logging out and back in.
- A worn item is never duplicated or lost, even if the game disconnects in the middle of a swap (D5).
- A dead character can't change equipment (proposal).
- When unequipping lowers max HP or MP below the current value, the current value drops to the new maximum (proposal).

## Not in this feature

- Equipment changing how the character looks (A4, P2).
- Seeing what other players wear.

## Rules

Systems → Items
