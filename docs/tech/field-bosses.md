# Field bosses

> **Status: approved · 2026-11-12.** The high-level design for [C10. Field bosses](../product/features/combat-and-progression/c10-field-bosses.md) and [Systems → Combat → Field bosses](../product/systems/combat.md). It builds on `main` after [Animation cancel](./animation-cancel.md), whose pace step retunes the same mobs, and describes the change it plans. Since [Mobs fight back](./mobs-fight-back.md) (C12), a boss fights with attacks, not touches, as every mob does.

## 1. What has to hold

- **The world decides everything:** the kill count, when a boss appears and leaves, every hit, and who gets what. The count is never sent to a client.
- **The tick never waits.** Announcing a boss to every player goes through a queue off the tick, as party member updates do.
- **A boss is a mob.** It moves, attacks, takes hits, and shows in frames like any other, so combat, actions, and drops need no second path.
- **Rewards follow damage, and only the living on the map collect.** Damage is counted as HP actually taken, so overkill and a last hit earn nothing extra.
- **Content is data:** which mob is a map's boss, where it stands, how many kills bring it, how long it stays, and its stats and drops.

## 2. The change at a glance

| Part | Today | After |
| --- | --- | --- |
| `data/mobs.yaml` | Normal mobs and elites | Also 12 bosses (`boss: true`), and `field_boss: {life, drop_share}` |
| Hunting maps (`.tmj`) | Object layers for spawns, portals, NPCs | Also a `boss` layer: one area, with its `mob` and `kills` |
| Mob arena | Spawns and respawns from map spawns; mobs don't know who hit them | Spawns and removes a boss on the instance's word; each blow reports the HP it took |
| Instance | Kills give EXP, loot, and quest progress | Also counts kills, raises and retires the boss, tallies its damage, and pays out by share |
| Social module | Delivers party lines to online players | Also `Announce`, delivered to every online player |
| Client protocol | Mobs in frames; no system messages | `FieldBoss`, `FieldBossGone`, `Snapshot.field_boss`, and `Announcement` |
| Client | Mob HP bars over each mob | A boss bar at the top, the boss notice, and a `[Notice]` line in the General tab |
| Balance harness | Normal mobs and elites | Also each boss against one player alone |

## 3. Game data

### `data/mobs.yaml`

```yaml
field_boss:
  life: 10m             # a boss nobody beats leaves this long after it appears
  drop_share: 0.01      # the share of its HP a player's damage, or their party's, must reach for a drop roll
mobs:
  - id: king_slime
    name: King Slime
    boss: true
    level: 6            # at least its map's top mob level; the product sets top + 2
    hp: ...
    attack: ...         # twice its map's mob's
    super_armor: true   # every boss: no flinch, push or knockdown (C12)
    attacks: [{use: hop, distance: 160, spread: 32}, {use: slam}] # its family's, reaching about twice as far, and a slam (C12)
    width: 96
    height: 84
    look: {sprite: slime, tint: 0x6fd36f, scale: 3}
    drops: [...]
```

- **`boss`** marks a field boss. A mob is never both a boss and an elite.
- **`attacks`** are its family's attack, reaching about twice as far as its map's mob's, and a `slam` all around it, which it takes in turn. Their cooldowns, not a guard of its own, set how often it hits, tuned to the potion budget (§8). Every boss has `super_armor`, so blows never stagger it.
- **Size** is `width` and `height` for the hitbox, and `look.scale` for the art, as for elites.
- **Its map's mob,** wherever a rule compares a boss with one, is the map's normal mob of the top level, the one players at the boss's level hunt. Only the forest and the cave have two normal mobs, so King Slime takes the green slime's look and the orange mushroom's measure.

### The `boss` layer in each hunting map

One rectangle in a new object layer, `boss`. The boss appears at the rectangle's bottom center and patrols the ground there, as a spawned mob does. Every map has the layer, as every map has `spawns`; a town's is empty:

```json
{"name": "king_slime", "x": 900, "y": 420, "width": 400, "height": 120,
 "properties": [{"name": "mob", "value": "king_slime"}, {"name": "kills", "type": "int", "value": 300}]}
```

