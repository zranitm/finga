# Animation cancel

> **Status: approved · 2026-11-12.** The high-level design for [C9. Animation cancel](../product/features/combat-and-progression/c9-animation-cancel.md) and [Systems → Combat → Actions](../product/systems/combat.md). It describes the code as it stands on `main` (after classes and skills, part 4) and the change it plans.

## 1. What has to hold

- **One action at a time, as today.** A basic attack or an active skill is an action. The next one starts when the current one ends, unless it cancels.
- **Only the other kind cancels.** A skill may cut off a basic attack from the attack's cancel point. A basic attack may cut off a skill from the skill's cancel point, if game data marks the skill cancelable. Neither kind cuts off its own.
- **The client computes, the server validates.** The client times every action and decides when to send the next one, including the early-press margin, and plays it at once. The server checks each attack and skill against the player's timeline as it already checks cooldowns. A cancel isn't a message of its own, so the check adds a comparison, not traffic.
- **Timing lives in game data.** Each action's length, cancel point, and wind-up are data, so tuning never needs a release.
- **A modified client gains nothing past a perfect chain.** The server holds every action to its earliest allowed tick, whatever the client sends.

## 2. The change at a glance

| Part | Today | After |
| --- | --- | --- |
| `data/classes.yaml` | `attack.cooldown`: the basic attack's turn | `attack.length`, with optional `hit` and `cancel` |
| `data/skills.yaml` | A skill takes the attack's turn and starts its cooldown | Each active skill has its own `length`, `cancelable`, and optional `hit` and `cancel`; a stage may change them |
| `data/actions.yaml` | — | New: the default cancel point, and the early-press margin |
| World | `cooldownUntil`: one turn shared by attacks and skills | An action timeline per player: kind, hit, cancel point, end |
| `StatSheet` | `attack_cooldown_ticks` | `attack_length_ticks` |
| Client | `AttackGate`, which skills take turns from | `ActionClock`: the same timeline, with the early-press margin |
| Balance harness | Basic attacks only | Also skills, one at a time and in chains |

## 3. Game data

### `data/actions.yaml` (new)

