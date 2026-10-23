# C13. Chat and party

- **Depends on:** C12
- **Features:** E1, E3
- **Milestone:** M5. Play together
- **Order:** 13
- **Status:** Done
- **Step:** C13. Chat and party

**Goal:** talk on the map and in the party, and run a party of up to four from a window.

**Layer in this step:** `features/chat`, `features/party`, `features/hud` (party panel) (page 7 layer 4).

## Scope

- Chat box in the HUD: Enter focuses the input (setting `ui.captureKeyboard`), Enter again sends `ChatSend` (channel `map` by default; a `/p` prefix sends channel `party`), Escape leaves; 120-character limit shown; a refused line comes back as a Response with no request ID (`rate_limited`; `invalid_argument` for an empty or too-long line; `not_found` for a party line with no party), shown through net's `onUnmatchedResponse` as a brief message; lines from `ChatMessage` with sender name, map or party tag, and a 100-line scrollback.
- Party window (P key): invite a player on the same map, picked from the players in the latest Snapshot and Deltas, since `PartyInvite` takes a character ID, not a name; the incoming `PartyInvited` toast with accept and decline (`PartyRespond`) and its 30 s expiry countdown, leave, kick and promote for the leader; members with level, HP bar, map and online state from `PartyState`.
- HUD party panel: the compact member list always visible while in a party.
- Party EXP share shows through the normal `ExpGained` path; nothing extra on the client.

## Design notes

- All party commands carry request IDs and show the server's Response reason on failure; the window never changes membership locally.
- The chat input and the party window are the two places keyboard capture matters most; both go through `ui.captureKeyboard` from C05.

## Tests first

- chat (Testing Library): Enter sends ChatSend with the typed text; 121 characters is blocked with the counter red; `/p` sends a party line; an incoming ChatMessage renders with the right tag; rate\_limited shows the wait message
- party: inviting from the nearby-players list sends PartyInvite with that player's character ID and a request ID; a PartyInvited toast counts down from 30 and disappears; accept sends PartyRespond; a PartyState with four members disables invite; `party: null` closes the panel
- keyboard capture: while the chat input has focus, the input sender (C08) sends nothing (tested in the sender's own test through the `ui` store, since a feature test may not import `game`)
- the nearby store (who can be invited) keeps the players sorted and notifies only when the set changes; decline sends PartyRespond with accept false; the leader sees kick and promote, members leave only

## Done when

- [ ] Against `make up` with four browsers: party up, chat on the map and across maps, hunt and see shared EXP, leave and watch the lead pass. (Not run: no browser is attached to the implementation session; owed to the author with the pooled play-through.)
- [ ] M5 milestone recorded.

## Review focus

- No party state inferred locally; every change comes from PartyState.