- `gamedata.Map` gains `Boss *BossSpot{Mob, Kills, Area}`.
- **The client's copy** of each map, made before every build, leaves the `boss` layer out, since players don't see the kill count. `maps.ts` never reads it.

### Validation refuses

- a hunting map without exactly one boss area, or a town with one;
- a boss area whose `mob` isn't a boss, or a boss used in `spawns`, on two maps, or on none;
- a boss below its map's top level, or with a `kills` under 1;
- no ground under the area's bottom center, or less of it within the area than the boss is wide;
- a `life` of zero, or a `drop_share` outside 0 to 1.

## 4. World

### The count

- **The instance counts kills** in `strike`, the one path every attack and skill kill goes through. A kill counts when no boss is up and the mob isn't the boss.
- **At the map's number,** the boss appears at the end of that tick, and the count goes back to 0.
- **It lives only in the instance,** like mob positions. A world restart starts it again at 0, and a boss up then is lost.
- **Each instance counts on its own,** so once a map has several channels (B4), each channel has its own count and boss.

### The arena

- **`SpawnBoss(mob, area)`** adds the boss outside the spawn table, so it never respawns.
- **`Remove(id)`** takes a mob away without a kill, for a boss that leaves. It goes out in the frame's `removed_mobs`, whose comment changes from "mobs killed" to "mobs gone".
- **Each `Blow` reports `Dealt`:** the HP the hit actually took, without overkill.

### The fight

- **The instance holds the boss's state:** its mob id, the tick it leaves, and a damage tally by character. Each blow on the boss adds its `Dealt` to the striker's tally.
- **Leaving:** once the tick passes its time, checked after attacks so a kill in its last tick still counts, the arena removes it. `FieldBossGone{beaten: false}` goes to the map.
- **Its attacks** are a mob's, with its own attack and damage type: a windup the players see, then the landing. It takes its family attack and the slam in turn, or whichever is ready if only one is. After any mob's hit or MISS, no mob's attack hits that player for the shared `hit_guard`, 1.5 s, and their blink follows it.

### Paying out

When the boss falls, through the same `strike` kill path:

1. **Who collects:** the players alive and on the map. Each is grouped with the other present members of their party, or stands alone.
2. **A group's damage** is the sum of its whole party's tallies, members gone or dead included. A lone player's is their own.
3. **EXP:** a group's share is the boss's EXP × the group's damage ÷ its max HP, rounded. It's split among the group's present members by the rule `shareExp` uses for any kill: equal shares and the party bonus. That rule moves into `splitExp`, which `shareExp` calls with the killer first and the boss path with the group in join order; the first takes what doesn't divide.
4. **Drops:** each present player whose group reached `drop_share` of the boss's HP gets their own roll. A `loot.Kill` names them as killer and has no party, so `Ground.Roll` makes every drop theirs and party turns don't apply. Drops fly to their owners after `collect_delay`, as any drop does.
5. **Quests:** the killer's kill counts as any kill does. No quest asks for a boss yet.
6. `FieldBossGone{beaten: true}` goes to the map, and the count starts again.

Damage from players who aren't there to collect is not handed to the others. Shares are of the boss's whole HP, so a fight that loses players pays out less, not more to each.

### Events

| Event | To | When |
| --- | --- | --- |
| `FieldBoss{mob, type, leaves_in_ms}` | The map | It appears; and in the `Snapshot` of anyone joining while it's up |
| `FieldBossGone{mob, beaten}` | The map | It's beaten or leaves |
| `Announce{boss_appeared: {boss, map}}` | Every online player, through social | It appears |

The client counts the time left down itself from `leaves_in_ms`.

## 5. Social module: announcing to everyone

- **`Announce(Announcement)`,** a new RPC on the social service, sends the announcement once to each gateway that anyone in `presence` is online through, over its `Deliver` stream, with no character named. The gateway hands it to every player it has in the world. One message per gateway, not per player, keeps the burst within a gateway's delivery queue however many are online; one per player would fill the queue at a few thousand players and end that gateway's stream. An announcement of no kind is refused.
- **The world sends it off the tick:** the instance hands the announcement to the `announce` queue, which a goroutine sends with a 2-second timeout, as `party.Copy` sends member updates. The queue is the same coalescing one, keyed by the news, so a boss's notice still waiting is replaced by the next one from its map rather than piling up. A failure is logged and dropped. The boss still appears; only the notice is lost.
- **`Announcement` is typed:** a oneof whose first case is `boss_appeared{boss, map}`. The client writes the words from game data, so names stay in one place and later notices add cases.
- **The gateway** maps the new `DeliverResponse` case to a push of its kind, `model.BossAppeared`, which the outbox writes as an `Announcement`; a later kind adds its own push. It comes as a `SocialBroadcast`, which every active session in the world gets.

