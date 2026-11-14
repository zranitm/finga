# Animation cancel

> **Status: approved · 2026-11-12.** The high-level design for [C9. Animation cancel](../product/features/combat-and-progression/c9-animation-cancel.md) and [Systems → Combat → Actions](../product/systems/combat.md). It describes the code as it stands on `main` (after classes and skills, part 4) and the change it plans. Since [Mobs fight back](./mobs-fight-back.md) (C12), mobs attack instead of touching, and `touch_guard` is `hit_guard`. Since [Energy](./energy.md) (C14) there is no attack speed: `TicksIn` counts every action, and the Thief's, Bowman's, and Magician's attacks are re-paced (section 4). Since 2026-11-14, a skill cuts an attack just after its strike, and a press at any time during an action goes at its cancel point (section 15).

## 1. What has to hold

- **One action at a time, as today.** A basic attack or an active skill is an action. The next one starts when the current one ends, unless it cancels.
- **Only the other kind cancels.** A skill may cut off a basic attack from the attack's cancel point. A basic attack may cut off a skill from the skill's cancel point, if game data marks the skill cancelable. Neither kind cuts off its own.
- **The client computes, the server validates.** The client times every action and decides when to send the next one, and plays it at once. The server checks each attack and skill against the player's timeline as it already checks cooldowns. A cancel isn't a message of its own, so the check adds a comparison, not traffic.
- **Timing lives in game data.** Each action's length, cancel point, and wind-up are data, so tuning never needs a release.
- **A modified client gains nothing past a perfect chain.** The server holds every action to its earliest allowed tick, whatever the client sends.

## 2. The change at a glance

| Part | Today | After |
| --- | --- | --- |
| `data/classes.yaml` | `attack.cooldown`: the basic attack's turn | `attack.length`, 1.5× today's, with optional `hit` and `cancel` |
| `data/skills.yaml` | A skill takes the attack's turn and starts its cooldown | Each active skill has its own `length`, `cancelable`, and optional `hit` and `cancel`; a stage may change them |
| `data/actions.yaml` | — | New: the default cancel point (and, until section 15, the early-press margin) |
| `data/movement.yaml` | Walking at 200 px/s; jumps at 620 px/s against 1900 px/s² | 0.8× every speed and 0.64× gravity: the same jumps, 1.25× the air time |
| `data/mobs.yaml` | Mob speeds; a 1 s touch guard in code | 0.8× speeds; `touch_guard: 1500ms` in data; EXP and drop chances 1.5× |
| World | `cooldownUntil`: one turn shared by attacks and skills | An action timeline per player: kind, hit, cancel point, end |
| `StatSheet` | `attack_cooldown_ticks` | `attack_length_ticks` |
| Client | `AttackGate`, which skills take turns from | `ActionClock`: the same timeline, and when a press goes |
| Balance harness | Basic attacks only | Also skills, one at a time and in chains |

## 3. Game data

### `data/actions.yaml` (new)

```yaml
cancel: 0.3   # where an action's cancel point falls, as a share of its length, unless the action names its own: just after the strike
```

### `data/classes.yaml`

```yaml
attack: {kind: melee, damage: physical, width: 64, height: 56, length: 600ms}   # was cooldown: 600ms
```

- **`cooldown` becomes `length`.** Skills now have both a length and a cooldown, and the basic attack's value is a length: how long the action keeps the player busy.
- **`hit`** (optional, default 0): where the hit lands, as a share of the length. 0 is the start, as today.
- **`cancel`** (optional, default from `actions.yaml`): where the cancel point falls, as a share of the length.
- **Every basic attack can be cut off by a skill.**

### `data/skills.yaml`

```yaml
- id: strike
  kind: active
  effect: damage
  length: 700ms         # how long using it keeps the player busy; its cooldown is separate
  cancelable: true      # a basic attack may cut it off from its cancel point
  cooldown: 2s
  ...
  stages:
    - {at: 25, name: Crushing Blow, ..., length: 900ms}   # a stage changes only what it names, timing included
```

