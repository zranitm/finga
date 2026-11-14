# Combat feel

> **Status: approved · 2026-11-12.** The high-level design for [C11. Combat feel](../product/features/combat-and-progression/c11-combat-feel.md) and [Systems → Combat → Actions and Pace](../product/systems/combat.md). It describes the code as it stands on `main` (after field bosses) and the change it plans. A camera zoom, first planned here too, is on hold until the user has played the clearer animations (section 13).

## 1. What has to hold

- **A player can time a cancel by eye.** The pose is bold enough to read at today's character size, and shows where an action is up to, all the way to the cancel point.
- **Using a skill commits the player:** slower walking and climbing and a held facing, the same in the server's sim and the client's prediction, so a player using a skill isn't corrected every time.
- **Every class keeps basic attacks between its skills,** so cancels stay part of every class's fight.
- **The client computes, the server validates.** Poses and shapes are drawn by the client from game data and the action timeline it already holds. The server adds only one small event, so others can see basic attacks, and one movement field.

## 2. The change at a glance

| Part | Today | After |
| --- | --- | --- |
| Action pose | The hit frame for 225 ms (`SWING_MS`), own player only | Three bold beats over the action, ending at its cancel point, for everyone |
| Melee basic attacks | No shape | A bright slash arc over the attack box |
| Skill shapes | Faint, fading over 300 ms | Stronger, with a flash on the caster, fading out by the cancel point |
| Others' basic attacks | Not drawn | A new `AttackUsed` event, drawn like `SkillUsed` |
| `data/skills.yaml` | Every skill 1050 ms | 1500 ms; damage skills' cooldowns 1.5× |
| Skill cancel point | 85% of the length, like attacks; an attack pressed earlier waits for the end | `cancel: 0.3`, just after the strike; an attack pressed or held any time during the skill goes then |
| `data/movement.yaml` | — | `casting_speed: 0.5` |
| Sim | Facing follows input; one speed | `State.casting`: walking and climbing at `casting_speed`, facing held |
| Protocol | `Attack` carries the input seq; `UseSkill` doesn't | Both do. `PlayerView.casting`, and `AttackUsed` |

"Casting" below means using any active skill, a melee one included.

## 3. Game data

### `data/skills.yaml`

- **Every active skill's `length` is 1500 ms:** 30 ticks. Heal and the buffs keep the default cancel share of 0.85, so their cancel point is ⌊30 × 0.85⌋ = 25, though nothing can cut them. The cancelable skills cut from 9 (below).
- **Damage skills' cooldowns grow 1.5×:**

  | Skill | Now | After |
  | --- | --- | --- |
  | Strike, Throw, Power Shot, Fire Arrow | 3 s | 4.5 s |
  | Spark | 3.75 s | 5.625 s |
  | Slash Blast | 4.5 s | 6.75 s |
  | Double Stab | 2.25 s | 3.375 s |

- **Heal (4.5 s) and the buffs keep their cooldowns and durations.** They aren't in the damage rotation.
- **Every cancelable skill has `cancel: 0.3`:** a basic attack may cut it from tick ⌊30 × 0.3⌋ = 9, 450 ms in, just after its strike (section 7). Heal and the buffs aren't cancelable and run to their end.
- **Powers don't change.** Section 8 has what this does to fights.

### `data/movement.yaml`

```yaml
casting_speed: 0.5   # walking and climbing while using a skill, as a share of the usual speed
```

- Validation refuses a value outside (0, 1].
- The client generator carries it, as it carries the other movement constants. The vectors pin it (section 6).

## 4. Beats

### The pose

`player.ts` replaces `SWING_MS` and `swingUntil` with a pose for the current action. It is set from the action's start and its hit, cancel point, and end in local milliseconds:

1. **Wind-up,** only when the hit is after the start: the character leans back until the hit. No action has one yet.
2. **Strike,** from the hit: the hit frame, leaning about 15° into the blow and lunging about 8 px forward, with a little squash and stretch. Only the drawing moves, never the body's position. It holds for the first third of the time from the hit to the cancel point.
3. **Follow-through,** the other two thirds: the lean, the lunge, and the squash ease out, and the usual frames (stand, walk, jump, climb) return.
4. **Ready,** at the cancel point: the pose is gone. From there to the end, the character looks idle, and a cancel can start.

