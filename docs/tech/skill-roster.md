# A fuller skill roster

> **Status: approved · 2026-11-17.** The high-level design for [C17. A fuller skill roster](../product/features/combat-and-progression/c17-skill-roster.md). It adds the mechanics the new skills need, then the 26 skills as data. Traits and the skill window's new layout come after it.

## 1. What has to hold

- **The user's words** (2026-11-17): "firstly just add more skills to the games, skills to approach, to buff, escape,...". Before traits: "different traits add different effects to the skill", up to two a skill, one chosen at a time, and a skill window whose "details only show when hovering".
- **Their choices:**
  - five new skills per first class and two for the Beginner, as C17's table lists;
  - escape skills give a short dodge;
  - six keys stay.
- **The server decides,** as for today's skills: it moves players and mobs, applies every effect, and resolves every hit. The client computes what it can predict and draw.
- **Skills stay data.** Each mechanic below is a field in `skills.yaml`, so traits can later swap one field for another.
- **No new movement keys.** Moving skills are skills: energy, cooldown, one action at a time. The no-dash rule stands for plain movement.

## 2. New data fields

| Field | On | Meaning |
| --- | --- | --- |
| `level` | skill | The character level from which it takes its first point. 1 if absent |
| `move` | active | `{kind: dash \| leap \| blink, distance, time, toward: lock \| back \| heading}`. `delay` (default 0) holds its start that long past the press's next input, as Deflect leaps once its guard is down; the delay and the time fit in the skill's length. `stop` (default true) ends a dash at the first mob body it meets. `through`, a break-free's, passes mob bodies and ends clear of them, as a blink does; it can't be named with `stop: true` or on a blink |
| `dodge` | active | A time from the action's start during which mobs' attacks pass through the user |
| `status` | damage, status | `{id, time, power}`: a status from `statuses.yaml` on what the skill lands on ([Abnormal statuses](./statuses.md)); `power` for damage over time alone, and no `time` on a knockdown |
| `push` | damage, status | `{distance}`: what the skill lands on is pushed away over 0.2 s, then flinches |
| `hits` | active | `{count, every}`: the hit repeats, `count` times, `every` apart, from the hit point |
| `shape.at: lock` | area | The circle centres on the locked mob, if it's within `shape.reach`; else at `reach` along the aim |
| `reach_party` | buff | The buff reaches the caster and party members within the shape's radius, not the caster alone |
| `on_hit` | buff | `{status, time, power}`: while the buff lasts, each of the user's hits inflicts the status |
| `shield` | buff | `{power}`: a shield of `power` × the user's magic attack that takes damage before HP, for the buff's duration |

- **Validation** refuses:
  - a field on the wrong kind;
  - a `move` with no distance or time;
  - `toward: lock` on a blink;
  - a `status` that `statuses.yaml` doesn't have, or with a time or power its status doesn't take;
  - `hits.count` below 2;
  - `shape.at` on anything but an area;
  - `level` outside 1 to the cap.
- **A stage may change any of these,** as stages change other fields today. Traits will lean on that.
- **Since C19's PR A** ([Abnormal statuses](./statuses.md) section 10), `control` is `status` and `push`, `poison` is `on_hit`, and the `control` effect is `status`. The table above is current; the PR 1 notes below keep their first names.
- **As built (PR 1), with the calls made then:**
  - **Blink toward the lock:** allowed, despite the list above. It ends just past the far contact ring, behind the mob, which is Shadow Step's "behind". A dash or leap toward the lock stops at the near ring. A blink refuses `time` and `stop` instead.
  - **Two new active effects** for the skills that hit nothing:
    - `control`: a shape and a `control`, with no power or damage type. Taunt and Smoke Bomb.
    - `move`: a `move`, with `dodge` allowed and no shape. Roll Back and Blink.
  - **Where each field goes:**
    - `move` and `dodge` on any active;
    - `control` on a damage or control skill;
    - `hits` on a damage skill;
    - `poison`, `shield` and `reach_party` on a buff.
  - **A buff** gives at least one of a bonus, a poison and a shield. A passive still gives a bonus. `reach_party` needs an area, whose radius it reaches.
  - **Control parameters:** stun, root, taunt and lose_sight take a `time` and no distance. A push takes a `distance` and no time. A knockdown takes neither.
  - **Stages:** a stage changes only what it names inside `move`, `control`, `hits`, `poison` and `shield`. Naming a new kind of move or control starts it afresh. A dash or leap stops at a body unless it says `stop: false`.
  - **`level`** is the skill's alone, not a stage's. It's checked against the cap once `exp_curve.yaml` is read.
  - **The gate:** a point into a skill below its level is refused as SKILL_LOCKED (330030, a 409). The skill window shows "Lv N" over the skill's icon, with no add button, and its card names the level (C20 PR 1).
  - **The client** gets every field from the generator, resolved stage by stage as the loader resolves them.