## 6. Protocol

All additive, so `buf breaking` passes:

- **Client:**
  - `ServerMessage` gains `FieldBoss`, `FieldBossGone`, and `Announcement`, at the next free field numbers.
  - `Snapshot` gains `field_boss`.
- **Module contracts:**
  - The world module's events gain the two boss messages, and its snapshot the boss (in step 2).
  - The social module gains `Announce`, and its deliveries an announcement.
- **Contracts:** golden files for the new messages, and the Snapshot's with a boss up.

## 7. Client

- **Boss bar:** a `fieldBoss` store holds the boss's mob id, type, and when it leaves, counted from when the `FieldBoss` or the `Snapshot` came. The world scene puts the boss's HP in it as it draws the boss, only when it changes. `BossBar`, at the top center under the connection notice's place, shows the boss's name from game data, that HP, and the time left counting down each second. It's built on the shared `Bar`. It shows while the scene draws the boss and the store holds it.
- **The mob itself** draws with its `look`. A boss's small overhead bar isn't drawn, since the top bar shows the same.
- **Players draw over every mob,** by the client's one draw-order table, `game/depth.ts`: a melee player fights from inside a boss three times their size, and would otherwise vanish behind one that appeared after them.
- **Notices:**
  - **`Announcement`** shows an info toast and adds a `[Notice]` line to the General tab: "King Slime has appeared in the Forest!" The chat store's lines are a player's (`said`) or a `notice`; the Party tab shows party lines only.
  - **`FieldBossGone{beaten: false}`** shows "King Slime has left." as an info toast, to the players on the map, who alone get it. A beaten boss needs no notice: the combat channel already lists each player's EXP and drops.

## 8. Balance

- **The harness checks each boss** against one on-level player of every class, the character it already builds at its map's top level, with its split and band gear:
  - **Time:** a kill takes 2.5 to 3.5 minutes at the class's pace, misses included.
  - **Potions:** for a melee class standing in reach the whole fight, the hits it takes, less its HP, come to at most 30 of its band's potion.
  - **EXP:** about 40 of its map's normal mob's.
- **A Beginner's time is only logged:** in the forest, the four splits fight with one weapon and their times differ by half again, 2.4 to 3.6 minutes, more than the window holds. Their potions are held.
- **Hits come at the boss's cooldowns.** Each boss keeps twice its map's mob's attack. The harness plays its family attack and slam in turn, each on its cooldown, at a player in reach who never dodges. Four bosses went over 30 potions at the table's cooldowns, so their family attack's cooldown is the shortest quarter second that keeps them within it: Dune Baron 2.5 s, Cliff Reaper 2.75 s, Crypt Warden 3 s, Shade Lord 3.5 s.
- **The table is logged** for tuning, as it is for mobs, with each class's time using its skills beside it, which skills make 0.6 to 2.5 minutes.

## 9. When things fail

| Failure | What players see | Why it is safe |
| --- | --- | --- |
| The social module is down when a boss appears | No notice elsewhere; the map sees the boss as usual | The announcement is dropped after its timeout. The boss doesn't depend on it |
| The world restarts during a boss fight | The boss is gone, and no one is paid | The count and the boss live only in the instance, like every mob |
| A player disconnects or leaves the map mid-fight | Nothing, if they aren't back when it falls | Only the present collect; their damage still counts for their party |
| The item module is slow as many drops fall at once | Drops fly in a little later | Each drop is an ordinary claim, batched and keyed as any other |
| The boss would leave in the tick it's killed | It's beaten | The leave check runs after attacks |
| Two kills reach the number in one tick | One boss | It appears once, at the end of the tick |

## 10. Testing

