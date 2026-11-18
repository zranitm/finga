# B6. Key bindings

- **Feature:** B6. Key bindings
- **Group:** B. World and movement
- **Priority:** P0
- **Status:** Agreed

**Story:** As a player, I set every key the way I like it in Settings. Out of the box my right hand walks on the arrows, and my left hand has my skills under it, on Q W E R and A S D F.

## Acceptance

- **One default layout,** in place of the Classic and WASD presets:

  | Action | Default key |
  | --- | --- |
  | Walk up, down, left, and right on screen; two together walk diagonally | Arrow keys |
  | Jump | Space |
  | Attack | X |
  | Lock the next closest mob (C15) | Tab |
  | Talk to an NPC in reach, or enter the portal the player stands in | Z |
  | Drink an HP potion | 1 |
  | Skills on the skill bar's eight keys | Q, W, E, R, A, S, D, F |
  | Inventory | I |
  | Character window | C |
  | Skill window | K |
  | Party | P |
  | Quest log | J |
  | Map | M |
  | Open the chat; send or close it while typing | Enter; Esc |
  | Close the top window | Esc |

- **The skill bar has eight keys,** up from six. Players drag skills onto them as before, and chains turn them into follow-ups (C18).
- **The key-binding window,** from a Key bindings button in Settings:
  - It lists every action, grouped: Movement, Combat, World, and Windows. Each shows its key.
  - Clicking an action's key and pressing another key binds it. Esc, or a click elsewhere, cancels.
  - **One key per action.** A key that another action has swaps the two, and the window says what moved, such as "Q is now Attack; Skill 1 moved to X."
  - **Fixed:** Enter opens the chat and Esc closes windows, so a player can never lock themselves out of either. They show in the list, greyed.
  - **Not allowed:** keys the browser keeps for itself (F5, F11, F12), and the Windows or Command key. The window says why.
  - **Shift, Ctrl and Alt on their own** can be bound, and the game's other keys still work while one is held. With Ctrl bound, the window warns that Ctrl with other keys can trigger browser shortcuts, such as Ctrl+W closing the tab.
  - **Reset to default** puts every key back, after a confirm.
  - Keys show by the names on the player's keyboard where the browser can tell them, so a French keyboard shows A where its key sits. Otherwise they show US names. The skill bar shortens long ones, such as LShift for Left Shift.
  - A change applies at once, in the game too, and is kept with the account, so it follows the player to another browser.
- **While Settings is open,** the game's keys do nothing, a key held as it opens included, and Esc closes it.
- **Every key the game names reads the bindings:** the skill bar's keys, the potion's key, the hint to talk or enter a portal, and the skill window's tips.
- Attacks and skills go to the locked mob, and the lock key switches it (C15). No mouse aiming.

## Not in this feature

- Two keys for one action.
- Key combinations, such as Shift and a letter.
- More than eight skill keys: twelve, with Z X C V, may come later.
- Gamepads.

## Rules

Screens and controls.