## 3. Moving skills

### What moves

- **`dash`** moves the user along the ground at `distance ÷ time`.
- **`leap`** does the same, with a hop for the look: `VY` set so it lands as the time runs out.
- **`blink`** moves the user at once, to the farthest point along the way, up to `distance`, that the sim would let them stand on. Each 0.25 m is tested, stopping at a solid, a too-steep cell, a mob body, or the map's edge.

### The way

The client sends it, the server bounds it, and both sims step it the same:
- **`toward: lock`:** at the locked mob, as far as `distance`, ending at its contact ring (the lock's radius plus the user's).
- **`toward: back`:** away from the lock, or backward from the heading with none.
- **`toward: heading`:** along the keys' heading, or backward from it while standing.
- **The wire:** `UseSkill` carries the move as a unit vector `(mx, mz)` and a length, worked out by the client as it starts the skill.
  - The world clamps the length to `distance`, and refuses a vector more than 1e-6 from unit length.
  - It takes the rest as sent. Walking lets a player face any way, and the length is bounded, so this gives nothing a client couldn't otherwise reach.
  - Shadow Step's "behind the lock" is `toward: lock`, and the world aims it itself, judging the lock where the world has it:
    - **The aim:** from the locked mob's body in the frame the player's input names, the one whose bodies the move meets, to 0.05 m past its far contact ring. The margin keeps the landing clear of the body however the sums round.
    - **Lag:** the client's drawn mob trails the world's, and a way aimed at it landed in front of a mob walking away. Aimed by the world, it lands behind.
    - **Range:** "within `distance`" is the mob. A mob within it is blinked behind even when that lands past `distance`, by up to the mob's ring and the margin.
    - **The client's way** is taken, held to `distance`, for a mob past `distance` or one that frame doesn't hold.
    - The end is a point the sim allows. With a wall too close behind the mob, the blink stops at the wall and lands in front, at the last point clear of the body.

### The sim

Both sims (`server/internal/sim`, `web/src/sim`) gain a scripted move:
- **`State.Move`:** `VX`, `VZ`, the sub-steps left, and `Stop`.
- **Each sub-step**, while it runs:
  - the walk velocity is replaced by the move's;
  - collisions work as for walking, sliding along solids;
  - with `Stop`, the move ends at a mob body;
  - gravity runs as usual, so a leap's hop rises and falls.
- **A blink** is a move of one sub-step whose length the sim tests in 0.25 m steps, as above.
- **Start:** the move starts from the first input after the press's seq, as the casting once did. So the client predicts it exactly. The predictor's marks come back for it: a mark per move start.
- **`PlayerView.move`:** the move left, so a reconcile resumes it.
- **No trigonometry:** the vector comes in from outside, so the sim does none.
- **Vectors** for dash, leap, and blink:
  - along open ground, into a wall, along a wall at an angle, up a steep cell, and off a mesa;
  - stopping at a mob;
  - a blink stopped short;
  - a move cut by the move running out mid-input.

### The dodge

- While `dodge` runs from the action's start, a mob's attack whose area holds the user passes through it. There's no damage and no knockback, and the user sees "DODGE" where MISS would show.
- The world keeps the dodge's end tick on the player. `PlayerView.dodging` tells others to draw them faded.
- The hit guard is untouched: a dodged attack starts no guard.

