# Target lock

> **Status: approved · 2026-11-16.** The high-level design for [C15. Target lock](../product/features/combat-and-progression/c15-target-lock.md) and Systems → Combat → Aiming. It replaces the auto-aim of [3D world](./3d-world.md) section 6. All of it is the client's: the world takes each action's `aim` as sent, as today.

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
- **Keyboard only.** One new key, Tab, in both presets.

## 2. The change at a glance

| Part | Today | After |
| --- | --- | --- |
| `server/data/actions.yaml` | `aim_cone: 70` | `lock_range: 12`, in metres. `aim_cone` goes, and the loader refuses it as an unknown key |
| `web/src/game/systems/aim.ts` | `aimAt`: the nearest mob within the action's reach and the cone, picked as each action starts | `lock.ts`: a lock kept between actions (section 3). `reachOf` and `aimYaw` stay |
| Aim | At the pick, else the heading | At the lock, else the heading |
| The turn | The body faces the aim until the action's ready point | An attack or a skill only to the end of its strike beat (section 4) |
| `world3d/outline.ts` | Outlines the mob a basic attack would pick | Outlines the locked mob |
| `world3d/lock-ring.ts` | — | New: a gold ring on the ground under the locked mob |
| `input/presets.ts`, `input/actions.ts` | — | `nextTarget`: Tab in both presets |
| `state/target.ts`, `features/hud/TargetFrame.tsx` | — | New: the target window (section 5) |

## 3. The lock

`systems/lock.ts` is pure logic over the mobs as drawn (`Mark`: id, x, z, radius, hp) and the player's feet. Its tests run without a scene.

- **Distance** is across the ground, as the auto-aim's was. Height, walls, and ledges don't count. That is how mobs' sight treats players.
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
- **A lock out of reach is still aimed at,** so the swing or the shot goes toward it and misses. This follows "go to that locked target". Falling back to a nearer mob would split attacks from the marker. `reachOf` stays for the beats and the looks, but no longer filters the target.
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
  - It turns slowly, a full turn in 4 s, so it reads on any map's ground, snow and sand included.
  - It's drawn after the telegraphs, so a mob's red attack area never hides it.
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
- **Placement:** under the field boss's bar when that shows, in the same style.
- **The field boss:** when the lock is the field boss, the window hides, since the boss bar already shows its name and HP.
- **No lock:** the window hides.

## 6. Input

- **`nextTarget`** is a new action in `input/actions.ts`, bound to Tab in both presets. Tab is free in both today.
- **The key handler** calls `preventDefault` on Tab while the game has focus, so the browser doesn't move focus between page elements.
- **While typing in chat,** Tab is ignored, as the other game keys are (`input/typing.ts`).
- **The controls table** (Screens and controls) gains the row.

## 7. Data

- **`actions.yaml`:** `lock_range: 12   # metres across the ground within which the client locks a mob and Tab switches; the server doesn't check the lock`.
- **Validation** refuses `lock_range` at or below 0. The generator carries it to `web/src/gamedata/actions.ts`.
- **Range:** 12 m is a little over a mob's sight of 9.4 m. The live test checks that a mob locked at 12 m is on screen in every direction. If one isn't, the range comes down to what the camera shows.

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
- **End to end:** with two mobs either side of the player, Tab switches the lock, and the next attack's `Damage` lands on the new lock.
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
2. **The lock:** `lock.ts` and its tests; aim from the lock; `aimAt` gone.
3. **The turn:** the basic attack's facing released at its strike's end, for the player's own body and for others'.
4. **Showing and input:** the outline on the lock, the ring, Tab, and the controls row.
5. **The target window:** `state/target.ts`, `TargetFrame.tsx`, and their tests.

**Docs the build keeps true:** this page, and [3D world](./3d-world.md) section 6, whose auto-aim this replaces.

## 10. Decisions

The user's points are in section 1. The calls below were made in planning and logged as decisions for the user to push back on, under the rule the user set on 2026-11-14:

1. **The auto-lock is sticky:** the nearest mob is locked when nothing is, and kept until it dies, leaves range, or the player switches. Re-picking whenever a mob came closer would jump the lock about in a pack, sending attacks to mobs the player didn't choose.
2. **Any direction, 12 m across the ground, with no line of sight**, as mobs' sight works.
3. **A lock out of reach is still aimed at, and misses.**
4. **Tab switches to the next farther mob, wrapping round,** including mobs behind.
5. **Basic attacks and skills face the lock for their strike only.** At first skills kept facing it for their second, since the user had asked only about the basic attack. The user then said "skills should free my face".
6. **Arcs and areas still hit every mob they cover.**
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