- **`length`** is required on every active skill. A passive has none.
- **`cancelable`** (default false) is the list of skills a basic attack may cut off.
- **`hit` and `cancel`** work as on attacks.
- **A stage may change `length`, `hit`, `cancel`, and `cancelable`.**

### Validation refuses

- an active skill without a length, or a passive with any timing;
- a length that isn't positive;
- a share outside 0 to 1, or a `hit` after `cancel`;
- any key it doesn't know, `early_press` included since section 15.

The data doesn't know the tick rate, which is the world's setting. So the world refuses to start with a tick longer than any action, as it already refuses a tick that splits a movement step.

The client generator carries all of it, `actions.yaml` included.

## 4. The timeline

**An action's length in ticks:**
- **A basic attack and a skill alike:** ⌈length ÷ tick⌉. Cooldown speed shortens only a skill's cooldown. Until C14, attack speed shortened a basic attack: ⌈length ÷ (1 + attack speed) ÷ tick⌉.
- Both go through `TicksIn`, so a length of whole ticks counts as exactly that many, and anything counts as at least one.

**Its points,** as offsets from the start in ticks, where *L* is the length:
- **Hit:** ⌊*L* × hit⌋.
- **Cancel point:** ⌊*L* × cancel⌋, held to at least the hit and at least one tick, so a cancel never lands before the hit or in the action's own first tick.

Every attack and every cancelable skill takes the default 0.3, just after the strike (section 15). Heal and the buffs, which nothing cuts, name 0.85: their cancel point only times their pose, their shape, and the recovery lean.

| Class | Length | Ticks | Cancel point | A skill cuts in at |
| --- | --- | --- | --- | --- |
| Beginner | 1050 ms | 21 | 6 | 300 ms |
| Warrior | 900 ms | 18 | 5 | 250 ms |
| Thief | 650 ms | 13 | 3 | 150 ms |
| Bowman | 1050 ms | 21 | 6 | 300 ms |
| Magician | 1250 ms | 25 | 7 | 350 ms |

