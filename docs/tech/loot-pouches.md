# Loot pouches and timed bosses

> **Status: approved · 2026-11-19. Steps 1 and 2, timed bosses and pouches, as built. Step 3, slower camps and fewer pouches, approved 2026-11-20.** The high-level design for the changes to [D1. Drops and loot](../product/features/items-and-inventory/d1-drops-and-loot.md) and [C10. Field bosses](../product/features/combat-and-progression/c10-field-bosses.md), with E3's party loot and E5's lines. It replaces auto pickup ([Auto pickup and the combat channel](./auto-pickup-and-combat-channel.md) sections 3 to 5) and the boss's kill count ([Field bosses](./field-bosses.md)).

## 1. What has to hold

- **The user's words** (2026-11-19):
  - "change the boss appear condition to a certain place every map, respawn every 5 mins";
  - "boss should drops a treasure box, player chooses what to pick, player goes near the box, press T to pick";
  - "same as mobs, it drops a pouch, decrease the drop rate so that players dont have to pick too many times".
- **Their choices:**
  - A boss stands at its spot until it's beaten, and comes back 5 minutes later. It never leaves on its own.
  - Only players on the boss's map, or in its area of a region, hear that it appeared.
  - Less loot overall: fewer kills drop a pouch, and a pouch holds what one kill drops.
  - Items that quests collect come in pouches like the rest.
- **The user's words** (2026-11-20): "make mob spawn slower, leave time for player to loot the pouch and decrease drop rate again".
- **Their choices:**
  - Camps bring mobs back three times as slowly: today's 8 to 15 s become 24 to 45 s. Field bosses stay at 5 minutes.
  - A pouch on 1 kill in 6, and 1 in 3 for an elite, a third fewer than 1 in 4 and 1 in 2. A pouch still holds what one kill drops.
- **The client computes, the server validates:**
  - The client knows its pouches and their contents, works out which are in reach, and draws the window.
  - The server decides every roll and owner, checks reach and ownership on each take, and grants.
- **Nothing is lost by mistake:** an item is created only when taken, under its own idempotency key, as drops are today.

## 2. Today

- **Drops:**
  - Each kill scatters its drops round the body, each owned by the killer or the next party member in turn.
  - After `collect_delay` (1 s) each flies to its owner, wherever they are. One grant call is in flight per owner, batched.
  - A full tab leaves a drop waiting, retried every 2 s for its 60 s life.
  - Everyone sees drops. There is no pickup key, and `PickUp` is unused.
- **Bosses:**
  - A map counts kills to its `boss.kills` (300 in the forest), then the boss appears at its circle.
  - It leaves after `field_boss.life` (10 min) unbeaten.
  - Every online player hears it appear, through social's `Announce`.
  - Each eligible player gets their own roll of its drop list.

## 3. Data

- **`loot.yaml`:**

  ```yaml
  pouch: {normal: 0.1667, elite: 0.3333}  # the chance a kill drops a pouch: 1 in 6, and 1 in 3 for an elite; a boss always leaves each eligible player a box
  reach: 2            # metres from the character's middle to a pouch or box to open it
  pouch_life: 60s     # a pouch still holding items then is gone
  box_life: 2m        # a boss's box still holding items then is gone
  pouches_max: 30     # a player's pouches and boxes on a map at most: a new one ends the oldest's life (added as built)
  ```

  `collect_delay` and `room_check_every` go.
- **`mobs.yaml`:**
  - `field_boss` becomes `{respawn: 5m, drop_share: 0.01}`, and `life` goes.
  - Drop lists are unchanged. The pouch chance thins them.
- **Maps:** `boss: {at, radius, mob}`, and `kills` goes. A region's area names its boss's circle the same way (large-regions.md section 6).
- **Camps** (step 3): every camp's `respawn` in `maps/` and `regions/` is three times what it was: 8 s becomes 24 s, 9 s 27 s, 10 s 30 s, 12 s 36 s and 15 s 45 s.
  - The shortest, 24 s, is several times the few seconds it takes to walk to a pouch and take it, so the camp doesn't refill on a player still looting.
  - A test over the shipped maps and regions holds every camp to at least 24 s, so camps added later with a region's layout keep the room to loot. Test fixtures keep their own times, so tests don't wait.
