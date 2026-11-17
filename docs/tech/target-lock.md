# Target lock

> **Status: approved · 2026-11-16; built in the target-lock PR; revised 2026-11-17 (section 12).** The high-level design for [C15. Target lock](../product/features/combat-and-progression/c15-target-lock.md) and Systems → Combat → Aiming. It replaces the auto-aim of [3D world](./3d-world.md) section 6. All of it is the client's: the world takes each action's `aim` as sent, as today. As built after the live pass, `Attack` and `UseSkill` also carry the lock's id, and the world hits that mob first when the action's shape reaches it (section 4).

## 1. What has to hold

- **The user's words** (2026-11-16, after playing `main` at e931cfe1):
  - "lockin a target and a button to change target to the next closest one";
  - "auto lockin the closest target";
  - "skill and normal attack go to that locked target";
  - "skills and normal atk no longer go in the way we face";
  - "normal atk doesnt delay our face in the atk direction, our direction should be free after a normal atk";
  - "UI should show which target is lockin by us".
- **Then, after reading the first plan:** "i think we'll need the target window" and "skills should free my face".
- **The client computes, the server validates.** The lock lives on the client. `Attack` and `UseSkill` already carry `aim`, and the world uses it for the action's shape without checking the pick, since walking lets a player face any way. So the protocol, the world, and the bots don't change.
  - **As built after the live pass (2026-11-17),** aim alone wasn't enough for "go to that locked target". The world hit the nearest body the shape reached, so a nearer slime in the arc took a Warrior's swings at a locked elite.
  - `Attack` and `UseSkill` now also carry `target`, the locked mob's id, 0 for none. It's a new field number, so the change is additive.
  - The world takes that mob first when the action's shape reaches it. It still checks every hit by the shape, so an id out of reach gains nothing.
  - Bots send no id, and keep the nearest rule.
- **Keyboard only.** One new key, Tab, in both presets.

## 2. The change at a glance

| Part | Today | After |
| --- | --- | --- |
| `server/data/actions.yaml` | `aim_cone: 70` | Nothing: `aim_cone` goes. `lock_range` came with the first build and went with section 12. The loader refuses both as unknown keys |
| `web/src/game/systems/aim.ts` | `aimAt`: the nearest mob within the action's reach and the cone, picked as each action starts | `lock.ts`: a lock kept between actions (section 3). `aimYaw` stays; as built, `reachOf` went too, as nothing else used it |
| Aim | At the pick, else the heading | At the lock, else the heading |
| `Attack`, `UseSkill` | `aim` | As built: also `target`, the locked mob's id, 0 for none |
| The world's pick (`mobs.Arena.Strike`) | The nearest bodies the shape reaches, up to `most` | As built: the target first when the shape reaches it, then the nearest |
| The turn | The body faces the aim until the action's ready point | An attack or a skill only to the end of its strike beat (section 4) |
| `world3d/outline.ts` | Outlines the mob a basic attack would pick | Outlines the locked mob |
| `world3d/lock-ring.ts` | — | New: a gold ring on the ground under the locked mob |
| `input/presets.ts`, `input/actions.ts` | — | `nextTarget`: Tab in both presets |
| `state/target.ts`, `features/hud/TargetFrame.tsx` | — | New: the target window (section 5) |

## 3. The lock

`systems/lock.ts` is pure logic over the mobs as drawn and the player's feet. A `Mark` has an id, x, z, radius, hp, and two flags the scene sets each frame (section 12): `inView` and `inFrame`. Its tests run without a scene.

Since section 12, "in range" below means on screen: a mob is picked only if it's `inView`, and kept only while it's `inFrame`. Section 12 replaced the first build's 12 m `lock_range`.

- **Distance** is across the ground, to the mob's centre, as the auto-aim's was. Height, walls, and ledges don't count. That is how mobs' sight treats players.
- **Each frame,** `update(x, z, mobs)`:
  - **drops** the lock if its mob is gone from the tracker, dead (hp at or below 0), or farther than `lock_range`;
  - **picks** the nearest living mob within `lock_range` if nothing is locked, with ties going to the lower id;
  - **otherwise keeps** the lock, so a mob that comes closer doesn't take it.
