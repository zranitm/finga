# Key bindings

> **Status: approved · 2026-11-18.** The high-level design for [B6. Key bindings](../product/features/world-and-movement/b6-key-bindings.md), which replaces the Classic and WASD presets, and the skill bar's eight keys.

## 1. What has to hold

- **The user's words** (2026-11-18): "make the keybinding window in setting - change the skill default button from numbers to chars near the left hand like qwerasdfzxcv", then "get 8 keys first qwerasdf".
- **Their choices:**
  - eight skill keys, Q W E R A S D F;
  - X attacks and Space jumps;
  - one default layout, every key rebindable, and no presets.
- **Keys are the client's.** It reads `KeyboardEvent.code`, the key's place, not its letter, as it does today. The world never sees keys, only actions.
- **Saved with the account,** in its settings, as the minimap's flag and chat's pin are, so bindings follow the player to another browser.
- **The bar's size is one number on each side,** so twelve keys later, with Z X C V, is that number and four defaults.

## 2. The default

| Action | `code` |
| --- | --- |
| `up`, `down`, `left`, `right` | `ArrowUp`, `ArrowDown`, `ArrowLeft`, `ArrowRight` |
| `jump` | `Space` |
| `attack` | `KeyX` |
| `nextTarget` | `Tab` |
| `talk` | `KeyZ` |
| `hpPotion` | `Digit1` |
| `skill1` … `skill8` | `KeyQ`, `KeyW`, `KeyE`, `KeyR`, `KeyA`, `KeyS`, `KeyD`, `KeyF` |
| `inventory` | `KeyI` |
| `character` | `KeyC` |
| `skills` | `KeyK` |
| `party` | `KeyP` |
| `quests` | `KeyJ` |
| `map` | `KeyM` |
| `arena` | `KeyV` |
| `chat` (fixed) | `Enter`, `NumpadEnter` |
| `close` (fixed) | `Escape` |

- Classic's keys stay where the letters weren't taken: Z talks, X attacks, C opens the character window.
- **Moved:** the HP potion from A to 1, and the quest log from Q to J (a journal).
- **Free for later:** G, T, B, 2 to 5, and the right hand's letters not listed.
- **`input/presets.ts`** becomes `input/bindings.ts`: one `defaults` table, and `keyTable(overrides)`, the defaults with the player's changes laid over. `Preset` and the WASD table go.

## 3. Eight skill keys

- **Client:** `SkillKey` runs `skill1` to `skill8`, and `skillKeys` lists eight. The skill bar store holds eight slots, `live` and all. The HUD draws eight keys, each labelled with its bound key.
- **Server:** one constant, `SkillBarKeys = 8`, in the character module.
  - `skill_bar text[]` holds eight entries.
  - `PUT /api/characters/{id}/skill-bar` takes eight. Anything else is `BAD_SKILL_BAR`, as six were.
  - The dev database is wiped as usual, and `make testers` fills eight.
- **What reads the bar** takes its length, not six: chains (skill-chains.md), the frame row's hidden cooldowns (frame-icons.md), the skill window's drag, and Guard's swap at promotion (blocking.md).
- **The balance harness** carries eight keys per class, not six (skill-chains.md section 5).
- **Bots** are unchanged: they send skills, not keys.

## 4. The key-binding window

- **Where:** a Key bindings button in Settings opens `features/settings/KeyBindings.tsx`, a window over whatever is behind it, the game included.
- **The list,** in groups:
  - Movement: up, down, left, right, jump;
  - Combat: attack, next target, HP potion, skills 1 to 8;
  - World: talk or enter a portal;
  - Windows: inventory, character, skills, party, quests, map, arena;
  - Fixed, greyed: chat on Enter and close on Esc.
- **A row:** the action's name and a chip with its key.
- **Binding:**
  1. A click on the chip sets it to "Press a key" and starts listening.
  2. The next `keydown` binds its `code`, with `preventDefault`, so Tab or Space doesn't move the page.
  3. Esc, or a click elsewhere, stops listening and changes nothing.
  - While listening, the game acts on no key.
- **Refused,** with a shake and a line saying why:
  - `Escape`, `Enter` and `NumpadEnter`, which are fixed;
  - `MetaLeft`, `MetaRight` and `ContextMenu`, which the system takes;
  - `F5`, `F11` and `F12`, which the browser keeps.
  - Modifier keys on their own, such as `ShiftLeft`, `ControlLeft` or `AltLeft`, are allowed. With a Ctrl key bound, the window says "Ctrl with other keys can trigger browser shortcuts.", since Chrome keeps Ctrl+W, T and N whatever the page does.
- **One key per action.** A key another action holds swaps them: that action takes this one's old key. The line under the list names both: "Q is now Attack; Skill 1 moved to X."
- **Reset to default** asks first, then clears every change.
- **Names:**
  - From `navigator.keyboard.getLayoutMap()` where the browser has it (Chrome and Edge), so a key shows the letter printed on it on the player's layout.
  - Otherwise, US names: `KeyQ` is Q, `Digit1` is 1, `Numpad1` is Num 1, `ArrowUp` is ↑, `ShiftLeft` is Left Shift, `Space` is Space.
  - One `keyName(code)` serves the window and every hint (section 6).
