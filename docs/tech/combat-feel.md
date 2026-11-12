# Combat feel

> **Status: approved · 2026-11-12.** The high-level design for [C11. Combat feel](../product/features/combat-and-progression/c11-combat-feel.md), [Systems → Combat → Actions and Pace](../product/systems/combat.md), and [Screens and controls → The view](../product/screens-and-controls.md). It describes the code as it stands on `main` (after field bosses) and the change it plans.

## 1. What has to hold

- **A player can time a cancel by eye.** The character is big enough to read, and its pose shows where an action is up to, all the way to the cancel point.
- **The view changes only the drawing.** Hitboxes, reach, jumps, maps, and what the server sends stay as they are.
- **Using a skill commits the player:** slower walking and climbing and a held facing, the same in the server's sim and the client's prediction, so a player using a skill isn't corrected every time.
- **Every class keeps basic attacks between its skills,** so cancels stay part of every class's fight.
- **The client computes, the server validates.** Poses, shapes, and the cue are drawn by the client from game data and the action timeline it already holds. The server adds only one small event, so others can see basic attacks, and one movement field.

## 2. The change at a glance

| Part | Today | After |
| --- | --- | --- |
| Camera | Zoom 1: the view shows as much world as the window has pixels | Zoomed to show 960 × 540 px of world at any window size, up to 4× |
| Labels | 12–18 px text in world space | The same size on screen at any zoom, and sharp |
| Action pose | The hit frame for 225 ms (`SWING_MS`), own player only | Three beats over the action, ending at its cancel point, for everyone |
| Skill shapes | Fade over 300 ms | Fade out by the cancel point |
| Cancel cue | None | A glint at the cancel point less `early_press`, own player only |
| Others' basic attacks | Not drawn | A new `AttackUsed` event, drawn like `SkillUsed` |
| `data/skills.yaml` | Every skill 1050 ms | 1500 ms; damage skills' cooldowns 1.5× |
| `data/movement.yaml` | — | `casting_speed: 0.5` |
| Sim | Facing follows input; one speed | `State.casting`: walking and climbing at `casting_speed`, facing held |
| Protocol | `Attack` carries the input seq; `UseSkill` doesn't | Both do. `PlayerView.casting`, and `AttackUsed` |

"Casting" below means using any active skill, a melee one included.

## 3. Game data

### `data/skills.yaml`

- **Every active skill's `length` is 1500 ms:** 30 ticks, with the cancel point at ⌊30 × 0.85⌋ = 25, so the window is 250 ms, plus the 100 ms early press. Today's 1050 ms gives 21 and 17, a 200 ms window.
- **Damage skills' cooldowns grow 1.5×:**

  | Skill | Now | After |
  | --- | --- | --- |
  | Strike, Throw, Power Shot, Fire Arrow | 3 s | 4.5 s |
  | Spark | 3.75 s | 5.625 s |
  | Slash Blast | 4.5 s | 6.75 s |
  | Double Stab | 2.25 s | 3.375 s |

- **Heal (4.5 s) and the buffs keep their cooldowns and durations.** They aren't in the damage rotation.
- **Powers don't change.** Section 8 has what this does to fights.

### `data/movement.yaml`

```yaml
casting_speed: 0.5   # walking and climbing while using a skill, as a share of the usual speed
```

- Validation refuses a value outside (0, 1].
- The client generator carries it, as it carries the other movement constants. The vectors pin it (section 7).

## 4. The view

All client: `web/src/game`.

- **Zoom:** `min(width ÷ 960, height ÷ 540, 4)`, set when the World scene builds and on every `RESIZE`. 960 × 540 and the cap live as constants beside `ART_SCALE`.
  - The cap is the art's own resolution (128 px art on the 32 px grid), so the world never draws past it.
  - A window narrower than 960 × 540 zooms below 1 and still shows the same area.
  - 960 wide keeps a Bowman's 420 px reach on screen in front of a centred player.
- **Camera:** bounds and follow as today. A map smaller than the zoomed view keeps today's handling: held at the top left, the below-floor fill under it, and the backdrop colour beside it.
  - The follow rounds to whole pixels today, which can jitter at a zoom like 1.6. If it does, the rounding goes.
