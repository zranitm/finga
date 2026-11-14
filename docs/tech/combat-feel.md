# Combat feel

> **Status: approved · 2026-11-12.** The high-level design for [C11. Combat feel](../product/features/combat-and-progression/c11-combat-feel.md) and [Systems → Combat → Actions and Pace](../product/systems/combat.md). It describes the code as it stands on `main` (after field bosses) and the change it plans. Since 2026-11-14, the pose, the shapes, and the recovery lean time to each action's own ready point, not its cancel point ([Animation cancel](./animation-cancel.md), section 15). Since the same day, a skill lasts 1 s and doesn't slow its user (section 15). A camera zoom, first planned here too, is on hold until the user has played the clearer animations (section 13).

## 1. What has to hold

- **A player can time a cancel by eye.** The pose is bold enough to read at today's character size, and shows where an action is up to, all the way back to ready.
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

- **Every active skill's `length` is 1500 ms:** 30 ticks. Section 15 makes it 1000 ms. Heal and the buffs keep the default cancel share of 0.85, so their cancel point is ⌊30 × 0.85⌋ = 25, though nothing can cut them. The cancelable skills cut from 9 (below).
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
- Section 15 removes it: a skill no longer slows its user.

## 4. Beats

### The pose

`player.ts` replaces `SWING_MS` and `swingUntil` with a pose for the current action. It is set from the action's start and its hit, ready point, and end in local milliseconds:

1. **Wind-up,** only when the hit is after the start: the character leans back until the hit. No action has one yet.
2. **Strike,** from the hit: the hit frame, leaning about 15° into the blow and lunging about 8 px forward, with a little squash and stretch. Only the drawing moves, never the body's position. It holds for the first third of the time from the hit to the ready point.
3. **Follow-through,** the other two thirds: the lean, the lunge, and the squash ease out, and the usual frames (stand, walk, jump, climb) return.
4. **Ready,** at the ready point: the pose is gone, and from there to the end the character looks idle. The ready point is the action's own `ready` share: the default 1 since section 15, which attacks override with 0.85. The cancel point, where the other kind may cut in, is apart from it ([Animation cancel](./animation-cancel.md), section 15). Until 2026-11-14 the two were one.

- A new action replaces the pose and starts at its own strike, so a cancel visibly cuts the follow-through.
- **After a skill's ready point, while casting,** the character held a slight recovery lean, and the walk animation played at the casting pace. Since section 15, a skill doesn't slow its user, and every skill's look fills it, so neither shows. An attack that cuts the skill drops both at once, with its strike. For other players, it ends at the skill's end or at their next `AttackUsed`.
- **The local player's timing** comes from `ActionClock`'s running action.
- **Another player's timing:**
  - for a skill, `SkillUsed`'s stage gives the length and points from game data;
  - for a basic attack, `AttackUsed` gives the length, the class's, and the class gives the shares (section 5).

### Shapes

The character stays at today's 56 px, so every shape is drawn to be read at that size.

- **Skills:**
  - At the strike, the caster flashes in the skill's colour.
  - The shape (`skill-effect.ts`) draws stronger than today, with a higher alpha and an outline.
  - It stays at full strength through the strike and fades to nothing at the ready point, instead of over `EFFECT_MS`.
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

Section 15 removes the slow below, and `casting` with it. This section and the next describe the code before that.

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
- **The cancel point:** each cancelable skill's is at 0.3 (section 3), just after its strike. The strike still shows, since the beats run from the hit to the ready point, which each cancelable skill names at 0.3 too ([Animation cancel](./animation-cancel.md), section 15).
- **The press rule** (`ActionClock.startAt`):
  - An attack pressed at any time during a cancelable skill goes at the skill's cancel point, or at once if it has passed. It no longer needs to come within `early_press` of it.
  - A skill pressed during an attack kept the rule as it was: from the cancel point less `early_press`, else at the end. Since 2026-11-14 it goes at the attack's cancel point from a press at any time, as an attack does here ([Animation cancel](./animation-cancel.md), section 15).
- **The held attack key:**
  - While a cancelable skill runs, a held key fires at its cancel point, as a press does.
  - After an attack, it still repeats at each action's end, so holding attack never cuts attacks short.
  - Requiring a fresh press kept a held key from cancelling without any timing. Cutting a skill now needs no timing, so the held key needs no exception.