```yaml
cancel: 0.85        # where an action's cancel point falls, as a share of its length, unless the action names its own
early_press: 100ms  # a press this long before a cancel point still cancels; 0 makes cancels strict
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
- a length under one tick;
- a share outside 0 to 1, or a `hit` after `cancel`;
- `early_press` below 0 or over 500 ms.

The client generator carries all of it, `actions.yaml` included.

## 4. The timeline

**An action's length in ticks:**
- **A basic attack:** ⌈length ÷ (1 + attack speed) ÷ tick⌉, as `AttackCooldownTicks` counts it today, so attack speed shortens it.
- **A skill:** ⌈length ÷ tick⌉. Attack speed doesn't change it. Cooldown speed still shortens only its cooldown.
- Both go through `TicksIn`, so a length of whole ticks counts as exactly that many, and anything counts as at least one.

**Its points,** as offsets from the start in ticks, where *L* is the length:
- **Hit:** ⌊*L* × hit⌋.
- **Cancel point:** ⌊*L* × cancel⌋, held to at least the hit and at least one tick, so a cancel never lands before the hit or in the action's own first tick.

Rounding the cancel point down keeps every class near the target. At 0.85, the five basic attacks save 17–21% of their length per cancel. Rounding up would give 8–14%.

| Class | Length | Ticks | Cancel point |
| --- | --- | --- | --- |
| Beginner | 700 ms | 14 | 11 |
| Warrior | 600 ms | 12 | 10 |
| Thief | 500 ms | 10 | 8 |
| Bowman | 750 ms | 15 | 12 |
| Magician | 850 ms | 17 | 14 |

**When the next action may start,** given the current one:
- **The other kind:** from the cancel point, if the current action may be cut off by it. Every attack may be cut off by a skill, and a skill by an attack only if it's cancelable.
- **Otherwise:** at the end.

**The world's `model/ticks.go` holds this,** beside the cooldown counts: `AttackCooldownTicks` becomes `AttackLengthTicks`, joined by `SkillLengthTicks` and `ActionPoints`. `web/src/progression/actions.ts` ports them, and shared vectors in `server/testdata/actions/` pin the two. Each vector has a length, an attack speed, a hit, a cancel, and a tick, and gives the expected ticks and points. Both sides replay them.

## 5. World

- **Each player holds an action** in place of `cooldownUntil`: its kind (none, attack, or skill), whether it may be cut off, and the ticks of its hit, its cancel point, and its end. Only the map instance holds it. A map change, a death, or a fresh join starts with none, as `cooldownUntil` does today.
- **Attacks:** `attackQueued` stays. In `resolveAttacks`, a queued attack goes once the tick reaches its next allowed start, as an early one waits today.
  - It starts a new action at this tick.
  - Its hit lands at the hit tick: now when `hit` is 0, as for every action in the data today.
- **Skills:** `castSkills` keeps its queue, `SKILL_WAITING`, and its refusals.
  - The turn it waits for becomes the next allowed start for a skill.
  - It still goes up to `castSlack` (150 ms) early. Its action counts from the allowed start, not from the early use, so a chain gains nothing from the slack.
  - **It no longer starts the attack's cooldown.** Its own action's length replaces that.
- **Order in the tick is unchanged:** buffs expire, skills go, then attacks. So when both are ready, a skill goes ahead of a held attack, and the attack takes the next allowed start after the skill.
- **Wind-up:**
  - When `hit` is above 0, the action holds a pending hit, landed in the tick it comes due, before skills and attacks. It uses the player's position and facing then, and the attack and power the action started with.
  - A death or a map change drops the pending hit.
  - A skill's MP and cooldown are spent at the start, as today.
  - No action in the data has a wind-up yet; the world tests cover it on test data, as #44 did for heals before heal skills existed.
- **What a press can't do:** an attack sent before its allowed start waits for it; a skill waits in the queue. Neither is refused for timing, so a press too early simply goes later. That is the whole check.

## 6. Protocol

- **No new messages.** `Attack` and `UseSkill` carry every action, early or not.
- **`StatSheet.attack_cooldown_ticks` becomes `attack_length_ticks`.** It keeps field number 6, so `buf breaking` (WIRE) passes. The JSON name changes, and the gateway and the client change with it.
- **No new codes.** A press can't be refused for timing (section 5).

## 7. Client

- **`ActionClock`** (`web/src/game/systems/actions.ts`) replaces `AttackGate` and the skill keys' `turnIn` and `takeTurn`. It holds the same action as the world, in local time from the shared tick counts: kind, cancelable, hit, cancel point, and end.
- **A press:**
  - **The other kind, pressed from `early_press` before the cancel point on:** it goes at the cancel point, or at once if the point has passed.
  - **Otherwise:** it waits for the end.
  - A waiting press is held on the client and sent when its moment comes, as a skill key waiting for its turn is today. A newer press takes its place.
- **Holding the attack key** repeats attacks at each action's end. Only a fresh press cancels, so holding attack doesn't cut off every cancelable skill with no timing at all.
- **Sending starts the action at once** on the client: the swing pose, a ranged shot's flight, a skill's shape, its cooldown, and its MP check, as today.
  - A refusal of a skill gives back its action, as it gives back the turn today.
  - A timeout or a disconnect keeps it, as it may have landed.
- **Cut short:** a new action replaces the pose and the shape of the one it cancels. A ranged shot already flying keeps flying, since its hit already landed.
  - For other players, a caster's new `SkillUsed` replaces their last skill shape still showing.
  - Their basic attacks aren't drawn for others today, and this doesn't change that.
- **The skill window** shows each active skill's length, and "A basic attack can cut it short" on cancelable ones, so players can learn the list.

## 8. Balance

- **The harness** (`world/internal/model/balance_test.go`) today paces a fight by the basic attack alone: a skill was "a stronger hit in place of a normal one".
- **It gains a rotation model.** For each class at its checkpoints, with its skills at the points the bots' plan gives them, it counts damage per second three ways:
  1. basic attacks alone;
  2. skills on cooldown, one action at a time, with no cancels;
  3. a perfect chain: every cancel that's allowed, taken at its point.
- **It holds the chain's gain** (3 over 2) to at most 22% for every class and checkpoint, which leaves room around the product's "up to about 20%". Time-to-kill targets stay checked against 2, so a player who never cancels still meets them.
- **Tuning moves only data:** skill lengths, the cancel points, `cancelable`, and the skill powers.

## 9. When things fail

| Failure | What the player sees | Why it is safe |
| --- | --- | --- |
| An action reaches the server a tick early, from jitter | Nothing unusual | An attack waits for its tick. A skill within 150 ms goes, but its action counts from the allowed start |
| A modified client sends every action at once | Its actions go no faster than a perfect chain | The server holds each action to its earliest allowed start |
| A lag spike delays an action | The hit lands late, and the chain resumes from there | Each action starts when it reaches the server; nothing builds up |
| Death or a map change mid-action | The action ends; a pending wind-up hit is lost | It is held only in the map instance |
| A deploy changes timing data while a player is on a map | One action may use the old timing | The world and the client load the same data at start; the next join brings the new data to both |

## 10. Testing

- **Game data:** the new fields, defaults, stage overrides, and each validation refusal.
- **Timeline vectors:** ticks and points across attack speeds and shares, including the one-tick minimum and the hit floor; replayed by Go and TypeScript.
- **World:**
  - an attack cut off by a skill at its cancel point, and not before;
  - a cancelable skill cut off by an attack at its cancel point; a skill that isn't cancelable runs to its end;
  - attack after attack and skill after skill waiting for the end;
  - an early attack waiting, and an early skill within the slack counting from the allowed start;
  - attack speed moving an attack's cancel point and leaving a skill's alone;
  - a wind-up hit landing late, at the position then, and dropped at a death or a map change.
- **Client:** `ActionClock`'s press rules, including the margin, a press just outside it, a held key versus a fresh press, and a newer press replacing a waiting one; refusals giving the action back; a cut-off shape.
- **End to end:** an attack, Strike, and an attack again, chained, land sooner than the same three one at a time.
- **Balance:** the chain-gain bound for every class and checkpoint.

## 11. Build order

On its own branch and PR. Each step is one commit, green on `make test`, `make lint`, `make cover`, and `make breaking`:

1. **Data and timing:** `actions.yaml`, `length` (renamed from `cooldown`), `hit`, `cancel`, and `cancelable`, validation, the generator, `AttackLengthTicks`, `SkillLengthTicks`, and `ActionPoints` in Go and TypeScript, and the shared vectors.
2. **World:** the action timeline in place of `cooldownUntil`, cancels in `resolveAttacks` and `castSkills`, and wind-up hits.
3. **Protocol and client:** `attack_length_ticks`, `ActionClock`, the press rules, cut-off shapes, and the skill window's line.
4. **Content and balance:** skill lengths and the cancelable list, the harness's rotation model and bound, and a `make bots` run.

## 12. Decisions

Agreed on 2026-11-12:

1. **Holding the attack key repeats at each action's end; only a fresh press cancels.** Holding attack would otherwise cut off every cancelable skill without any timing, which the strict timing is meant to avoid.
2. **The first cancelable list:** the damage skills (Strike, Throw, Spark, Slash Blast, Double Stab, Power Shot, Fire Arrow). Heals and buffs (Heal, Iron Body, Haste, Focus) run to their end.
3. **First skill lengths:** 700 ms each, the Beginner's attack, then tuned in step 4 with the harness.
4. **Rename the attack's `cooldown` to `length`** in data, code, and the sheet, since skills now have both.
5. **The skill window shows each skill's length and whether an attack can cut it short,** so players can learn the list.
6. **Build wind-up now** (`hit`), though no action uses it yet, so the first charged skill is data only.

## 13. Alternatives considered

- **The client decides cancels alone:** it saves the server nothing, since it checks every action already. And a modified client could then cancel every action at once and multiply its damage.
- **The server applies the early-press margin:** it would judge presses by when they arrive, so network jitter would decide some cancels. The client knows exactly when the player pressed, so it applies the margin. The server only holds actions to their earliest allowed tick.
- **A cancel message, or a field on `Attack` and `UseSkill`:** the server needs nothing beyond the action itself to check one.
- **Cancel points in milliseconds instead of a share:** attack speed would then need its own rule for moving them. A share moves with the length.
- **Rounding the cancel point up:** cancels would save only 8–14% of an attack's length at 0.85, short of the target, and unevenly by class.
