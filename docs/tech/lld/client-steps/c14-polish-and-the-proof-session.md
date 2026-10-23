# C14. Polish and the proof session

- **Depends on:** C13
- **Milestone:** M6. Proof session
- **Order:** 14
- **Status:** Ready for review
- **Step:** C14. Polish and the proof session

**Goal:** the Stage 1 exit criterion as written: three or four people in browsers finish a 30 to 60 minute session, and the evidence is recorded.

**Layer in this step:** all; no new layer.

## Scope

- Fixes found while playing M1 to M5, each with a test where one applies. Rough edges the implementation session already knows of, to look at first (no browser was attached while building, so none is confirmed): (1) the HUD, party panel, toasts, invites, chat box and the "Back to characters" button were placed by hand and may overlap on a small window; (2) the drop hint scales the sprite by 1.25 from its current scale, and the tinted flask's size was chosen by its pixel size, not seen; (3) the Bowman's arrow is a plain streak and the Warrior's swing is the hit pose for 150 ms; (4) inventory icons are initials coloured by tab (decision 141); (5) the map-name banner shows on every scene start, including a reconnect to the same map that changed; (6) remote players stand still past one tick of extrapolation, which may show as a stutter at 20 Hz when a Delta is late; (7) the Correction path (page 4, reserved) has never run live; (8) the chat box keeps focus until Enter or Escape, so a click on the canvas does not release the keyboard; (9) the death overlay covers the whole screen, including the chat; (10) keyboard presets are KeyboardEvent.code, so a non-QWERTY layout gets the physical positions, as designed, but the settings labels name letters.
- A `make play` target (or documented steps) that starts `make up` and the client preview build (`vite preview --host`) so testers on the LAN can join by URL. (Added ahead of the session: `make play` runs `make up`, the production build and `vite preview --host --port 4173`; the preview server reuses the dev proxy for `/api`, `/ws`, `/healthz` and `/readyz`, checked by the live prediction test through port 4173.) `vite preview` reuses the dev proxy, so the client stays same-origin and `cors_origins` and `VITE_API_ORIGIN` stay as they are. Plain http on a LAN address is not a secure context, which is why C01 builds IDs from `crypto.getRandomValues`.
- Sound off by default; a volume toggle in Settings if any sound is added.
- Client build served as static files from the `preview` command only; nginx and Cloudflare stay in Stage 2.
- The proof session: three or four people, 30 to 60 minutes, covering sign-up, movement, fighting, levelling, loot, inventory, portals, chat and party. Server tick p99 from `/metrics` recorded alongside. Disconnects, kicks, and anything that looked wrong go in the log row with the commit that fixed it.
- `docs/ai-coding-log.md` row with the session's evidence, and the Stage 1 page's exit criteria checked off.

## Design notes

- This is the client's answer to backend step 17: the bots proved the server under load; people prove the game is playable.
- Playwright is Stage 2 (page 7, Tests); no end-to-end browser test is added here.

## Tests first

- one regression test per fix made in this step, named after the bug
- `make cover` still holds 80% on `lib`, `protocol`, `sim`, `net`, `state` and `game/systems`

## Done when

- [ ] The session happened with the counts and duration recorded, and every Stage 1 exit criterion on the Stage 1 page has evidence.
- [ ] `make test`, `make lint`, `make cover` and the client `build` pass; CI green; pushed to [github.com/zranitm/finga](https://github.com/zranitm/finga).

## Review focus

- The log row is specific: who played, how long, what broke, what was fixed.
