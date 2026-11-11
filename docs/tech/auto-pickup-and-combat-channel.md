# Auto pickup and the combat channel

> **Status: approved · 2026-11-10,** with the defaults in section 12. The high-level design for the new [D1. Drops and loot](../product/features/items-and-inventory/d1-drops-and-loot.md), round-robin party loot ([E3](../product/features/social/e3-party-up-to-4.md), [Systems → Social](../product/systems/social.md)), and [E5. Combat channel](../product/features/social/e5-combat-channel.md). It builds on the equipment work ([Equipment, rolled stats, and item details](./equipment-and-item-details.md), PR #2, merged on 2026-11-08).

## 1. What has to hold

From the product pages:

- **Ownership:** every item drop belongs to one player from the moment it falls. That is the killer when alone; in a party, the next living member on the map, taking turns.
- **Collection:** it flies to its owner after a short, tunable delay (1 second), wherever they are on the map, even if they died, left the map, or logged out. There is no pickup key.
- **Full inventory:** the drop waits for its owner, is collected within a few seconds of room appearing, and is gone after 60 seconds.
- **Combat channel:** a Combat tab shows EXP, items collected (with which party member got each), level-ups, and drops waiting or lost.
- **Currency (later, F1):** split evenly among the same members, with the remainder to the killer.

From the code (the rules this design keeps):

- **The tick never waits on another service.** Grants run on the world's item-call pool, and their results come back through the instance's inbox, as pickups do today.
- **One drop is one item, whatever is retried.** Each drop keeps its own idempotency key (`drop.key`), so a grant retried after a lost reply or a crash can't add a second item.
- **The protocol only grows.** Fields and messages are added. `PickUp` stays in the client protocol, unused, so an older client's key press does nothing harmful.

## 2. The change at a glance

| Part | Today | After |
| --- | --- | --- |
| World: drops (`loot/ground.go`) | A drop has a killer and a 10-second loot window. A player's `PickUp` reserves the nearest one in range, and a worker grants it. | A drop has an owner, set at the kill. The ground submits each drop's grant itself once its delay has passed. There is no range, no window, and no reservation by command. |
| World: who owns a drop | — | The killer, or the next living party member on the map in join order. Each map remembers whose turn it is per party. |
| World: a full inventory | The pickup fails; the drop is free again | The drop waits, owned. One grant per owner is retried every 2 seconds until there is room or the drop's life ends. |
| Social stream | `Membership{character_ids, members}` | Adds `party_id`, so a map can keep a turn per party |
| Game data | Loot timings are constants in `ground.go` | `data/loot.yaml`: `collect_delay` (1s), `drop_life` (60s), `room_check_every` (2s) |
| Client protocol | `PickUp`; drops have no owner; a removed drop just goes | `DropView.owner_id`; `Delta.collected_drops`; a new `Loot` message; `PickUp` unused |
| Gateway | Routes `PickUp` to the world | Answers `PickUp` with nothing and calls no service; `pickup_busy` and `pickups_unavailable` retire |
| Client | Pickup key, pickup hint, one chat list | No pickup key; drops fly to their owner; chat box tabs General, Party, Combat; a combat log; a toast for 4★ and up |
| Item service | `Add` per pickup | `AddMany`: several drops for one owner in one transaction, each under its own key and savepoint; `Add` stays |
| World: grant calls | One call per pickup | At most one call in flight per owner; the drops that came due meanwhile go together in the next (section 5) |

## 3. Who owns a drop

`Ground.Roll` (at mob death, on the tick) now takes the eligible members as well as the killer, and gives each drop an owner as it rolls it:

- **Eligible members:** the killer's party members (`party.Copy.Members`, in join order) who are on this map and alive. This is the set `shareExp` already uses for EXP (`instance/progress.go`). Alone, or with no eligible member besides the killer, every drop goes to the killer.
- **Round-robin:** the instance keeps one turn per party: the last recipient and the member order it saw then. A kill's drops go one by one to the next eligible member after the last recipient, wrapping around. A kill with three drops and three members gives one each.
  - **The turn carries over** from kill to kill.
  - **A member who leaves** doesn't reset it: the next member is found from the order seen before they left.
  - **A dead member is skipped**, and so is one on another map.
- **The party ID:** the social service's party stream gains `party_id` in `Membership` (internal, additive), and the world's party copy keeps it, so the turn is keyed by party and not by a member list that changes.
- **Turns are per map:** members on two maps take turns separately, which matches the rule that only members on the map are eligible.
- **A world restart** loses the turns and starts each party again from its first member. Nothing else depends on them. A map also forgets a party's turn it hasn't used for 10 minutes.
- **Saved with each drop at the kill:** the owner's account ID, which scopes the idempotency key, and the owner's delivery sink. A grant made after the owner has left the map still reaches their connection, through `tell` as today.

## 4. Collecting a drop

A drop goes through these states, all inside `Ground` on the map's tick:

| State | What happens |
| --- | --- |
| **Falling** | From the kill until `collectAt` = kill tick + `collect_delay`. Everyone sees it on the ground. |
| **Collecting** | From `collectAt` the drop is due. Its owner's due drops go to the item-call pool as one batch (`item.AddMany`, section 5), each with its key, template, quantity, and roll. When the pool's queue is full, they just try again next tick; nobody is told, since nobody asked. |
| **Collected** | The result comes back through the inbox (`Settle`). The drop leaves the ground, listed in this tick's `collected_drops`, and the owner gets `InventoryChanged` and a `Loot` (collected). Party members on the map get the same `Loot`, naming the owner. |
| **Waiting for room** | The item service answered that the drop's tab is full (`equip_tab_full`, `use_tab_full` or `etc_tab_full`). The drop stays on the ground, owned, and the owner gets one `Loot` (waiting). The owner's waiting drops are retried together, as one batch, every `room_check_every`, counted from the last answer, and they go along with any other call the owner makes meanwhile. A player with a full inventory therefore costs one item-service call every 2 seconds, however many drops wait. |
| **Refused** | The item service refused it by another rule, such as a bad roll or an unknown item. No retry passes such a rule, so the drop is lost at once, and the world logs the reason. |
| **Unsettled** | The item service didn't answer, so the grant may have committed. As today, it is retried under the same key: every `room_check_every`, and once more as the drop's life ends. A drop past its life whose call is still out waits for the answer, and one never sent, the pool being full, still gets that one try. The key makes every retry safe. |
| **Lost** | `drop_life` passed with the drop still waiting. It leaves the ground (`removed_drops`, not `collected_drops`), and the owner gets a `Loot` (lost). |

- **What goes:** `PickUp`, `Picker`, `pickupRange`, `lootWindow`, `reservedBy`, the `pickMsg` inbox message, `PickupFailed`, `ErrPickupBusy` and `ErrPickupsUnavailable`. `Settle` stays, now once per call, settling each drop from its own outcome, and so does the single quick retry on a worker. The inventory read after a lost reply goes too: a retried batch's already-granted drops answer their stored replies.
- **Logged out or reconnecting:** the grant still commits, since it needs only the account and character IDs. The `InventoryChanged` and `Loot` go to a connection that no longer exists and are dropped. The player sees the item in the next Welcome.
- **Death of the owner** doesn't change anything: ownership was settled at the kill.

## 5. Batching grants

With auto pickup every drop is a write: at least as many as before, and more wherever players used to leave drops on the ground. Without batching that is one transaction and one call per drop. Batching groups an owner's drops into one call and one transaction, so under load the calls stay near the number of owners collecting, not the number of drops. The step 6 bot run (4 bots for 30 minutes: every call carried one drop, `AddMany` p99 22.9 ms) shows it adds no delay at low load; its gain under heavy load is not measured yet.

### In the world: one call in flight per owner

- **The rule:** an owner never has more than one grant call in flight. When that call's result comes back through the inbox, every drop of theirs that came due meanwhile goes in the next call, oldest first, up to `item_calls.batch_max` (world config, 32 to start).
- **No waiting at low load:** when nothing is in flight, a due drop goes at once, alone or with the other drops of the same kill. The 1-second delay is never stretched to fill a batch.
- **Under load, batches grow by themselves:** the slower the item service answers, the more drops each call carries. Calls per second stay near the number of owners who are collecting, not the number of drops.
- **The same rule covers waiting drops:** the owner's waiting drops are the batch retried every `room_check_every`, and they ride along in any call the owner makes before then. Drops that are still unsettled go back into the owner's next batch under their own keys.
- A metric, `finga_item_calls_batch_size` (a histogram), shows how full the batches run.

### In the item service: `AddMany`

- **`AddMany{actor, drops}`**, where each drop is `{idempotency_key, template, quantity, stats, cause}`, all for the one owner in `actor`. It runs one transaction:
  1. Every drop's key lock, sorted, then the owner's lock and inventory rows. `Add` takes its key's lock before the owner's, so no batch and no `Add` can wait on each other in a circle.
  2. For each drop, in order, a savepoint and then the existing per-key `idempotencypg.Do` under that drop's key, with the same fingerprint as `Add` (`item.add/1`). A drop granted once, by `Add` or by an earlier batch, answers its stored reply; a new one runs `Add`'s body.
  3. A drop that fails a rule, such as a full tab or a bad roll, rolls back to its savepoint, so a half-filled stack never stays, and stores no reply. The other drops carry on. A malformed key or cause is refused for that drop alone, too.
  4. One inventory revision for the whole batch. The first drop that changes something takes it, so each drop's stored reply carries it, and a batch that grants nothing takes none.
- **The answer** has, per drop, its outcome: `GRANTED`, `ALREADY_GRANTED`, or `REFUSED` with the rule's reason. It also has the slots this call's grants changed, each as the last of them left it, and the batch's revision. An already-granted drop carries its stored reply, slots and revision, as `Add` would replay it. The world settles each drop from its own outcome and sends the owner one `InventoryChanged` for the batch's grants; a replayed reply goes as its own, which the client applies only where it is newer.
- **Keys stay per drop**, and that is what makes a batch safe to retry. A batch retried after a lost reply carries the same keys, and its drops may have been regrouped with new ones. Each key answers for its own drop: one item per drop, whatever the grouping.
- **Errors that aren't a drop's own,** such as the database or the network, fail the whole call. All its drops become unsettled and are retried under their keys, as a single grant is today.
- **Size:** at most 32 drops per call; the service refuses an empty or larger batch (`bad_batch_size`), and the world's `batch_max` can't be set above it.
- **`Add` stays,** for other callers and for a world that is still the old version during a deploy. The item service deploys first, as for equipment.

At the load-test target of 1,000 players on one node, the drops come to the low hundreds a second. With batching, the calls stay below the number of owners collecting at any moment, well within the pool of 8 workers and its queue of 1,024 jobs. `finga_item_calls_inflight`, the batch-size histogram, and the item service's transaction latency show the margin.

## 6. Protocol

### Client protocol (`api/proto/client/v1/client.proto`), all additive

| Message | Change |
| --- | --- |
| `DropView` | Gains `owner_id` (character ID), so every client knows where a drop will fly |
| `Delta` | Gains `repeated uint32 collected_drops`: drops that left the ground because they were collected. `removed_drops` keeps meaning "gone": expired or lost. |
| `Loot` (server) | New: `kind` (`COLLECTED`, `WAITING`, `LOST`), `map_id`, `drop_id`, `owner_id`, `owner_name`, `template`, `quantity`. Drop IDs are a map's own, so `map_id` tells two maps' drops apart. `COLLECTED` goes to the owner and to party members on the map; `WAITING` and `LOST` go to the owner only. |
| `PickUp` (client) | Marked deprecated; the gateway answers it with nothing and calls no service, so a press from an older client does nothing. Unrouted, it would answer `not_implemented`, which an older client shows as a toast. |

- EXP and level-ups need nothing new: the combat channel reads the existing `ExpGained` (a negative amount is the death penalty) and `LevelUp`.
- Error codes `pickup_busy` and `pickups_unavailable` retire: nothing answers them any more. `codes.json` and the client's `errorCodes` lose them in the same commit.
- Golden files in `server/testdata/contracts/` cover the new and changed messages; `make breaking` passes.

### Internal contracts

- `social.v1`: `Membership` gains `party_id`.
- `world.v1`:
  - the `PickUp` rpc is deprecated: it answers and does nothing, for a gateway that still routes the key;
  - `pickup_failed` and the reasons `pickups_unavailable` and `pickup_busy` are deprecated, never sent;
  - `DeliverResponse` gains `Loot`;
  - the delta frame gains `collected_drops`;
  - `DropView` gains `owner_id`.
- `item.v1`: new `AddMany` (section 5); `Add` unchanged.

## 7. Client

By layer:

- **protocol:** regenerated; `errorCodes` loses the two pickup codes.
- **state:**
  - a `combatLog` store, holding up to 200 lines, fed by `ExpGained`, `LevelUp`, and `Loot` in `app/wiring`;
  - in the same store, the player's drops waiting for room, by map and drop ID, for the HUD notice:
    - a `WAITING` adds one, and a `COLLECTED` or `LOST` for it takes it away;
    - a reconnect keeps them, since the world tells an owner on its map through their new connection;
    - one never heard of again, as after a world restart, goes `drop_life` after its `WAITING`, since no drop waits longer. The client's game data gains `loot.yaml` for that;
  - the `chat` store unchanged.
- **input:** the `pickup` action and its keys (Z, K) go from both presets.
- **game:**
  - the pickup system and its hint go;
  - a drop sprite listed in `collected_drops` flies to its owner's sprite, or fades if the owner isn't on the map;
  - one listed in `removed_drops` fades where it lies;
  - a drop waiting for its owner stays put.
- **features:**
  - The chat box gains the tabs General, Party, and Combat. General shows map and party lines, Party only party lines, Combat the combat log. Typing sends map chat from General and Combat and party chat from Party, and `/p` still works everywhere. New lines never switch the tab.
  - Item names in combat lines take their star colour.
  - A `Loot` (collected) of 4★ or more also pushes a toast, naming who got it: the player or a party member.
  - The HUD shows "Inventory full: N waiting" while the count is above zero.

## 8. When things fail

| Failure | What the player sees | Why it is safe |
| --- | --- | --- |
| One drop of a batch doesn't fit | That drop waits; the others arrive | Its savepoint rolls back alone, and it stores no reply |
| A batch's reply is lost, then it's retried with new drops added | Nothing unusual | Each drop answers by its own key: the old ones their stored replies, the new ones fresh grants |
| Item service down | Drops stay on the ground a little longer, then fly | Grants are retried under each drop's key; nothing is lost while the drop lives |
| Item service down for longer than a drop's life | The drop is lost, as a pickup missed today would be | The last try under its key either committed or didn't; never twice |
| Reply lost after the commit | Nothing unusual | The retry under the same key gets the stored reply |
| World crashes between the commit and `Settle` | A short freeze; the item is in the inventory at the rejoin | The item service is the record; the drop, in memory, goes with the crash |
| World crashes before a drop is collected | That drop is gone, as today | Drops live only in memory; an item exists once its grant commits |
| Pool queue full | Collection waits a tick or two | The drop retries on the next tick |
| Owner logs out mid-flight | The item is there at the next login | The grant needs no connection |

## 9. Currency, when F1 arrives

Nothing is built for currency now. When it is, a currency drop in a party becomes one share per eligible member at the kill, with the remainder to the killer, the way `shareExp` splits EXP. Each share is a drop of its own, with its own owner and key, so it takes exactly the path above. Its grant credits the wallet instead of adding an item (currency joins the item service, beside items, as the services split planned), and `Loot` gains a currency amount.

## 10. Testing

- **Game data:** `loot.yaml` validation: positive durations, and a collect delay and a room check both shorter than the drop's life.
- **Item service, `AddMany`:**
  - a batch of new drops;
  - a batch mixing granted and new keys;
  - a drop that half-fills a stack and then doesn't fit, rolled back alone;
  - a reused key with other arguments refused for that drop only;
  - one revision per batch;
  - a batch over the limit refused.
  - The rapid property test gains `AddMany` among its operations, with checks 5 to 12 after every run.
  - A concurrency test runs a batch and a single `Add` of the same key at once: one item.
- **World, `Ground` and instance tests with a fake item client:**
  - ownership: solo; three members in join order; a dead member skipped; a member on another map skipped; a member who leaves mid-turn; one kill with several drops;
  - collection: at `collect_delay` and not before; the pool full, then a retry next tick; a full inventory waiting, one call per owner every 2 seconds, collected after room appears; lost at the end of the drop's life; unsettled and retried under the same key; the last try on expiry;
  - batching: never two calls in flight for one owner; drops due meanwhile go together in the next call, oldest first, up to `batch_max`; a lone drop goes at once; per-drop outcomes settle each drop separately;
  - an owner who left the map or logged out still gets the grant;
  - `Loot` and `collected_drops` go to the right players.
- **End to end** (`server/test/loot_e2e_test.go`, rewritten), one row per D1 and E3 rule:
  - one item per drop with no command sent;
  - two party members taking turns;
  - a dead member skipped;
  - a full inventory: the drop waits, a discard makes room, and the drop arrives;
  - a drop lost at the end of its life;
  - a grant retried under its key;
  - the owner moving through a portal before collection.
- **The crash test** (`TestCrashDuringPickups`) keeps its shape. Three clients hunt, so collection runs constantly, and the server is killed mid-grant and restarted. Checks 5 to 12 must pass.
- **The bot** (`cmd/bot`) stops sending `PickUp` and counts `Loot` (collected). The proof-session report keeps its "pickups" column under the new name "items collected".
- **Contracts:** golden files, and `make breaking` passes.
- **Client:** Vitest for the combat log, the waiting count, and the drop's flight-or-fade decision. Testing Library for the chat tabs and where typing goes.

## 11. Build order

On `feat/auto-pickup`, based on `main` after PR #2. Each step is one commit, green on `make test`, `make lint`, and `make cover`:

1. **Game data:** `data/loot.yaml`, loaded and validated, replacing the constants in `ground.go`. No behaviour changes.
2. **Item service, `AddMany`:** the batch with savepoints and per-drop keys, its tests, and the property and concurrency tests. Nothing calls it yet.
3. **Party ID on the stream:** `Membership.party_id` from the social service, kept by the world's party copy.
4. **World, ownership and collection:**
   - owners at the kill and round-robin;
   - automatic grants batched per owner over `AddMany`;
   - waiting for room and lost drops;
   - `Loot` and `collected_drops` in the world's own model and in `world.v1`, its contract with the gateway.
   `PickUp` stops doing anything in the world.
5. **Gateway and protocol:** `DropView.owner_id`, `Delta.collected_drops`, `Loot`, `PickUp` answered with nothing, the two codes retired, contracts, and the bot.
6. **End to end and the crash test**, rewritten for auto pickup, and a `make bots` run to record the batch sizes and the item service's latency.
7. **Client, drops:** no pickup key or hint; drops fly or fade.
8. **Client, combat channel:** the combat log, chat tabs, the 4★ toast, and the HUD waiting notice.

## 12. Open questions

1. **Who sees a party member's loot.** Only party members on the same map get `Loot` for each other's drops; members elsewhere don't. They didn't take part, and the world can reach only the players on its map. Systems → Social says this.
2. **A drop's owner on the ground.** Default: the drop looks the same for everyone until it flies. Showing the owner's name on it is a later option.

Decided: batching grants is in from the start (section 5).

## 13. Alternatives considered

- **Batch on a timer** (hold due drops for, say, 250 ms): fewer calls even at low load, but every collection gets slower. One call in flight per owner adds no delay when the service keeps up and batches exactly when it doesn't.
- **One idempotency key per batch:** simpler storage, but a retried batch can be regrouped with new drops, so its key would cover a different request. Keys per drop make any grouping safe.
- **Batch across owners:** fewer calls still, but one owner's full inventory or lock would hold up everyone else's drops, and the item service's lock is per owner.

- **Grant at the kill with no ground moment:** simpler, but the drop wouldn't read as a reward, and a full inventory would need somewhere else to hold the item.
- **The client animates at the delay on its own:** the flight would then show for drops that fail (a full inventory) and have to fly back. Flying on `collected_drops` shows only what happened.
- **Pass a drop to the next member when the owner's inventory is full:** friendlier, but the world doesn't know anyone's free space, so it would mean asking the item service per member, and the product page keeps the drop with its owner.
- **Keep the turn in the social service:** one turn per party across maps, but then every kill waits on another service, or the turn is a stale copy anyway. Turns per map need nothing outside the tick.
