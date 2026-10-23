# Proof session: the Stage 1 exit criteria

Stage 1 is done when the four criteria below hold on `make up` from a clean
clone. This page is the checklist for the playtest and the record of the
session run for it. Numbers come from the server's own `/metrics` and from
the test clients; the bot that drives the session is `server/cmd/bot`
(`make bots`).

## Checklist

| # | Criterion | How it is shown | Where |
|---|-----------|-----------------|-------|
| 1 | A 30 to 60 minute session with 3 to 4 players: walk, fight, level up, loot, travel, party, chat, no crash, no desync | `make bots` plays 4 bots for 30 minutes: town → forest, hunting in the slime field, pickups, map chat, a party, trips back to the town; the report counts kills, pickups, hops, chat lines, refused commands and reconnects | the session record below |
| 2 | Tick p99 under 25 ms with the session's players on | `tick_duration_seconds` per map, read by the bot at the end and computed the way `histogram_quantile` does | the session record below |
| 3 | A hard kill of the game server never creates or loses an item | `TestCrashDuringPickups` (server/test/loot_e2e_test.go): the server as its own process, three clients looting concurrently, the process killed without warning, restarted, and `item.CheckInvariants` (checks 5, 6 and 8 of page 6) passing before and after the restart | `make test`, in CI |
| 4 | A double pickup and a retried pickup each give exactly one item | end-to-end rows in server/test/loot_e2e_test.go: two PickUp messages in one tick, the same PickUp re-sent with the same `seq` after ignoring the reply, and the worker's add retried under the drop's key; one item event per drop taken, under distinct drop IDs | `make test`, in CI |

Before the playtest with people:

- [ ] `make up` from a clean clone, `make smoke` green
- [ ] four accounts created, one per tester, on the same LAN (add the client origin to `FINGA_SERVER_CORS_ORIGINS`)
- [ ] `/metrics` scraped or the bot's report run at the end for the tick p99
- [ ] the crash test and the pickup rows green on the commit being played (`make test`)

## Session record

Run on 2026-10-22 against `make up` (commit of step 17), on the developer's
Windows machine with Docker Desktop, with four bots and no human players
(none were available; the bots are the record). The game server ran the
whole session without a restart, a warning or an error in its log.

```txt
proof session: 4 bots for 30m0s against http://localhost:8080

bot1010140     kills 467  pickups 176  deaths  0  hops   7  chat  53  messages   39395  refused   0  reconnects 0  login failures 0
bot1010141     kills 470  pickups 180  deaths  2  hops   7  chat  54  messages   39395  refused   0  reconnects 0  login failures 0
bot1010142     kills  18  pickups  14  deaths  7  hops  15  chat  52  messages   38730  refused   0  reconnects 0  login failures 0
bot1010143     kills 443  pickups 329  deaths  0  hops   5  chat  54  messages   39499  refused   0  reconnects 0  login failures 0
total          kills 1398  pickups 699  deaths  9  hops  34  chat 213  messages  157019  refused   0  reconnects 0  login failures 0

cave     ticks    36245  p50 0.25 ms (bucket ≤ 0 ms)  p99 0.50 ms (bucket ≤ 0 ms)  overruns 0
forest   ticks    36245  p50 0.34 ms (bucket ≤ 0 ms)  p99 1.48 ms (bucket ≤ 2 ms)  overruns 0
town     ticks    36245  p50 0.25 ms (bucket ≤ 0 ms)  p99 0.50 ms (bucket ≤ 0 ms)  overruns 0
all      ticks   108735  p50 0.27 ms (bucket ≤ 0 ms)  p99 0.97 ms (bucket ≤ 1 ms)  overruns 0
```

| # | Criterion | Result |
|---|-----------|--------|
| 1 | 30 to 60 minute session, 3 to 4 players | 30 minutes, 4 bots: 1,398 kills, 699 pickups, 9 deaths with respawns in the town, 34 map changes, 213 chat lines, a party of four, 157,019 messages received, 0 refused commands, 0 disconnects; the bowman bot that kept walking into the mushrooms died seven times and came back each time |
| 2 | Tick p99 under 25 ms | forest 1.48 ms, town and cave 0.50 ms, all maps 0.97 ms; 0 overruns in 108,735 ticks (the histogram's 25 ms bucket was never reached, so the real p99 is at most the 2 ms bucket's upper edge) |
| 3 | A hard kill never creates or loses an item | `TestCrashDuringPickups` green in `make test` on this commit |
| 4 | Double and retried pickups give one item | the D1/D2/D5 rows of `TestLootAndKeep` green in `make test` on this commit |