### As built (PR 2)

- **The blink**, two calls approved in the build:
  - **Mobs don't stop it.** Solids, an uphill steep cell and the map's edge stop it. It passes through mob bodies and ends at the farthest point tested that no mob's body holds. So Shadow Step goes past the locked mob, which the sim can't tell from the others. A test sweeps lengths past one mob, two, and a large one, and none ends inside a body.
  - **It lands at the start of the input's tick,** before that input's sub-steps, in both sims and in the predictor's replay. Gravity and the ground take it on from there, so a blink off a mesa falls.
  - **It tests at the start's height.** Its 0.25 m tests are the walk's x-then-z slides, all at the height it set off from. So a blink down a slope passes a steep uphill cell whose top is below that height, where a walker on the slope would stop. That's intended: the blink crosses at the start's height. It's only lenient on the way down.
- **A press waiting for its turn** (a skill pressed mid-swing) still starts its move from the input after its press:
  - The client holds the press until its own action clock lets the skill start, then marks and sends it with that input's seq.
  - The gateway posts no later input until the world answers the press, so the world steps the input after it only once the move is marked.
  - A scene test covers the client side and an e2e the server side. With the gateway's hold broken, the e2e's move starts an input late and the test fails.
- **A rescue** to the spawn clears a waiting move's mark, as a death does.
- **Dash and leap:**
  - `State.Move` is `{vx, vz, left, stop}`, and the JSON leaves it out when idle.
  - The keys still turn the heading and jump, but the move's velocity replaces the walk's.
  - With `stop`, the move ends at the first mob body that cuts a sub-step short, even partly.
  - **With `through`**, the move slides past no mob; solids, uphill steep cells and the map's edge still stop it.
    - It keeps the farthest point it reached that no mob's body held, `clear_x, clear_z`, starting from where it set off. If it runs out inside a body, it ends there, as a blink does.
    - `SkillMove` on the wire carries `through` and that point, so a client corrected mid-leap resumes the same move.
    - Vectors: `leap-through-mob`, `leap-through-into-mob`, `leap-through-touching-mobs` (no room between two bodies, so it ends before the first), `leap-through-to-gap` (it ends between two), `leap-through-into-box`, `leap-through-never-clear` (from inside a body it never leaves, so it ends where it set off) and `leap-through-round-trunk` (rounding a trunk past a mob).
  - A leap sets `VY = g·dt·(n+1)/2`, which brings the hop back to the start's height on its last sub-step, before landing on the next.
  - A move lasts its time's ticks × the sub-steps a tick (12 for 200 ms at 20 Hz).
- **The way:**
  - UseSkill's `SkillWay` holds `x, z, length` as doubles, so both sims get the same bits.
  - The world refuses a vector more than 1e-6 off unit length, or a negative or NaN length, as `BAD_SKILL_WAY` (330031, a 400). It holds the length to `distance`, and a skill that doesn't move ignores its way.
  - The client holds the length to `distance` too, but for a blink at a lock within it, so its prediction matches.
- **`toward` on the client** (`systems/skill-way.ts`):
  - **lock:** to the near contact ring for a dash or leap, or 0.05 m past the far one for a blink, as the world aims it. With no lock, along the heading the player faces.
  - **back:** away from the lock, or back from the heading.
  - **heading:** along the keys held, as the sim heads them, or back from the heading while standing.
- **The start:**
  - The world marks the move for the first input past the press's seq. A press during an input it hasn't stepped yet waits for the one after.
  - A move waits for an input, so a held, starved player doesn't start one.
  - The predictor keeps its marks per tick for a replay. A refusal drops the mark. An ack past its history keeps the marks still waiting.
  - `PlayerView.move` resumes a move after a reconcile.
- **The dodge:**
  - It runs from the action's start, the tick the action begins, for its time.
  - Damage carries `dodge`, and the client shows DODGE with a miss's puff.
  - A death clears the move, its mark and the dodge.
  - The fade for `PlayerView.dodging` comes with PR 5's looks.