- **Saving:** a change applies at once. It's saved to the account 500 ms after the last change, one save at a time, as the skill bar's saves go. A failed save keeps the change on screen and says so.

## 5. Saved with the account

- **`account.Settings`** loses `control_preset` and gains `keys`: a map from action to `code`, holding only the actions that differ from the default.
  - An empty map is the default.
  - A new action added later starts at its default for everyone.
- **The server checks the shape, not the actions,** since actions are the client's:
  - at most 64 entries;
  - each action and each code matching `^[A-Za-z0-9]{1,32}$`;
  - no code held by two actions;
  - none of the fixed or refused codes.
  - Anything else is `BAD_KEY_BINDINGS`, a 400, the next number in area 11. `BAD_CONTROL_PRESET` goes.
- **The client** ignores a stored action it doesn't know, and a stored code that would clash with a default it hasn't overridden. Such a code keeps its default, so a player never loses a key.
- **The local cache:** the preset's cache in `lib/storage` (`finga.settings.preset`) becomes `finga.settings.keys`, under the same guard. The game starts on it before the account's settings arrive.
- **No compatibility:** a stored `control_preset` is dropped on read. Every account starts on the default.

## 6. Every hint reads the bindings

- **The skill bar's keys** show their bound keys, not 1 to 6.
- **The HP potion's key** shows its bound key, once the HUD has a potion slot to show it on; it has none yet (decided 11-18).
- **The talk and portal hint** over an NPC or a portal names the talk key.
- **The skill window's card** says "Drag onto the skill bar to use it with its keys", with no numbers.
- **Any other text** naming a key, found by a search for key names in `web/src`, reads `keyName`.

## 7. Testing

- **Client:**
  - `keyTable`: the defaults, an override, an unknown action ignored, and a stored clash keeping the default;
  - the window: binding, the swap and its line, each refused key, Esc cancelling, the game not acting while listening, and Reset;
  - `keyName` with and without a layout map;
  - the skill bar's eight keys and their labels.
- **Server:**
  - the settings' shape checks, each refusal;
  - `control_preset` gone;
  - the skill bar's eight entries, with six or nine refused.
- **End to end:** a player binds Attack to V, reloads in another browser context on the same account, and attacks with V.
- **Live:** each class plays a map on the defaults, then with movement moved to W A S D and skills moved to the right hand.

## 8. Build

1. **Eight keys and the new defaults:** `bindings.ts`, the server's bar of eight, and the hints. Presets go.
2. **The window and the saved bindings:** `KeyBindings.tsx`, `keys` in the account's settings, and the cache.

### As built: PR 1

- **The bar's eight keys** are `SkillBarKeys` on the server and `SKILL_KEYS` in the client's bar store, with a test that the input has a skill key for each.
- **No wipe:** migration 00011 pads each stored bar with two empty keys, keeping its six in order, and sets the eight-key default and check.
- **`control_preset` goes from the server in PR 1,** since the presets went from the client. A settings save naming it is a malformed body.
  - `BAD_CONTROL_PRESET` (110007) goes, and `SESSION_RACE` moves from 110008 to 110007, as an area's codes count up with no gap.
  - PR 2's `BAD_KEY_BINDINGS` takes 110008.
- **`input/bindings.ts`** holds `defaults`, `keyName` with US names, and `keyOf`, the name of an action's first key. The keyboard reads its table through a function at each key, so PR 2's saved keys apply at once. `keyTable(overrides)` and the layout map come with PR 2, which has their input.
- **Hints:** the skill bar's keys and their tooltips, and the card's "with its keys". The client named no other key: there's no potion or talk hint yet.
- **The HUD** widens from 257 px to 339 px, so the eight keys stay one row, Q to F, under the player's frame. Two rows of four were tried and left half the tray empty.

### As built: PR 2

- **Server:** `account.Settings` gains `Keys`, checked by `Settings.Check` on each save with section 5's rules; anything else is `BAD_KEY_BINDINGS` (110008). A read always carries `keys`, `{}` for the defaults. A `control_preset` stored before PR 1 is dropped on read, which a repository test pins.
- **`keyTable(changes)`** lays the changes over the defaults, worked out once per set of changes. A change to an unknown or fixed action, to a reserved key, or to a key another action keeps by default is dropped, and that action keeps its default. It drops changes until no key does two things, since a dropped change can put its action back on a key another change took.
- **`rebind`** binds a key and moves the action that held it onto the old key, and keeps only the changes that differ from the defaults.
- **The settings store** holds `keys`, cached in `finga.settings.keys`. The page starts on the cache, and the account's keys replace it at login. The world reads `keyTable(settings.keys)` at each key, and the HUD reads it through `useKeys`.
- **Key names:** `app/keyboard-layout.ts` reads the Keyboard Map API into `state/keyboard-layout` at start and whenever the window regains focus. It also listens for `layoutchange` only where the keyboard is an EventTarget. Chrome's isn't: its `navigator.keyboard` has `getLayoutMap` alone, and listening on it threw during the app's mount and blanked the page. `keyName(code, printed)` uses what the keyboard prints, upper-cased, else the US name.
- **The window,** `features/settings/KeyBindings.tsx`, opens from a Key bindings button in Settings, in place of the panel, with a Back button.
  - While a chip listens, capture listeners on `window` take every key before the game or the modal, so Esc cancels without closing Settings.
  - The key up of the key just bound is taken too.
  - A pointer down anywhere else cancels.
  - A refusal shakes the chip red and says why.