- A new action replaces the pose and starts at its own strike, so a cancel visibly cuts the follow-through.
- **After a skill's cancel point, while casting,** the character holds a slight recovery lean, and the walk animation plays at the casting pace. An attack that cuts the skill drops both at once, with its strike. For other players, it ends at the skill's end or at their next `AttackUsed`.
- **The local player's timing** comes from `ActionClock`'s running action.
- **Another player's timing:**
  - for a skill, `SkillUsed`'s stage gives the length and points from game data;
  - for a basic attack, `AttackUsed` gives the length, the class's, and the class gives the shares (section 5).

### Shapes

The character stays at today's 56 px, so every shape is drawn to be read at that size.

- **Skills:**
  - At the strike, the caster flashes in the skill's colour.
  - The shape (`skill-effect.ts`) draws stronger than today, with a higher alpha and an outline.
  - It stays at full strength through the strike and fades to nothing at the cancel point, instead of over `EFFECT_MS`.
- **Melee basic attacks** draw a bright, thick slash arc over the class's attack box, on the same timing, so it shows the real reach. Basic melee attacks draw no shape today.
- **Ranged basic attacks** keep their arrow or orb.
- **Cutting short** removes the shape as it does today (`cutShort`).

### No cue

- Nothing marks the cancel point except the follow-through easing into ready. The user wanted players to find the timing themselves. Since a press at any time goes at the cancel point ([Animation cancel](./animation-cancel.md), section 15), there is no timing left to find.
- #61 shipped a cue, `entities/cue.ts`: a white flash and a spark burst at the cancel point less `early_press`, on the player's own character. #63 removed it and its tests.

## 5. Others' attacks

- **World:** when a basic attack starts in `resolveAttacks`, the instance adds `model.AttackUsed{Tick, Attacker, Facing, Length}` before the `Damage` its hit makes that tick, as `SkillUsed` comes before its own. `Length` is the attack's length in ticks, its class's.
- **Delivery:** the sink maps it to a `DeliverResponse` event, and the gateway to a new `AttackUsed` server message. Both follow `SkillUsed`'s path.
- **Client:**
  - `onAttackUsed` draws the pose on the attacker's sprite, with the class's hit and cancel shares over `length_ticks`. It adds a slash arc for melee, or a shot from the sprite in its facing for ranged.
  - It replaces that player's pose and shape still showing, as a new `SkillUsed` does.
  - The player's own attack comes back too and is ignored, as their own `SkillUsed` is for drawing.
- **Cost:** one small event per basic attack, at most about one a second for each fighting player, to the players on the map. That's the same order as `SkillUsed`.

## 6. Moving while using a skill

### The sim

Both sims, `server/internal/sim` and `web/src/sim`:

- **`State` gains `casting`,** the sub-steps left of moving at the casting pace. It is 0 when not casting.
- **`Params` gains `CastingSpeed`** from `movement.yaml`. `Faster` (the move speed stat) leaves it alone, so the two multiply.
- **While `casting` is above 0,** `Step`:
  - doesn't change `facing` from the input;
  - multiplies the walking velocity by `CastingSpeed`, on the ground and in the air, and the climbing speed too;
  - leaves the jump velocity, gravity, and falls alone, so jumps keep their height and lose half their reach;
  - counts `casting` down by one at the end of the step, beside `tickTimer`.
- Go keeps its `float64(...)` wrapping, so the products don't fuse.

**New vectors** in `server/testdata/movement`, listed in `requiredVectors`:
- walking while casting: half speed, and the other way walks backward with the facing held;
- climbing while casting;
- a jump while casting: the same height, half the reach;
- casting running out mid-walk: full speed returns, and turning works again;
- casting with a move speed factor.

`vectors_test.go` and `step.test.ts` read `casting` in `State`, and both replay every file.

### When casting starts and stops

The slow is tied to the input stream, not to the server's tick, so the client predicts it exactly.