## 4. Control on mobs

> Replaced on 2026-11-17 by [Abnormal statuses](./statuses.md): one status system for mobs and players, with `status`, `push`, and `on_hit` in place of `control` and `poison`. This section describes the first plan.

The mob state machine (`application/mobs/`) gains statuses beside its poses. Each has an end tick:

| Status | Effect | Drawn |
| --- | --- | --- |
| `stun` | No movement, no attack. A windup in progress is cancelled, as a knockdown cancels it | Stars, or ice from Frost Nova |
| `root` | No movement; attacks what it reaches | Roots at its feet |
| `knockdown` | Today's down pose and rising, forced | As today |
| `push` | Moved `distance` away from the user over 0.2 s, stopped by solids, then a flinch | As a flinch |
| `taunt` | The provoker becomes the user for `time`, as today's provoke rule does | A red mark over its head |
| `lose_sight` | Drops the user as a target, and can't see them for `time` | A puff of smoke |

- **Super armor** (every elite and boss) ignores `stun`, `root`, `knockdown`, and `push`, as it ignores today's knockdown. `taunt` and `lose_sight` still apply.
- **A new status of the same kind** restarts the time, and never stacks.
- **`MobView`** gains `status`, a bit set, so the client draws it.

### Poison

- A poisoned mob holds poisons, each from a user: their attack then, the power, and an end tick.
- Each second, each poison deals `power` × that attack as a hit of the user's damage type.
  - It always lands: no MISS and no critical hit.
  - It's counted for kills, EXP, and the boss's damage share, as the user's.
  - It shows as small green numbers.
- **Two users** each poison on their own. **The same user** poisoning again restarts their poison.

## 5. Other mechanics

- **`hits`:** a repeat schedules the next hits on the pending-hit list that wind-ups already use. Each uses the user's position and aim at its own tick, so Flurry follows a moving player. A death or a map change drops what's left.
- **`shape.at: lock`:**
  - The circle centres on the server's position of the locked mob, from the lock id `UseSkill` already carries, if it lies within `reach`.
  - Otherwise it centres at `reach` along the aim.
  - The client draws a target circle where it falls.
- **`reach_party`:** the buff goes to the caster and to every party member on the map within the radius at its hit, each keeping their own timer.
- **`shield`:**
  - The player gets a shield pool: `power` × their magic attack at the hit.
  - A mob's damage takes the pool first, then HP. The pool ends at the buff's end or at 0.
  - `PlayerView.shield` carries what's left, for the bubble and the HUD.
- **`level`:** spending a skill point on a skill below its level is refused (`SKILL_LOCKED`). The skill window shows the level beside a locked skill.

### As built (PR 4)

- **`hits`:**
  - The repeats go on the pending-hit list at the first hit's tick plus `k × every`. Each strikes from where the user stands and aims at its own tick.
  - A repeat of an `at: lock` area finds the lock again, so Arrow Rain follows a moving mob.
  - A death or a map change drops what's left. A cancel doesn't: the repeats are the skill's, already under way, as a wind-up's hit is.
- **`shape.at: lock`:**
  - The lock's position is the world's own, at the hit.
  - With no lock, or a lock past `reach`, the circle falls at `reach` along the aim.
- **`reach_party`:**
  - The radius is measured in 3D from the caster's middle, as a heal measures it.
  - A party buff refuses a `move_speed` bonus at load: a member's client predicts its own walk and doesn't know another player's buff.
- **`shield`:**
  - The pool is `round(magic attack × power)` at the hit.
  - Shields take a mob's damage in their skills' order before HP. The hit still knocks back and guards, and its Damage still shows the whole amount.
  - The buff stays to its end with an empty pool.
  - A map change carries what's left. A checkpoint doesn't, so a relog or a crash brings the buff back with an empty pool: the pool is seconds of combat, like the dodge.

## 6. The 22 skills

They're in `skills.yaml`, listed in each class's `skills` in `classes.yaml`, with an icon each (C16) and one evolution at 10 points. Starting values are below. The harness and the playtest tune them.

