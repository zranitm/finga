# PvP

> **Changed by [Outfits](./outfits.md):** the first time a character's shown rating reaches a rank above Bronze, the pvp module grants that rank's outfit piece, and it checks `pvp_wins` feats at a match's end (section 4 there). Ranks still give no items, EXP, currency or stats. PvP bots wear random boss and feat pieces.

> **Status: approved · 2026-11-18.** The high-level design for [I1. PvP matches](../product/features/pvp/i1-pvp-matches.md), [I2. Arena: 1v1 and 3v3](../product/features/pvp/i2-arena.md), [I3. Battlefield: 15v15](../product/features/pvp/i3-battlefield.md) and [I4. PvP rating and ranks](../product/features/pvp/i4-pvp-rating.md). A lost connection's 20 s grace was added on 2026-11-24, after a live test (section 4). Enemies' bodies block each other since 2026-11-28 (section 19).

## 1. What has to hold

- **The user's words** (2026-11-18): "lets build pvp feature - match making - for now we only have bot", then "we'll have 1v1 and 3v3 then 15 v 15 in a bigger map for guild war, we dont add guild now and all of them can be experienced with bots".
- **Their choices:**
  - 1v1 and 3v3, best of three rounds. 1v1 rounds last 60 s, 3v3 rounds 90 s, and the larger HP share wins on time. In 3v3 the last team standing wins the round.
  - 15v15 in this feature as an open queue, on a bigger map, won by capture points. Guild war later puts a guild on each side.
  - Equal stats, own build: a class's set stats and gear, every skill at full, with the player's traits and bar as their build.
  - Players are matched with players first, and anyone still waiting after 15 s gets bots.
  - Rating and rank only, one rating for 1v1 and one for 3v3, and none for 15v15. No items, EXP or currency.
- **The client computes, the server validates.**
  - The client counts down queue, round and respawn times, and moves capture rings between updates.
  - The world decides every hit, round and point.
  - The pvp module decides pairings and ratings.
- **Nothing changes outside a match:** in the world no player is anyone's enemy.
- **Modes are data:** team size, map, round rules and bot tiers. Guild war becomes a mode whose sides are guilds.
- **Names in code:** `mobs.Arena` already means a map's mob container, so code says `pvp` and `match`. Players read "Arena".

## 2. Shape

- **The pvp module** (new: `application/pvp`, its store `repository/pvp`) owns:
  - the queue and the matcher;
  - offers and bot fill;
  - ratings and match history;
  - the leaderboard and the arena build.
- **The world module** owns:
  - match instances and moving players in and out;
  - round and capture rules;
  - combat between players;
  - bots.
- **A match's path:**
  1. `PvpQueue{mode}` goes through the server to `pvp.Queue`. That asks the world whether the characters can fight (`world.CanFight`): promoted, online and not in a match.
  2. Each second the matcher forms matches, with bot places where players are missing. It pushes an offer to every player in each one.
  3. When everyone accepts, pvp calls `world.OpenMatch(spec)`.
  4. The world builds a match instance, moves the players in with their world state set aside, adds the bots, and runs the rules.
  5. At the end it reports the result through a port (`Results`), a queue the pvp module follows, moves everyone back and closes the instance.
  6. pvp stores the match and the new ratings in one transaction, and pushes each player's rating change.
- **One queue owner:** the queue is in memory, in the pvp module, on one node. A second node will need a single owner, such as a lease row. Until then there's one node.
- **Ports both ways,** as the other modules do (`internal/adapter`):
  - pvp → world: `CanFight`, `OpenMatch` and `SetBuild`.
  - world → pvp: `Results`.
  - As built (step 4), pvp sends each player's build after `OpenMatch` returns, rather than the world reading it at entry. The world is built before pvp, so a world → pvp port would need a cycle. Section 6 has the order that keeps a save from being lost.
  - The server serves both modules' commands and pushes.

## 3. Data

### `pvp.yaml`

```yaml
modes:
  duel:   {name: 1v1,   team: 1,  party: 1, map: ring,        rated: true,  rounds: {best_of: 3, most: 5, length: 60s}, reconnect: 20s}
  team:   {name: 3v3,   team: 3,  party: 3, map: ring,        rated: true,  rounds: {best_of: 3, most: 5, length: 90s}, reconnect: 20s}
  battle: {name: 15v15, team: 15, party: 4, map: battlefield, rated: false, rating_from: team, reconnect: 20s}
queue:  {bots_after: 15s, offer: 10s, window: {start: 2, per_second: 0.2, most: 8}}  # window in mu
rounds: {prep: 10s, countdown: 3s, between: 5s}
battle: {prep: 20s, length: 12m, goal: 1000, per_point: 1, per_down: 5, respawn: 10s, base_heal: 0.10,
         capture: {time: 8s, per_extra: 0.25, fastest: 2}}
result: 10s
kit:    {level: 30, classes: {warrior: {hp: 0.48, damage: 1.19}, thief: {hp: 0.96, damage: 1.10},
                              bowman: {hp: 1.46, damage: 0.81}, magician: {hp: 1.48, damage: 0.94}}}
scale:  {hp: 4.0, damage: 0.42}  # beside each class's own factors; tuned by the harness, section 14
rating: {mu: 25, sigma: 8.333, beta: 4.167, tau: 0.083, shown: {base: 1000, per_point: 40}, placement: 5}
ranks:  [{id: bronze, from: 0}, {id: silver, from: 1200}, {id: gold, from: 1400},
         {id: platinum, from: 1600}, {id: diamond, from: 1800}, {id: master, from: 2000}]
bots:
  most: 600  # bots one node runs at once
  tiers:
    - {id: novice,  mu: 15, reaction: 450ms, defend: 0.15, chain: 0.3,  break_free: 1.2s, aim: 20}
    - {id: adept,   mu: 20, reaction: 350ms, defend: 0.35, chain: 0.55, break_free: 0.8s, aim: 12}
    - {id: veteran, mu: 25, reaction: 260ms, defend: 0.55, chain: 0.75, break_free: 0.5s, aim: 7}
    - {id: expert,  mu: 30, reaction: 190ms, defend: 0.7,  chain: 0.9,  break_free: 0.3s, aim: 4}
    - {id: master,  mu: 35, reaction: 140ms, defend: 0.85, chain: 1,    break_free: 0.2s, aim: 2}
  names: [Ash, Birch, Cedar, ...]
  builds:  # per class, each an arena build plus the order its skills are used in; shape only, tuned in build step 3
    warrior: [{bar: [...8 skills], traits: {skill: trait}, order: [...skills]}]
```

- **Loader checks:**
  - Each mode's map is a `pvp` map with at least `team` starting spots a side.
  - `rating_from` names a rated mode.
  - The ranks rise.
  - The tiers' mu rise.
  - Every bot build is a valid arena build for its class (section 6).
  - Every class has a build.
  - There are at least 30 names.
- **`gen-gamedata`** carries the modes, ranks, kit level and the client's share of the rules: round lengths, capture rates and respawn. It leaves out the bots and the rating's constants.

### Maps

- **A new kind, `pvp`,** in `mapfile`:
  - It has no portals, spawns, NPCs, boss, town or levels.
  - It needs `sides`: two lists of starting spots `{at, yaw}`.
  - A battle map adds:
    - `bases`, one area a side, with the `gate` solids that close it during prep;
    - `points`: `{id, name, at, radius}`.
  - Every `pvp` map has a `prep_wall`: solids across the middle that stand only during prep and countdown.
- **The loader:**
  - `pvp` maps sit outside the world's links. They're not on the world chart (`world.yaml` `places`), and no portal or teleport reaches them.
  - Their nav grid is built as for any map, so bots path on it.
- **`ring`:** a walled round ring 36 m across, with four pillars, and three spots a side 14 m apart.
- **`battlefield`:**
  - About 140 m by 90 m, with bases at the west and east ends behind gates.
  - Three points, west, middle and east, 10 m across.
  - Hills and ruins from the keep's and desert's art.
  - Fifteen spots in each base.
- **As built (step 6a):**
  - **The format:** a battle map's `bases` are two circles `{at, radius}`, side 1's then side 2's, and its `points` are `{id, name, at, radius}`. Its gates are its `prep_wall`, one across each base's bay.
  - **The loader checks:**
    - the two bases are apart;
    - each side's spots stand in its base;
    - each point has an id of its own and a name, its circle is inside the map, and it overlaps no base.
    - A mode with no `rounds` is a battle, and its map must have bases and points.
  - **`battlefield`** is 140 m by 90 m, mirrored about x = 70, so no point is nearer one base than the other:
    - each base a 9 m circle at the end of a bay 30 m across, its gate 11.5 m from the base's centre;
    - the bays' cliffs are 3 m: too steep to climb and over a jump's 2 m, and low enough that the camera, looking north, sees the whole base over the south one at every zoom;
    - three 5 m points along z = 45, at x = 38, 70 and 102;
    - a fallen tower's columns and a boulder by each outer point, walls between the points, four columns round the middle one, and low hills on the flanks.
  - **The crossing test** reaches every point and every spot of both sides from each side's spots.
  - **A second check** floods each side's spots on a battle map:
    - with the other side's base standing, it reaches every point and its own spots at every speed, and none of the other side's spots;
    - with the prep wall up, it reaches no point and none of the other side's spots.
    - Both "none" floods jump as high as a jump gets at any speed, plus a margin, so a cliff or gate a jump clears fails them.

## 4. Match instances

- **`World.OpenMatch(spec)`:**
  - **The spec:**
    - a match id (UUIDv7) and the mode;
    - two sides, each a list of fighters: a character, or a bot with its tier, class, build and name.
  - **What it does:**
    - Builds an `instance.Instance` from the mode's map, with `Deps.Match` set: the mode's rules, the sides and the results port.
    - Keeps it in `w.matches`, a map by match id under its own mutex, and runs it on its own goroutine under the world's run context.
    - Moves each player in.
  - **`w.instances` doesn't change:** map instances stay fixed and are read without a lock. Presences point at their instance, so commands route to a match with no change.
- **Moving in:**
  - It's a transfer, under the character lock: the player's map instance hands them off, and the match instance takes them.
  - `Mover.Transfer`'s `mapID` becomes a destination, either a map or a match.
- **The return point:**
  - The player's `model.Player` and spot, as they were at hand-off, are kept as the match player's `back`.
  - That covers their map, position, HP, energy and timers. Timers are wall-clock ends, as `timersOf` gives them.
  - **Checkpoints in a match write `back`,** never the match. A crash or a relog lands the player where they queued from.
- **Moving out:**
  - At the end, or on a forfeit, the player is handed off to `back`'s map and spot, with `back`'s state.
  - `startTimers` re-bases its timers, so a cooldown that ended meanwhile is ready, and a buff that ended is gone.
