# D4. Consumables

- **Feature:** D4. Consumables
- **Group:** D. Items and inventory
- **Priority:** P1
- **Status:** Draft

**Story:** As a player, I can use potions to restore HP.

## Acceptance

- A potion is a Use-tab item that restores HP. There are no MP potions, since skills cost energy (C14). Using one restores up to its amount, never past the maximum, and removes one from the stack.
- A key drinks a potion (A in Classic, R in WASD). It picks the smallest potion carried that fills what is missing, or the largest when none does.
- In the inventory, double-click a potion, or select it and press Use.
- With HP already full, a potion is refused. The player is told why, and nothing is used.
- With no potion, the player is told so.
- A dead character can't drink. Potions work in towns.
- No cooldown: the key drinks again once the last potion it drank has applied.
- The HP bar moves, the amount restored rises over the drinker in green, and the Combat tab says "Drank a Red Potion."
- A character who dies while the potion is being used loses it and is not healed.
- One press uses one potion and heals once, even if the game disconnects and the request is sent again (D5).

## Not in this feature

- Buffs and other effects over time.
- Assigning potions to other keys (remappable keys, P2).