| Skill | Energy | Cooldown | Length | Mechanics |
| --- | --- | --- | --- | --- |
| Lunge | 2 | 6 s | 1 s, cancelable | `move: dash 4 m, 0.2 s, lock`; arc 1.875 m 120°; power 1.2 (+0.04) |
| Roll Back | 2 | 8 s | 0.6 s | `move: leap 4 m, 0.3 s, back`; `dodge: 400ms` |
| Charge | 3 | 10 s | 1 s, cancelable | `move: dash 7 m, 0.3 s, lock`; hits the first mob met, power 1.8 (+0.05); `control: stun 1.5 s` |
| Ground Slam | 3 | 12 s | 1 s | area 3.5 m; power 1.6 (+0.05); 6 targets; `control: knockdown` |
| War Cry | 3 | 60 s | 1 s | buff 30 s, `reach_party`, area 8 m; physical and magic attack +8 (+1) |
| Taunt | 2 | 15 s | 0.6 s | area 6 m; `control: taunt 5 s` |
| Toughness | — | — | — | passive; max HP +50 (+10) |
| Shadow Step | 3 | 10 s | 1 s, cancelable | `move: blink 8 m, lock`, behind; arc 1.875 m 90°; power 1.6 (+0.05) |
| Smoke Bomb | 2 | 15 s | 0.6 s | `move: leap 5 m, 0.3 s, back`; `dodge: 500ms`; area 6 m `control: lose_sight 2 s` |
| Flurry | 3 | 7 s | 1 s, cancelable | arc 1.875 m 90°; `hits: 5 every 100ms`; power 0.7 (+0.02) each |
| Poison Blade | 3 | 45 s | 1 s | buff 20 s; `poison: 4 s, 0.2 (+0.01)` |
| Keen Edge | — | — | — | passive; crit rate +3% (+0.3%) |
| Disengage | 2 | 12 s | 0.6 s | `move: leap 5 m, 0.3 s, back`; `dodge: 400ms`; arc 3 m 90° ahead, power 1.0, `control: push 2 m` |
| Pinning Shot | 2 | 9 s | 1 s, cancelable | shot 13 m; power 1.6 (+0.05); `control: root 3 s` |
| Arrow Rain | 3 | 12 s | 1 s | area 3 m `at: lock`, reach 14 m; `hits: 3 every 400ms`; power 0.9 (+0.03) each; 6 targets |
| Eagle Eye | 3 | 60 s | 1 s | buff 30 s; accuracy +15 (+2), crit damage +15% (+1.5%) |
| Archery | — | — | — | passive; physical attack +4 (+1) |
| Blink | 2 | 10 s | 0.4 s | `move: blink 6 m, heading`; `dodge: 300ms` |
| Frost Nova | 3 | 14 s | 1 s | area 3.5 m; power 1.2 (+0.04); 6 targets; `control: stun 2 s` (ice) |
| Barrier | 3 | 30 s | 1 s | buff 10 s; `shield: 2.0 (+0.1)` |
| Meteor | 3 | 15 s | 1 s, hit at 0.6 | area 3 m `at: lock`, reach 12 m; power 3.2 (+0.08); 6 targets; `control: knockdown` |
| Arcane Mind | — | — | — | passive; magic attack +4 (+1) |

- **Levels** are C17's.
- **Shorter lengths** (0.6 s and 0.4 s) for escapes and Taunt keep them snappy. They aren't cancelable, so their dodge always runs out.
- **The evolutions** at 10 points raise power, reach, or time by about a third, named in data.

### As built (PR 5a: the roster)

