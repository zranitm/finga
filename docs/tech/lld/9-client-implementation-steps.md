# 9. Client implementation steps

The Stage 1 client, split into steps you review one at a time before any code is written, the same way as the backend steps. Each step page lists its scope by layer, design notes, the tests to write first, what "done" means, and what to look at closely. The design itself stays in page 7 (Client), page 4 (Protocol), page 5 (Gameplay rules), and the product page Screens and controls; a step page only says what to build in that step.

The backend for Stage 1 is complete and published at [https://github.com/zranitm/finga](https://github.com/zranitm/finga), so every client step builds against a running server (`make up`) and the golden messages in `testdata/contracts/`.

## How a step runs

1. **Review:** you read the step page. Status moves from Ready for review to Approved, or to Changes requested with comments.
2. **Tests first:** the tests listed on the page are written before the code, as `test.each` tables. The case names are the main review surface.
3. **Build:** only what the step's scope lists, on top of steps already done.
4. **Green:** `make test`, `make lint`, and `make cover` pass (they run the client's scripts through `npm --prefix web`), then one commit for the step.
5. **Log:** one row in `docs/ai-coding-log.md`: what was produced, how it was verified, what changed, mistakes caught, and the commit. Status moves to Done.

## Rules

- **Bottom-up:** steps follow page 7's dependency layers (lib and config, then protocol and sim, then net, state, input and components, then game and features, then app), so each one builds only on tested code.
- **Small:** a step is one reviewable unit. If one grows past a day or two of work, it's split.
- **The design wins:** if a step shows that a design page is wrong, that page is fixed in the same step, not only the step page.
- **The server is the reference:** the client decodes and re-encodes every golden message in `testdata/contracts/` and replays every movement vector in `testdata/movement/`. If the client needs something the server does not send, the backend gets a follow-up step; the client does not guess. The review of these steps found two: backend step 18 (one Input per tick, which C08 needs) and step 19 (EXP after death and invulnerability, which C10 needs).
- **Rules stay on the server:** a client copy of a server rule (the attack cooldown, the pickup range, the portal rectangle, no attacks in a town) only decides whether to send a command or show a hint. It never changes state, and its numbers come from the generated `gamedata` module (C03), never typed by hand.
- **Stage 1 only:** nothing from Stage 2 (currency, shop, marketplace, Protobuf, Playwright, nginx).
- **Open decisions:** a step that depends on one builds the current default and names it.
- **By layer:** each step says which page 7 layers it builds and groups its scope and tests by layer. `sim`, `protocol`, `net`, `state` and `game/systems` are tested with Vitest over plain data; `features` with Testing Library; scenes and entities by playing against `make up`.
- **Checked by playing:** every milestone from M1 on ends with the step's author playing it in a browser against the real server and recording what was seen in the log row.

## Milestones

| Milestone (Stage 1 page) | Steps |
| --- | --- |
| Foundations | C00 to C06 |
| M1. Walk together | C07 to C09 |
| M2. Fight and grow | C10 |
| M3. Loot and keep | C11 |
| M4. Travel | C12 |
| M5. Play together | C13 |
| M6. Proof session | C14 |

The proof session in C14 is the one the Stage 1 page asks for: three or four people in browsers for 30 to 60 minutes. The backend's bot session (backend step 17) proved the server; C14 proves the game.

- [Client steps](./client-steps/README.md)
