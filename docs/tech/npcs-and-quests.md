# NPCs and task quests

> **Status: approved · 2026-11-10.** The high-level design for [H1. NPCs](../product/features/npcs-and-quests/h1-npcs.md), [H2. Task quests](../product/features/npcs-and-quests/h2-task-quests.md), and [Systems → NPCs and quests](../product/systems/npcs-and-quests.md). It builds on `main`, after the [stat system](./stat-system.md) (PR #5 and #6). It is the first of two designs: [Classes and skills](./classes-and-skills.md) builds on it, since promotions happen at NPCs and need trial quests.

## 1. What has to hold

From the product pages:

- **NPCs:**
  - They stand at set places in towns and hunting maps, the same for everyone.
  - Talk (Z in Classic, E in WASD), within reach, opens a dialog with what the NPC offers.
  - A marker shows when an NPC has something for this player.
- **Task quests:**
  - Objectives are kills, items to collect, an NPC to talk to, or a level to reach.
  - Accepting needs the quest's conditions; at most 20 are active.
  - Progress survives maps, deaths, and logouts, and is shown in the quest log and the combat channel.
  - Kills count for everyone who shares the kill's EXP.
  - Handing in takes the collected items and gives the rewards at once, or, when an item reward wouldn't fit, takes nothing.
  - Quests are one-time or repeatable, and finished quests are remembered.
- **Integrity:** items handed in and item rewards are never lost or duplicated, and their history names the quest.

From the code, the rules this design keeps:

- **The tick never waits on another service.** Item changes run on the item-call pool, and their results come back through the inbox.
- **No transaction spans two services.** A hand-in touches items (item service) and quest state (character, through the world). The design makes the item side idempotent, so the two can be applied one after the other safely.
- **The client computes, the server validates.** Whatever is display-only, and derivable from data the client already holds, the client works out itself, so the server can serve more players. Markers and collect progress are examples. The server checks every action, and computes whatever grants or changes state: whether a quest can be accepted or handed in, and whether a promotion is open.

## 2. The change at a glance

| Part | Today | After |
| --- | --- | --- |
| Game data | Maps with portals and spawns | `data/npcs.yaml`, `data/quests.yaml`, and an `npcs` object layer in each map's Tiled file |
| World | Players, mobs, drops | Also NPCs (static, from the map), Talk, the NPC dialog, and the quest log per player: accept, abandon, progress, hand-in |
| Character service | Level, EXP, points | Also each character's quests: active ones with progress, and finished ones with a count |
| Item service | `Add`, `AddMany`, `Move`, `Discard`, `Equip`, `Unequip` | Adds `HandIn`: take some items and give others, in one transaction, under one key |
| Client | — | NPCs drawn from map data, the Talk key, the NPC dialog, the quest log (Q), markers, progress lines in the combat channel |

## 3. Game data

- **`data/npcs.yaml`:** one entry per NPC: `id`, `name`, `look` (a sprite key), and what it offers:
  - `trainer: <class>` (used by the classes design);
  - `gives: [quest ids]`;
  - `takes: [quest ids]`, the quests handed in to it.
- **Placement:** an `npcs` object layer in each map's Tiled file, one point object per NPC, named by the NPC's `id`. An NPC can stand in a town or a hunting map, and in one place only.
- **`talk_reach`** (in `npcs.yaml`, 60 px to start): how close the character's centre must be to the NPC's point.
- **`data/quests.yaml`:** one entry per quest:
  - **who:** `id`, `name`, `giver` (an NPC), and `turn_in` (an NPC, often the giver);
  - **conditions:** a minimum `level`, a `class` (or any class below it in the class tree), and `quests` that must be finished first;
  - **objectives:** a list, each one of `{kill: <mob>, count}`, `{collect: <item>, count}`, `{talk: <npc>}`, or `{level: n}`;
  - **rewards:** `exp`, and `items` (`[{item, quantity}]`);
  - **`repeatable`:** true or false.
- **Validation** refuses:
  - an NPC on a map but not in `npcs.yaml`, or one placed twice;
  - a quest whose giver or turn-in NPC doesn't list it under `gives` or `takes`;
  - an unknown mob, item, NPC, class, or required quest;
  - a cycle in required quests;
  - a count below 1;
  - an equipment reward with a quantity other than 1.
- **The client generator** emits `npcs.yaml`, `quests.yaml`, and the NPC positions per map (the client already reads the maps).

## 4. NPCs in the world

- **NPCs are static map content.** Each map instance loads its NPCs from the map at start. They aren't in Snapshots or Deltas, since the client draws them from the same map data. They don't collide, can't be attacked, and never change.
- **`Talk{npc}`** comes from the client through the gateway, with a Response, under a "dialog" rate limit (5 a second). On the tick:
  - It is refused with `npc_out_of_reach` when the NPC isn't on this map or is out of reach, or with `character_dead` while dead.
  - Otherwise the world sends the player an **`NpcDialog`**: the NPC, and what it offers this character now.
    - **quests to give:** each with its state, *available*, *active*, *finished* (one-time), or *conditions unmet*, and the unmet conditions listed;
    - **quests to take back:** each *ready* or *not ready*;
    - **promotions,** for trainers (classes design): each condition with *met* or *not met*.
  - The dialog is computed from the player's live state, so it can't disagree with what the server would allow.
- **Dialog actions** (`AcceptQuest`, `HandInQuest`, `Promote`) each name the NPC and are re-checked on their own tick: reach, state, conditions. A dialog left open while the player walks away fails cleanly with `npc_out_of_reach`.
- **Markers** are computed by the client, from its generated data and its copy of the character's state: level, class, stats, skills, the quest log, and inventory counts.
  - **When they change:** they are recomputed on the events that can change them: a level-up, points spent, a quest accepted, progressed, abandoned, or handed in, a promotion, a map change, or an inventory change.
  - **No server messages:** all of these already reach the client, so markers cost the server nothing.
  - **The dialog is the truth:** markers are display only. A marker can't grant anything.

## 5. Quest state

- **Stored by the character service,** in a new table `character_quests`:
  - `character_id`, `quest_id`, `status` (`active` or `finished`);
  - `progress int[]`, one count per objective;
  - `times_finished`, and timestamps.
  - Active quests are at most 20 per character; finished one-time quests stay as rows with `times_finished = 1`.
- **Held live by the world.** The world loads it at join with the character (`Load` returns it), and saves it with the character's checkpoint, which carries only the changed rows. Quest progress therefore has the same durability as EXP: saved every 30 seconds and at once on accept, abandon, and hand-in. A crash loses at most the kills counted since the last save.

## 6. Progress

- **Kills:** when a mob dies, each character who gets EXP from it (the killer and the party members `shareExp` already picks) gets +1 on every active objective `{kill: that mob}`, up to its count.
- **Talk:** a `Talk` to an NPC named by an active `{talk}` objective completes it.
- **Level:** read from the live level when needed. It needs no counter.
- **Collect:** counted by the item service when the quest is handed in, since the world doesn't hold inventories. The client shows the progress ("Slime Gel 7/15") from its own inventory store, for display.
- **Telling the player:** each change sends the owner a **`QuestChanged`** (the quest's status and progress), which the quest log applies and the combat channel turns into a line. Kills are merged per tick, so a burst of kills makes one message.

## 7. Accepting, abandoning, and handing in

- **`AcceptQuest{npc, quest}`:** the NPC must give it and be in reach, and the quest's conditions must be met. It is refused when the quest is already active, a one-time quest is already finished, or 20 are active. On success the quest becomes active with zero progress, and is saved at once.
- **`AbandonQuest{quest}`:** no NPC is needed. The quest leaves the log, its progress is lost, and the change is saved at once.
- **`HandInQuest{npc, quest}`:**
  1. **On the tick:** the NPC must take this quest and be in reach. Every kill, talk, and level objective must be complete, or the hand-in is refused with `quest_not_ready`.
  2. **Rolls:** equipment rewards are rolled now, with the map's seeded generator, as drops are.
  3. **The item call:** a job goes to the item-call pool: `item.HandIn` with the collect objectives to take and the rewards to give.
     - Its idempotency key is derived from the character, the quest, and `times_finished + 1` (a UUIDv5). A retry, or a hand-in repeated after a crash, gets the same key.
  4. **The result comes back through the inbox:**
     - on success: the quest is marked finished, `times_finished` goes up, the reward EXP is awarded in memory, and the change is saved at once. The player gets `InventoryChanged`, `QuestChanged`, and `ExpGained`;
     - on `not_enough_items` or `inventory_full`: nothing changed anywhere, and the player is told.
- **Repeatable quests** finish with a new count each time, so each completion has its own key.

## 8. Item service: `HandIn`

- **`HandIn{actor, idempotency_key, take: [{template, quantity}], give: [{template, quantity, stats}]}`** runs one transaction:
  1. The owner's lock and inventory rows, as other item calls take them.
  2. **Take:** each `take` line must be covered by the inventory's stacks of that template, never by worn items, or the call is refused with `not_enough_items`. The items are taken from the smallest stacks first.
  3. **Give:** each `give` line goes in as `Add` would place it. If it doesn't all fit, the call is refused with `inventory_full`, and nothing is taken.
  4. **Item events:** a new kind, `handed_in` (a negative quantity, cause `quest`), for what is taken; `created` and `stacked` with cause `quest` for what is given.
  5. One inventory revision, and the stored reply under the key, as for `Add`.
- **Check 6 changes:** "per template, held equals drops and starters less discards" becomes "held equals drops, starters, and quest rewards, less discards and hand-ins".

## 9. Protocol

### Client protocol, all additive

| Message | Direction | Change |
| --- | --- | --- |
| `Talk` | client | New: `npc`; answered with a Response, after an `NpcDialog` |
| `NpcDialog` | server | New: the NPC, its quests to give and take with their states and unmet conditions, and its promotions with each condition met or not |
| `AcceptQuest`, `HandInQuest` | client | New: `npc`, `quest`; Response |
| `AbandonQuest` | client | New: `quest`; Response |
| `QuestLog` | server | New, in `Welcome`: active quests with progress, and finished quests with counts |
| `QuestChanged` | server | New: one quest's status, progress, and count |

- **New error codes:**
  - `npc_out_of_reach`: "move closer";
  - `quest_unavailable`: "that quest isn't offered here";
  - `quest_conditions_unmet`: "you don't meet that quest's conditions yet";
  - `quest_already_active`;
  - `quest_already_finished`;
  - `too_many_quests`: "you have 20 quests already";
  - `quest_not_ready`: "that quest isn't finished yet";
  - `not_enough_items`: "you don't have everything it asks for".
- **Gateway routes:** `Talk`, `AcceptQuest`, `AbandonQuest`, and `HandInQuest` go to the world under a new dialog limit (5 a second), with `respond`.

### Internal contracts

- **`world.v1`:** `Talk`, `AcceptQuest`, `AbandonQuest`, and `HandInQuest`, unary. The world's handler waits for the tick's outcome, as `Equip` does.
- **`item.v1`:** `HandIn`.
- **`character.v1`:** `Load` returns the quests, and `SaveCheckpoint` carries changed quest rows.

## 10. Client

- **game:** NPC sprites with name labels, drawn from the generated positions, and markers above them.
- **Talk key:**
  - The Talk key (Z in Classic, E in WASD) finds the nearest NPC within `talk_reach` and sends `Talk`; with none in reach it does nothing.
  - The Z key in Classic was pickup's. The `input` presets gain a `talk` action.
- **The NPC dialog** (`features/npc`):
  - It lists what `NpcDialog` offers, with unmet conditions greyed and explained.
  - Accept, Hand in, and Promote send their requests, and close the dialog on success.
  - Walking out of reach closes it.
- **The quest log** (`features/quests`, Q): active quests with each objective's progress. Collect objectives are counted from the inventory store. Each quest has an Abandon button.
- **The combat channel** gains lines for quest progress, quests ready to hand in, and quests finished.

## 11. When things fail

| Failure | What the player sees | Why it is safe |
| --- | --- | --- |
| A `HandIn` reply is lost; the pool retries | Nothing unusual | Same key: the stored reply comes back, and nothing is taken or given twice |
| World crashes after the item commit, before the quest is saved | After the rejoin the quest is still active; handing it in again finishes it | Same key (the count didn't move), so the item service answers its stored reply and gives nothing twice |
| That crash also lost some kills since the last save | The quest may need a few kills again before it can be handed in | Progress is saved like EXP; nothing is duplicated |
| The player walks away with the dialog open | The action is refused with "move closer" | Every action re-checks reach |
| Item service down during a hand-in | "Try again in a moment"; nothing changed | The call failed as a whole, or committed and answers the same next time |

## 12. Testing

- **Game data:** the validation rows, including NPCs in maps, cross-references, and quest cycles.
- **Item service `HandIn`:**
  - take and give;
  - not enough items;
  - a reward that doesn't fit, with nothing taken;
  - worn items never taken;
  - smallest stacks taken first;
  - one revision;
  - a retry under the same key returning the stored reply;
  - the property test with `HandIn` among its operations, and check 6 with hand-ins and quest rewards.
- **Character service:** quests stored, loaded, and saved by changed rows.
- **World:**
  - `Talk` in and out of reach, and the dialog's states;
  - accept: its conditions, the 20-quest limit, and one-time quests;
  - abandon;
  - kill progress for the killer and for the EXP sharers, and not for a dead member or one on another map;
  - talk objectives;
  - hand-in: not ready, success, `not_enough_items`, and a full inventory;
  - a hand-in repeated after a simulated crash, getting the same key;
  - repeatable quests with a new key each time.
- **End to end:** accept a quest, kill, collect, hand in, see items taken and given; a party kill counting for both members; a hand-in refused for a full inventory.
- **Client:** Vitest for markers and collect progress; Testing Library for the dialog and the quest log.

## 13. Build order

First of the two designs, on its own branch and PR after PR #5. Each step is one commit, green on `make test`, `make lint`, `make cover`, and `make breaking`:

1. **Game data:** `npcs.yaml`, `quests.yaml`, the `npcs` map layers, validation, and the client generator.
2. **Item service:** `HandIn`, the `handed_in` event, and the new check 6.
3. **Character service:** `character_quests`, `Load`, and the checkpoint rows.
4. **World, NPCs:** static NPCs, `Talk`, and `NpcDialog` (quests only; promotions arrive with the classes design).
5. **World, quests:** accept, abandon, progress, and hand-in through the item-call pool.
6. **Gateway and protocol:** routes, messages, error codes, and contracts.
7. **Client:** NPCs, the Talk key, the dialog, the quest log, markers, and combat-channel lines.
8. **Content:**
   - the town guide's starter quests;
   - the four trials;
   - the four trainers placed on their maps (they offer their trial quests until promotion arrives);
   - a `make bots` run with bots that accept and hand in a quest.

## 14. Decisions

Agreed on 2026-11-10:

1. **The dialog is computed by the server, and markers by the client,** following the rule that the client computes and the server validates. The client can't open a promotion or a quest the server wouldn't. Markers update on events the client already receives, and the client holds the exact inventory, which the world doesn't.
2. **Quest progress is saved like EXP** (checkpoints, and at once on accept, abandon, and hand-in), so a crash can cost a few counted kills, never items.
3. **A hand-in's key is derived from the character, the quest, and the completion count,** so retries and repeats after a crash are safe without storing anything extra.
4. **Collect progress is counted by the item service** at hand-in, and shown by the client from its inventory.
5. **`talk_reach` is 60 px.**

## 15. Alternatives considered

- **A separate quest service:** quest progress changes inside the tick on every kill, so a service would mean a stream of calls the tick can't wait on. Kept with the character's progress instead, which the world already holds and saves.
- **Quest state in the item service, to make the hand-in one transaction:** it would mix two owners. The derived key gives the same safety across the two.
- **The world counting collected items:** the world would need every inventory, or a call that waits. The item service counts at hand-in, where it matters.
- **NPCs sent in Snapshots:** they never change, and the client already has the maps.
- **Server-pushed markers:** they would be event-driven and cheap: recomputed on the same events, and sent only when a marker changes.
  - **The catch is inventory:** moves and discards go straight from the gateway to the item service and never pass through the world. The world couldn't tell when a discard leaves a "bring N items" quest short again, unless every inventory change were also sent to it, or it asked the item service for counts.
  - **The client already knows:** it holds the exact inventory and gets every one of those events already.
