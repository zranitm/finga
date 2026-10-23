# C06. app shell: sign up, log in, characters, settings

- **Depends on:** C04, C05
- **Features:** A1, A2, B6
- **Milestone:** Foundations
- **Order:** 6
- **Status:** Done
- **Step:** C06. app shell: sign up, log in, characters, settings

**Goal:** the React screens before the world, and the wiring that turns server messages into store changes.

**Layer in this step:** `features/auth`, `features/characters`, `features/settings`, `app` (page 7 layers 4 and 5).

## Scope

- `features/auth`: sign-up and log-in forms with the username and password rules from page 4 shown inline; the login throttle's `rate_limited` shown as a wait message.
- `features/characters`: list, create (name 3 to 12 letters and digits starting with a letter; Warrior or Bowman in Stage 1, S1-Q1; the 3-per-account cap shown when hit), and pick one to play.
- `features/settings`: choose Classic or WASD; saved with `PUT /api/settings` and cached (B6).
- Log out from the characters screen: `DELETE /api/sessions/current`, then the token is cleared and the login screen shows.
- `app/App.tsx` and the screen flow: login, characters, world (world is a placeholder until C07); `app/wiring.ts` subscribes the `GameSocket` to the store actions from C05; `app/PhaserGame.tsx` is added in C07.
- `styles/`: design tokens and global CSS; the overlay root with `pointer-events: none`.

## Design notes

- Login and character screens are React only. Phaser starts with the app and runs only Boot and Preloader behind them, so assets load while the player logs in (page 7, Phaser and React together); the World scene starts when the player enters. C07 adds both.
- `wiring.ts` is the single place `net` and `state` meet, so each is tested alone and this file is tested with the fake socket.

## Tests first

- auth (Testing Library): invalid username blocks submit with the rule shown; a 401 shows the server's message; a successful login moves to the characters screen
- characters: create with a bad name is blocked locally; a 409 shows the conflict in the server's words; once the list holds three, the create form is disabled with the cap notice, and a stale list still shows the server's cap conflict on the fourth attempt
- settings: choosing WASD calls `PUT /api/settings` and the cached preset updates
- log out: calls `DELETE /api/sessions/current`, clears the token, and returns to the login screen
- wiring: with the fake socket, a pushed InventoryChanged, PartyState, ChatMessage and LevelUp each land in the matching store; a Kicked frame sets `connection` to kicked and returns to the login screen

## Done when

- [x] Against `make up`: sign up, log in, create a character, pick a preset, and reach the world placeholder, which shows the Welcome character's name and level. Checked with the smoke script through the Vite proxy and temporary live tests (jsdom App, Node GameSocket); the browser run is owed at the M1 end (decision 118).
- [x] Foundations milestone: C00 to C06 Done.

## Review focus

- No feature imports another feature; `app` composes them.
- Keyboard capture: typing in a form never leaks keys to the game later.