- **The world doesn't change.** It already holds an attack sent before its allowed start to that start, which is now the skill's cancel point. Every attack that starts already ends the casting from the input after its press (section 6).
- **Damage:** when this shipped, the harness's chain kept the no-cancel order, so a skill cut early mostly gave the player their movement back (section 8). Since [Animation cancel](./animation-cancel.md) section 15, the harness chains as players fight, and attacks fill the time a cut saves. An attack cutting each skill then added 11–47% over the same chain with skills run to their end: Warrior 26–36%, Thief 41–47%, Bowman 13–20%, and Magician 11–25%. With the 1 s skills of section 15, a cut saves less, and it adds 2–18%: Warrior 11–13%, Thief 11–18%, Bowman 7–10%, and Magician 2–10%.

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

Agreed on 2026-11-14 (section 15):

7. **A skill roots its user instead of slowing them.** The user's words: "we'll not make it slow when throwing skill, we make it unmovable instead".
8. **Every active skill takes 1 s, Heal and the buffs included, and its animation plays slowly enough to fill it.** The user found the root too long at 1.5 s. An attack still cuts a cancelable skill at 450 ms.
9. **Rooted means no walking, jumping, climbing, or dropping through.** Mid-air, the player drops straight down. On a rope, they hold still. A mob's knockback still pushes them.
10. **The root is dropped before it shipped:** a player moves freely while using a skill, and another skill waits for its end. The user's words: "we will let player move, but just cant use other skills rightaway as the animation of the last skill is still in effect". The 1 s length and the look filling it stay. `casting` goes, since nothing is left for it to do.

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
- **A 1.5 s root** (2026-11-14): the user chose 1 s.
- **A 1 s root inside a 1.5 s action:** the player could move for the last half second but couldn't use a skill unless an attack cut in. That's a wait nothing shows.
- **`casting_speed: 0` in place of a root:** walking would stop, but jumps, drop-through, and jumping off a rope would still go, since they aren't speeds. A root rule says what it does.
- **Keeping a jump's sideways drift through a skill:** "unmovable" means the player stops. They drop straight down.
- **An attack cutting the 1 s skill at 0.3 of it, 300 ms:** the user found the 450 ms cut right, so the cancelable skills name 0.45.
- **Keeping a held facing while moving through a skill,** as the slow did: the player would walk backward. The skill's hit lands at its start and its shape stays where it was cast, so turning changes nothing but the pose.
- **Keeping `casting` for a later use:** nothing reads it, so it goes. It's easy to bring back with the next rule that needs it.

## 15. One-second skills, moving freely

Agreed with the user on 2026-11-14 (section 13). Using a skill slowed walking and climbing to half for its 1.5 s.
- The user first asked for a root, then for 1 s skills with their animation filling them.
- Before the root's build merged, they dropped it: "we will let player move, but just cant use other skills rightaway as the animation of the last skill is still in effect".
- So a skill now lasts 1 s and doesn't touch movement at all.

### The rule

- **Every active skill lasts 1 s, 20 ticks,** Heal and the buffs included. Cooldowns don't change.
- **Movement is free while it runs:** full speed, jumps, ropes, drop-through, and turning, as during a basic attack.
- **Another skill waits for its end.** This is the one-action-at-a-time rule ([Animation cancel](./animation-cancel.md)), unchanged: a skill never cuts a skill.
- **A basic attack still cuts a cancelable skill** from tick ⌊20 × 0.45⌋ = 9, 450 ms, the same moment as before.
- **The look fills the second:**
  - A skill's beats and shape run to its end: the strike over the first third, about 330 ms, then the follow-through. They're drawn over the walk, as an attack's are.
  - The shape stays where and how it was cast.
  - Turning ends the pose, as it does for an attack.
  - An attack at 450 ms cuts the follow-through, and the afterimage shows it.

### The change