Until section 15, attacks cut at 0.85 (the Thief's at 0.8): ticks 17, 15, 10, 17, and 21, leaving windows of 150 to 200 ms at their ends.

**When the next action may start,** given the current one:
- **The other kind:** from the cancel point, if the current action may be cut off by it. Every attack may be cut off by a skill, and a skill by an attack only if it's cancelable.
- **Otherwise:** at the end.

**The world's `model/ticks.go` holds this,** beside the cooldown counts: `TicksIn` and `ActionPoints`. `web/src/lib/fixed-step.ts` and `web/src/progression/actions.ts` port them, and shared vectors in `server/testdata/actions/` pin the two. Each vector has a length, a hit, a cancel, and a tick, and gives the expected ticks and points. Both sides replay them.

## 5. World

- **Each player holds an action** in place of `cooldownUntil`: its kind (none, attack, or skill), whether it may be cut off, and the ticks of its hit, its cancel point, and its end. Only the map instance holds it. A map change, a death, or a fresh join starts with none, as `cooldownUntil` does today.
- **Attacks:** `attackQueued` stays. In `resolveAttacks`, a queued attack goes once the tick reaches its next allowed start, as an early one waits today.
  - It starts a new action at this tick.
  - Its hit lands at the hit tick: now when `hit` is 0, as for every action in the data today.
- **Skills:** `castSkills` keeps its queue, `SKILL_WAITING`, and its refusals.
  - The turn it waits for becomes the next allowed start for a skill.
  - It still goes up to `castSlack` (150 ms) early. Its action and its cooldown count from the allowed start, not from the early use, so a chain gains nothing from the slack.
  - **It no longer starts the attack's cooldown.** Its own action's length replaces that.
- **Order in the tick is unchanged:** buffs expire, skills go, then attacks. So when both are ready, a skill goes ahead of a held attack, and the attack takes the next allowed start after the skill.
- **Wind-up:**
  - When `hit` is above 0, the action holds a pending hit, landed in the tick it comes due, before skills and attacks. It uses the player's position and facing then, and the attack and power the action started with. A heal finds the hurt in reach then, and a buff's time runs from then.
  - Pending hits are a list, not one slot: a skill that goes within the slack before an attack's cancel point leaves the attack's hit still to land.
  - A death or a map change drops the pending hit.
  - A skill's energy and cooldown are spent at the start.
  - No action in the data has a wind-up yet; the world tests cover it on test data, as #44 did for heals before heal skills existed.
- **What a press can't do:** an attack sent before its allowed start waits for it; a skill waits in the queue. Neither is refused for timing, so a press too early simply goes later. That is the whole check.

## 6. Protocol

- **No new messages.** `Attack` and `UseSkill` carry every action, early or not.
- **`StatSheet.attack_cooldown_ticks` becomes `attack_length_ticks`.** It keeps field number 6, so `buf breaking` (WIRE) passes. The JSON name changes, and the gateway and the client change with it.
- **No new codes.** A press can't be refused for timing (section 5).

## 7. Client

- **`ActionClock`** (`web/src/game/systems/actions.ts`) replaces `AttackGate` and the skill keys' `turnIn` and `takeTurn`. It holds the same action as the world, in local time from the shared tick counts: kind, cancelable, cancel point, and end. The client draws at the press, so it needs no hit.
- **A press:**
  - **An attack pressed any time during a cancelable skill:** it goes at the skill's cancel point, or at once if the point has passed, so it frees the player from the skill's slow. The cancelable skills cut off at 0.3, just after their strike.
  - **A skill pressed any time during an attack:** it goes at the attack's cancel point, just after its strike, or at once if the point has passed. Until section 15 it had to come within `early_press` of the point, and one pressed earlier waited for the end.
  - **Otherwise:** it waits for the end.
  - A waiting press is held on the client and sent when its moment comes, as a skill key waiting for its turn is today. A newer press takes its place, except that an attack press never takes a waiting skill's: while a skill waits, the attack key holds back, so mashing attack doesn't lose skills.
- **Holding the attack key** attacks again as soon as an attack may start, timed to that moment rather than the next frame: at a cancelable skill's cancel point, as a fresh press would, and otherwise at each action's end.
- **Sending starts the action at once** on the client: the swing pose, a ranged shot's flight, a skill's shape, its cooldown, and its energy check.
  - A refusal of a skill gives back its action, as it gives back the turn today.
  - A timeout or a disconnect keeps it, as it may have landed.
- **Cut short:** a new action replaces the pose and the shape of the one it cancels. A ranged shot already flying keeps flying, since its hit already landed.
  - For other players, a caster's new `SkillUsed` replaces their last skill shape still showing.
  - Their basic attacks aren't drawn for others today, and this doesn't change that.
- **The skill window** shows each active skill's length, and "A basic attack can cut it short" on cancelable ones, so players can learn the list.

## 8. Balance

- **The harness** (`world/internal/model/balance_test.go`) paced a fight by the basic attack alone: a skill was "a stronger hit in place of a normal one".
- **It gains a rotation model** (`rotation_test.go`). For each class against each mob of its level, it counts damage per second three ways:
  1. basic attacks alone;
  2. skills on cooldown, one action at a time, with no cancels: as each action ends, the strongest damage skill ready and paid for goes, else a basic attack;
  3. a chain as players fight (section 15): a skill goes the moment it's ready and paid for, at the running attack's cancel point at the earliest, an attack cuts each cancelable skill at its cancel point, and the time saved goes to more attacks.
- **Skill points** follow the rule the bots spend them by: the promotion's skills up to what it asks, the rest in the main skill, which is the promotion's first before it and the class's first after.
- **Each action's damage** is its mean on the mob, misses included, from the world's own combat rules. A skill also waits for the [energy](energy.md) it costs, counted from none, which each basic attack gives.
- **It logs the chain's gain** (3 over 2) for every class and mob. Until section 15 it held it to at most 22%, around the product's "up to about 20%". The hits-to-kill bands stay checked against 1, as before, so a player who never cancels or uses skills still meets them.
- **With the first data** (every skill 1050 ms, cancel at 0.85, the damage skills cancelable), a perfect chain adds 0–13%, most often 5–8%. Skills on cooldown carry most of the damage, and a cancel shaves only an action's tail.
- **Kill speed with skills on cooldown, and skills' share of a fight's damage, are logged for the user's playtest, not checked.** Whether the hits-to-kill bands move to the skill rotation is decided after it.
- **Tuning moves only data:** skill lengths, the cancel points, `cancelable`, and the skill powers.

## 9. Pace

The product's slower pace is data, plus one constant moved into data, so cancels have a window a player can feel.

- **Actions, 1.5×:** the class attacks' lengths are in section 4's table.
  - Every active skill's length started at 1050 ms. [Combat feel](combat-feel.md) makes it 1500 ms.
  - Damage and heal cooldowns grew 1.5×: Strike, Throw, Power Shot, and Fire Arrow 3 s; Spark 3.75 s; Slash Blast and Heal 4.5 s; Double Stab 2.25 s. Combat feel grows the damage skills' 1.5× again.
  - Buffs keep their cooldowns and durations.
- **Movement, 0.8×:** `movement.yaml` scales every speed by 0.8 and gravity by 0.64, the scaling the move speed stat already applies. A jump keeps its height (v² ÷ 2g) and its reach, and its air time grows 1.25×.

  | Constant | Now | Paced |
  | --- | --- | --- |
  | `walk_speed` | 200 | 160 |
  | `climb_speed` | 140 | 112 |
  | `jump_velocity` | 620 | 496 |
  | `gravity` | 1900 | 1216 |
  | `max_fall_speed` | 900 | 720 |
  | `drop_through` | 0.25 s | 0.3125 s |

  - `drop_through` grows 1.25×, so a dropped-through platform is ignored for the same fall distance.
  - The knockback hop, `knockbackVY` (−250 px/s), becomes −200 to keep its height. Its sideways push is a distance and stays.
  - The movement vectors in `server/testdata/movement` are made again, the move speed cases included, and both sims replay them. The maps' route tests show every platform is still reachable.
- **Mobs:**
  - Every mob's `speed` is scaled by 0.8 and rounded.
  - The touch guard, `invulnerability` in `fight.go` (1 s), becomes `touch_guard: 1500ms` in `mobs.yaml`.
- **Rewards:**
  - Every mob's `exp` is scaled by 1.5 and rounded, and every drop's `chance` by 1.5, held to 1, so EXP and drops an hour stay about where they are.
  - Hits to kill and touches survived keep their balance targets. In the harness, `refPace` grows 1.5× (0.75 s to 1.125 s, the warrior's paced attack at 80% landing), `killsPerLevel` falls from 30 to 20 with the larger EXP, and `touchEvery` reads `touch_guard`.
- **Client:** the swing pose (`SWING_MS`) grows to 225 ms. Shots keep their flight speed.

## 10. When things fail

| Failure | What the player sees | Why it is safe |
| --- | --- | --- |
| An action reaches the server a tick early, from jitter | Nothing unusual | An attack waits for its tick. A skill within 150 ms goes, but its action counts from the allowed start |
| A modified client sends every action at once | Its actions go no faster than a perfect chain | The server holds each action to its earliest allowed start |
| A lag spike delays an action | The hit lands late, and the chain resumes from there | Each action starts when it reaches the server; nothing builds up |
| Death or a map change mid-action | The action ends; a pending wind-up hit is lost | It is held only in the map instance |
| A deploy changes timing data while a player is on a map | One action may use the old timing | The world and the client load the same data at start; the next join brings the new data to both |

## 11. Testing

- **Game data:** the new fields, defaults, stage overrides, and each validation refusal.
- **Timeline vectors:** ticks and points across lengths, shares, and tick rates, including the one-tick minimum and the hit floor; replayed by Go and TypeScript.
- **World:**
  - an attack cut off by a skill at its cancel point, and not before;
  - a cancelable skill cut off by an attack at its cancel point; a skill that isn't cancelable runs to its end;
  - attack after attack and skill after skill waiting for the end;
  - an early attack waiting, and an early skill within the slack counting from the allowed start;
  - a wind-up hit landing late, at the position then, and dropped at a death or a map change.
- **Client:** `ActionClock`'s press rules, the other kind pressed at any time going at the cancel point, a held key versus a fresh press, and a newer press replacing a waiting one; refusals giving the action back; a cut-off shape.
- **End to end:** an attack, Strike, and an attack again, chained, land sooner than the same three one at a time.
- **Pace:** the movement vectors at the new constants, the route tests on every map, and the touch guard read from data.
- **Balance:** the chain's gain logged for every class and checkpoint.

## 12. Build order

On its own branch and PR. Each step is one commit, green on `make test`, `make lint`, `make cover`, and `make breaking`:

1. **Data and timing:** `actions.yaml`, `length` (renamed from `cooldown`), `hit`, `cancel`, and `cancelable`, validation, the generator, `AttackLengthTicks`, `SkillLengthTicks`, and `ActionPoints` in Go and TypeScript, and the shared vectors.
2. **World:** the action timeline in place of `cooldownUntil`, cancels in `resolveAttacks` and `castSkills`, and wind-up hits.
3. **Protocol and client:** `attack_length_ticks`, `ActionClock`, the press rules, cut-off shapes, and the skill window's line.
4. **Pace:** the attack lengths, skill cooldowns, movement constants and their vectors, mob speeds, the touch guard in data, EXP and drop chances, and the swing pose.
5. **Content and balance:** skill lengths and the cancelable list, the harness's rotation model and bound, and a `make bots` run.

## 13. Decisions

Agreed on 2026-11-12:

1. **Holding the attack key repeats at each action's end; only a fresh press cancels.** Holding attack would otherwise cut off every cancelable skill without any timing, which the strict timing is meant to avoid. The user changed this on 2026-11-12, with C11's casting slow: an attack, pressed or held, frees the player from a skill at its cancel point.
2. **The first cancelable list:** the damage skills (Strike, Throw, Spark, Slash Blast, Double Stab, Power Shot, Fire Arrow). Heals and buffs (Heal, Iron Body, Haste, Focus) run to their end.
3. **First skill lengths:** 1050 ms each, the Beginner's paced attack, then tuned in step 5 with the harness.
4. **Rename the attack's `cooldown` to `length`** in data, code, and the sheet, since skills now have both.
5. **The skill window shows each skill's length and whether an attack can cut it short,** so players can learn the list.
6. **Build wind-up now** (`hit`), though no action uses it yet, so the first charged skill is data only.
7. **A slower pace** (section 9): actions take 1.5× as long, movement goes 0.8× as fast, and mobs match. Fights keep their hits to kill, and EXP and drop chances grow 1.5×, so the level cap still takes about 10 hours.

Agreed on 2026-11-13, with [Energy](./energy.md) (C14):

8. **The Thief's attack cancels at 0.8 of its 650 ms,** until every attack moved to 0.3 on 2026-11-14, so its window stays 150 ms and its earliest cancel falls at 400 ms. Its one cancel saves 23% of the attack, above the 15–20% band. The harness's chain gain and boss potions don't move, since its rotation waits on skill cooldowns.

Agreed on 2026-11-14, after the user played C14 (section 15):

9. **A skill cuts an attack from 30% of its length,** just after its strike, as an attack already cut a cancelable skill. The user found an attack cutting a skill felt right, and a skill cutting an attack too slow to feel the same.
10. **A press at any time during an action goes at its cancel point,** both ways. The early-press margin goes, and so does the Thief's 0.8.
11. **The "up to about 20%" reward and the harness's 22% bound go.** With no timing to a cancel, a chain is how everyone fights, so its gain isn't a reward to cap. The harness models a chain as players fight and logs it.
12. **A skill short of the energy the running attack gives waits for the server's count,** then goes or says not enough. The client still doesn't predict gains.

## 14. Alternatives considered

- **The client decides cancels alone:** it saves the server nothing, since it checks every action already. And a modified client could then cancel every action at once and multiply its damage.
- **The server applies the early-press margin:** it would judge presses by when they arrive, so network jitter would decide some cancels. The client knows exactly when the player pressed, so it applies the margin. The server only holds actions to their earliest allowed tick.
- **A cancel message, or a field on `Attack` and `UseSkill`:** the server needs nothing beyond the action itself to check one.
- **Cancel points in milliseconds instead of a share:** a length changed later, as C14 re-paced three attacks, would then need its cancel point moved by hand. A share moves with the length.
- **Rounding the cancel point up:** cancels would save only 11–14% of an attack's length at 0.85, short of the target.
- **The Thief at 0.85, or at 700 ms:** at 650 ms and 0.85, its window would be 100 ms, two ticks, too short to feel. A 700 ms attack keeps 150 ms at 0.85, but the harness had its kills 7% slower than before C14, and its worst boss at 30 potions, the limit.
- **Keeping today's pace:** a Warrior's window would be 100 ms, two ticks, too short to feel. The window is about 15% of an action's length whatever the pace, since a larger share would pass the reward target, so only longer actions widen it.
- **Same fight length at the slower pace,** with fewer and heavier hits: mob HP and the balance targets would all change. Keeping hits to kill and raising EXP and drops keeps the harness's targets and the time to the level cap.
- **A skill cutting an attack at 0.5** (2026-11-14): the Warrior's skill would cut in at 450 ms, as an attack cuts a skill. The user chose 0.3, just after the attack's strike, which mirrors where a skill is cut.
- **An earlier point with the early-press margin kept:** a skill pressed with the attack, as when mashing, would still wait for the attack's end.
- **The client predicting an attack's gain** for an early cancel: it would need its own reach check against mobs and a rollback. Waiting for the count costs at most what's left of a round trip, and only when the skill needs that gain.
- **Lowering skill powers to offset the faster chain:** the user's playtest decides first.

## 15. Skills cut attacks just after the strike

Agreed on 2026-11-14 (section 13). A skill could cut an attack only from 85% of it, 750 ms into the Warrior's 900, and one pressed more than 100 ms before that waited for the attack's end. A basic attack already cut a cancelable skill just after its strike, from a press at any time.

### The rule

- **Every cancel point the other kind can use is at 0.3 of the action, just after its strike:** an attack's for a skill, and a cancelable skill's for an attack. A skill cuts in 150 to 350 ms into an attack (section 4).
- **A press of the other kind at any time during an action that it may cut goes at the action's cancel point,** or at once if the point has passed.
- **Nothing else moves.** A skill still can't cut a skill, nor an attack an attack. After an attack, a held attack key still repeats at each action's end. Heal and the buffs still run to their end.

### The change

| Part | Before | After |
| --- | --- | --- |
| `server/data/actions.yaml` | `cancel: 0.85`, `early_press: 100ms` | `cancel: 0.3`; no `early_press` |
| `server/data/classes.yaml` | The Thief's attack names `cancel: 0.8` | Every attack takes the default |
| `server/data/skills.yaml` | Each cancelable skill names `cancel: 0.3` | They take the default. Heal, Iron Body, Haste, and Focus name `cancel: 0.85`, so their pose, shape, and recovery lean keep their timing |
| Loader (`load/actions.go`) and generator | `early_press`, its 500 ms limit, and `early_press_ms` | Gone. The loader refuses the key as any unknown one |
| `ActionClock.startAt` | A skill goes at an attack's cancel point only if pressed from `early_press` before it | Any press of the other kind that may cut the running action goes at its cancel point |
| `skill-keys` | Short of energy: "Not enough energy" | Short while the player's latest attack isn't counted yet: it waits for the count (below) |
| Balance harness | A perfect chain in the no-cancel order, held to 22% | A chain as players fight, logged (below) |
| World | — | No change. It already holds a skill sent early to the attack's cancel point, which the data moves |

### Energy at an early cancel

- [Energy](./energy.md) left gains unpredicted because the earliest cancel came at least 400 ms into an attack, after the server's count was back. It now comes 150 to 350 ms in, about one round trip. So a skill that needs the running attack's energy can be pressed before the client has the count.
- **The client's own `AttackUsed` tells it the count has come:**
  - The world counts the gain in the tick the attack starts, as its hit lands then.
  - That tick's frame carries the count in the player's `Delta`. `broadcast` sends the frame before the tick's events, through the one queue to the gateway, which keeps their order on the connection. So the `Delta` with the gain arrives before the attack's `AttackUsed`.
  - The client ignores its own `AttackUsed` today. Now it marks its latest attack counted.
- **The wait, in `skill-keys`:** a press that finds the shown energy short while the latest attack is uncounted waits until that attack's `AttackUsed` arrives or the attack ends, whichever comes first. Then it checks again, and goes or shows "Not enough energy".
  - The attack's end bounds the wait. An attack the world never started sends no `AttackUsed`, and by its end the skill would have gone without a cancel anyway.
  - A skill the shown energy already covers doesn't wait.
  - The attack key holds back while a skill waits, as it does while the clock holds one, so mashing attack doesn't lose the skill. A newer skill press takes its place.
  - With a round trip under about 100 ms, the count is back by every class's cancel point, so nothing waits. A slower one delays only that skill, by what's left of the round trip.
- **The server needs nothing new.** A skill that waited reaches it after the attack started there, so its arrival check counts the gain.

### The balance harness

- **Its chain kept the no-cancel rotation's order,** so time saved by a cancel went to waiting on the next skill's cooldown, not to more attacks. It read a 6–15% gain, inside the 22% bound, while a chain as played, with skills cut from 0.3 since C11, gives 19–44%.
- **The new chain fights as players do.** After each action, the strongest damage skill that is paid for and will be ready before the running attack ends goes, at the attack's cancel point or its ready time, whichever is later. An attack cuts each cancelable skill at its cancel point. Otherwise attacks follow attacks.
- **The no-cancel rotation stays,** and the gain is still the chain over it.
- **A trial on `main`'s data with attacks cut from 0.3** gave, over one action at a time:

  | Class | At 0.3 | Before, measured the same way |
  | --- | --- | --- |
  | Warrior | +39 to +47% | +26 to +32% |
  | Thief | +37 to +55% | +30 to +44% |
  | Bowman | +32 to +47% | +23 to +33% |
  | Magician | +38 to +42% | +19 to +32% |

  A chain deals 7–10% more than the best chain before.
- **No bound holds it.** `maxChainGain` and its check go, and the gain is logged with kill speed and skills' share.
- **The hits-to-kill bands and the boss bounds read basic attacks alone** (`fightOf`), so they don't move. Skill powers stay until the playtest.

### Testing

- **Game data:** the default share, the four skills that name 0.85, and `early_press` refused as an unknown key.
- **Client:**
  - a skill pressed as an attack starts goes at its cancel point, and one pressed after the point goes at once;
  - an attack pressed during a cancelable skill still goes at its cancel point;
  - an attack after an attack, and a skill after a skill, still wait for the end;
  - a held attack key still repeats at each end after an attack;
  - a skill short while its attack is uncounted waits, then goes when the count covers it;
  - after a whiff, the count leaves it short, and it shows the notice once;
  - the attack's end ends the wait;
  - a skill the energy covers doesn't wait;
  - the attack key holds back while it waits.
- **End to end:**
  - with no energy, an attack on a mob and a skill pressed at once: the skill goes and lands;
  - the `Delta` carrying an attack's gain reaches the client before that attack's `AttackUsed`.
- **Harness:** `TestRotation`'s made-up cases for the new chain, including a skill ready mid-attack going at the attack's cancel point and an attack filling the time saved.
- **Live:** a playtest of attack into skill for every class, the energy wait from none, and skill into attack as before.

### Build

One PR, each step one commit, green on `make test`, `make lint`, `make cover`, and `make breaking`:

1. **Data:** the shares above, and `early_press` out of the data, loader, validation, generator, and their tests. The action vectors name their own shares and don't change.
2. **The press rule:** `ActionClock.startAt` without the margin, and its tests.
3. **The energy wait:** the own `AttackUsed` marks the latest attack counted, `skill-keys` waits, and the end-to-end order test.
4. **Harness:** the chain model, `maxChainGain` gone, and `TestRotation`.

**Docs the build keeps true:** this section, [Energy](./energy.md) sections 6 and 8, and [Combat feel](./combat-feel.md) section 7.