- **Loader rules:**
  - `pouch` chances must be in (0, 1].
  - `reach` must be at least the talk reach (1.875 m), so a pouch at a player's feet is always in reach.
  - Both lives must be positive, and `respawn` too.
  - `pouches_max` must be at least 1.

## 4. Pouches in the world

- **The roll** (`loot/ground.go`, at the kill, on the tick):
  1. The owner is found as today: the killer, or the party's next living member on the map. The turn now moves once per pouch, not per item.
  2. The pouch's chance is rolled: `pouch.normal`, or `pouch.elite` for an elite.
     - If it passes, the drop list is rolled as today: chance, quantity, and stats for equipment.
     - If it fails, only the items the owner's active quests still need are rolled, at their own chances.
     - So a quest takes no more kills than before, and its items still come in a pouch.
  3. If anything came of it, one pouch falls where the mob died, holding it all.
- **A boss's boxes:** each player whose share reached `drop_share` gets their own box, holding their own roll as today, where the boss fell. A box is never gated, and party turns don't apply.
- **A pouch is its owner's alone:**
  - Only the owner is sent it, in their private part of the frame (large-regions.md section 5), so nobody else sees it.
  - Its contents come with it: each item's template, quantity, and rolled stats, so the window and item cards need no round trip.
- **Taking** (`TakeLoot{picks}`, each pick a pouch and an item in it):
  1. The world checks that the player is alive, every pouch is theirs, and each is within `reach` of the player's position on the server, plus 0.5 m for the client's lead. Otherwise it's `POUCH_GONE`, `POUCH_OUT_OF_REACH` or `CHARACTER_DEAD`.
  2. The items picked go to the item module as one `AddMany` batch, each under its pouch item's key (cause `drop`). The one-call-in-flight rule per owner stays.
  3. **Granted items** leave the pouch. The owner gets `InventoryChanged` and `Loot{taken}`, and party members on the map get the same `Loot`, naming them.
  4. **An item whose tab is full** stays in the pouch, and the answer names it with `*_TAB_FULL`.
  5. **An emptied pouch** is gone at once.
  6. **No answer from the item module:** the items stay marked as in flight and are retried under the same keys, as unsettled drops are today.
- **Taking in combat** is allowed. A dead player can't take.
- **Life:** a pouch is gone `pouch_life` after it fell, and a box after `box_life`, with whatever it still holds. The owner gets `Loot{left}` listing it. Nothing was created, so nothing is lost from the item history.
- **What goes:**
  - flying to the owner and the collect delay;
  - waiting for room and its retries;
  - `DropView.owner_id`, `collected_drops` and the unused `PickUp`.