| Part | Before | After |
| --- | --- | --- |
| `server/data/skills.yaml` | Every active skill `length: 1500ms`. The cancelable ones name `ready: 0.3` and take the default `cancel: 0.3` | `length: 1000ms`. The cancelable ones name `cancel: 0.45` and no `ready` |
| `server/data/actions.yaml` | `ready: 0.85` | `ready: 1`: an action's look fills it unless it names its own |
| `server/data/classes.yaml` | Attacks take the default `ready` | Each attack names `ready: 0.85`, so its look doesn't change |
| `server/data/movement.yaml` | `casting_speed: 0.5` | Gone, with its check in `load/movement.go`, `Params.CastingSpeed` in both sims, and the generator's copy. The loader refuses the key as an unknown one |
| Both sims | `State.Casting`: walking and climbing at `CastingSpeed`, facing held | Gone. `Step` no longer knows about skills |
| World | `castMark` sets `casting` from the input after a skill's press; every attack clears it | Gone, with its tests. A skill press's `seq` stays, since the energy count reads it |
| Protocol | `PlayerView.casting` (16) | Reserved, so `make breaking` passes. `Attack`'s `seq` stays if anything but the casting reads it, otherwise it's reserved too |
| Client | The predictor's casting marks; `beats.ts`'s `slows`, which held the recovery lean to a skill's end; `player.ts` walking at `casting_speed` | All gone. A skill's pose ends at its ready point, its end, like any action's |

### The sim

- `casting` and everything that sets or reads it go from both sims.
- The five casting vectors go from `requiredVectors` and `server/testdata/movement`, and every file drops `casting_speed` from its params.
- No new vector is needed: a player using a skill moves exactly as one who isn't.

### Balance

- **A trial on `main`'s data** (2026-11-14), with 1 s skills cut at the same 450 ms:
  - A chain's damage doesn't move, within 1%.
  - One action at a time deals 6–18% more: the Warrior +12%, the Thief 9–17%, the Bowman 6–10%, and the Magician 15–18%.
  - So a chain's gain over one action at a time falls to about 25%, the figure the docs quote.
- **The hits-to-kill bands and the boss bounds** read basic attacks alone (`fightOf`), so they don't move.
- **A caster can walk out of a mob's attack** while their skill plays, as they could at half speed before.
- **The bots** don't change.

### Testing

- **Game data:**
  - the skills' lengths;
  - `cancel: 0.45` at tick 9 and `ready: 1` at the end;
  - the attacks' `ready: 0.85`;
  - `casting_speed` refused as an unknown key.
- **World:**
  - a player using a skill walks, jumps, and turns at full speed;
  - a second skill waits for the first's end;
  - an attack cuts a cancelable skill at tick 9.
- **Client:**
  - a skill's beats end at its end;
  - walking during a skill causes no prediction correction;
  - an attack at 450 ms cuts the look, with an afterimage.
- **Live, for every class:**
  - walking, jumping, and climbing through a skill;
  - pressing a second skill during one: it goes at the first's end;
  - Heal and a buff lasting 1 s;
  - an attack cutting a skill at 450 ms.

### Build

One PR, each step one commit, green on `make test`, `make lint`, `make cover`, and `make breaking`:

1. **Data:** the lengths, `cancel`, and `ready`; the attacks' `ready`; and `casting_speed` out of the data, loader, validation, generator, and their tests.
2. **Casting out:** `State.Casting` and its vectors from both sims, `castMark` and its tests from the world, the predictor's marks, `PlayerView.casting` reserved, and the casting e2e test.
3. **Client look:** `slows` and the slowed walk out, and the tests above.

As built:
- **The look went second and the casting third.** The walk animation's time scale read `casting_speed` from the generated data, so the key could leave the data only with or after it. With the look first, `casting_speed` left with the rest of the casting, once nothing read it.
- **`Attack`'s `seq` goes from the world's API,** as `EnterPortal`'s already had: only the casting mark read it. It is the envelope's `seq`, which `Input` and `UseSkill` still read, so there is nothing to reserve. `pressedDuring` stays, since the energy count bounds a press's `seq` with it.
- **The vector files carry no params,** so only the sims' `Params` and the Go test's `params()` drop `casting_speed`. The files drop their `casting` keys and replay as before.
- **The casting e2e test's other half,** a second player seeing the first's basic attacks, stays as its own test; nothing else covered it.
- **The client test:** the predictor no longer has anything a skill could change. A scene test walks, turns and jumps through Strike, checks that the drawn stride stays the same, and checks that the press calls nothing on the predictor.
- **The Haste speed test** reads Haste's pace from the input after its press again.
- **The harness** changes nothing, and its checks pass. Its numbers match the trial above, and the gains quoted in section 7 and [Animation cancel](./animation-cancel.md) section 15 follow them.

**Docs the build keeps true:** this section, sections 6 and 7 (which describe the casting as it was), and [Animation cancel](./animation-cancel.md) section 15's skill shares.
