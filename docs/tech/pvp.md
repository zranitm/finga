# PvP

> **Status: approved · 2026-11-18.** The high-level design for [I1. PvP matches](../product/features/pvp/i1-pvp-matches.md), [I2. Arena: 1v1 and 3v3](../product/features/pvp/i2-arena.md), [I3. Battlefield: 15v15](../product/features/pvp/i3-battlefield.md) and [I4. PvP rating and ranks](../product/features/pvp/i4-pvp-rating.md).

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
  1. `PvpQueue{mode}` goes through the gateway to `pvp.Queue`. That asks the world whether the characters can fight (`world.CanFight`): promoted, online and not in a match.
  2. Each second the matcher forms matches, with bot places where players are missing. It pushes an offer to every player in each one.
  3. When everyone accepts, pvp calls `world.OpenMatch(spec)`.
  4. The world builds a match instance, moves the players in with their world state set aside, adds the bots, and runs the rules.
  5. At the end it reports the result through a port (`Results`), a queue the pvp module follows, moves everyone back and closes the instance.
  6. pvp stores the match and the new ratings in one transaction, and pushes each player's rating change.
- **One queue owner:** the queue is in memory, in the pvp module, on one node. A second node will need a single owner, such as a lease row. Until then there's one node.
- **Ports both ways,** as the other modules do (`internal/adapter`):
  - pvp → world: `CanFight`, `OpenMatch` and `SetBuild`.
  - world → pvp: `Results` and `Build`.
  - The gateway serves both modules' commands and pushes.

## 3. Data

### `pvp.yaml`