PR 5 ships as two PRs (approved): 5a, the skills and their data; 5b, skill chains' client half and the looks.
- **Statuses** are C19's fields: `status: {id, time, power}`, `push`, and Poison Blade's `on_hit`, in place of `control` and `poison`. Pinning Shot's root is `pin`, and Frost Nova's stun is a `freeze`.
- **A move skill's hit lands where its move ends,** with its cancel point at or after it: Lunge's at 0.2, Charge's at 0.3, and Shadow Step's at 0.15, three ticks after its blink. Meteor's hit and cancel point are both at 0.6.
- **Charge** strikes a 120° arc of 1.875 m at the dash's end, with one target: the lock first when it reaches it, else the nearest.
- **Disengage** strikes one target in its 3 m arc ahead, then leaps. Its power doesn't grow with points.
- **The evolutions,** each adding about a third to what defines the skill:

  | Skill | At 10 points | Change |
  | --- | --- | --- |
  | Lunge | Long Lunge | dash 5.25 m |
  | Roll Back | Tumble | dodge 0.55 s |
  | Charge | Rampage | stun 2 s |
  | Ground Slam | Earthshaker | power 2.1 |
  | War Cry | Battle Roar | 40 s |
  | Taunt | Challenge | taunt 6.5 s |
  | Toughness | Iron Will | max HP +65 base |
  | Shadow Step | Shadow Dance | blink 10.5 m |
  | Smoke Bomb | Thick Smoke | lose sight 2.75 s |
  | Flurry | Blade Storm | power 0.95 |
  | Poison Blade | Venom Edge | poison power 0.27 |
  | Keen Edge | Razor Edge | crit rate +4% base |
  | Disengage | Vault | leap 6.5 m |
  | Pinning Shot | Nailing Shot | pin 4 s |
  | Arrow Rain | Arrow Storm | 4 waves |
  | Eagle Eye | Hawk Eye | 40 s |
  | Archery | Marksmanship | physical attack +5 base |
  | Blink | Far Blink | blink 8 m |
  | Frost Nova | Glacial Nova | freeze 2.75 s |
  | Barrier | Aegis | shield 2.7 |
  | Meteor | Comet | power 4.25 |
  | Arcane Mind | Arcane Focus | magic attack +5 base |

- **C19's four status evolutions** come here too, as new 10-point stages: Slash Blast's Rending Blast (armor_break 5 s), Double Stab's Bleeding Stab (bleed 4 s, 0.15), Power Shot's Blinding Shot (blind 3 s), and Fire Arrow's Searing Arrow (burn 2 s, 0.3).
- **Icons,** chosen at 40 px from game-icons.net: Lunge lorc/spiral-thrust, Roll Back lorc/dodging, Charge delapouite/charging-bull, Ground Slam lorc/quake-stomp, War Cry lorc/sonic-shout, Taunt delapouite/enrage, Toughness lorc/strong, Shadow Step lorc/shadow-follower, Smoke Bomb darkzaitzev/smoke-bomb, Flurry lorc/sword-array, Poison Blade lorc/dripping-blade, Keen Edge lorc/sparkling-sabre, Disengage lorc/run, Pinning Shot lorc/pierced-body, Arrow Rain lorc/striking-arrows, Eagle Eye delapouite/eagle-head, Archery lorc/bowman, Blink lorc/teleport, Frost Nova lorc/snowflake-2, Barrier lorc/magic-shield, Meteor lorc/burning-meteor, Arcane Mind lorc/brain. Credits spell DarkZaitzev as the site does, and a test ties each credited name to `public/icons/LICENSES.md`.
- **Colours:** a move skill and a passive take their class's attack colour, so Blink and Arcane Mind are the Magician's purple.
- **The skill window** says how each moves and which way, its dodge, its repeated hits, an area at the lock, a party buff's reach, and a shield's pool.
- **The dev tools spend a point in each skill their level opens,** after the promotion's asks and before the main skill, so bots and test accounts have every skill on hand. In 5a the harness planned the same way; 5b went back to the promotion's asks and the main skill (below).
- **The harness** counts each of an action's hits, and the damage over time it inflicts until it runs out or the skill can restart it. A buff's on-hit poison counts for the buff's share of its cooldown.
  - Logged at level 30, skills one action at a time: Warrior 201, Thief 379, Bowman 481 and Magician 771 damage a second, against 324, 429, 542 and 785 before. The chain gains +146%, +73%, +65% and +27%, against +24 to +26% before.
  - Skills one action at a time fall because the harness uses each 1-point skill as it comes ready, spending energy and action time a stronger skill would use. Without them, each class is within 3 to 5% of before. The bands and boss bounds read basic attacks, so they don't move.
  - So 5b's harness plans as before 5a, the promotion's asks and then the main skill (decided 11-17). The logged chain gain is back to before 5a's spread: +14 to +33%, median +25%, the +20 to +30% Combat describes for nine rows in ten.