- **A world restart** loses pouches on the ground, as it loses drops today.
- **As built (step 2):**
  - `loot.Ground` holds a map's pouches (`loot/ground.go`) and their takes (`loot/take.go`). A pouch lies on the ground under where the mob died. Its items are numbered from 1 within it, each with its own key from the roll.
  - **The turn moves only when a pouch falls.** A kill that leaves nothing keeps the party's turn where it was.
  - **"Still needs" is "an active quest of the owner's collects it".** The world holds each player's quest log but not their bag. So a kill whose chance failed rolls a quest's item until the quest is handed in, even once the player holds enough. That costs a few extra pouches, never a missing item.
  - **A box** is never gated: `Ground.Box` rolls the boss's whole list for each player given one.
  - **Each pick is checked on its own,** and the rest of the take goes on:
    - `POUCH_GONE`: a pouch that's gone, empty or not the taker's; an item not in it, or already taken; or the same item twice in one take.
    - `TAKE_BUSY`: an item already being taken, by a call in flight or a take waiting.
    - `POUCH_OUT_OF_REACH`: a pouch farther than reach plus 0.5 m from the character's middle, in 3D.
  - **One call in flight per owner, and one take waiting:**
    - A take while a call is in flight waits for it.
    - A further take while one waits is `TAKE_BUSY` (330041), unless every pick is from the waiting take's pouches. Then it joins it, so pressing T again never queues up calls.
    - A call carries at most `batch_max` (32) items. Picks past that are `TAKE_BUSY`, left for the next take.
    - A `TakeLoot` names at most 64 picks, or it's refused whole as malformed.
  - **Each pick is answered once its call settles:** taken, or the code of why not.
    - A full tab answers with its tab: `EQUIP_TAB_FULL`, `USE_TAB_FULL` or `ETC_TAB_FULL`.
    - **No answer from the item module:** the pick is answered `UNAVAILABLE`. Its items stay marked as being taken, and are asked for again every 2 s under the same keys until an answer comes. The owner then hears `InventoryChanged` and `Loot{taken}`, as from any take. Only owners with such items are looked at for a retry.
      - As built: a grant call, its retry included, has 3.5 s. That's inside the 5 s the gateway gives a take, so each pick is answered rather than the whole take failing at the gateway's deadline.
    - **Every item-call worker's queue full:** the take is answered `UNAVAILABLE` at once, and its items stay to take. A retry that finds the queue full waits another 2 s.
    - **An item refused by another rule** is given up and logged, answered `POUCH_GONE`.
  - **Lives:** at a pouch's end, the items not being taken are listed in `Loot{left}`. An item still being asked for then gets one more try; with no answer it's given up and logged.
    - **A given-up item's outcome is unknown:** its grant may have committed before the answer was lost. It's answered `POUCH_GONE` either way. Its key keeps it from ever being granted twice, so the player has it at most once: in their bag when they next load it, or not at all.
    - **An owner off the map hears the left notice through the connection they last joined it by.** One who has since logged out or reconnected misses it. That's cosmetic: the pouch was theirs alone, and nothing was granted.
  - **At most `pouches_max` (30) pouches and boxes per owner on a map.** A new one past that ends the oldest's life in the same tick, as its life ending would: what's left in it is `Loot{left}`, and an item in a call still settles. This bounds a party left farming, or a pile of boxes. Pouches are held per owner and by id, so a take, a join's `Snapshot` and a retry look only at that owner's own.
  - **Private frames:** each recipient of a frame carries its own pouches that changed (`model.Recipient.Pouches`), as it carries its ack and energy. The gateway splices them into that player's `Delta` beside the shared part, which stays encoded once. A joiner's `Snapshot` lists their own pouches.

## 5. Timed bosses

- **Up from the start:** a map's instance, or a region's channel, raises each of its bosses as it starts.
- **Beaten:**
  - Rewards are paid as today, with the drops in boxes.
  - The instance notes the tick `respawn` later, and raises the boss again at its spot then.
  - The kill count, and kills not counting while a boss is up, go.
- **No leaving:** a boss stays until it's beaten. `life`, the leave check and `FieldBossGone{beaten: false}` go.
- **Restarts:** when it's back lives only in the instance, as mob positions do, so an instance rebuilt while its boss is down raises it at once.
- **Calm until hit** (decision 11):
  - A standing boss sees nobody. Any blow from a player wakes it, an area hit or a miss included, and so does a status or a taunt; a skill whose every status only hides its user doesn't.
  - Awake, it fights as any mob: sight, chase, attacks and warning areas. It goes after whoever hit it, and others it sees as any fighting mob does.
  - It's calm again once its reset takes it home: led past its leash, hit from beyond it, or with nobody left it sees, which includes its map emptying. On an empty map it gives up as it would with players there, walks home and calms, so whoever joins next, even beside it, is let be. A blow on the way home that still provokes it when it arrives keeps it awake. One caught on the way is put back at its spot. HP stays as it is, as for any mob. There's no calm cue.
  - Mobs in its circle are as before.
- **Who hears it:**
  - Each player on the map, or in the area in a region, gets a notice and a line in the General tab: "King Slime has appeared!"
  - The instance sends these itself, as `FieldBoss` to the map. Bosses were the only news social's `Announce` carried, so it goes whole: the `Announcement` message (field 28, reserved), social's `Announce` and the world's announce queue.