- **Fixed to the screen:**
  - The background image, the below-floor fill, and the level banner are laid out in screen pixels today (`cam.width`, `cam.height`, and the `RESIZE` size).
  - Phaser zooms screen-fixed objects about the camera's centre too, so each is sized and placed from the zoomed view instead, to keep filling or centring on the screen.
- **Labels keep their screen size:**
  - Name tags, NPC markers, portal labels, and damage numbers are rendered at the zoom's resolution and scaled by 1 ÷ zoom, through one helper they all use.
  - Their offsets above a sprite stay in world pixels, so they still sit over the bigger sprites. They follow a zoom change.
- **Drawn textures stay sharp:** the item placeholders (`scenes/placeholders.ts`) are generated at 32 px today. They are drawn at 128 px and shown at `ART_SCALE`, like the art.
- **Everything else zooms with the world:** sprites, tiles, HP bars, shots, skill shapes, and drops.
- **The server doesn't change.** Its frames already cover the whole map.

## 5. Beats and the cue

### The pose

`player.ts` replaces `SWING_MS` and `swingUntil` with a pose for the current action. It is set from the action's start and its hit, cancel point, and end in local milliseconds:

1. **Wind-up,** only when the hit is after the start: the character leans back until the hit. No action has one yet.
2. **Strike,** from the hit: the hit frame, leaning forward and nudged a few pixels forward. Only the drawing moves, never the body's position. It holds for the first third of the time from the hit to the cancel point.
3. **Follow-through,** the other two thirds: the lean and the nudge ease out, and the usual frames (stand, walk, jump, climb) return.
4. **Ready,** at the cancel point: the pose is gone. From there to the end, the character looks idle, and a cancel can start.

- A new action replaces the pose and starts at its own strike, so a cancel visibly cuts the follow-through.
- **The local player's timing** comes from `ActionClock`'s running action.
- **Another player's timing:**
  - for a skill, `SkillUsed`'s stage gives the length and points from game data;
  - for a basic attack, `AttackUsed` gives the length after attack speed, and the class gives the shares (section 6).

### Shapes

- **Skill shapes** (`skill-effect.ts`) stay at full strength through the strike and fade to nothing at the cancel point, instead of over `EFFECT_MS`.
- **Melee basic attacks** draw a slash arc over the class's attack box, on the same timing. Basic melee attacks draw no shape today.
- **Ranged basic attacks** keep their arrow or orb.
- **Cutting short** removes the shape as it does today (`cutShort`).

### The cue