- **Seqs reach the world:**
  - The client stamps `UseSkill` with the input seq in `ClientMessage.seq`, as it already stamps `Attack`.
  - The gateway passes both stamps on, in new `seq` fields on `UseSkillRequest` and `AttackRequest`.
  - **The world bounds a press's seq** to between `lastSeq` and the newest input received plus 2.
    - A forged seq far in the future would otherwise let a modified client skip the slow in the server's own sim. Bounded, it gains at most 2 inputs.
    - A stale seq casts from the next input, and a resend after a rejoin starts over.
- **Start:**
  - A skill pressed during input *S* starts casting with input *S* + 1. In the world, when `cast` starts a skill, the player keeps that seq and the skill's length.
  - `simulate` sets `casting` to length × sub-steps just before it steps the first input past *S*.
  - If that input was already stepped, because the timeline or the slack held the skill, it sets it before the next one.
- **Ended by an attack:**
  - Every basic attack that starts, pressed during input *S′*, ends any casting the same way: `casting` goes to 0 before the first input past *S′*.
  - It needs no test of whether the attack cut the skill short. An attack starts only at a skill's cancel point or its end, so ending the casting is always right.
  - A test on the action's end would run on ticks while `casting` counts inputs, and the server and the client could then disagree.
- **Counting:** `casting` counts inputs stepped, not ticks. A starved tick that steps no input doesn't count it down, on either side.
- **Resets:** a death, a respawn, or a map change resets it with the rest of the state. A knockback or a rescue from falling off the map leaves it running, since the skill runs on.
- **The client's view of it:** `PlayerView` gains `casting`, so the client's reconcile resets to the server's own value and its replay matches.

### The client's prediction

- **Start:**
  - `beginSkill` notes the seq the request carried.
  - The predictor sets `casting` when it begins input *S* + 1.
  - The tick entry for that input records the start, so a replay from an older server state applies it again.
- **Ended by an attack:** every local attack is noted the same way, and clears `casting` at *S′* + 1.
- **Refusal:** a refused skill drops its start. If the start was already applied, the predictor clears it and replays from the last server state.
- **Wire:** `fromWire` reads `casting`, and the reconcile counts a `casting` that differs from the server's as a difference, as it counts position.
- **Facing:** the shape's facing is the predicted facing at the press, as today. With the facing held, it stays right for the whole skill.

## 7. Getting free of a skill

Attacking is how a player gets free of a skill's slow: the user's words were "you use a skill, which slows you down, but if you cancel by a normal atk, you're no longer slowed".

- **Before this:** at a skill's cancel point of 85%, an attack could cut it only from 1.25 s of 1.5 s. One pressed earlier than 100 ms before that waited for the skill's end. So the slow always ran at least 1.25 s, and usually the whole 1.5 s.
- **The cancel point:** each cancelable skill's is at 0.3 (section 3), just after its strike. The strike still shows, since the beats run from the hit to the cancel point.
- **The press rule** (`ActionClock.startAt`):
  - An attack pressed at any time during a cancelable skill goes at the skill's cancel point, or at once if it has passed. It no longer needs to come within `early_press` of it.
  - A skill pressed during an attack kept the rule as it was: from the cancel point less `early_press`, else at the end. Since 2026-11-14 it goes at the attack's cancel point from a press at any time, as an attack does here ([Animation cancel](./animation-cancel.md), section 15).
- **The held attack key:**
  - While a cancelable skill runs, a held key fires at its cancel point, as a press does.
  - After an attack, it still repeats at each action's end, so holding attack never cuts attacks short.
  - Requiring a fresh press kept a held key from cancelling without any timing. Cutting a skill now needs no timing, so the held key needs no exception.
- **The world doesn't change.** It already holds an attack sent before its allowed start to that start, which is now the skill's cancel point. Every attack that starts already ends the casting from the input after its press (section 6).
- **Damage:** when this shipped, the harness's chain kept the no-cancel order, so a skill cut early mostly gave the player their movement back (section 8). Since [Animation cancel](./animation-cancel.md) section 15, the harness chains as players fight, and attacks fill the time a cut saves. An attack cutting each skill then adds 11–47% over the same chain with skills run to their end: Warrior 26–36%, Thief 41–47%, Bowman 13–20%, and Magician 11–25%.

## 8. Balance