- **The bar:** name and HP. It has no time left, since a boss doesn't leave.
- **While it's down:**
  - The map window marks the boss's spot, greyed, with when it's back: "King Slime: back in 3:12". The label sits under its crown, or over it, whichever keeps 6 px clear of every other label. Where neither does, it's lifted clear of the rest as any label is.
  - `FieldBossGone{mob, back_in_ms}` and the snapshot carry the time, and the client counts down itself.
  - The client's copy of each map now carries the boss's circle. Only the kill count was hidden, and it's gone.
- **Balance:**
  - A boss's EXP is 40 times its map's top mob's, for about 3 minutes' fight alone.
  - Every 5 minutes, that's about what a player earns hunting in the same time, so it isn't a farm to retune.
  - Its gear chances, at 10 times its mob's, are rolled per eligible player every 5 minutes in place of every 300 kills, so boss gear becomes the main way to gear up.
  - The balance harness measures both after the change.

## 6. Protocol

| Message | Way | What |
| --- | --- | --- |
| `PouchView{id, kind, x, y, z, items: [{item, template, quantity, stats}], gone_in_ms}` | server, private | A pouch or box of the player's; `kind` is `pouch` or `box` |
| `Delta.pouches` and `Delta.pouches_gone` | server, private | Pouches added or changed, and ids gone |
| `TakeLoot{picks: [{pouch, item}]}` | client | Take these. The answer lists each pick taken or refused, with why |
| `Loot{kind: taken \| left, ...}` | server | The combat channel's line: taken, by the owner or a party member, or left behind |
| `FieldBoss{mob_id, type}` | server, the map | It appears, and in the snapshot while it's up |
| `FieldBossGone{mob_id, type, back_in_ms}` | server, the map | It's beaten, and when it's back |

- `DropView`, `Delta.drops`, `collected_drops`, `removed_drops` and `PickUp` go. `Loot`'s kinds `collected`, `waiting` and `lost` become `taken` and `left`.
- **Failures,** in the world's area after `TRAIT_LOCKED`:

  | Code | Name | HTTP | When |
  | --- | --- | --- | --- |
  | 330039 | `POUCH_GONE` | 409 | A pouch that's gone, empty, or not the player's; an item already taken, or named twice in one take |
  | 330040 | `POUCH_OUT_OF_REACH` | 409 | A pouch farther than reach |
  | 330041 | `TAKE_BUSY` | 409 | Added as built: an item already being taken, by a call in flight, a waiting take, or a retry after no answer, since it's still in the pouch; or a take while another already waits (section 4) |
- **As built (step 2):**
  - `TakeLoot{picks: [{pouch, item}]}` is answered in its `Response`: `ok`, with `picks: [{pick, code}]`, code 0 for taken. A take refused whole, such as a dead player's, is a failed `Response` with its code.
  - `Snapshot.pouches` (6), `Delta.pouches` (10) and `Delta.pouches_gone` (11) carry the player's own. `PouchView.kind` is `POUCH_KIND_POUCH` or `POUCH_KIND_BOX`, and each `PouchItem` carries equipment's `stats`.
  - `Loot{kind, map_id, pouch_id, owner_id, owner_name, items: [{template, quantity}]}`, with `LOOT_KIND_TAKEN` = 1 and `LOOT_KIND_LEFT` = 3. 2, the old waiting, is reserved.
  - **Gone, their numbers and names reserved:**
    - `ClientMessage.pick_up` (5) and `Snapshot.drops` (3);
    - `Delta.drops`, `removed_drops` and `collected_drops` (4, 7, 8);
    - `Loot`'s `drop_id`, `template` and `quantity` (3, 6, 7).

    `DropView` and `PickUp` are deleted.
  - **One more failure:** 330041 `TAKE_BUSY`, in the table above. An item being taken is busy, not gone, because it's still in its pouch.

## 7. Client

- **Models:**
  - A pouch is KayKit's sack, small, from the Medieval Hexagon Pack.
  - A box is KayKit's chest, from Dungeon Remastered, gold for a box holding a 4★ item or better.
  - Both are KayKit's own (CC0, the same author as the characters), recorded in `web/public/models/LICENSES.md`.