- **Game data:** the boss fields and layer, and each validation refusal.
- **Arena:** `SpawnBoss` outside respawns, `Remove`, and `Dealt` without overkill.
- **World:**
  - the count: kills counted, the boss's own not counted, none while it's up, and back to 0 after;
  - appearing at the number, once, at the end of the tick;
  - leaving at its time, and a kill in the last tick winning;
  - the damage tally and the payout: a lone player; a party with a dead member and one on another map; a group under `drop_share`; damage from a player who left;
  - each drop's owner, with party turns untouched for the next ordinary kill;
  - the events, and the snapshot for a player joining mid-fight;
  - a boss taking its two attacks in turn (C12's arena tests).
- **Social and gateway:** `Announce` going once to each gateway with players online and to none without, the gateway handing it to each of its players, and the new push.
- **Client:** the bar's countdown and HP, the joining snapshot, the toast and the `[Notice]` line, and the leaving notice.
- **End to end,** on test data with a small `kills`: kill until the boss appears, see the announcement on another map, beat it in a party, and check each player's EXP and drop owner.
- **Balance:** the boss checks in the harness.

## 11. Build order

After animation cancel is merged, on its own branch and PR. Each step is one commit, green on `make test`, `make lint`, `make cover`, and `make breaking`:

1. **Game data:** `boss`, `field_boss`, the `boss` layer in all 12 hunting maps with first stats by the rules above, validation, and the generator.
2. **Arena and world:** `SpawnBoss`, `Remove`, `Dealt`, the count, appearing and leaving, the tally, the payout, and the events, with the world contract's messages and snapshot field: the world's stream must carry every event it raises, and the gateway passes over one it doesn't know yet.
3. **Social, gateway, and protocol:** `Announce`, the fan-out, the client's messages and push, its snapshot field, and contracts.
4. **Client:** the boss bar, the boss's look, the announcement toast and `[Notice]` line, and the leaving notice.
5. **Content and balance:** the harness's boss checks, the 12 bosses tuned to them, an end-to-end run, and `make bots`.

## 12. Decisions

Agreed on 2026-11-12:

1. **Announcements go through the social module,** as a typed message the client puts into words. It already tracks every online player across gateways, so the world needs no list of its own.
2. **Boss EXP:** about 40 of its map's normal mob's, so one player beating it alone in 3 minutes earns more than 3 minutes of ordinary hunting.
3. **Drop list:** its map's band gear at 10 times its normal mob's chances, plus potions, to tune.
4. **A solo melee player's potion budget:** at most 30 of the band's potion for the 3-minute fight, so a boss is hard alone but not a wall.
5. **The boss's overhead HP bar is hidden;** the top bar replaces it.
6. **The count and the boss aren't saved.** A world restart loses them, as it does every mob, so they need no storage.
7. **Each hunting map's boss is set in its Tiled file,** as a `boss` layer beside `spawns`, so a map's whole layout stays in one place.
8. **Slower, heavier touches:** a boss keeps twice its map's mob's attack and touches less often, by a `touch_guard` of its own tuned to the potion budget. During it the player is safe from every mob, which is simplest. *Since C12, touches are attacks: a boss hits as often as its attacks' cooldowns allow, and the guard is the one `hit_guard` for every mob.*

## 13. Alternatives considered

- **Lower a boss's attack to fit the potion budget at a mob's 1.5 s guard:** its attack would come to 0.9 to 1.7 times its map's mob's, so some bosses would hit no harder than the mobs around them.
- **One `field_boss.touch_guard` for every boss:** at twice the attack, the bosses need 2 to 3.5 seconds, so one guard would leave some far under the budget and others over it. A mob's own guard fits each, and any later mob can use it.
- **Count kills in the arena:** the arena doesn't know which kills count, or when a boss is up. The instance already handles every kill in `strike`.
- **Send the count to clients and hide it in the UI:** the product keeps it hidden, and anything sent could be read.
- **Announce over the world's per-player streams:** each instance reaches only its own map, so it would need every other instance's players. The social module already reaches everyone.
- **Last hit or top damage takes the drops:** a public fight would turn into a race for the kill. Personal rolls by damage share keep it cooperative.
- **Split a dead player's share among the living:** a fight where others die would pay the survivors more, which rewards letting others fall.
- **Save the boss and the count:** they would outlive a restart, but no other mob does, and a restart mid-fight is rare.