- **Leaving:**
  - **Logout and Leave match** are a forfeit at once. The instance removes the player as any leave does, and checkpoints `back`.
  - **A dropped connection holds the place** for the mode's `reconnect`, 20 s. A closed tab is a drop too, as the server can't reliably tell one from the other.
    - **Why:** in a live test (2026-11-24) a 0.3 s drop under load, as round 1's fight began, cost a rated 3v3. Players on Wi-Fi and phones will see blips like that.
    - **The body stays:** the presence stays in the instance with no connection, and what's sent to it is dropped. The body takes no input but is otherwise the player's: HP, energy, statuses, buffs and their own cooldowns all run on. It can be hit and downed, and a round's reset applies to it as to anyone.
    - **`back` is untouched,** and its checkpoints go on as before.
    - **The return:** a `Hello` for a character whose place is held goes straight to the match instance, by the presence the world still holds, never through town or `back`. The player gets `Welcome` on the match's map, on the body as it stands, then `MatchState` and a `StatsChanged` with every timer. Nothing resets.
    - **Past the grace,** the drop is a forfeit and the leaver path runs (section 7), the stand-in taking the body as it stands then.
    - **A second drop** in the same match gets a fresh grace: with no bot during it, dropping gains nothing.
    - **The match ends during the grace:** the held player is rated by the result, as anyone there, never as a leaver. No connection is there to follow a hand-off, so they're saved at `back`, where their next login lands. They miss `MatchEnded`, and are told at their next `Welcome` (below).
    - **No bot during the grace:** a stand-in starts with cooldowns ready and no statuses, so a free return would make a drop a reset. A player weaker than the match's bots could also drop to let one fight for them. The price is a team one short for up to 20 s on a long drop.
    - **As built (step 7a):**
      - **A drop or a logout:** the server tells them apart by how the session ended. A logout or an ended login (`session_expired`) is a `Leave`. Any other close is the world's `Drop`: a closed socket, a login elsewhere, or a presence on a connection the server doesn't know.
      - **`Drop`** asks the match to hold the place. Off a match it's a `Leave`, and so is a drop the match holds no place for: another connection's, a mode with `reconnect: 0`, or the outcome showing.
      - **The held body** keeps its presence under the dropped connection, and what's sent to it goes nowhere. Its held keys and queued inputs are cleared, so it stands. A hit still to land, a queued attack and a skill waiting its turn are dropped. Keys sent before the drop that come after it are refused.
      - **The return:** `Join` first offers the account's presence to its match. The match puts the new connection on the body, with its inputs counted again from the new client's first, and retells the party. A join the match doesn't take, as with the outcome showing or the match's map stopped since the join looked, is a plain one. As this return loads no character, only the character's own account's presence is offered.
      - **A return whose connection closes** before it's on the body answers busy and holds the place again, to the grace's first end: failed returns buy no time.
      - **The match ending during the grace:** the held player isn't handed off with the rest, as no connection is there to follow. They're saved at `back` and dropped, so their next login lands there.
  - **Told on return (as built):** a player away from a match at its end is told how it went at their next `Welcome`. That covers one held through a drop, a 1v1 duelist past the grace, and a 3v3 or 15v15 leaver.
    - **Told once it's written:** the outcome stores their fighter row `untold`. A `Welcome` reads those rows and marks each one it lists or counts told once the socket's writer has written it, so a `Welcome` that never goes out leaves them for the next. A `MatchEnded` handed to them live, as to a team leaver playing elsewhere, marks it told then.
    - **At most five:** the `Welcome` lists the newest five and counts the rest, so a long absence isn't a wall of lines.
    - **Best effort:** a store that can't answer fails no login; the rows wait for the next `Welcome`. A double beats a loss, so these are left as is: a reconnect just as the match ends may hear it both live and at the `Welcome`, and two logins at once may both list it.
  - After a server stop or a crash, a match has no result and moves no rating.
  - A match that expected players and saw none by prep's end closes with no outcome. A match of bots alone runs to its end, as its test runs one; the queue never makes one.
- **Teardown:** after the result phase, the instance hands everyone out and its `Run` returns. Then it's dropped from `w.matches`.
- **Party:** `tellParty` names the match's map, which the party frame shows as "In a match".
- **Bots:**
  - A bot is a `player` in the instance, with a made-up character id (UUIDv7).
  - It has no connection: its sink drops what's sent. It has no checkpoint and no database row.
  - Its brain runs in the instance's tick (section 8).

## 5. Combat between players

### Who is an enemy

- `i.enemies(a, b)`: in a match, players on the other side; elsewhere, never.
- `i.allies(p)`: in a match, the player's side; elsewhere, their party, as `present` gives it today.
  - `buffed` and `hurtInReach` take it, so War Cry and heals reach teammates in a match.
- **As built (step 5):**
  - In a match, `allies(p)` is the live fighters on p's side, other than p. Outside one it's still their party's members alive on the map.
  - So in a match a heal or a `reach_party` buff never reaches an enemy, even one in the caster's world party. A teammate outside that party is reached.
  - Strikes already judge enemies by side, so an area blow never hurts a teammate.

### Targets

- `Attack.target`, `UseSkill.target` and `SkillUsed.target` become refs, `"m:<id>"` or `"p:<id>"`, as `Damage`'s ends already are.
- `model.SkillUse.Target` and `attackMsg.target` become `model.EntityRef`.
- One lookup, `i.at(ref)` and `i.controlled(ref)`, finds a mob or a player. `aimOf`, `chainReady` and `holds` use it, so a lock and a chain read an enemy as they read a mob.

### Reach

- A strike reaches the map's mobs and the striker's enemies together:
  - everything in its shape, by `model.Reached`;
  - the lock first;
  - cut to the skill's `targets`.
- `arena.Strike` keeps the mobs' share. A new `strikePlayers` takes the players'.

### A hit

- **Damage:**
  - `model.Landed(PlayerAttacker(attacker.derived, type).Times(power), defender.statuses.Defending(PlayerDefender(defender.derived)))`, times `scale.damage` and the attacker's class's `damage`.
  - **No MISS between players:** `Landed` rolls variation and crit but no MISS. Accuracy and evasion count everywhere else, mobs both ways.
  - Damage over time uses `Steady`, as on mobs, scaled the same way.
- **One function for any hit on a player,** `hitPlayer`. `mobHits`'s three steps move into it, so dodge and block stay one path:
  1. **A dodge** passes it: `Damage.dodge`.
  2. **A block** from the front arc stops it: `Damage.block`, with the block's energy once per use. Nothing a player does is unblockable.
  3. **Otherwise it lands:**
     - shields take it first, then HP;
     - the hurt blink shows;
     - the skill's statuses take.
     - From a player: the skill's push as a shove with no hop, and no hit guard (`nextHit` untouched), so combos land.
     - From a mob: the guard and the knockback as today.
- **Down:**
  - At 0 HP in a match, a player is down, not dead.
  - Like a death, it clears the action, dodge, block, statuses and buffs, and sends `Died`.
  - Unlike one, there's no death penalty, no respawn timer and no trip to town. The match decides what comes next (section 7).
  - `deathsAndRespawns` skips match players.
- **Energy:** an attack that reaches an enemy gives `per_attack`, as reaching a mob does.
- **No credit:**
  - In a match, `credit` gives no EXP, drops, quest counts or boss counts.
  - Who downed whom goes to the match's rules: the 15v15 score and the result screen.
- **Statuses:**
  - Players' afflictions on players take through `inflict`, as mobs' do. Break-free is unchanged.
  - A knockdown lasts the down pose, `react.down`, as a mob's lays a player down.
  - The aggro group (taunt, lose sight) doesn't take on players, as `statuses.yaml` says.
  - Damage over time on a player is credited to its inflicter.
- **Chains:** `dodged` and `blocked` count any enemy's attack, a player's as a mob's.
- **In combat:** `stampFights` already stamps both players of a `Damage`.
- **Telegraphs:** players' attacks draw no warning areas, as today. The swing is the warning.

### Bodies

- Enemies' bodies block each other, as mobs block players (section 19). Teammates' don't, and outside a match no player's body blocks another's.

### Refused or off in a match

- **Refused with `PVP_IN_MATCH`:**
  - `UseItem`;
  - equip and unequip;
  - stat and skill points;
  - `ChooseTrait`;
  - `EnterPortal` and `Teleport`.
- **Regen:** none for match players. A battle base heals instead.

### The wire

- `PlayerView` gains:
  - `team`: 0 outside a match, else 1 or 2;
  - `bot`.
- Proto comments that say "mob" for dodging, block, shield and invulnerable say "enemy".

## 6. Equal stats: the kit and the arena build

### The kit

- **Stats:** `progression.DeriveStats(rules, class, kit.level, own, worn)`.
  - **`own`:** the class's starting stats, plus `(level − 1) × points_per_level` split by the class's `auto_assign`, as the Auto-assign button spends them.
  - **`worn`:** in each equip slot, the item the class can wear with the highest `min_level` up to the kit's level.
    - Ties go to more stars, then the item id.
    - Each stat is at the middle of its range.
    - The loader picks them, so the kit follows new items.
  - Max HP is multiplied by `scale.hp` and the class's own `hp`. HP regen is 0.
  - Every hit and tick of damage over time a fighter deals is multiplied by `scale.damage` and their class's own `damage`.
- **Pinned:** a test lists each class's kit, its derived stats, in a table. A data change that moves one then shows in review.
- **Skills:**
  - Every skill the class learns, its own and the Beginner's, at its last stage and maximum points.
  - Every trait open.

### The arena build

- **Stored:** `pvp_builds(character_id, skill_bar text[8], traits jsonb)`.
  - With no row, the build is a copy of the world bar and traits. A Guard on the bar becomes the class's block, as at promotion.
- **Set:** `PUT /api/characters/{id}/pvp-build {skill_bar, traits}` through the pvp module.
  - It holds eight keys.
  - Each skill must be one of the class's active skills, and each trait one of that skill's. Anything else is `BAD_PVP_BUILD`.
  - The client saves it after the last change, as the skill bar does.
- **In a match:**
  - The match takes the build as it opens.
  - A build saved while the player is in a match goes to the world too, through `SetBuild`. It applies from the next round's start.
  - **In a match, a skill goes only from a key on the arena bar.** A follow-up goes from its root's key. Any other is `NOT_ON_ARENA_BAR`. Out of a match, no bar is judged.
- **As built (step 4a):**
  - **Only a promoted class has a build,** as only it fights in PvP. A Beginner's save is `PVP_NOT_PROMOTED`.
  - **The copy:** `gamedata.PvpCopy` makes it from the world bar and traits, which the character module holds.
    - A block that isn't the class's own takes the class's block, moved there if it stood on another key.
    - Keys the class can't use, and repeats, are left empty.
    - The traits kept are those of the class's active skills, on the bar or not (step 4b): a follow-up rides on its root's key, so its trait has to stand apart from the bar.
  - **The rows:** `pvp_builds` (migration 00014) has `revision`, which counts the saves.
  - **Into a match:**
    1. `OpenMatch` registers the match's players in the world's index of who is in which match, before it returns.
    2. pvp then reads each player's build, the saved one or the copy, and sends it with `SetBuild`.
    3. A save from then on finds the match through the same index.
    - The match keeps the newest revision it's sent, so an older read landing late doesn't undo a newer save.
  - **If a read fails:** the reads and sends run after the match is open, so they never hold it. Each player's runs on its own, bounded to 5 s, and ends as the server stops. A player whose build doesn't come fights with no bar check, on their world traits. Their client still shows the bar they chose.
  - **When a build applies:** the match wears each build it was sent at the next countdown: its bar, and its traits from then on. That's round 1's countdown for a build sent during prep.
  - **Bots** are never judged, as they keep to their own bars.
  - **The save delay** is the skill bar's own.
