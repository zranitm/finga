# D4. Consumables

- **Feature:** D4. Consumables
- **Group:** D. Items and inventory
- **Priority:** P1
- **Status:** Draft

**Story:** As a player, I can use potions to restore HP and MP.

## Acceptance

- A potion is a Use-tab item that restores HP, MP, or both. Using one restores up to its amount, never past the maximum, and removes one from the stack.
- Two keys drink a potion: one for HP (A in Classic, R in WASD) and one for MP (S in Classic, F in WASD). Each picks the smallest potion carried that fills what is missing, or the largest when none does.
- In the inventory, double-click a potion, or select it and press Use.
- With HP already full, an HP potion is refused, and so is an MP potion with MP full. The player is told why, and nothing is used.
- With no potion for a key, the player is told so.
- A dead character can't drink. Potions work in towns.
- No cooldown: a key drinks again once the last potion it drank has applied.
- The bars move, the amount restored rises over the drinker (green for HP, blue for MP), and the Combat tab says "Drank a Red Potion."
- A character who dies while the potion is being used loses it and is not healed.
- One press uses one potion and heals once, even if the game disconnects and the request is sent again (D5).

## Not in this feature

- Buffs and other effects over time.
- Assigning potions to other keys (remappable keys, P2).