- **Tab,** `next(x, z, mobs)`:
  - It orders the living mobs within `lock_range` by distance, then id.
  - It locks the first after the current lock in that order. After the last, it wraps round to the first.
  - With only the locked mob in range, the lock stays.
  - With nothing locked, it works as `update`'s pick.
  - It acts on a fresh press only. Holding Tab doesn't repeat, as with the portal key.
- **Cleared** at a death, a respawn, a map change, and a rejoin, with the `ActionClock`'s reset. The next frame picks again.

## 4. Aim and the turn

- **The aim** is fixed as an attack or skill starts:
  - the yaw from the player's feet to the locked mob's drawn position (`aimYaw`);
  - the heading's yaw with nothing locked, or with the lock right underfoot.
- **A lock out of reach is still aimed at,** so the swing or the shot goes toward it and misses. This follows "go to that locked target". Falling back to a nearer mob would split attacks from the marker. Nothing filters the target by reach.
- **As built, the lock's id goes with the aim.** The world hits the locked mob first when the action's shape reaches it, whether an arc, a shot or an area. It then takes the nearest, up to the action's `targets`.
  - A shot hits the lock even with a nearer mob in its lane.
  - With the lock outside the shape, the hit lands on the nearest the shape reaches, as before. So does a dead or unknown id, which is never refused.