## 7. Client

- **`skill-keys`** works out a moving skill's vector and length as it starts the skill:
  - from the lock and the drawn positions for `lock`;
  - from the heading for `heading` and `back`.
  - It marks the predictor with the move from the press's next input.
- **The look,** one effect id each (C13), rebuilt in 3D:
  - trails for dashes and leaps;
  - a flash at both ends of a blink;
  - a fade while dodging, for the player and for others from `PlayerView.dodging`;
  - status marks on mobs;
  - green poison numbers;
  - a bubble and a pale bar over the HP for the shield;
  - a target circle for `at: lock`;
  - each skill's own effect.
- **The skill window** shows a locked skill's level, and refuses points into it.

### As built (5c-1: the moves and the dodge)

- **A move is part of its skill's look,** under the skill's own effect id, so art for `skill.charge` replaces its trail too. A move skill with a shape shows its shape's look as well, such as Charge's swing. One with none, Roll Back or Blink, shows the move alone, not the aura a shapeless skill used to get.
- **Each move reads the body as it's drawn,** so the player's own predicted move and another's, drawn behind the newest frame, show alike:
  - **Dashes and leaps:** four thin speed lines in the skill's colour, from knee to head, trail the body over its last 160 ms of path. They show only while it moves faster than twice a walk, so walking before or after draws none. A leap kicks up dust where it started once it's 0.3 m out.
  - **Blinks:** a tall glow with sparkles where the caster stood at the start, and again where the body lands, once it's drawn more than 1 m from there.
- **The dodge fade:** the body swaps to its toon materials at 0.4 alpha and hides its ink. The faded materials draw a depth pre-pass, so only the front shows, not the head through the helmet.
  - **The player's own** fades from the action's start for the stage's `dodge_ms`, as predicted. A refusal, a status that stops acting, a death or a new map ends it.
  - **Others** fade by `PlayerView.dodging` as of the frame being drawn. It isn't blended between frames.
- **No stall on the first dodge:** each faded shader is built ahead for each kind of mesh it draws. A sword and a skinned body can share a material and not a shader. Warming by material alone left the skinned one to the first dodge, which compiled it in that frame: 7–9 ms against a 4 ms median. The place's faded solids are warmed the same way.

### As built (5c-2: the shield and the area at the lock)

- **Two fields on the wire,** both additive:
  - **`Damage.shielded`:** what of `amount` a player's shields took. A mob's blow and a damage-over-time tick both work out the shield's part before the event goes out, so two blows in one tick each say their own.
  - **`SkillUsed.target`:** the mob the use was aimed at, 0 for none, so the others' clients can centre an area at the lock as the caster's does.
- **The split number:** a blow past a shield shows what came off HP, then the shield's part in pale blue above it. A blow the shield took whole shows only the shield's.
- **The bubble:** a pale sphere round each player with shield left, the player's own as the newest frame has it and others' as their drawn frame does. It's brightest at its rim, so the body shows through. Its shader is built when the map loads.
- **The HP bar:** a pale part from the fill's end, ending at the bar's end at most, with "+N" after the numbers. Only the own frame shows it: mobs have no shields, and the party's state carries none.
- **The area at the lock:** a rim in the skill's colour where the world centres it, following the lock each frame. It sits on the locked mob while that's within reach of the caster, else at reach along the aim. Its fill grows over the windup and flashes at each wave. It replaces the aura Meteor and Arrow Rain used to get.
- **Damage over time** splits too: the HP part small in its status's colour, as statuses B2a draws a tick, and the shield's in pale blue over it.

## 8. Balance