The harness (`balance_test.go`, `rotation_test.go`) ran on `main` with this section 3 data. Every check passes, the chain bound included.

| Class | Damage a second with skills, against today | A perfect chain adds | Skills' share | Hits to kill with skills, against basic attacks alone |
| --- | --- | --- | --- | --- |
| Beginner | ×0.82 | 4% | 49% | 1.75 against 2.4 |
| Warrior | ×0.78 | 10% | 67% | 4.3 against 7.1 |
| Thief | ×0.73 | 6–8% | 83% | 3.3 against 6.9 |
| Bowman | ×0.67 | 12–17% | 84% | 2.6 against 7.1 |
| Magician | ×0.74 | 4–9% | 88% | 1.8 against 7.3 |

- **The chain column is with skills cut from 0.3** (section 7). With their cancel point at 0.85, a chain added 4% for the Beginner, 8% for the Warrior, and 9–10% for the Bowman, with the Thief and the Magician the same. A perfect chain deals at most 1.04× what it did at 0.85, and stays under the 22% bound.

- **Fights with skills take about 20–50% longer** than today. They're still 1.4 to 4 times faster than with basic attacks alone. The basic-attack bands don't move.
- **Field bosses:** a lone player using skills takes 0.6 to 2.5 minutes, from 0.5 to 2.1. With basic attacks alone it's 3, unchanged.
- **Why the cooldowns grow:** with 1.5 s skills and today's cooldowns, a Bowman's and a Thief's two damage skills cover every tick back to back. Their rotation is then 100% skills, and a skill can't cut off a skill, so they never cancel (a perfect chain adds 0%).

## 9. Protocol

- **`client.proto`:**
  - `PlayerView.casting = 16`, as `uint32`, in sub-steps.
  - A new `AttackUsed { attacker, facing, length_ticks }` in the server message's `oneof`.
  - `ClientMessage.seq`'s comment adds `UseSkill`.
- **The world module's contract:**
  - `Attack` and `UseSkill` take `seq`;
  - `AttackUsed` is one of its events;
  - its `PlayerView` carries `Casting` in its state, which the gateway copies across.
- **All additions,** so `buf breaking` (WIRE) passes. There are no new codes.

## 10. When things fail

| Failure | What the player sees | Why it is safe |
| --- | --- | --- |
| The server holds a skill past the client's moment, from the slack or jitter | Casting starts an input later on the server; a correction of a few pixels at most | The reconcile replays from the server's state, which carries `casting` |
| A skill is refused after the client started casting | A step or two of slow walking that gets corrected | The predictor drops the start and replays |
| A modified client ignores the slow or the held facing | Its own screen is wrong; everyone else sees the server's sim | The server moves every player by its own sim |
| A modified client stamps a skill with a far-future seq to put off the slow | The slow starts at most 2 inputs late | The world bounds the seq to its newest input plus 2 |
| Inputs stall | The slow lasts the same number of inputs on both sides | `casting` counts inputs stepped, not ticks |
| `AttackUsed` arrives late or is dropped with a frame | Another player's swing shows late, or not at all | It's drawing only; damage comes as `Damage` |

## 11. Testing

- **Game data:** `casting_speed` and its refusals; the new lengths and cooldowns load.
- **Sim:** the new vectors, replayed by Go and TypeScript.
- **World:**
  - casting starts at the input after the skill's press, with the full length;
  - it starts at the next input when the skill started late;
  - any attack that starts ends it at the input after its press;
  - a press seq out of bounds is held to `lastSeq` or the newest input plus 2;
  - a knockback or a rescue leaves it running;
  - it runs out at the length;
  - a refused skill never sets it, and a death or a map change clears it;
  - the facing holds while casting;
  - `AttackUsed` goes out with the class's length.
- **Client:**
  - the predictor applies a start and a cut at the right seq and applies them again in a replay, and a refusal drops them;
  - the pose's beats, with ready at the cancel point;
  - a remote `AttackUsed` draws a pose and a shot.
- **Getting free of a skill:**
  - an attack pressed early in a cancelable skill goes at its cancel point, and frees the player from the input after its press;
  - a held attack key does the same;
  - an attack after an attack still waits for the end;
  - a skill pressed during an attack keeps the `early_press` rule (until [Animation cancel](./animation-cancel.md), section 15).