- **Hint:** a pouch or box in reach shows the `loot` key over it ("T"), as an NPC in reach shows the talk key.
- **The loot window,** `features/loot/LootWindow.tsx`:
  - The `loot` key (`KeyT`, key-bindings.md) opens it on every pouch and box of the player's within reach, as one list.
  - Each row has the item's icon, name in its star colour, and quantity. Hovering shows its card, with stats and comparison (D7).
  - Clicking a row takes it. Take all, or the `loot` key again, takes every row.
  - Esc, or walking out of reach of all of them, closes it. It closes by itself once they're empty.
  - A refused row stays, with the reason under it, such as "Etc tab full".
- **The combat channel:**
  - "Took Iron Sword ★★";
  - in a party, "Ayla took Iron Sword ★★";
  - "A pouch was left behind: Red Potion, Slime Gel ×2".
  - A 4★ or better taken still shows a toast.
- **Gone:** drops' gems and their flight.
- **The map window's** boss mark while it's down (section 5).
- **As built (step 2):**
  - **Models:**
    - The sack is 0.43 m across and the chests 0.85 m. Each is turned its own way by its id.
    - Textures are shrunk to 128 px for the sack and 256 px for the chests, every colour within 1/255.
    - The gold chest is KayKit's chest with its grey metal made gold in the source file. Its gold coins sit under a lid that stays shut, so they don't show from the game's camera. `npm run models` reproduces the output.
    - A box shows gold if it held a 4★ item when it first appeared, and stays gold after that item is taken.
  - **Reach and the hint:**
    - Reach is measured in 3D from the character's middle (feet + 0.875 m), at most `loot.reach`, with no slack on the client.
    - The "T Loot" hint sits 0.9 m over the nearest pouch in reach. It shows only while the player is free to act and the window is shut.
  - **The `loot` key** opens the window when it's shut, and takes everything when it's open. It does nothing while the player isn't free, or with nothing in reach.
  - **The window** (`features/loot/LootWindow.tsx`, state in `state/pouches.ts`):
    - It lists every pouch in reach in pouch order, and counts down to the first one's end ("Gone in 35 s").
    - A row being taken is dimmed and can't be clicked again until its item leaves the pouch.
    - A refused row stays, with its reason, until it's asked for again or its item goes. A request with no answer refuses each of its picks with that error.
    - Nothing is ever sent again on its own.
    - It closes once nothing is in reach: walked away, emptied, gone, or dead. The store clears on a map change.
  - **The words:** 330039 "That's gone already.", 330040 "Too far away.", 330041 "Still taking that."
  - **Gone:**
    - the drop gems and their flight;
    - the combat log's waiting list and the HUD's waiting notice;
    - the NPC window counting waiting drops into a reward's room.

## 8. cmd/bot

- **Step 1, as built:**
  - A bot takes each map's boss as standing unless it heard it beaten there, until its `back_in_ms` runs out or it sees it back.
  - A calm boss blocks no way, so bots no longer wait bosses out in the town.
  - A bot keeps its prey and its hunt 15 m from a boss (`bossClear`). That's past the boss's body plus either the widest area a skill covers with a bot's reach, or the longest shot that goes on past its first mob, so no blow of a bot's wakes it. A test checks the margin against the data.
- **Step 2:** bots open every pouch in reach and take all, so the soak (large-regions.md section 13) counts the takes' calls.
- **Step 2, as built:**
  - A bot walks to its nearest pouch within 20 m. There it takes everything in that pouch, and in any other within half the reach, one take at a time.
  - It leaves a pouch it finds no way to, or one holding an item its bag has no room for.
  - Its stats count the items taken.

## 9. Testing

- **Rolls:**
  - the pouch chance by kind;
  - an active quest's item rolled outside the gate;
  - one pouch per kill holding the whole roll;
  - turns per pouch;
  - a box per eligible player and none for others.
- **Visibility:** a pouch reaches only its owner's frames, and a box only its owner's.
- **Taking:**
  - each refusal;
  - reach on the server's position with its slack;
  - a full tab leaving the item;
  - a batch partly refused;
  - retries under the same key never giving twice;
  - emptied pouches going;
  - lives ending with `Loot{left}`.