- **The rotation harness** gains the new damage skills: Lunge, Charge, Ground Slam, Shadow Step, Flurry, Disengage, Pinning Shot, Arrow Rain, Frost Nova, and Meteor. `hits` and poison are counted in.
  - Control, dodges, and moves aren't modelled, and are left to the playtest.
  - The chain's gain and kill speed with skills are logged, as today.
- **The bands and the boss bounds** read basic attacks alone, so they don't move.
- **Six keys** cap how many skills a rotation carries. With [Skill chains](./skill-chains.md), a key carries its root and follow-ups, so the harness counts six chains, not six skills. Each class's six chains with the most damage it has points in. Since [Key bindings](./key-bindings.md), eight. A follow-up whose condition the harness can't model, such as a dodge or a control, isn't counted.

## 9. Testing

- **Data:** each field's refusals; `level` gating points; stage overrides.
- **Sim vectors:** section 3's, on Go and TypeScript, on amd64 and arm64.
- **World:**
  - each move kind and way, with clamping and the vector check;
  - Shadow Step's end check;
  - the dodge passing a mob's hit, with no guard;
  - each status, and super armor ignoring four of them;
  - a status restarting, not stacking;
  - poison ticking, credited, and two users' poisons;
  - `hits` following the user;
  - `at: lock` on and off the lock;
  - a party buff reaching members within its radius;
  - the shield taking damage first and ending;
  - `SKILL_LOCKED`.
- **Client:**
  - a move predicted with no correction;
  - each look and status mark;
  - the shield bar;
  - the locked level in the skill window.
- **End to end:**
  - a Charge lands at its mob and stuns it;
  - a Roll Back dodges a mob's slam;
  - a Meteor centres on the lock.
- **Live:** every new skill, for each class, against normal mobs, an elite, and the field boss.

## 10. Build

Five PRs, in this order. The first three are the mechanics, each with its tests, and can be built side by side once the data fields are in:

1. **Data fields:** the fields of section 2, their validation, `level` gating and `SKILL_LOCKED`, and the generator.
2. **Moving skills and the dodge:** the sim's move and vectors, `UseSkill`'s move vector, the world's clamping, the dodge, `PlayerView.move` and `dodging`, and the predictor's marks.
3. **Control and poison:** mob statuses, super armor, `MobView.status`, and poison.
4. **The rest of the mechanics:** `hits`, `at: lock`, `reach_party`, and `shield`, with `PlayerView.shield`.
5. **The 22 skills and their looks:**
   - the data, icons, and evolutions;
   - the classes' lists;
   - the harness run and its numbers;
   - the client's looks, status marks, shield bar, and target circle.

**Docs the build keeps true:**
- this page;
- Systems → Classes and skills: the placeholder note goes, and the level gate is added;
- Systems → Combat: dodge, control, and poison.

## 11. Decisions

The user's choices are in section 1. These calls were made in planning and logged for the user to push back on:

1. **The roster and its numbers** (section 6, C17's table), as a starting point for tuning.
2. **Skills unlock by level** (`level`), spread from 10 to 20 for classes and 3 and 5 for the Beginner's two.
3. **Super armor ignores stun, root, knockdown, and push;** taunt and losing sight still work on elites and bosses.
4. **The client sends a moving skill's vector and length;** the world bounds them. Moves are exactly predictable, and a bounded length gives nothing walking couldn't.
5. **Poison counts as the user's damage** for kills, EXP, and boss shares, and always lands.
6. **Party buffs reach members within the radius** at the hit, and each keeps their own timer.
7. **Escapes and Taunt are shorter actions** (0.4 s to 0.6 s), not cancelable.

## 12. Alternatives considered

- **The server choosing the move's way from its own positions:** its positions differ from the client's drawn ones, so every approach would end in a correction.
- **Blink through walls:** it would skip map design, and reach places the reach check doesn't cover.
- **Statuses stacking:** harder to read and to balance. A restart is clear.
- **Party-wide buffs on the whole map:** a party spread over a map would buff people who aren't fighting together.
- **Eight keys:** the user kept six.