- **As built (step 4b):**
  - **A trait** may be any of the class's active skills', not only those on the bar, so a follow-up's can be chosen. A trait for another class's skill is `BAD_PVP_BUILD`.
  - **A follow-up on a key of its own** is `BAD_PVP_BUILD`, as the client refuses that drop: it rides on its root's key. The world bar's save refuses one too, and the loader refuses chains a promotion would strand one with, so a copy never holds one.
  - **The countdown tells each player their build:** `ArenaBar` carries the bar the match put on, and a `StatsChanged` sent with it carries the traits. So the client shows exactly the bar the match judges, from the same countdown.

## 7. Match rules

### Rounds: 1v1 and 3v3

- **Phases:**
  1. `prep`: 10 s, before the first round only.
  2. `countdown`: 3 s.
  3. `fight`: the mode's length.
  4. `between`: 5 s, then back to `countdown`.
  5. `result`: 10 s at the end, then everyone goes out.
- **During prep and countdown:**
  - the `prep_wall` stands;
  - in the countdown, movement is held too.
- **Outside a round's fight** (prep, countdown, between and result), attacks and skills are refused with `MATCH_NOT_STARTED`.
- **A round ends** when a whole side is down, at once, or at its time.
  - On time, each side's share is ΣHP ÷ Σmax HP. The larger wins, and equal shares make an even round.
- **A match ends** when a side has `best_of ÷ 2 + 1` rounds, or after `most` rounds as a draw.
- **A round's reset:**
  - everyone back to their side's spots;
  - full HP and full energy;
  - every cooldown, buff, shield, status, dodge, block and action cleared;
  - the downed back up;
  - a build changed meanwhile applied.
- **A forfeit** is Leave match, logout, or a drop whose grace ran out (section 4):
  - In 1v1 the other side wins at once.
  - In 3v3 a bot of the tier the match's bots have takes over the leaver's body, as it stands: the same class, build and HP. The match goes on.
- **As built (step 5):** in a team mode, before the outcome, a bot takes the leaver's place in the same tick they go. That covers a forfeit, logging out or dropping, and never arriving by prep's end.
  - **The body:** it's the leaver's as it stands: position, heading, class, name, HP and energy, and down if they were down. A body is never at 0 HP and up: the tick's downs settle before anyone can leave. Their statuses and buffs aren't carried, and its cooldowns start ready.
  - **A dropped connection:** a player whose sends are refused is removed once the tick's events have all gone out. What the removal raises, the stand-in's `MatchState` or the match's end, goes out with the next tick's events, so every player hears it.
  - **One who never came:** stood in at their side's spot at full HP. As a name and class are only known on arrival, it takes the class their side has fewest of and a free name from the bots' list.
  - **Tier and build:**
    - The tier is the spec's `StandIn`, which pvp gives: the one nearest the mean `mu − σ` of all the match's players.
    - The build is that tier's build for the class, chosen from the leaver's id. A bot plays from its build's `order`, and a player's bar has none, so the leaver's bar isn't kept.
  - **Its place:** the bot takes the leaver's slot on their side, so rounds, resets, `MatchState.sides` and the bots' ring see the body. `MatchState` goes out again, with the slot now the bot's id and `bot` set: the stand-in's arrival sends it, as any fighter's does.
  - **The outcome:** it still lists the leaver in their slot, with `left` set and their own downs, never the bot. pvp rates the leaver as losing, and the side by the result, with the stand-in counted at the leaver's mu (section 10).
  - **No way back:** a leaver who reconnects mid-match lands on their world map, not in the match. The place stays the bot's to the end.
  - **With the reconnect grace (section 4):** a drop reaches this path only when its grace runs out. Until then the player is no leaver, and a reconnect lands in the match.
  - **When no player came at all,** the match still closes with no outcome, and no stand-ins are made.
  - **A side's last player** gets no stand-in: their side concedes (section 10).