- **Bosses:**
  - up at an instance's start;
  - back `respawn` after a kill and never before;
  - never leaving;
  - notices only on its map or area;
  - `back_in_ms` in the snapshot of a player joining while it's down;
  - calm: a player walking the forest past King Slime, stopping by it, is never chased; a blow wakes it, even from someone it can't see; it's calm again after a reset, by its leash or with nobody left; mobs by it still fight;
  - rewards as before.
- **Item invariants** hold over randomized takes. Every created item has its `drop` cause.
- **Camps:** every camp in the shipped maps and regions waits at least 24 s.
- **Client:**
  - the window's list from several pouches;
  - take one, take all, and the second press;
  - closing out of reach;
  - the hint;
  - the boss mark's countdown.
- **Live** (the player harness): hunt with pouches on each class, a boss's box with a party, a full Etc tab, and a boss coming back after 5 minutes.

## 10. Build

1. **Timed bosses:** the respawn, no leaving, map-only notices, the bar without time, `back_in_ms`, and the map window's mark.
2. **Pouches,** server and client together, since auto pickup goes as T comes:
   - the roll and gate, owners and boxes, private views, `TakeLoot`, lives;
   - the models, hint, window, `loot` key and combat lines;
   - cmd/bot taking.
3. **Slower camps and fewer pouches,** data only:
   - `loot.yaml`'s chances, and every camp's `respawn` tripled (section 3);
   - the test holding shipped camps to 24 s;
   - any test on the shipped maps that waits for a camp to refill moves to a fixture, or is judged by ticks.

## 11. Decisions

The user's choices are in section 1. These calls were made in planning and logged for the user to push back on:

1. **Pouches and boxes are seen only by their owner.** Nobody else can take them, and others' pouches would only clutter the ground.
2. **One window for every pouch in reach,** and T again takes all, so a kill streak is one press to open and one to take.
3. **Nothing follows its owner anymore.** A pouch waits where it fell: 60 s for a pouch, 2 min for a boss's box.
4. **A pouch on 1 kill in 4, and 1 in 2 for an elite,** cut by the user on 2026-11-20 to 1 in 6 and 1 in 3 (section 1). A boss always leaves each eligible player a box.
5. **Items an active quest still needs are rolled at their own chance,** outside the pouch's gate, so quests take no more kills. They still come in a pouch. As built, "still needs" is "an active quest collects it", since the world doesn't hold the bag (section 4).
6. **Party turns move per pouch,** not per item.
7. **A full tab refuses that item only;** it stays in the pouch until its life ends.
8. **A boss is up when its map starts,** never leaves, and its bar shows no time. The map window shows when it's back.
9. **Taking works in combat,** not while dead.
10. **The reach is 2 m,** with 0.5 m of slack on the server for the client's lead.
11. **A standing boss fights only once provoked,** so bosses on narrow maps never block paths. With bosses up for good, King Slime saw the whole forest's way from the town to the cave, and the Vampire Bat stands among the cave's bats.
12. **A take waits behind its owner's call in flight, and only one waits.** A further take is busy, unless it picks from the same pouches, which it joins. Pressing T over and over never piles up calls.

## 12. Alternatives considered

- **Auto pickup for potions and quest items:** kept the rules in two places, and the user wanted quest items in pouches.
- **One shared box per boss, with need or greed:** bosses already give each player their own roll, and taking turns over a box is slower.
- **Pouches everyone sees:** clutter, and a pouch nobody else can open invites confusion.
- **Fuller pouches with the same loot per hour:** offered, and the user chose less loot overall.
- **Camps twice as slow (16 to 30 s), or 30 s for every camp:** offered with three times as slow, which the user chose. A flat time would also lose the elites' slower return.
- **Pouches halved to 1 in 8, with or without fuller pouches:** offered, and the user chose a third fewer.
- **A boss on a 5-minute clock, or leaving after 10 minutes:** offered, and the user chose 5 minutes after it's beaten.