- **Balance:** the harness passes with section 8's numbers.
- **End to end:** a skill slows its user on the server and holds their facing, and a second player sees the first's basic attacks.

## 12. Build order

Three PRs, each step one commit, green on `make test`, `make lint`, `make cover`, and `make breaking`.

**PR 1, the clearer animations, client only, merged as #61.** It went first and on its own, so the user could play it before anything else changed:

1. **Beats:** the bold pose, the slash arc, the skill flash, and stronger shapes fading by the cancel point. Other players' skills get the same timing from `SkillUsed`. It also shipped a cue, which the user then dropped.

**PR 2, after PR 1, merged as #63:**

2. **No cue:** `cue.ts` and its calls and tests go. This is also where the C11 docs land on `main`.
3. **Data:** skill lengths and cooldowns, and the harness numbers in the PR.
4. **Others' attacks:** `AttackUsed` from the world through the gateway to the client.
5. **Moving while using a skill:** the sim and its vectors, seqs on `Attack` and `UseSkill`, casting in the world, `PlayerView.casting`, and the client's prediction.

**PR 3, getting free of a skill (section 7), merged as #65:**

6. **Data:** `cancel: 0.3` on every cancelable skill, and the harness numbers in the PR.
7. **Client:** the press rule and the held key for attacks during a skill, and the slowed look after a skill's cancel point.

## 13. Decisions

Agreed on 2026-11-12:

1. **No zoom for now.** The user suspects the animations, not the characters' size, kept cancels from being read. So the clearer animations ship first, as their own PR, and a zoom waits until they have been played.
2. **Three bold beats over each action, ready at the cancel point,** drawn to read at today's size. **No cue marks the cancel point:** after playing #61, the user dropped it so players find the timing themselves.
3. **Other players' basic attacks animate,** through a new `AttackUsed` event.
4. **Every skill takes 1.5 s, and damage skills' cooldowns grow 1.5×.** Powers stay, so fights with skills slow down.
5. **Half speed for walking and climbing, and a held facing, while using any active skill.** Jumps keep their height, and basic attacks stay free.
6. **An attack gets the player free of a skill:** a cancelable skill can be cut from 0.3 of its length, just after its strike. An attack pressed or held at any time during it goes then. Agreed after playing #63.

## 14. Alternatives considered

- **A camera zoom, first agreed and now on hold:** the view would show 960 × 540 px of world at any window size, up to 4×, the art's own resolution.
  - Labels would keep their screen size, and screen-fixed objects would be laid out for the zoom.
  - Only the drawing would change.
  - If the clearer animations aren't enough, it comes back as designed.
- **Bigger sprites, not a zoom:** bigger bodies need bigger hitboxes, taller gaps under platforms, and new maps, with the reach tests and every vector made again. A zoom gives the same picture with none of that.
- **A cue at the cancel point, or a timing bar:** either is easier to read, but the user wants the timing found by feel. #61's cue, a flash and a spark burst, was played once and dropped.
- **1.5 s skills with today's cooldowns:** a Bowman and a Thief would chain skills back to back and never cancel (section 8).
- **Raising skill powers to keep today's kill speed:** the user had found fights with skills already ending fast. Cooldowns alone keep the data change small, and the harness reports the result.
- **The slow as a per-tick parameter, not state:** the client's replay would need each tick's parameter recorded, and the server's snapshot wouldn't carry it. As state, it travels in `PlayerView` and the replay matches by itself.
- **Starting the slow at the server tick the skill starts:** the server steps inputs a few ticks behind their arrival, from its input buffer. The slow would land on different inputs than the client predicted, and nearly every skill would cause a correction. Tying it to the input after the press makes both sides agree.
- **Cutting a skill from its hit:** the attack would come out before the skill's strike could be seen. From 0.3, the strike shows first.
- **Keeping 0.85 for skills and the strict press rule:** the slow would always run at least 1.25 s, so attacking would never feel like getting free.
- **A field on `PlayerView` for the latest attack:** every attack would resend the whole view to the map. An event the size of `SkillUsed` is smaller and matches how skills are shown.