- **Arrivals, as built after the 3v3 playtest:** each fighter's arrival sends `MatchState` to everyone in the ring, not only to the one arriving. A state names only fighters who have arrived, so without it the first in saw blank frames until the countdown's state, 10 s on.
- **Once a tick, as built after step 6a:** whatever changes the match in a tick (arrivals, a phase, a point's holder or contest, the score) marks it, and one `MatchState` goes out as the tick's last event, as the tick left it.
  - Before, each change sent its own. Two fighters arriving on one tick sent everyone two states, the first naming only one of them; in a 15v15 that grows with every pair.
  - The state rides in the tick's events after its `Delta`, as before.

### Points: 15v15

- **Phases:** `prep` (20 s, the gates closed), then `fight` (up to 12 min), then `result`.
- **Each tick, for each point:**
  - It counts the living players of each side inside it.
  - **One side alone:** progress moves toward that side by `(1 + per_extra × (n − 1))`, at most `fastest`, over `capture.time`.
    - A point the other side holds first drains to no one's.
    - Full progress makes it the side's.
  - **Both sides inside:** it holds still.
- **Score:**
  - Each held point gives `per_point` a second.
  - Each down gives `per_down` to the downer's side, by the last hit.
  - The first side to `goal` wins. Otherwise the higher score wins at the time, and an equal score is a draw.
- **Respawn:** a downed player is back up at a free spot in their base after `respawn`, at full HP. Their cooldowns keep running.
- **The base:**
  - A `sim.Blocker` for each side keeps the other side's bodies out.
  - Standing inside heals `base_heal` of max HP a second.
- **A leaver:** a bot takes over their body, as in 3v3.
- **Updates:** `MatchState` carries the score and the points each second and at each change. The client moves the rings between updates at the rate it knows.
- **As built (step 6a):**
  - **No countdown:** prep (the gates shut) goes straight into the fight, and the builds sent in prep go on as it starts. A build saved during the fight goes on at the player's next respawn.
  - **A point** counts the living fighters whose centre is in its circle each tick. Progress moves by the pace above times the tick over `capture.time`. Drained past 0, a held point is no one's, and at full it's the side's. With no one in it, its progress holds where it was, with no decay, and a held point stays held.
  - **Scoring:** a side scores for each point it holds at every whole second from the fight's start, a contested or draining one included. The last second scores on the fight's final tick, so a point held through a 12-minute fight scores 720.
  - **A down scores for the other side outright.** Only an enemy's hit, or the damage over time it left, takes a fighter down: the battlefield has no mobs or hazards. So the last hit is always the other side's, with no hitter to track and no time window. A hazard added later needs this revisited.
  - **A whole side down** ends nothing: its fighters get up after `respawn`. The first side to `goal` wins on that tick. If both reach it on one tick, the higher score wins, and an equal score is a draw. On time, the side ahead wins, and an equal score is a draw.
  - **Getting up:** at the base spot whose nearest body is farthest, the first on a tie, at full HP, energy and cooldowns as they were. A stand-in for a downed leaver gets up when the leaver would have.
  - **The base:** each side moves through its own geometry, the map's with the other side's base standing as a cylinder 1,000 m tall. So the other side can't walk, leap or be knocked back into a base, while sight and the side's own moves are as before. It heals `base_heal` of max HP each second, carrying fractions as regen does.
  - **The gates** are the battlefield's `prep_wall`, one across each base's bay.
  - **The wire:** a battle's `MatchState` has round 0 and its score and points, and no wins. Its `MatchEnded` has the score and no wins or rating. A round mode's sends wins and no score.

## 8. Bots

- **Where:** `application/world/bots`, a package the instance calls each tick for its bots. The instance builds each bot's view and takes its decision as its client's input and presses.
- **A bot only presses keys.**
  - Its brain queues what a player sends: keys held, `Attack`, and `UseSkill` with aim, target and chain root.
  - These go through the same checks: `usable`, `chainReady`, energy, cooldowns and the action clock. A bot can do nothing a player can't.
- **The brain:**
  - A priority list: the first rule whose condition holds acts.
  - It decides every 100 ms, every other tick.
  - It's seeded from the match id and the bot's, so a match replays in tests. Each chance is rolled once for each thing it sees: an action, a hold, a follow-up offered.
  - A behaviour tree library was considered and left out (section 18).
- **What it sees:**
  - Its own state, now.
  - The others as they were `reaction` ago, in ticks, read from a ring the instance keeps of the last `gamedata.BotMemory` (a second) and the tick now, for matches with bots. A tier's reaction must be under it. The ring holds:
    - each tick's fighters: positions, HP, whether a hard status holds them, and their class's basic attack, which anyone can tell from their class;
    - every attack and skill started, as the world showed it: the tick it showed, its hit, and its user's next blow. That's at a basic attack's end, and at a skill's cancel point when it's cancelable, else its end. A blow's also has its shape, aim, lock, and any dash or leap toward the lock. A basic attack has no lock, as `AttackUsed` shows none.
  - Never a player's keys, presses, cooldowns or energy.
- **The rules, first first:**
  1. **Break free:** when hard-controlled with its break-free ready, it presses after `break_free`, at its tier's `defend` chance, rolled once for each hold. It presses from the break-free's root.
  2. **Defend:** it guards against an enemy's action it has seen start, unless it sees that enemy down or held, which ends the action:
     - **A delayed hit:** a blow whose shape will reach it at the hit time, by `model.Reached` at its predicted spot. A dash or leap toward the lock lands its blow from there. Few blows are delayed: a basic attack and most skills land as they start. The ones that don't are Lunge, Charge, Shadow Step and Meteor.
     - **The next blow:** with the enemy within its basic attack's reach at the guessed tick, the blow that may follow their action.
       - After a basic attack, the bot expects the next swing at its end; cancelling early beats it. A skill cut in at the attack's cancel point lands before the guard.
       - After a skill, it's guessed at the skill's cancel point when it's cancelable, else its end.
       - A player who holds the blow past the guess, or feints, also gets through, the guard spent and cooling.
     - It presses the first of its dodges and blocks that's ready and runs over the guarded tick, at the `defend` chance, rolled once for each action, and guards each action once.
     - No guard comes sooner than its reaction after the action showed.
     - A block faces the attacker; a dodge's move goes away from them.
     - A slow reaction can't beat a fast action. A basic attack ends 650 ms (Thief) to 1,250 ms (Magician) after it shows, past every tier's reaction, so plain X spam is guarded at about the tier's `defend` chance while a guard is ready. A guess within its reaction is one a tier misses.
     - If testers beat this with a fixed rhythm, the next step is a small jitter per tier on the guessed tick, not tracking habits.
  3. **Follow up:** when a key offers a chain's follow-up, by the same `holds` the world uses, it takes it at the `chain` chance, rolled once each time it's offered.
  4. **Objective** (15v15): it goes to its point while no enemy is within 10 m.
  5. **Fight:**
     - It picks its target: the nearest enemy still up.
     - It keeps its range:
       - Warrior and Thief: 70% of the attack's reach;
       - Bowman and Magician: from past 9 m it walks in to 6 m, and it steps away inside 3 m.
     - It uses the next skill of its build's `order` that's ready and paid for, its shape reaching past any dash to the lock, else it attacks.
     - It aims off by up to `aim` degrees.
  6. **Move:** it walks the nav path toward where the rules want it.
- **Walking:** `internal/shared/steer` is cmd/bot's walker, and both cmd/bot and the server's bots walk with it: `Walker`, `KeysToward`, `YawTo` and `Stall`. A path keeps `gamedata.BotPathMargin`, 0.25 m, beyond a player's radius where it can. The movement loader checks that sum against the walk grid's steps.
  - **As built (step 6c):**
    - A battle's bots path on their side's walk grid, built from the geometry that keeps them out of the other side's base, and built with the other grids as the world starts.
    - A waypoint counts as reached within 0.6 of the distance the body moved since the walker's last call, at least 0.3 m and at most 1 m. A bot sprinting out of combat moves 0.84 m between its decisions, past the 0.3 m reach of a path's half-metre cells. It swung round a waypoint it stepped past or beside, crawling, and in 3v3 too. Walkers called each frame move less than half a metre a call, so they keep 0.3 m.
- **Teams (3v3):**
  - Each side's bots share a focus: the enemy with the lowest HP share within 12 m of a teammate. They hold it 2 s before changing.
  - Heals go to the lowest teammate under 60%. Buffs go out at the round's start.
- **As built (step 5):** in a team mode the instance gives each side's bots a `bots.Team`; 1v1 has none, and plays as before.
  - **The focus:**
    - It's the enemy up with the lowest HP share within 12 m of the deciding bot or of any of its side up, as that bot sees them.
    - Whichever of the side's bots decides first picks it, and the side holds it 2 s while it's up. A focus that goes down is dropped at once.
    - With no enemy that near, a bot fights the nearest, as in 1v1, and the side holds nothing.
    - Between fights the side lets its focus go, so each round picks afresh.
    - Breaking free still steps away from the nearest enemy, not the focus.
  - **Heals:**
    - A bot with a heal in its bar, ready and paid for, heals the lowest of its side under 60%, itself among them.
    - It presses once the heal would take them as the world deals it: the caster if hurt, then the nearest hurt within the radius, up to its targets.
    - Out of reach, it walks to them and presses no other skill on the way, keeping the energy. It still attacks what's in reach.
    - It comes after defending and follow-ups, and before the build's order. A heal needn't be in the order: the Magician's isn't.
  - **Buffs at the round's start:** every buff in the shipped builds, War Cry and Focus, is first in its build's order. As no blow reaches from the spots, the first decision of a fight is the buff. The spots are 4 m apart, so War Cry from the middle spot takes both teammates.
- **Roles (15v15):** on each respawn a bot picks a point.
  - First the nearest one its side doesn't hold, then a contested one.
  - No point gets more than half the side's bots.
  - At a held point with no enemy near, one or two stay, and the rest go on.
- **As built (step 6c):** a battle gives its bots its points, as `MatchState` shows them, and the number of bots on their side.
  - **Picking:**
    - A bot drops its point when it goes down. At its first decision up, it picks the nearest point (straight line) its side doesn't hold, else the nearest contested one, else the nearest, to guard.
    - No point takes more than half the side's bots, rounded down. Once every point has its half, as on a map of one point, the nearest takes any number.
    - It stands on a spot rolled within half the point's radius, or the point's middle where no body fits on the spot, so bots don't stack.
  - **Going on:** in a point its side holds, uncontested, the first two of its bots standing in it, by id, stay. A third goes on to a point that needs it (one its side doesn't hold, or contested) and has room. With none, it stays and holds still.
  - **The objective:** with no enemy it can reach within 10 m, a bot walks to its spot, or stands on it, after defending and breaking free. An enemy standing in their own base, which the bot's side can't enter, is never fought or counted. So a chase ends 10 m out or at a base, and the bot walks back.
  - **No shared focus in a battle:** a side spread over three points fights the nearest enemy each. The 3v3 focus would pull bots from one point to another's fight.
  - **Heals in a battle** reach only teammates within 12 m. Farther ones are at another point.
- **Tiers:** a bot gets the tier whose mu is nearest the players it meets.
  - In 1v1 that's the player's `mu − σ`.
  - In a team mode it's the average `mu − σ` of the side's players, else the other side's.
  - A new player, with a large σ, meets easy bots, and the bots strengthen as the rating settles.
- **Who a bot is:**
  - a random class, with no class three times on one 3v3 side;
  - its tier's build for that class;
  - a name from the list that isn't in use in the match.
- **As built (step 5):**
  - Each side's bots take the tier nearest that side's players' mean `mu − σ`, else the other side's.
  - The class rule counts the side's players too: a bot's class is drawn from those its side holds fewer than two of.
  - **As built (step 6b):** "two" became the mode's class cap, ⌈team/4⌉ + 1: still 2 in 3v3, and 5 in 15v15, so a side of fifteen mixes its classes. The loader refuses a mode whose bot classes, at the cap each, couldn't fill a side.
- **Cost:**
  - A decision is a few shape checks and at most one path a second.
  - `bots.most` caps the bots one node runs at once. A match that would pass it waits in the queue, and its players see "Waiting for room".
  - **As built (step 6b):**
    - The world counts the bots its matches' specs place, from the open until the match's instance stops. A leaver's stand-in isn't counted: it takes a player's place, at most the match's players.
    - Each matcher step, the room is `bots.most` less the world's count, the bots of the standing offers, and those of the matches being opened.
    - Every mode's offers take from it. A match needing more bots than are left waits, and a match of players alone never does.
    - Each of its tickets hears `PvpQueued` with `waiting_room` set, once, and again with it clear just before its offer.

## 9. Matchmaking

- **A ticket:**
  - its mode and members: one character, or a party of up to the mode's `party`;
  - each member's mu and σ in the mode's rating, or in `rating_from`'s for 15v15;
  - when it joined.
- **Who can queue:**
  - The world checks at queue time and again at the offer: promoted, online and not in a match.
  - A party ticket comes from its leader (`NOT_PARTY_LEADER` otherwise), and every member must pass.
- **The matcher** runs each second for each mode. A ticket's window in mu is `start + per_second × seconds waited`, at most `most`.
  - **1v1:** tickets in mu order; it pairs neighbours within both windows, oldest first.
  - **3v3:**
    - It builds two sides of three from tickets within each other's windows, oldest first, keeping parties whole.
    - It balances the sides' mean mu by trying the few splits.
  - **15v15:** when 30 players wait, or the oldest ticket has waited `bots_after`, it deals the parties whole into two sides of 15, alternating by mu.
  - **Bots:** a ticket that has waited `bots_after` is matched with bots in every place no player in its window fills.
- **Offers:**
  - Each player gets `PvpOffer`: the mode, how many bots, and when it lapses. They answer with `PvpAnswer`.
  - Everyone accepting opens the match.
  - A decline or a lapse takes those players out of the queue (`PvpUnqueued{declined}`). Those who accepted go back with their wait kept.
  - A dead player's accept is refused with `CHARACTER_DEAD`.
- **Leaving the queue:** `PvpLeaveQueue`, logging out, or a change to the party of a party ticket.
- **As built (step 5):**
  - **Tickets:**
    - In a mode whose `party` is 1, a party member queues alone, as before.
    - In a mode that takes parties, the queuer's party is read from the social module with its leader (`social.Module.PartyOf`). Only the leader queues it; anyone else is refused `NOT_PARTY_LEADER`. A party over `party` is `PVP_PARTY_TOO_BIG`.
    - Each member must be neither queued nor going into a match, and the world must say they can fight. Any member's refusal is `PVP_PARTY_NOT_READY`, and the queuer's own refusal stays theirs. A refused ticket leaves nobody queued.
    - Those checks run unlocked, and a party change in them finds no ticket to take out. So the party is read again under the queue's lock, just before the ticket goes in, and a ticket whose party moved is refused with `PVP_PARTY_CHANGED`. Any later change finds the ticket.
    - Every member hears `PvpQueued` with the members.
  - **Offers:**
    - Each member hears the offer. It opens once every character, not every ticket, has accepted.
    - One member's decline or lapse takes the whole ticket out, and the others go back as before.
    - Every member hears `PvpUnqueued` with `by`: the member who declined, or those who let it lapse. So a member who accepted is told it was someone else (section 13).
    - One member's `PvpLeaveQueue` or going offline takes the whole ticket out too.
  - **A party change:**
    - pvp follows the social module's membership changes under a subscriber of its own.
    - A ticket in a mode that takes parties goes out with `PARTY_CHANGED` when a member's party is no longer the ticket's members. That's a member leaving, one joining, or a player queued alone joining a party.
    - A change that leaves the members as they were, such as a map, HP or leader change, keeps it.
    - After a break in the stream, each queued party is looked at again. One that can't be read goes out with `PARTY_CHANGED`, never left unchecked.
    - Once its match opens, nothing is queued, so a change does nothing.
  - **The 3v3 matcher:**
    - For each waiting ticket, oldest first, it gathers the tickets within each other's windows, oldest first, up to six players. It skips one that would make six that no two whole sides of three can hold, such as three parties of two.
    - Six players meet at once, at the split with the least gap between the sides' mean mu.
    - Past `bots_after`, the players found meet with bots in the places left. Players who fit on one side fight together against bots.
  - **The 15v15 deal, as built (step 6b):**
    - It's an open queue, with no windows. It deals once the waiting tickets hold 30 players, or the oldest has waited `bots_after`.
    - The oldest tickets that fit in 30 go in, parties whole. They're dealt high mu to low, each to the side with fewer players, ties to the lower total mu, so the sides' mu stays even.
    - A ticket that doesn't fit that side fits neither, and stays queued for the next deal: greedy dealing can leave out a party a perfect split would fit. Bots take the places left.
- **State:** in memory. A restart empties the queue, and the client shows no queue after it reconnects.
- **Results:** `MatchResult` reports:
  - the match id and mode;
  - the sides, with each fighter's downs, points taken, and whether they left;
  - the winner, 0 for a draw;
  - the rounds won or the scores.

## 10. Ratings

- **The model:** Weng-Lin's Bradley-Terry, with full pairing, for two sides.
  - It's from "A Bayesian Approximation Method for Online Ranking" (Weng and Lin, JMLR 2011), the method OpenSkill implements.
  - It's written in `internal/shared/rating`, about 80 lines.
- **For each side:**
  - μ is the sum of its members' μ, and σ² the sum of their σ².
  - Against the other side: `c = √(σ₁² + σ₂² + 2β²)` and `p = e^(μ₁/c) ÷ (e^(μ₁/c) + e^(μ₂/c))`.
  - The result `s` is 1 for a win, ½ for a draw and 0 for a loss.
  - Each member's μ moves by `σⱼ²/c × (s − p)`, and their σ² shrinks by the paper's `δ`, never below κ.
  - `tau` is added to σ before each update, so a rating never stops moving.
- **Pinned:** test vectors computed apart from the Go, in Python, from the paper's formulas as openskill.py's `BradleyTerryFull` (MIT, about 380 stars, on PyPI) writes them. They're kept as a fixture: 1v1, 3v3, draws, uneven ratings and a bot side, with tau and without. Vectors from openskill.py itself would replace them once taken from it.
- **Bots:**
  - A bot takes part with its tier's mu and σ 1.
  - Its new values are thrown away.
- **Leavers:**
  - A leaver is rated as losing to the other side.
  - Their side is rated by the result, with the bot that took their place counted at the leaver's mu.
  - A player whose place is held when the match ends (section 4) isn't a leaver, and is rated by the result.
- **As built (step 5):**
  - A leaver is told a loss, whatever their side does.
  - **A side whose players have all left concedes at once,** as a 1v1 forfeit does. The other side wins and is rated as winning against the leavers' ratings. The leavers are rated as losing.
    - Its last leaver gets no stand-in.
    - So players who stayed never play on against three stand-ins, and a match whose one player leaves among bots ends then, no bots playing on for nobody.
    - A side of bots alone from the start never concedes.
- **The shown rating** is `base + per_point × (μ − 3σ)`, at least 0. It starts at 1,000 and rises as σ shrinks with play.
- **Storage** (migration 00012):
  - **`pvp_ratings`:**
    - columns `character_id`, `mode`, `mu`, `sigma`, `matches`, `wins`, `losses`, `draws` and `updated_at`;
    - `shown`, written with `mu` and `sigma` by `pvp.yaml`'s rule, which a generated column couldn't follow;
    - key `(character_id, mode)`;
    - as built (step 4a), `placed`, written with `shown` by `pvp.yaml`'s `placement`, and the index `pvp_ratings_board` on `(mode, shown desc, character_id) where placed`. An index's predicate can't read `pvp.yaml`, so the flag is a column. The board reads its top 100 straight from that index, and a place counts those above from it.
  - **`pvp_matches`:** `id`, `mode`, `started_at`, `ended_at` and `winner`.
  - **`pvp_match_fighters`:**
    - `match_id`, `side` and `character_id`, null for a bot;
    - `bot_tier`, `left`, `downs` and `points`;
    - `mu_before`, `sigma_before`, `mu_after` and `sigma_after`;
    - as built (migration 00016), `untold`, set for a player away at the match's end until they're told (section 4), with the partial index `pvp_match_fighters_untold` on `character_id`.
  - **`pvp_builds`:** section 6, in its own migration with the arena builds (step 4, migration 00014).
- **One transaction per result:**
  - It inserts the match. The id is the key, so a repeated result does nothing.
  - It locks the players' rating rows `FOR UPDATE`, rates them and writes them.
  - 15v15 stores the match, wins and losses only.
  - **As built (step 6a):** a battle records under its own mode, `battle`, in `pvp_ratings` as a rated mode does. Its rows keep mu and σ as they started, a leaver's too, and are never placed, so the board never lists them. Its `MatchEnded` carries no rating change. The `points` column of `pvp_match_fighters` isn't built: nothing reads it yet.
- **Leaderboard:**
  - `GET /api/pvp/leaderboard?mode=duel|team` lists the top 100 by `shown`, among players past `placement`.
  - It's cached 30 s.
  - It includes the caller's own place: those above them, plus one.
  - As built (step 4a), a read is bounded to 5 s, and the callers waiting on one share it. The caller's own place comes in their profile instead, so the board is the same for every caller and one read serves them all. A tie shares its place. Names come from the character module by ID, as the modules share no tables.
- **Profile:** `GET /api/characters/{id}/pvp` gives each mode's shown rating, rank, placement matches left, wins, losses and draws. As built (step 6a), an unrated mode's standing is its record alone: its rating, placement and place are 0 and its rank empty. The board refuses an unrated mode as unknown. As built (step 4a), it also gives the place, 0 before placement, and the arena build, the saved one or the copy.

## 11. Protocol

| Message | Direction | What |
| --- | --- | --- |
| `PvpQueue{mode}` | client | Join a mode's queue; a party's leader queues the party |
| `PvpLeaveQueue{}` | client | Leave the queue |
| `PvpAnswer{offer, accept}` | client | Accept or decline an offer |
| `PvpForfeit{}` | client | Leave the match as a loss |
| `Attack.target`, `UseSkill.target` | client | Become refs, `"m:<id>"` or `"p:<id>"` |
| `PvpQueued{mode, since_ms, members, waiting_room}` | server | In the queue; sent on joining and to each party member, and again each time `waiting_room` flips: the match found waits for room for its bots |
| `PvpUnqueued{reason, by}` | server | Out of the queue: left, declined, lapsed, party changed; `by` names the ticket's members whose answer took it out |
| `PvpOffer{offer, mode, bots, until_ms}` | server | A match found |
| `PvpOfferGone{reason, requeued}` | server | Someone declined or it lapsed; `requeued` says whether this player is back in the queue |
| `MatchState{match, mode, phase, round, wins, ends_in_ms, sides, score, points}` | server | On entry and at each change, once a tick at most, as the tick left it; `sides` lists each fighter's id, name, class and `bot`; `points` each point's holder, progress and whether contested |
| `MatchState.sides[].away` | server | Set while a fighter's dropped connection holds their place (section 4). A drop and a return each send `MatchState` |
| `MatchEnded{result, wins, score, rating, mode, left}` | server | The outcome; `rating` has the mode's shown rating and rank before and after, and placement matches left; `left` is set for a player who left before the outcome |
| `Welcome.missed_matches{mode, result, left, ended_at}`, `Welcome.missed_more` | server | The matches that ended while the player was away, newest first and at most five, and how many more (section 4, as built) |
| `PlayerView.team`, `PlayerView.bot` | server | The side, 0 outside a match, and the bot flag |
| `SkillUsed.target` | server | Becomes a ref |
| `StatsChanged.timers` | server | Set when it replaces every cooldown and buff the client holds: into a match, at each round's reset, and back out |
| `ArenaBar{skill_bar}` | server | At a countdown, the arena bar the match puts on this player (step 4b, as built); its traits come in the `StatsChanged` sent with it |

| REST | What |
| --- | --- |
| `GET /api/characters/{id}/pvp` | Ratings, ranks, records and the arena build |
| `PUT /api/characters/{id}/pvp-build` | Save the arena build |
| `GET /api/pvp/leaderboard?mode=` | The top 100; the caller's place is in their profile (step 4a, as built) |

## 12. Failures

Area 55, PvP:

| Code | Name | HTTP | When |
| --- | --- | --- | --- |
| 550001 | `PVP_NOT_PROMOTED` | 409 | A Beginner queues |
| 550002 | `PVP_ALREADY_QUEUED` | 409 | Queued already, in any mode |
| 550003 | `PVP_NOT_QUEUED` | 409 | Leaving or answering with no queue |
| 550004 | `PVP_PARTY_TOO_BIG` | 409 | A party over the mode's `party` |
| 550005 | `PVP_PARTY_NOT_READY` | 409 | A member offline, not promoted, in a match or queued |
| 550006 | `PVP_IN_MATCH` | 409 | Queueing, or a refused action (section 5), in a match |
| 550007 | `PVP_OFFER_GONE` | 409 | Answering an offer that lapsed or was declined |
| 550008 | `PVP_NOT_IN_MATCH` | 409 | Forfeiting outside a match |
| 550009 | `MATCH_NOT_STARTED` | 409 | Attacking or using a skill in prep or countdown |
| 550010 | `BAD_PVP_BUILD` | 400 | An arena build that isn't the class's |
| 550011 | `PVP_UNKNOWN_MODE` | 400 | A mode that isn't in `pvp.yaml` |
| 550012 | `PVP_PARTY_CHANGED` | 409 | The party changed while its ticket was checked (step 5, as built) |
| 550013 | `NOT_ON_ARENA_BAR` | 409 | In a match, a skill from a key not on the arena bar (step 4, as built) |

## 13. Client

- **The Arena window,** `features/pvp/ArenaWindow.tsx`, opened by a new action, `arena`, on V. It sits in the key-binding window's Windows group (key-bindings.md).
  - **Play:** a card for each mode, with the rating, rank or "Placing (n of 5)", the record, and Queue or Leave queue. A refusal shows the server's message.
  - **Leaderboard:** 1v1 or 3v3, the top 100, and the player's own row.
- **The queue tag,** `features/pvp/QueueTag.tsx`, by the minimap: the mode and the time waited, counted locally from `since_ms`, with ✕ to leave. As built (step 6b), it adds "Waiting for room" while `waiting_room` is set, and the Arena card's wait reads "Waiting for room" too.
- **The offer card,** `features/pvp/MatchOffer.tsx`, with Accept and Decline. It counts down locally. Accept shows "Waiting for the others…" at once, as its Response comes only when the match opens, after the MapChange; a refusal brings the buttons back.
- **The match bar,** `features/hud/MatchBar.tsx`:
  - **Rounds:** each side's pips and the round's time.
  - **15v15:** both scores, the three points in their holders' colours, each with its ring, and the time.
  - Times count locally from `ends_in_ms`.
- **Frames:**
  - In 1v1 and 3v3 each enemy shows as a compact frame, one per row, in the right column under the two buttons.
  - Teammates take the party frames' place, under the player's own frame at the left, in 1v1 and 3v3. In 15v15 the party frames stay the party.
- **Names:** enemies red, teammates green, and a BOT tag over bots and on their frames.
- **The lock on players:**
  - `Mark` ids become refs (`m:12`, `p:<uuid>`), and `marks` adds enemies in view.
  - Tab cycles enemies.
  - `aimFor` sends the ref.
  - `TargetFrame` shows a player's name, class, level and BOT tag.
  - The chains' `lock` reads statuses for either kind.
- **Down:** a grey tint with "Down until the round ends", or "Back in 7 s" in 15v15.
- **The result screen,** `features/pvp/MatchResult.tsx`: the outcome, the rounds or scores, the rating change and any new rank, and Return.
- **The skill window's Arena tab:**
  - every skill at full and every trait open;
  - the arena bar under it;
  - saves through `PUT pvp-build`.
- **The skill bar:** in a match it shows the arena bar, and on return the world bar.
- **The map window:**
  - In a match, This map shows the sides, bases and points. The World chart is hidden.
  - The minimap shows the points.

As built for 1v1 (build step 2):

- **The lock:**
  - Every mark is a ref. In a match, enemies drawn on screen are marked after the mobs, so the lock picks and Tab cycles both. A teammate is never marked.
  - A remote skill's area at the lock centres on either kind. The place opens round a locked enemy as round a mob.
  - The key-binding window calls Tab "Lock the next target".
- **Names:** enemies `#ff6b62`, teammates `#7fe08a`. A bot's name carries a BOT tag after it.
- **A match's map plays as a hunting map,** as the world has it: move speed counts, and the lock, attacks and skills work. Before this, the client read any kind but `hunting` as a town.
- **The prep wall:**
  - It's drawn as a see-through sheet of the portals' blue, `#5fb8ff` at 0.4, laid over the ground. Added light saturated to white on the ring's sand.
  - Prediction walks against the map's solids with the wall's appended after them, from the `MatchState` that starts or ends prep or the countdown.
  - While it stands, attacks wait, and a skill's key says "Wait for the round to start."
- **The count into a round** shows mid-screen in big numbers.
  - **Its hold is predicted:**
    - The world sets each input's hold to the count's end at the tick it runs the input.
    - The client knows those ticks before the count starts, from the state that ends prep or the gap between rounds, plus `countdown_ms` from `gen-gamedata`.
    - So the predictor holds each input as the world will, and nothing is corrected across a count.
    - From the count's own state alone it would come a tick late: a tick's Delta is sent before its events, `MatchState` among them.
- **Leave match** is on the match bar. It forfeits after a second press, "Leave and lose?".
- **A held place** (section 4):
  - A fighter whose `away` is set shows "Reconnecting" on their frame and over their name.
  - Their side gets a notice when it sets, "<name> lost connection", and when it clears, "<name> is back". The client reads both from the change between states.
  - The player's own client shows "Reconnecting…" as for any drop, and lands back in the match on its `Welcome`.
  - **As built (step 7b):**
    - "Reconnecting" is a note on the match frames, under the name as "Down" is, and the tag after the name in the world, where a bot's BOT goes.
    - The notices are toasts, for the player's own side only, and only between two states of the same match.
    - A `Welcome` on a match's map keeps the match, its arena bar and its frames: only a held place lands a join there. One anywhere else leaves the match as a trip out does, so a return past the grace says at once that it counts as a loss.
    - The server sends the arena bar the fighter wears again with the return, after the `Welcome`.
- **The offer card and the result** draw over any open window.
- **The result screen** stays through the trip back, until Return closes it.
  - **A forfeit's card:** a forfeit's MapChange comes before its `MatchEnded`. So the store keeps the match left (`left`) until `MatchEnded` arrives, and the card has the match's mode and counts rounds from the player's side.
- **The pointer:** the overlay lets the pointer through to the world. Windows, `.btn` buttons and anything with a `title`, a hover help, take it back, so the queue tag's ✕ and the frame rows' tooltips work.
- **Party:**
  - In a round mode the party frames give way, their place the teammates' (none in 1v1).
  - A member on a match's map shows "In a match" on their frame and in the Party window.
- **The Map window** shows This map alone in a match. Sides, bases and points come with 15v15.

As built for 3v3 (build step 5, the client):

- **Frames:**
  - The world scene tells a store, `state/fighters.ts`, each other fighter's level, HP and statuses as of the newest frame, and whether they're down. A fighter unchanged keeps its object, so its frame doesn't render again.
  - A fighter shows once the world has drawn them.
  - **Teammates** take the party frames' place in a round mode, in the left column under the HUD's tray, with their BOT tags. A teammate who is down is dimmed, with "Down".
  - **Enemies** show in every round mode, 1v1 included, in the right column under the Settings and Back to characters buttons: a 180 px compact frame each, one per row. The match bar never names them.
  - A mode the client doesn't know has no rounds, so the party frames stay the party, as they will in 15v15.
- **Party members:** the queue tag and the offer card say who is queued with the player ("with Robin and Jade"), from `PvpQueued`'s members, named as their party names them.

As built for builds and ranks (step 4b):

- **The skill window's Arena tab,** for a promoted character, beside its Skills tab:
  - The class's active skills, each at the kit's points, a follow-up's with its chain mark. No points to spend.
  - Every trait open, chosen with the same diamonds as in the world, and saved with the build. A choice of the default is kept as none, as the world keeps it.
  - The arena bar under the grid: a skill dragged from the grid goes on a key, keys swap, and one dropped off the bar empties. A follow-up dropped on a key is refused, as on the skill bar. Its keys drag apart from the skill bar's, so neither bar takes the other's.
  - **The save** is the skill bar's: 800 ms after the last change, at once on leaving the world, closing the tab or closing the skill window, one at a time. Leaving waits for every save, the skill bar's and the build's both. Closing the window saves the skill bar's change too, so a queue straight after fights with both.
  - The build comes from the profile, and is taken while the player hasn't changed theirs. Once they have, theirs is the newest.
- **The profile** is read on entering the world, after each `MatchEnded` and promotion, and as the Arena window opens. Only the newest read lands, and a failed one keeps the last.
  - The window's opening reads only when no read is under way and none landed in the last 10 s, so opening and shutting it in a burst spends one call of the account's REST limit.
- **A rate limit's refusal,** a 429 on any REST call, shows "Too fast. Wait a moment." as a toast while the player is in the world, whether or not its caller tells its own failures. The same words already showing aren't stacked twice.
- **The skill bar in a match:** from each countdown it holds the arena bar `ArenaBar` names, and back out of the match the world bar.
  - The world bar stays the player's own: a change waiting as a match starts still saves the world bar, never the match's.
  - While the arena bar is on, its keys take no drags or drops, and the tooltip points to the Arena tab. A drag or drop that comes anyway, as a script's could, changes nothing.
  - A player whose build didn't reach the match keeps the world bar on screen, and no key of it is refused.
- **The Arena window:**
  - **Play:** each rated card adds the player's rating and rank, with their place once placed, or "Placing (2 of 5)", and their record. Nothing shows until the profile is read.
  - **Leaderboard:** 1v1 or 3v3, the top 100 with the player's own row marked. Under it is the player's own place when they're past the top 100, or how far their placement is.
  - **One read per mode per 30 s,** as the server serves it. A read under way is shared, and a failed one is tried again at the next ask. So flipping between the modes spends no calls under the per-account REST limit.

As built after the 3v3 playtest:

- **Party notices:** a ticket taken out by another member's answer names them from `PvpUnqueued.by`, as their party names them:
  - "Robin didn't answer the match offer, so your party left the queue."
  - "Jade declined the match, so your party left the queue."
  - The player's own lapse still says "You didn't answer the match offer, so you left the queue." Their own decline says nothing.
  - A member the party doesn't name is "A party member". A lapse naming no one is a match the world couldn't open: "The match couldn't open, so you left the queue."
- **A team match's leaver:**
  - Leaving a match under way, by Leave match or by a rejoin after a drop whose grace ran out, says at once: "You left the match: it counts as a loss. Your rating updates when the match ends."
  - Its end comes as a General line, never the result card: "Your 3v3 match ended: Defeat. Rating 1000 → 968 (−32)." The card is for those the match ended with. The leaver may be anywhere by then, even in another match, so the line is drawn from `MatchEnded`'s own `mode` and `left`, not from the match the store holds.
  - A 1v1 forfeit ends the match at once, so its forfeiter still gets the card.
- **A match that ended while the player was away (as built):** each comes as a General line on their `Welcome`, timed by the server's clock: "Your 1v1 match ended 2 h ago, while you were away: it counted as a loss." A player held through a drop gets the result instead, "…while you were away: Victory." Past five, one more line: "…and 3 more matches ended while you were away."

As built for 15v15 (step 6d-1, the client):

- **The map:** `mapjson` writes a battle map's `battle`: each side's base, its points in the map's order, and the cylinder each side's geometry gains, the other side's base standing 1,000 m tall. The client adds that cylinder after the map's own solids, as the server's `Kept` does. `gen-gamedata` gives each battle map's point names, each mode's map, and the battle's goal, length and respawn.
- **Prediction:** through prep the player walks the map with its prep wall. In the fight and after, they walk their side's geometry, chosen from the match's sides. So a runner into the other base stops at its edge as the world stops them, never corrected.
- **The match bar in a battle:**
  - "You 312", the clock, then "290 Them": the scores from the player's side, in place of the pips. The phase reads "Battle".
  - Under the clock, the three points, each named by its name's first word ("West"), in its holder's colour, grey for no one's. Each has a ring filled as far as the point has turned, in the colour of the side it turns toward, and a wavy underline while contested. Their labels say "West Ruin: yours, contested".
  - The rings move with each `MatchState`: at each change of holder or contest, and every whole second with the score. They aren't moved between states.
- **Colours:** the HUD and the 2D map views (the bar, the map window, the minimap) use the bar's pair, `--ally` `#5fd35f` and `--enemy` `#e04848`. Things drawn in the 3D world (the point rings and the base wall of step 6d-2) use the name tags' pair, `#7fe08a` and `#ff6b62`, to match the tags beside them under the world's light.
- **The Arena card:** "15 against 15: first to 1,000 points, or the most in 12 minutes.", marked Unrated, with the record alone: no rating, rank or placement.
- **The result:** "Score 312 – 290" from the player's side, in place of the rounds, and no rating.
- **Down:** "Back in 7 s", counted from when the down showed to the battle's respawn, then "Back in a moment" till the world gets them up.
- **Frames:** the party frames stay the party, and the enemies get no frames in the right column.

As built for 15v15 (step 6d-2, the points and bases drawn):

- **Map window and minimap:** both draw the bases and the three points as circles, in the bar's pair from the player's side: their base and the points their side holds in ally green, the other's in enemy red, no one's grey. A contested point's edge is dashed, and it pulses from the player's side's colour to the other's and back each second (as built after item 1's pictures, 11-25); with reduced motion it stays still, the dashes marking it. With no match, all are grey. The map window labels each point by its name ("West Ruin"). `gen-gamedata` gives each point's spot and radius, and each base's.
- **The points in the world:** each has a ring on the ground at its edge, 25 cm wide, and a soft glow over the rest, in the holder's colour from the player's side: the name tags' `#7fe08a` and `#ff6b62`, or grey for no one's. While contested, ring and glow pulse from the player's side's colour to the other's and back each second, whoever holds it (as built after item 1's pictures, 11-25: a contested point drew as its holder's alone, the same as uncontested). The ring and glow follow the ground 4 cm over it, as the telegraphs do; the glow is the boss areas' premultiplied glow, at 0.2.
- **The other side's base:** while the player walks their side's ground (the fight and after, not prep), an open see-through wall in `#ff6b62` at 0.3 rings the other base, from under its lowest ground to 3 m over its highest. It marks the edge the player is stopped at.
- **Round them** (after 6d-2's review): a ring, a glow or a wall has 64 segments, or more so its edge runs at most 0.5 m, a terrain cell, between vertices. The shipped 5 m points keep 64; the 9 m bases' walls take 114.

## 14. Balance

- **The harness,** `pvp_balance_test.go`, sits beside the rotation harness in `model/world`, on its helpers, against a player's kit, whom no blow MISSes. Its targets:
  - **Time to down:** for each pair of classes, the attacker's rotation, chained as players fight on the default traits, on a still kit defender downs it in 12 to 18 s. The mean over all pairs is 14 to 16 s.
    - Presses come at human timing: each cut and each skill coming ready lands 100 ms (2 ticks) past the earliest point. An attack held to repeat follows at its end.
    - The burst below keeps perfect timing, as a cap should hold for the best hands.
  - **Burst:** the longest control-into-chain burst takes at most 40% of a kit's HP.
    - A burst starts with a control that keeps its target from acting (stun, freeze, sleep or knockdown; a pin holds only the feet), with every skill ready and the energy full.
    - It chains as players fight, a follow-up that waits on a control open while one holds, and each control that lands adding its hold. A sleep ends at the next blow.
    - It counts what lands until the target is free.
- **As tuned:**
  - `scale` alone moves every pair alike, so it sets the mean. Each class's own `hp` and `damage` bring the pairs together: without a MISS between players they leave them 1.27× apart, where they were 9× apart at scale 1.
  - With passive evasion between players the pairs stayed 2× apart, past the band's 1.5×, as a kitted Warrior landed 26% of its swings on a Thief. So players have no evasion against each other (section 17).

  | attacker → defender | Bowman | Magician | Thief | Warrior |
  | --- | --- | --- | --- | --- |
  | Bowman | 15.9 s | 13.3 s | 15.0 s | 16.8 s |
  | Magician | 13.6 s | 17.1 s | 13.6 s | 13.3 s |
  | Thief | 16.0 s | 13.5 s | 15.2 s | 17.0 s |
  | Warrior | 16.0 s | 13.5 s | 15.1 s | 16.9 s |

  - The mean is 15.1 s. The Thief's evasiveness in a match is its dodges': it downs the others in 13.5 to 17.0 s, and they down it in 13.6 to 15.2 s.
  - The longest burst is the Warrior's Charge stun into Ground Slam on a Magician: 38% of its HP.
  - The kits' HP in a match: Bowman 4,549, Thief 4,285, Magician 4,227, Warrior 3,452.
  - **Human timing,** after #294's live pass, where a Thief downed a Warrior in about 22.5 s against the table's 17.0:
    - The live presses cut at the cancel points within about 80 ms. Six presses a round went skill straight after skill, though, and a skill waits out its full second for the next. The table assumes an attack cuts every skill. The damage per press matched the harness.
    - Pressing 100 ms late made the Bowman's, Thief's and Warrior's rotations 5 to 14% slower. Their `damage` factors took it back: Bowman 0.81 to 0.92, Thief 1.10 to 1.21, Warrior 1.19 to 1.25. The Magician's rotation cuts little, so it kept 0.94.
    - At the old factors and human timing, Thief on Warrior took 18.7 s and the mean was 16.2 s, past the band.
- **Playtests** judge the rest, class against class:
  - block uptime, such as the Warrior's Bastion every 2 s;
  - dodges and heals;
  - whether 60 and 90 s rounds end by downs more often than by time.

## 15. Testing

- **pvp module:**
  - **The matcher:** pairing within windows, windows widening, parties kept whole, bots after 15 s, the 15v15 deal, and the bot cap.
  - **Offers:** accept, decline, lapse, and back to the queue with the wait kept.
  - **Ratings:** the pinned vectors, bots unchanged, leavers, the shown rating and ranks, and placement.
  - **The store,** with pgtest: the leaderboard query and a repeated result doing nothing.
- **World:**
  - `OpenMatch` and teardown.
  - The return point: checkpoints write it, and a forfeit on leaving lands the player there with their timers run on.
  - **The reconnect grace,** judged by ticks:
    - a drop holds the body, which takes hits and resets and runs its timers;
    - a `Hello` within the grace lands on the body in the match, with nothing reset;
    - past it, the stand-in in 3v3 and 15v15, and the win in 1v1;
    - a second drop's fresh grace;
    - a match ending during the grace, rated by its result;
    - a match ended while a player was away, told at their next `Welcome` once;
    - logout and Leave match forfeit at once.
  - **Combat between players:**
    - reach with the lock;
    - dodge and block;
    - statuses and break-free;
    - push without a hop, and no guard;
    - allies by side;
    - no credit;
    - refused actions.
  - **Rounds:** all down, time with HP shares, even rounds, the 5-round draw, and the reset.
  - **Capture:** rates, draining, contested points, score, respawn, the base's blocker and heal, and leavers replaced.
- **Bots:**
  - Rules on fixed views: defending at the hit time, breaking free, taking a follow-up, keeping range, and team focus.
  - The next blow through the ring, judged by ticks: plain X spam against an expert Thief parried at about its tier's chance; X cut into a skill at the cancel point landing through the guard; an X held back past the end landing while the guard cools.
  - A bot-against-bot match runs to its end, the same each time from one seed.
  - The tier choice.
- **End to end** (`test/`, pgtest), judged by ticks, not the wall clock:
  - **A match:**
    1. A player queues for 1v1 and accepts.
    2. The match runs against a novice bot to its end.
    3. The player is back at their spot with their timers, and their rating row has moved.
  - **A drop:** a player whose socket drops mid-round and reconnects within the grace is back in the match, on their body, and the match runs on. Dropped again and back past the grace, they've left: the match is rated, and they land where they queued (`TestADroppedDuelistIsHeldThroughTheGraceButNotPastIt`).
- **Client:** the lock on players, the match bar, the counting of the offer and the queue tag, the Arena tab, and the result screen.
- **Live:**
  - each mode with each class;
  - a party in 3v3 and in 15v15;
  - a forfeit, and a relog mid-match;
  - a dropped connection back within 20 s, and one that stays away past it.

## 16. Build

1. **1v1 against a bot, server:**
   - `pvp.yaml`, the `pvp` map kind and `ring`;
   - the pvp module with the 1v1 queue, offers and bot fill;
   - match instances and the return point;
   - the kit, and arena builds read from the default copy;
   - combat between players and rounds;
   - ratings and their tables;
   - area 55 and the protocol;
   - a first brain that walks, attacks and uses its order.
   - It may land as two PRs, as long as each new call ships with its caller.
2. **1v1, client:**
   - the Arena window (Play tab) and V;
   - the queue tag and the offer card;
   - the match bar and the lock on players;
   - names and BOT tags;
   - the down overlay and the result screen.
3. **Bots that fight:**
   - defending, breaking free, follow-ups, range and tiers;
   - `shared/steer`;
   - the PvP balance harness, with the kit and `scale` tuned.
4. **Builds and ranks:**
   - the Arena tab and `PUT pvp-build`, applied between rounds;
   - the profile, ranks and placement;
   - the leaderboard and its tab.
   - As built, it lands as two PRs: 4a the server (builds, the bar in a match, the profile and board), then 4b the client.
5. **3v3:**
   - party tickets and the 3v3 matcher;
   - allies by side;
   - leavers replaced;
   - team focus and heals for bots;
   - enemy frames.
   - As built, it lands as three PRs: the server's queue and match rules first, then the bots' team play, then the client.
6. **15v15:**
   - `battlefield`, capture points, score, respawns and bases;
   - the deal into sides and the bot cap;
   - bot roles;
   - the 15v15 match bar and points on the maps.
   - As built, it lands as four PRs: the battle's rules on the server (6a), the deal and the bot cap (6b), bot roles (6c), then the client (6d).
   - 6a hides the 15v15 card in the Arena window, as the client has no battle HUD yet. 6d shows it again, gives a mode without rounds its own card line (a battle has no "best of"), and builds the HUD.
   - 6d lands as two PRs: 6d-1 makes the battle playable (the map's battle data, prediction, the bar, the card, the result, the down and the frames); 6d-2 draws the points and bases on the map views and in the world.
   - 6c: the bots' nav blocks the other side's base, as the server's per-side geometry does.
   - 6d:
     - `mapjson` writes a battle map's bases and points for the client; 6a leaves it as it is;
     - the client's sim blocks the other side's base as the server does, or a runner into it is corrected at every input.
7. **The reconnect grace,** after 15v15:
   - `reconnect` in each mode;
   - the held presence and body, and the return through `Hello`;
   - `MatchState.sides[].away`;
   - the client's tag and notices;
   - their tests.

## 17. Decisions

The user's choices are in section 1. These calls were made in planning and logged for the user to push back on:

1. **Promoted characters only:** a Beginner's kit is a starter's.
2. **Queue from anywhere,** in combat or not, and play on while waiting. An offer moves the player at once when accepted.
3. **One mode at a time** in the queue.
4. **An offer lasts 10 s.** A decline or lapse leaves the queue, and those who accepted keep their place.
5. **The offer says how many bots** are in the match.
6. **The kit:** level 30, the class's auto-assign split, and the best item a class can wear by level 30 in each slot, at the middle of its rolls.
7. **The arena build is separate from the world's,** since every skill counts in a match and the world bar can't hold skills not yet learned. It starts as a copy.
8. **Rounds start at full energy.** No potions, regen, items or progression changes in a match.
9. **No hit guard between players,** and a plain hit doesn't knock back, so combos land. A skill's push still moves the target.
10. **Nothing a player does is unblockable.**
11. **Leaving is a forfeit.** In 1v1 the other side wins. In 3v3 and 15v15 a bot takes the leaver's body. Amended on 2026-11-24: Leave match and logout forfeit at once, and a dropped connection only after a 20 s grace with the body held and no bot (section 4).
12. **15v15 kills score 5 and points 1 a second.** Respawns take 10 s at a base the enemy can't enter, which heals 10% a second.
13. **An even round counts for neither side,** with a draw after five rounds.
14. **Bots carry a BOT tag** and names of their own, so no one mistakes them for players.
15. **Bot strength comes from `mu − σ`,** so new players meet easy bots.
16. **Ratings:** a shown rating of `1000 + 40 × (μ − 3σ)`, ranks every 200 from 1,200, and five placement matches.
17. **The queue lives on one node** until there are more.
18. **Ratings and bot brains are written in-house** (section 18).
19. **No passive evasion between players;** active dodge and block are the defence. A blow between players never MISSes; mobs keep evasion both ways, and crits stay.
20. **Each class has its own factors** on its kit's HP and the hits it deals, beside `scale`, so every pair of classes takes 12 to 18 s.

## 18. Alternatives considered

- **Libraries.** These were searched and vetted for adoption, a source scan and activity. Each was cloned and read, never run.
  - **go-openskill** (intinig, MIT): Weng-Lin with teams.
    - The source is clean: pure math over gonum, with no exec, network, cgo or init code.
    - But pkg.go.dev lists no importers and it has no tagged release, so it isn't proven.
    - The update is about 80 lines from the paper, pinned by vectors written from openskill.py's formulas, which are well used.
  - **Glicko-2 libraries** (zelenin, treepeck, Kashomon, mashiike): they rate one player against one, not teams, and each has few users.
  - **TrueSkill:** a patented model, so it was left out.
  - **go-behaviortree** (joeycumines, Apache-2.0):
    - The source is clean, it's maintained, and CodeQL runs on it.
    - But it has 5 importing modules, and the bots would use 2 of its 20 node types.
    - A priority list in the bots package does the job with nothing to vet.
  - **Open Match** (Google's matchmaker): it runs on Kubernetes, far beyond one queue in one process.
- **Bots as outside clients,** as cmd/bot is: each bot would need a login, a websocket and a server round trip per decision. In-process bots press the same keys at none of that cost.
- **Bots as mobs:** mobs have attacks, not skills, energy, chains or blocks. A bot as a player fights by the players' rules.
- **Own stats in matches:** the user chose equal stats.
- **One rating for every mode:** the user chose one each for 1v1 and 3v3.
- **One shared arena instance holding many matches:** separate instances keep each match's tick, rules and teardown its own, and a match is no bigger than a busy map.
- **Revives in 3v3, tag team, team kills or crystals in 15v15:** the user chose otherwise.
- **For a dropped connection:**
  - **No grace,** as first planned: every blip, common on Wi-Fi and phones, was a rated loss.
  - **A bot standing in during the grace:**
    - It would keep a team whole through a long drop.
    - But a stand-in starts with cooldowns ready and no statuses, so a free return would make a drop a reset. A player weaker than the bots could drop to let one fight for them.
    - Guarding both, by carrying the body's state both ways and one grace a match, would cost two hand-overs a drop.

## 19. Enemies are solid

Added on 2026-11-28. The user: "players should block each other way, because currently its hard to tell if it comes to the correct distance for an attack/skill, like it is with mob". Offered all players everywhere or enemies in matches only, they chose enemies in matches only.

### The rule

- **In a match, a fighter's body blocks their enemies',** by the rule a mob's body blocks a player's ([3D world](./3d-world.md) section 5):
  - a move may not bring two enemies' centres closer while their bodies overlap;
  - a body can always walk out of an overlap;
  - a jump clears an enemy only if the feet stay above the enemy's jump height, `enemy_height` in `movement.yaml`, from first touch until past their centre.
- **So a melee fighter walking up to an enemy stops touching them,** centres 0.75 m apart, in reach of every melee attack. That is the cue the user asked for, the one mobs give.
- **Whose bodies count:**
  - **Enemies:** living ones, down excepted. A held body, kept for a lost connection, counts, as does a bot that took over a leaver.
  - **Teammates** pass through each other.
  - **Outside a match** nothing changes: players pass through each other everywhere.
- **Moves:** a skill's move stops at an enemy where it stops at a mob, and one that passes through mobs (`through`) passes through enemies. A shove stops at the shoved player's enemies, where they stand that tick, as a knockback stops at mobs.
- **A jump over an enemy** should be easy, the user said ("a jump over an enemy should be easy"). A player is 1.75 m tall and the sim's jump peaks at 1.90 m, so the feet were over a head for only about 0.17 s, and a timed walk jump cleared from a window about 0.15 m deep. So an enemy is 1.25 m tall to a jump, `enemy_height` in `movement.yaml` beside the player's `height`, the height of the mobs a jump already clears easily:
  - the body stays as wide, so a walk into an enemy still stops at 0.75 m, in reach;
  - the feet are over 1.25 m for about 0.35 s at a match's walk speed (0.17 s over 1.75 m), and a walk jump taken from 1.5 to 2.6 m short of the enemy's centre clears, a window about 1.1 m deep, and from 1.9 to 4.1 m short at a sprint;
  - a jump pressed against an enemy rises over his top in about 0.1 s and walks over, so being pinned is no longer a tight escape.

  Every mob but a boss is low enough to clear easily.

### Seeing it the same on both ends

- **A step is judged against enemies where the mover's screen drew them,** at the input's `view`, as mobs are. The world keeps each tick's fighters' bodies for `mobs.History` ticks, as the arena keeps the mobs' frames. The client builds the same circles from the remote players' tick states, not their smoothed draw, as it does for mobs.
  - So walking into an enemy who stands still, or moves slowly, plays with no correction.
- **Two enemies moving into each other** can still end deep in each other. Each stops at the other's drawn place, and each has moved on since by their speed times the drawn delay and the round trip. Walking head-on with an ordinary lag, they can meet centre to centre and then walk through, which is the case the user wants gone.
- **So the world parts them:**
  - At the end of each tick's player steps, two enemies more than 0.25 m into each other are pushed apart along the line between their centres, half each, back to 0.25 m. 0.25 m is the overlap a walking mob may have before it counts as deep (3D world section 7).
  - Each push is stopped as a walk is: by solids, the map's edges, a base's blocker and the prep wall. A body that can't move leaves the rest to the other.
  - Pairs are parted in id order, so the result is the same every run.
  - A body in a `through` move isn't parted until its move ends.
  - Each client sees its own push as a correction, as it sees a skill's shove today, so a correction comes only from a head-on clash. The `?fps` readout counts them.

### Bots

- Bots step against enemies' bodies like any fighter, through their own view.
- **A bot whose walk an enemy blocks,** an enemy it isn't trying to hit, steps round them, so it doesn't stand pressing at a body on its way to a point or a base.
- A melee bot closing on its target stops touching them, as a player does, which is already in its reach.

### The change

| Part | Before | After |
| --- | --- | --- |
| `instance/players.go` (the player step) | `geometry(p)` plus the mobs `p.view` saw | Also the enemies' bodies `p.view` saw, in a match |
| The world | — | Keeps each tick's fighters' bodies for `mobs.History` ticks. Parts deep enemies at the end of the player steps |
| Shoves on a player | Stop at solids | Also at the shoved player's enemies, where they stand |
| Bots | Steer as if no body were in the way | Step round an enemy who blocks their walk |
| `web/src/game/systems/prediction.ts` | `blockersAt(view)`: the mobs | The mobs and, in a match, the enemies at `view`, from the remote players' tick states |

The sim doesn't change: enemies are `sim.Blocker` circles, as mobs are. Neither does the protocol.

### Testing

- **Sim** (Go and TypeScript, the shared vectors): a body 1.25 m tall to a jump as a blocker. A walk into it stops touching. A jump clears it at a walk from either end of the window (`enemy-jump-over-at-walk-late` and `-early`) and at a sprint, a jump taken too early lands short and stops, and one pressed against it walks over after the feet rise.
- **The world:**
  - In 1v1, a player walking into a standing enemy stops with centres 0.75 m apart, and the state the client would predict equals the world's.
  - In 3v3, teammates walk through each other. Outside a match, two players walk through each other.
  - A downed enemy doesn't block, and a held body does.
  - A skill's move stops at an enemy, and a `through` move passes. A shove stops at an enemy of the shoved.
  - **The head-on clash:** two enemies walk at each other with views a few ticks behind. They end no more than 0.25 m into each other and never pass through. Planted without the parting, they pass through, so the test must fail there.
  - Parting is stopped by a wall, and the free body takes the rest.
  - **The parting's fixes, each planted:** a walk jump from contact over a standing enemy leaves him where he stood; a round's reset after the fighters swapped sides starts each on their spot; a blink and a shadow step past an enemy end where the skill put them; a fighter pressed against an enemy off the centimetre grid stops at the wire's body, and a mob's kept body is the wire's centimetre.
- **Bots:** in 15v15, a bot walking to a point past a standing enemy gets round them.
- **Client:** the predictor's blockers hold the enemies at a view, never the teammates, and nothing outside a match.
- **Live:** a short 1v1 check, written by the scenario writer and waiting for the user's yes, as every live test does:
  - walk into an enemy and attack from there;
  - clash head-on;
  - jump over an enemy;
  - in 3v3, walk through a teammate.

### Build

One PR, server and client together, since a server that blocks with a client that doesn't predict it would correct every touch. Each step is one commit:
1. the world's kept bodies, the blocking in the player step and in shoves, and the parting;
2. bots stepping round;
3. the client's predictor.

### As built

- **The world** (`instance/foes.go`): `keepFoes` files each tick's living fighters, by id, as the frame is broadcast; `blockersFor` gives a step the mobs of its view and the other side's bodies of the same tick, built once per tick and side. The shove in `hitPlayer` meets `foesNow`. `part` runs at the end of `simulate`.
- **Parting keeps order, for a pair about to meet.** Two enemies closing at a walk each way come 0.55 m nearer a tick, more than the 0.5 m a parting leaves between centres, so with stale views they can cross within one tick, and a push along the line between their new centres would finish the swap. A pair whose line now points the other way from the last tick's frame is parted back along that last line, but only when it could be a step's crossing:
  - in the last frame their centres were within a tick's reach of touching (their bodies' two radii plus each body's own longest step, its move-speed bonus and a sprint included, and a centimetre each), and level, neither above the other's 1.25 m top;
  - neither has moved more than its own reach since the frame, so a body the world placed (a round's reset, a respawn, a teleport, a blink) is never pulled back;
  - they overlap now. A skill that puts a fighter past an enemy (a blink, a shadow step) leaves them there.
- **A jump over lands where it lands.** A pair one of whom was above the other's head in the last frame isn't parted this tick, and a pair whose centres are no nearer than in the last frame isn't parted: a jumper coming down in the enemy's body, or a body walking out, is left to the walk, as a mob's overlap is.
- **Bodies are at the wire's centimetre.** The world keeps each fighter's and each mob's body for a view at the centimetre the frames give (`quant`), and both expanders hold another body's whole view at the centimetre too (the player's own stays exact), so the page builds the same circles. A fighter pressed against an enemy, or a mob, off the grid stops where the page's sim stops, with no correction a tick. The opening-overlap check compares the wire's bodies of now with the last frame's.
- **A match is never tiered.** The crowd tier of large-regions.md section 5 holds a player's motion-only steps every second tick; on a match's map `stepPlayers` skips it, since an enemy's missed tick would leave a blocker guessed, as a mob's would. The check is the instance's, not a loader rule on `interest.crowd`.
- **A jump from contact clears.** The sim holds a body at an enemy's side until its feet rise over the 1.25 m top, about 0.1 s on, and then lets it walk over, as it does for a mob; the shared vector `enemy-jump-pressed-against` records it. The "first touch" in the rule above is not enforced, so a pinned fighter jumps out, and at 1.25 m it is quick.
- **Bots** (`bots/brain.go`, `round`): a bot pressing toward a point, or its side's focus, with another enemy ahead within 1.8 m goes along that enemy's side. A melee bot already stops at 0.7 of its reach, 1.3 m.
- **The client** (`RemotePlayers.foesAt`, wired in `world.ts`): the enemies' circles at a view from each player's state at that tick, in id order; the down flag is kept per frame beside the look.

### Decisions

1. **Enemies in matches only,** the user's choice. The reach problem only comes up against players, and that happens only in a match.
2. **The mob rule, not a new one,** so an enemy feels like a mob to walk into. A crowd of enemies can pin a player as a crowd of mobs can, and a jump is the way out.
3. **Parting head-on clashes in the world,** half each, rather than letting overlaps stand. Without it, enemies walking at each other could meet and walk through, which is the case the user asked to remove. The price is a small correction in a clash.
4. **Downed enemies don't block,** as dead mobs don't, so a body on the ground never walls a path.
5. **An enemy is 1.25 m tall to a jump,** the user's "a jump over an enemy should be easy". The height is data (`enemy_height`), and the world's kept bodies, the parting's above-the-top checks and the client's predicted blockers all read it, so the two ends stay the same number. The body's width, and so the stop at 0.75 m, is unchanged.

### Alternatives considered

- **All players solid everywhere:** offered, and turned down by the user. Players could stand in the way of NPCs, gates, waypoints and bridges, and towns would jam. Nobody fights a player outside a match, so it would judge no reach.
- **Teammates solid too:** a side could jam its own base gate in 15v15 or pin its own fighter in a corner.
- **A higher jump in matches,** and **airborne bodies passing through enemies:** a higher jump changes the jump everywhere in a match, and passing through would let any hop pass.
- **No parting, overlaps allowed:** a still or slow enemy would block perfectly, but a head-on clash could pass through.
- **Judging steps against where the enemy is now,** not where the mover's screen drew them: every touch with a moving enemy would correct, since the client can't know where the enemy is now.