- `ActionClock` already holds the running action's cancel point and the `early_press` margin.
- At the cancel point less the margin (the start, if that's earlier), the local sprite glints: a white flash of about 120 ms and a small spark at its front.
- It glints only if the other kind may cut the action off: always after a basic attack, and after a skill only if it's cancelable.
- It is a timer that `begin` cancels, so an action cut short or replaced before then never glints.
- With `early_press: 0`, it glints at the cancel point itself.
- No other player sees it.

## 6. Others' attacks

- **World:** when a basic attack starts in `resolveAttacks`, the instance adds `model.AttackUsed{Tick, Attacker, Facing, Length}`, beside `SkillUsed`. `Length` is the attack's length in ticks after attack speed.
- **Delivery:** the sink maps it to a `DeliverResponse` event, and the gateway to a new `AttackUsed` server message. Both follow `SkillUsed`'s path.
- **Client:**
  - `onAttackUsed` draws the pose on the attacker's sprite, with the class's hit and cancel shares over `length_ticks`. It adds a slash arc for melee, or a shot from the sprite in its facing for ranged.
  - It replaces that player's pose and shape still showing, as a new `SkillUsed` does.
  - The player's own attack comes back too and is ignored, as their own `SkillUsed` is for drawing.
- **Cost:** one small event per basic attack, at most about one a second for each fighting player, to the players on the map. That's the same order as `SkillUsed`.

## 7. Moving while using a skill

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
- **Start:**
  - A skill pressed during input *S* starts casting with input *S* + 1. In the world, when `cast` starts a skill, the player keeps that seq and the skill's length.
  - `simulate` sets `casting` to length × sub-steps just before it steps the first input past *S*.
  - If that input was already stepped, because the timeline or the slack held the skill, it sets it before the next one.
- **Cut short:**
  - A basic attack pressed during input *S′* that cuts a skill off (`begin` replacing a skill action before its end) stops casting the same way. `casting` goes to 0 before the first input past *S′*.
  - An attack that only waits for the skill's end changes nothing, since `casting` runs out by itself.
- **Counting:** `casting` counts inputs stepped, not ticks. A starved tick that steps no input doesn't count it down, on either side.
- **Resets:** a death, a respawn, or a map change resets it with the rest of the state. A knockback leaves it running.
- **The client's view of it:** `PlayerView` gains `casting`, so the client's reconcile resets to the server's own value and its replay matches.

### The client's prediction

- **Start:**
  - `beginSkill` notes the seq the request carried.
  - The predictor sets `casting` when it begins input *S* + 1.
  - The tick entry for that input records the start, so a replay from an older server state applies it again.
- **Cut short:** a local attack that cuts a skill off is noted the same way, and clears `casting` at *S′* + 1.
- **Refusal:** a refused skill drops its start. If the start was already applied, the predictor clears it and replays from the last server state.
- **Wire:** `fromWire` reads `casting`.
- **Facing:** the shape's facing is the predicted facing at the press, as today. With the facing held, it stays right for the whole skill.

## 8. Balance

The harness (`balance_test.go`, `rotation_test.go`) ran on `main` with this section 3 data. Every check passes, the chain bound included.

| Class | Damage a second with skills, against today | A perfect chain adds | Skills' share | Hits to kill with skills, against basic attacks alone |
| --- | --- | --- | --- | --- |
| Beginner | ×0.82 | 4% | 49% | 1.75 against 2.4 |
| Warrior | ×0.78 | 8% | 67% | 4.3 against 7.1 |
| Thief | ×0.73 | 6–8% | 83% | 3.3 against 6.9 |
| Bowman | ×0.67 | 9–10% | 84% | 2.6 against 7.1 |
| Magician | ×0.74 | 4–9% | 88% | 1.8 against 7.3 |

- **Fights with skills take about 20–50% longer** than today. They're still 1.4 to 4 times faster than with basic attacks alone. The basic-attack bands don't move.
- **Field bosses:** a lone player using skills takes 0.6 to 2.5 minutes, from 0.5 to 2.1. With basic attacks alone it's 3, unchanged.
- **Why the cooldowns grow:** with 1.5 s skills and today's cooldowns, a Bowman's and a Thief's two damage skills cover every tick back to back. Their rotation is then 100% skills, and a skill can't cut off a skill, so they never cancel (a perfect chain adds 0%).

## 9. Protocol

- **`client.proto`:**
  - `PlayerView.casting = 16`, as `uint32`, in sub-steps.
  - A new `AttackUsed { attacker, facing, length_ticks }` in the server message's `oneof`.
  - `ClientMessage.seq`'s comment adds `UseSkill`.
- **`world.proto`:**
  - `AttackRequest.seq` and `UseSkillRequest.seq`;
  - `AttackUsed` in `DeliverResponse`'s events;
  - `PlayerView.casting` on the world's own player view, which the gateway copies across.
- **All additions,** so `buf breaking` (WIRE) passes. There are no new codes.

## 10. When things fail

| Failure | What the player sees | Why it is safe |
| --- | --- | --- |
| The server holds a skill past the client's moment, from the slack or jitter | Casting starts an input later on the server; a correction of a few pixels at most | The reconcile replays from the server's state, which carries `casting` |
| A skill is refused after the client started casting | A step or two of slow walking that gets corrected | The predictor drops the start and replays |
| A modified client ignores the slow or the held facing | Its own screen is wrong; everyone else sees the server's sim | The server moves every player by its own sim |
| Inputs stall | The slow lasts the same number of inputs on both sides | `casting` counts inputs stepped, not ticks |
| `AttackUsed` arrives late or is dropped with a frame | Another player's swing shows late, or not at all | It's drawing only; damage comes as `Damage` |
| A very small or very wide window | The same 960 × 540 area, smaller; or more width, with backdrop beside a small map | The zoom keeps the area; the camera bounds hold the map |
| The window is resized mid-fight | Labels and the background follow the new zoom | The zoom is recomputed on `RESIZE` |

## 11. Testing

- **Game data:** `casting_speed` and its refusals; the new lengths and cooldowns load.
- **Sim:** the new vectors, replayed by Go and TypeScript.
- **World:**
  - casting starts at the input after the skill's press, with the full length;
  - it starts at the next input when the skill started late;
  - a cutting attack ends it at the input after its press, and a waiting attack doesn't;
  - it runs out at the length;
  - a refused skill never sets it, and a death or a map change clears it;
  - the facing holds while casting;
  - `AttackUsed` goes out with the length after attack speed.
- **Client:**
  - the predictor applies a start and a cut at the right seq and applies them again in a replay, and a refusal drops them;
  - zoom at 1920 × 1080 (2), 1280 × 1000 (1.33), 2560 × 1080 (2), and 3840 × 2160 (4);
  - labels keep their screen size across zooms;
  - the pose's beats, with ready at the cancel point;
  - the glint at the cancel point less the margin, only when the other kind can cancel, and not after a cut;
  - a remote `AttackUsed` draws a pose and a shot.
- **Balance:** the harness passes with section 8's numbers.
- **End to end:** a skill slows its user on the server and holds their facing, and a second player sees the first's basic attacks.

## 12. Build order

On its own branch and PR. Each step is one commit, green on `make test`, `make lint`, `make cover`, and `make breaking`:

1. **Data:** skill lengths and cooldowns, and the harness numbers in the PR.
2. **The view:** the zoom, screen-fixed objects, labels, and drawn textures. Client only.
3. **Beats and the cue:** the pose, the slash arc, shapes fading by the cancel point, and the glint. Client only.
4. **Others' attacks:** `AttackUsed` from the world through the gateway to the client.
5. **Moving while using a skill:** the sim and its vectors, seqs on `Attack` and `UseSkill`, casting in the world, `PlayerView.casting`, and the client's prediction.

## 13. Decisions

Agreed on 2026-11-12:

1. **A camera zoom to a 960 × 540 view, up to 4×,** not bigger sprites. Only the drawing changes, and every screen shows the same area.
2. **Three beats over each action, ready at the cancel point, and a glint** when a press starts to cancel. The glint is for the player's own character only, after every basic attack and after cancelable skills.
3. **Other players' basic attacks animate,** through a new `AttackUsed` event.
4. **Every skill takes 1.5 s, and damage skills' cooldowns grow 1.5×.** Powers stay, so fights with skills slow down.
5. **Half speed for walking and climbing, and a held facing, while using any active skill.** Jumps keep their height, and basic attacks stay free.

## 14. Alternatives considered

- **Bigger sprites, not a zoom:** bigger bodies need bigger hitboxes, taller gaps under platforms, and new maps, with the reach tests and every vector made again. A zoom gives the same picture with none of that.
- **A fixed zoom, such as 2×, for every window:** a big window would show far more map than a laptop, and a small one would show too little. A fixed area makes every screen show the same fight.
- **A timing bar under the character:** easier to read, but more interface over the fight. The glint keeps the eye on the character. A bar could come later as a setting.
- **1.5 s skills with today's cooldowns:** a Bowman and a Thief would chain skills back to back and never cancel (section 8).
- **Raising skill powers to keep today's kill speed:** the user had found fights with skills already ending fast. Cooldowns alone keep the data change small, and the harness reports the result.
- **The slow as a per-tick parameter, not state:** the client's replay would need each tick's parameter recorded, and the server's snapshot wouldn't carry it. As state, it travels in `PlayerView` and the replay matches by itself.
- **Starting the slow at the server tick the skill starts:** the server steps inputs a few ticks behind their arrival, from its input buffer. The slow would land on different inputs than the client predicted, and nearly every skill would cause a correction. Tying it to the input after the press makes both sides agree.
- **A field on `PlayerView` for the latest attack:** every attack would resend the whole view to the map. An event the size of `SkillUsed` is smaller and matches how skills are shown.