```yaml
modes:
  duel:   {name: 1v1,   team: 1,  party: 1, map: ring,        rated: true,  rounds: {best_of: 3, most: 5, length: 60s}}
  team:   {name: 3v3,   team: 3,  party: 3, map: ring,        rated: true,  rounds: {best_of: 3, most: 5, length: 90s}}
  battle: {name: 15v15, team: 15, party: 4, map: battlefield, rated: false, rating_from: team}
queue:  {bots_after: 15s, offer: 10s, window: {start: 2, per_second: 0.2, most: 8}}  # window in mu
rounds: {prep: 10s, countdown: 3s, between: 5s}
battle: {prep: 20s, length: 12m, goal: 1000, per_point: 1, per_down: 5, respawn: 10s, base_heal: 0.10,
         capture: {time: 8s, per_extra: 0.25, fastest: 2}}
result: 10s
kit:    {level: 30}
scale:  {hp: 1.0, damage: 1.0}  # tuned by the harness, section 14
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
  - A presence that goes, through logout or a dropped connection, is a forfeit. The instance removes the player as any leave does, and checkpoints `back`.
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
  - `model.Resolve(PlayerAttacker(attacker.derived, type).Times(power), defender.statuses.Defending(PlayerDefender(defender.derived)))`, times `scale.damage`.
  - Damage over time uses `Steady`, as on mobs.
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
  - The aggro group (taunt, lose sight) doesn't take on players, as `statuses.yaml` says.
  - Damage over time on a player is credited to its inflicter.
- **Chains:** `dodged` and `blocked` count any enemy's attack, a player's as a mob's.
- **In combat:** `stampFights` already stamps both players of a `Damage`.
- **Telegraphs:** players' attacks draw no warning areas, as today. The swing is the warning.

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
  - Max HP is multiplied by `scale.hp`. HP regen is 0.
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
  - The client saves it 500 ms after the last change, as it does the skill bar.
- **In a match:**
  - The world reads the build at entry, through `Build`.
  - A build saved while the player is in a match goes to the world too, through `SetBuild`. It applies from the next round's start.

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
- **A forfeit:**
  - In 1v1 the other side wins at once.
  - In 3v3 a bot of the tier the match's bots have takes over the leaver's body, as it stands: the same class, build and HP. The match goes on.

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
  - The others as they were `reaction` ago, in ticks, read from a ring the instance keeps of the last `gamedata.BotMemory` (a second), for matches with bots. A tier's reaction must be under it. The ring holds:
    - each tick's fighters: positions, HP, whether a hard status holds them, and their class's basic attack, which anyone can tell from their class;
    - every attack and skill started, as the world showed it: the tick it showed, its hit, and its user's next blow, at its cancel point when it's cancelable, else its end. A blow's also has its shape, aim, lock, and any dash or leap toward the lock.
  - Never a player's keys, presses, cooldowns or energy.
- **The rules, first first:**
  1. **Break free:** when hard-controlled with its break-free ready, it presses after `break_free`, at its tier's `defend` chance, rolled once for each hold. It presses from the break-free's root.
  2. **Defend:** it guards against an enemy's action it has seen start:
     - **A delayed hit:** a blow whose shape will reach it at the hit time, by `model.Reached` at its predicted spot. A dash or leap toward the lock lands its blow from there. Few blows are delayed: a basic attack and most skills land as they start. The ones that don't are Lunge, Charge, Shadow Step and Meteor.
     - **The next blow:** with the enemy within its basic attack's reach, the blow that may follow their action, guessed at that action's cancel point when it's cancelable, else its end. A player who holds the blow past the guess, feints or cancels early gets through.
     - It presses the first of its dodges and blocks that's ready and runs over the guarded tick, at the `defend` chance, rolled once for each action, and guards each action once.
     - No guard comes sooner than its reaction after the action showed.
     - A block faces the attacker; a dodge's move goes away from them.
     - A slow reaction can't beat a fast swing, which is the tiers' main difference. A basic attack's cancel point comes 250 ms (Warrior) or 150 ms (Thief) after it shows, the world counting whole ticks. So expert and master bots guard a Warrior's next blow, master bots alone a Thief's, and the rest guard neither.
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
- **Teams (3v3):**
  - Each side's bots share a focus: the enemy with the lowest HP share within 12 m of a teammate. They hold it 2 s before changing.
  - Heals go to the lowest teammate under 60%. Buffs go out at the round's start.
- **Roles (15v15):** on each respawn a bot picks a point.
  - First the nearest one its side doesn't hold, then a contested one.
  - No point gets more than half the side's bots.
  - At a held point with no enemy near, one or two stay, and the rest go on.
- **Tiers:** a bot gets the tier whose mu is nearest the players it meets.
  - In 1v1 that's the player's `mu − σ`.
  - In a team mode it's the average `mu − σ` of the side's players, else the other side's.
  - A new player, with a large σ, meets easy bots, and the bots strengthen as the rating settles.
- **Who a bot is:**
  - a random class, with no class three times on one 3v3 side;
  - its tier's build for that class;
  - a name from the list that isn't in use in the match.
- **Cost:**
  - A decision is a few shape checks and at most one path a second.
  - `bots.most` caps the bots one node runs at once. A match that would pass it waits in the queue, and its players see "Waiting for room".

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
- **The shown rating** is `base + per_point × (μ − 3σ)`, at least 0. It starts at 1,000 and rises as σ shrinks with play.
- **Storage** (migration 00012):
  - **`pvp_ratings`:**
    - columns `character_id`, `mode`, `mu`, `sigma`, `matches`, `wins`, `losses`, `draws` and `updated_at`;
    - `shown`, written with `mu` and `sigma` by `pvp.yaml`'s rule, which a generated column couldn't follow;
    - key `(character_id, mode)`, with an index on `(mode, shown desc)`.
  - **`pvp_matches`:** `id`, `mode`, `started_at`, `ended_at` and `winner`.
  - **`pvp_match_fighters`:**
    - `match_id`, `side` and `character_id`, null for a bot;
    - `bot_tier`, `left`, `downs` and `points`;
    - `mu_before`, `sigma_before`, `mu_after` and `sigma_after`.
  - **`pvp_builds`:** section 6, in its own migration with the arena builds (step 4).
- **One transaction per result:**
  - It inserts the match. The id is the key, so a repeated result does nothing.
  - It locks the players' rating rows `FOR UPDATE`, rates them and writes them.
  - 15v15 stores the match, wins and losses only.
- **Leaderboard:**
  - `GET /api/pvp/leaderboard?mode=duel|team` lists the top 100 by `shown`, among players past `placement`.
  - It's cached 30 s.
  - It includes the caller's own place: those above them, plus one.
- **Profile:** `GET /api/characters/{id}/pvp` gives each mode's shown rating, rank, placement matches left, wins, losses and draws.

## 11. Protocol

| Message | Direction | What |
| --- | --- | --- |
| `PvpQueue{mode}` | client | Join a mode's queue; a party's leader queues the party |
| `PvpLeaveQueue{}` | client | Leave the queue |
| `PvpAnswer{offer, accept}` | client | Accept or decline an offer |
| `PvpForfeit{}` | client | Leave the match as a loss |
| `Attack.target`, `UseSkill.target` | client | Become refs, `"m:<id>"` or `"p:<id>"` |
| `PvpQueued{mode, since_ms, members}` | server | In the queue; sent on joining and to each party member |
| `PvpUnqueued{reason}` | server | Out of the queue: left, declined, lapsed, party changed |
| `PvpOffer{offer, mode, bots, until_ms}` | server | A match found |
| `PvpOfferGone{reason, requeued}` | server | Someone declined or it lapsed; `requeued` says whether this player is back in the queue |
| `MatchState{match, mode, phase, round, wins, ends_in_ms, sides, score, points}` | server | On entry and at each change; `sides` lists each fighter's id, name, class and `bot`; `points` each point's holder, progress and whether contested |
| `MatchEnded{result, wins, score, rating}` | server | The outcome; `rating` has the mode's shown rating and rank before and after, and placement matches left |
| `PlayerView.team`, `PlayerView.bot` | server | The side, 0 outside a match, and the bot flag |
| `SkillUsed.target` | server | Becomes a ref |
| `StatsChanged.timers` | server | Set when it replaces every cooldown and buff the client holds: into a match, at each round's reset, and back out |

| REST | What |
| --- | --- |
| `GET /api/characters/{id}/pvp` | Ratings, ranks, records and the arena build |
| `PUT /api/characters/{id}/pvp-build` | Save the arena build |
| `GET /api/pvp/leaderboard?mode=` | The top 100 and the caller's place |

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

## 13. Client

- **The Arena window,** `features/pvp/ArenaWindow.tsx`, opened by a new action, `arena`, on V. It sits in the key-binding window's Windows group (key-bindings.md).
  - **Play:** a card for each mode, with the rating, rank or "Placing (n of 5)", the record, and Queue or Leave queue. A refusal shows the server's message.
  - **Leaderboard:** 1v1 or 3v3, the top 100, and the player's own row.
- **The queue tag,** `features/pvp/QueueTag.tsx`, by the minimap: the mode and the time waited, counted locally from `since_ms`, with ✕ to leave.
- **The offer card,** `features/pvp/MatchOffer.tsx`, with Accept and Decline. It counts down locally. Accept shows "Waiting for the others…" at once, as its Response comes only when the match opens, after the MapChange; a refusal brings the buttons back.
- **The match bar,** `features/hud/MatchBar.tsx`:
  - **Rounds:** each side's pips and the round's time.
  - **15v15:** both scores, the three points in their holders' colours, each with its ring, and the time.
  - Times count locally from `ends_in_ms`.
- **Frames:**
  - In 3v3 the enemies show as party-style frames under the bar.
  - Teammates take the party frames' place in 1v1 and 3v3. In 15v15 the party frames stay the party.
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
- **Leave match** is on the match bar. It forfeits after a second press, "Leave and lose?".
- **The offer card and the result** draw over any open window.
- **The result screen** stays through the trip back, until Return closes it.
- **Party:**
  - In a round mode the party frames give way, their place the teammates' (none in 1v1).
  - A member on a match's map shows "In a match" on their frame and in the Party window.
- **The Map window** shows This map alone in a match. Sides, bases and points come with 15v15.

## 14. Balance

- **The harness,** `pvp_balance_test.go`, sits beside the rotation harness in `model/world`. Its targets:
  - **Time to down:** for each pair of classes, the attacker's rotation on a still kit defender downs it in 12 to 18 s. The mean over all pairs is 14 to 16 s. `scale.hp` and `scale.damage` set it.
  - **Burst:** the longest control-into-chain burst takes at most 40% of a kit's HP.
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
  - A bot-against-bot match runs to its end, the same each time from one seed.
  - The tier choice.
- **End to end** (`test/`, pgtest), judged by ticks, not the wall clock:
  1. A player queues for 1v1 and accepts.
  2. The match runs against a novice bot to its end.
  3. The player is back at their spot with their timers, and their rating row has moved.
- **Client:** the lock on players, the match bar, the counting of the offer and the queue tag, the Arena tab, and the result screen.
- **Live:**
  - each mode with each class;
  - a party in 3v3 and in 15v15;
  - a forfeit, and a relog mid-match.

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
5. **3v3:**
   - party tickets and the 3v3 matcher;
   - allies by side;
   - leavers replaced;
   - team focus and heals for bots;
   - enemy frames.
6. **15v15:**
   - `battlefield`, capture points, score, respawns and bases;
   - the deal into sides and the bot cap;
   - bot roles;
   - the 15v15 match bar and points on the maps.

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
11. **Leaving is a forfeit.** In 1v1 the other side wins. In 3v3 and 15v15 a bot takes the leaver's body.
12. **15v15 kills score 5 and points 1 a second.** Respawns take 10 s at a base the enemy can't enter, which heals 10% a second.
13. **An even round counts for neither side,** with a draw after five rounds.
14. **Bots carry a BOT tag** and names of their own, so no one mistakes them for players.
15. **Bot strength comes from `mu − σ`,** so new players meet easy bots.
16. **Ratings:** a shown rating of `1000 + 40 × (μ − 3σ)`, ranks every 200 from 1,200, and five placement matches.
17. **The queue lives on one node** until there are more.
18. **Ratings and bot brains are written in-house** (section 18).

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