- **Saving:** `lib/save-queue.ts` is the skill bar's debounced, one-at-a-time saver, taken out so both use it. The window saves 500 ms after the last change, and at once when it closes or the page hides.
- **Settings in the world** (decided 11-18): a Settings button in the world corner, above Back to characters, opens the same Settings modal, so a change applies in the game at once.
- **The talk prompt** (decided 11-18): a `prompt` label, "Z Talk" or "Z Enter" with the bound key's name, over the portal the player stands in, or else over the NPC in reach whose dialog isn't open. It's placed each frame beside the dialog's reach check.
- **Not built:** the end-to-end test in another browser context, since the repo has no browser e2e suite. The Go e2e saves keys and reads them in a new session, and the client tests cover loading them at login. The live playtest checks the rest.

### As built: fixes from the live pass

- **A bound Ctrl or Alt acts.**
  - Chrome sends Ctrl's own key down with `ctrlKey` already set, and Alt's with `altKey`, and the game had dropped every key down with either as a browser shortcut.
  - Now the keyboard keeps the Ctrl and Alt keys down, by code. A Ctrl or Alt flag makes a key the browser's only if some Ctrl or Alt key down (the event's own included) isn't bound, or none was seen going down. Cmd stays the browser's.
  - So a bound modifier's own press acts, and while only bound ones are held, the game's keys act too: attack held on Left Ctrl while Q fires its skill. With an unbound Right Ctrl also down, Ctrl+W is the browser's.
- **Esc closes Settings wherever focus fell.**
  - Key bindings, Back and Reset's question each remove the button clicked, so focus fell to the page, and the dialog's own key handler never heard Esc.
  - `Modal` now listens for Escape on `window` while it's open, and only the newest open one closes.
  - With the focus inside it, the dialog still stops Esc itself, so a world window it sits in (the promotion's question, Reset stat points) doesn't close too.
  - `lib/modals.ts` keeps the open ones, since components and the game can't share a store.
  - Every `Modal` gets this: Settings, the join problem, Reset stat points and the promotion's question.
- **No game key acts behind a modal.** The keyboard drops every key down while one is open, Esc too, so the modal alone closes. The world's windows stay as they were.
  - **A key held as a modal opens is let go** (from the second live pass): holding → while clicking Settings had walked on 2.21 m behind it. `lib/modals.ts` calls back as each one opens, and the keyboard releases its held keys, as on blur but keeping the Ctrl and Alt keys down, whose key ups still come.
  - A key still held once the modal closes walks again on its next repeat, as after any release-all.
- **ß stays ß.** What a key prints is upper-cased only while it stays one letter: German Minus had shown "SS".
- **Short names on the skill bar:**
  - `shortKeyName` gives LShift, RShift, LCtrl, RCtrl, LAlt, RAlt, Bksp, Del, Ins, PgUp, PgDn and Caps, and N5 for Num 5.
  - A name over four letters is drawn at 8 px, and every name is one line, clipped short of the energy cost.
  - The key's label and the window keep the full name.
  - Checked in a headless render: LShift and RShift fit beside a cost.
- **Settings' checkboxes** sit on their text's line (`field-check`), where `.field`'s column had put them above it.

## 9. Decisions

The user's choices are in section 1. These calls were made in planning and logged for the user to push back on:

1. **The other defaults:** Z talks and C opens the character window, as in Classic. The HP potion moves to 1, and the quest log to J.
2. **Enter and Esc are fixed,** so a player can always reach the chat and close a window.
3. **One key per action,** and a taken key swaps.
4. **Refused keys:** the fixed ones, the system's, and F5, F11 and F12.
5. **Only changes are stored,** and the server checks their shape, not the action names.
6. **Key names follow the player's keyboard layout** where the browser tells it.
7. **The bar's size is one constant per side,** ready for twelve.

## 10. Alternatives considered

- **Keeping the presets:** the user chose one default with every key rebindable. A WASD player moves movement to W A S D and skills elsewhere in a minute.
- **Two keys per action:** more to explain; the fixed chat key's NumpadEnter is the only case today.
- **Saving per browser:** bindings would be lost on another machine, and the account's settings already carry the minimap and the chat pin.
- **Twelve keys now:** the user chose eight first.