- **Heal** faces the heading, as now. It's an area round the caster, and aims at no one.
- **The turn,** for the player's own body and for others' from `AttackUsed` and `SkillUsed`:
  - **A basic attack or a skill** faces the aim from its start to the end of its strike beat, the first third of the time from the hit to the ready point (`beats.ts`'s `strikeEnd`). After that, the body faces the heading again. This is the user's "free after a normal atk" and "skills should free my face":
    - the Warrior's attack turns back after about 250 ms of its 900 ms;
    - the Thief's after about 185 ms;
    - a skill after about 330 ms of its second.
  - **The rest of a skill's clip** plays facing the heading. On a moving body it plays on the upper body, as section 10 of 3D world has it.
  - **The server's heading** stays the keys', as today, so nothing the world decides changes.
- **A melee arc's drawn slash** keeps the aim's direction even after the body has turned back. It shows where the hit went.

## 5. Showing the lock

- **The outline** (`world3d/outline.ts`, `HighlightLayer`, gold) marks the locked mob in place of the auto-aim's pick.
- **The ring** (`world3d/lock-ring.ts`, new):
  - A flat gold ring on the ground under the locked mob, of the mob's radius plus 0.15 m, 0.08 m wide.
  - It follows the mob's drawn position and the ground height under it, as the telegraph decals do.
  - It turns slowly, a full turn in 4 s, so it reads on any map's ground, snow and sand included. As built, it's eight dashes, so the turn shows.
  - It's drawn after the telegraphs, so a mob's red attack area never hides it. As built, it's in rendering group 1 on the scene's depth: after every red area, and still behind a mob standing on it.
- **Only the player's own lock shows.** Others' locks exist only on their own screens.

### The target window

- **`state/target.ts`** (new) holds the locked mob's id, type, and HP.
  - The world scene sets it when the lock changes, and when that mob's HP changes in a frame.
  - It clears it when nothing is locked.
  - It's display only, read from the mobs the client already tracks.
- **`features/hud/TargetFrame.tsx`** (new) shows, at the top centre of the screen:
  - the mob's name (`mobName`) and level;
  - an Elite tag for an elite;
  - its HP as the HUD's `Bar`, with numbers: the frame's HP over the mob's `hp` from game data.
- **Placement:** under the field boss's bar when that shows, in the same style. As built, `TopBars` stacks the two at the top centre, so the window takes the bar's place when only it shows.
- **The field boss:** when the lock is the field boss, the window hides, since the boss bar already shows its name and HP.
- **No lock:** the window hides.

## 6. Input

- **`nextTarget`** is a new action in `input/actions.ts`, bound to Tab in both presets. Tab is free in both today.
- **The key handler** calls `preventDefault` on Tab while the game has focus, so the browser doesn't move focus between page elements.
- **While typing in chat,** Tab is ignored, as the other game keys are (`input/typing.ts`).
- **The controls table** (Screens and controls) gains the row.

## 7. Data

- **The first build** had `lock_range: 12` in `actions.yaml`, in metres across the ground.
- **Section 12 removes it.** The screen is the range, so no data sets it. The loader refuses the key as an unknown one.

## 8. Testing

- **`lock.ts`:**
  - the nearest is picked, in any direction;
  - a mob that comes closer doesn't take the lock;
  - the lock drops on death, beyond range, and gone from the tracker, and the nearest is picked again;
  - Tab goes to the next by distance, wraps after the farthest, counts mobs behind, keeps a lone lock, and acts on a fresh press only;
  - ties go to the lower id;
  - the lock clears at a death and a map change.
- **Aim:**
  - an attack and a skill aim at a lock behind the player;
  - a lock out of reach is aimed at;
  - nothing locked aims along the heading;
  - heal faces the heading.
- **The turn:**
  - a basic attack and a skill face the aim only until their strike beat ends;
  - the same for another player's body from `AttackUsed` and `SkillUsed`.
- **Showing:** the ring follows the locked mob at ground height and moves to the new lock on Tab; the outline marks the lock.
- **The target window:**
  - it shows the lock's name, level, Elite tag, and HP, and its bar falls as the mob is hit;
  - it moves to the new lock on Tab;
  - it hides with no lock and for the field boss;
  - it sits under the boss bar.
- **Input:** Tab in both presets, with no browser focus move, and ignored while typing.
- **End to end:** with two mobs either side of the player, Tab switches the lock, and the next attack's `Damage` lands on the new lock. As built, in two halves, since no test runs the client against the world: the world test's Tab case sends the next attack's aim at the new lock, and the instance's `TestAnAttackLandsOnTheMobItsAimPoints` lands each aim's `Damage` on the slime it points at, one either side of a player facing north.
- **Live, for every class:**
  - the lock on the nearest when entering a pack;
  - Tab cycling through it;
  - an attack hitting a mob behind;
  - a skill going to the lock while walking away;
  - facing free right after a basic attack;
  - the ring visible on every map.

## 9. Build

One PR, each step one commit, green on `make test`, `make lint`, `make cover`, and `make breaking`:

1. **Data:** `lock_range` in, `aim_cone` out, with validation and the generator.
2. **The lock:** `lock.ts` and its tests; aim from the lock; `aimAt` gone. As built, the outline moved onto the lock here, since the pick it drew went with `aimAt`.
3. **The turn:** an attack's and a skill's facing released at its strike's end, for the player's own body and for others'.
4. **Showing and input:** the ring and Tab; the controls row came with the docs.
5. **The target window:** `state/target.ts`, `TargetFrame.tsx`, and their tests.

**Docs the build keeps true:** this page, and [3D world](./3d-world.md) section 6, whose auto-aim this replaces.

## 10. Decisions

The user's points are in section 1. The calls below were made in planning and logged as decisions for the user to push back on, under the rule the user set on 2026-11-14:

1. **The auto-lock is sticky:** the nearest mob is locked when nothing is, and kept until it dies, leaves range, or the player switches. Re-picking whenever a mob came closer would jump the lock about in a pack, sending attacks to mobs the player didn't choose.
2. **Any direction, with no line of sight**, as mobs' sight works. The lock was first limited to 12 m across the ground. Since 2026-11-17 it's limited to the screen: the user said a lock should never be a mob "cant find it anywhere" (section 12).
3. **A lock out of reach is still aimed at, and misses.**
4. **Tab switches to the next farther mob, wrapping round,** including mobs behind.
5. **Basic attacks and skills face the lock for their strike only.** At first skills kept facing it for their second, since the user had asked only about the basic attack. The user then said "skills should free my face".
6. **Arcs and areas still hit every mob they cover**, up to their `targets`, nearest first. As built after the live pass, arcs, shots and areas take the lock first when it's in the shape, then the nearest up to `most`.
7. **Bosses get no priority:** distance alone, so Tab reaches them.
8. **A gold ground ring, the outline, and a target window** with the lock's name, level, Elite tag, and HP. At first there was no window, for the user's taste for marks in the world. The user then said "i think we'll need the target window".
9. **Locks are private.**

## 11. Alternatives considered

- **A lock that re-picks the nearest every frame:** the marker and the aim would flicker as a pack moves.
- **Falling back to the nearest mob in reach when the lock is out of reach:** attacks would land on a mob other than the marked one.
- **Refusing an attack out of reach:** a dead key, and no swing to read. A whiff shows the player they must close in.
- **Moving toward the lock on attack:** not asked, and it would take movement from the keys.
- **Shift+Tab for the previous target:** one key is what the user asked for. It can come later.
- **Keeping the facing to the ready point for basic attacks:** that's the delay the user asked to remove.
- **12 m across the ground** (the first build): the camera looks north from the south, so it shows less ground south of the player than north. A mob 12 m south could be locked off screen, which is the case the user wanted gone.
- **Aim-only steering** (the first build): the world hit the nearest body the shape reached, so a nearer mob took the hit. In the live pass, a Warrior's first 7 swings at a locked elite hit nearer slimes, and a Thief's Double Stab hit a slime 54° off the aim.
- **Dropping a lock that's out of the basic attack's reach** (offered on 2026-11-17): for melee, about 2.4 m, so Tab would only cycle the mobs right next to the player. The user chose the screen.

## 12. On screen, not 12 m

Revised on 2026-11-17. The user, on decision 3: "i think if its out of reach, we'll just remove the lock, to avoid the case when player sees a mob locked but cant find it anywhere". Offered the screen or the basic attack's reach as the limit, they chose the screen.

### The rule

- **The lock takes and keeps only mobs on screen.** Auto-lock and Tab pick among the living mobs that are on screen, nearest first across the ground, as before.
- **When the locked mob leaves the screen, the lock drops,** and the nearest mob on screen is locked at once.
- **Decision 3 stands for a mob on screen:** one beyond the action's reach is still aimed at, and the player steps in.

### On screen

The scene sets two flags on each `Mark` each frame. It projects the mob's middle, its feet plus half its height, through the camera into the canvas:
- **`inView`:** the point lies inside the canvas, inset by 8% of its width and height on every side. Picking (auto-lock and Tab) needs it, so a new lock is never at the very edge, or under the HUD's corners.
- **`inFrame`:** the point lies inside the canvas at all. Keeping a lock needs only this. The gap between the two is the hysteresis: a mob at the edge doesn't flicker in and out of the lock as the camera eases.

`lock.ts` reads the flags and keeps no camera, so its tests stay pure. The 8% inset is a constant in the scene, not game data, since it belongs to the view.

### The change

| Part | Before | After |
| --- | --- | --- |
| `server/data/actions.yaml`, `load/actions.go` | `lock_range: 12` and its check | Gone; the loader refuses the key as an unknown one |
| `systems/lock.ts` | `inRange`: living and within `lock_range` | Picks among living `inView` marks, and keeps a lock while it's living and `inFrame` |
| The scene | Builds marks from the tracked mobs | Also sets `inView` and `inFrame` by projecting each mob's middle |

### Testing

- **`lock.ts`:**
  - picks only `inView` mobs;
  - keeps a lock that's `inFrame` but not `inView`;
  - drops a lock that leaves the frame and picks the nearest `inView` mob;
  - Tab cycles only `inView` mobs.
- **The scene:** a mob's flags follow the projection, the 8% inset included, as the camera eases. A mob south of the player at the screen's bottom edge is `inFrame` and not `inView`.
- **Live:**
  - a locked mob that walks off screen loses the lock, and the nearest on screen takes it;
  - no lock ever points off screen, in any direction;
  - a mob at the edge doesn't flicker.

### Build

One PR:
1. `lock_range` out of the data, the loader, and the generator.
2. The flags in the scene, and `lock.ts` reading them, with the tests above.
