# Combat effects

> **Status: approved · 2026-11-13.** The high-level design for [C13. Combat effects](../product/features/combat-and-progression/c13-combat-effects.md) and [Systems → Combat → What players see](../product/systems/combat.md). It describes the client as it stands on `main` (after C11's beats in #61, #63 and #65) and the change it plans.

## 1. What has to hold

- **Drawing only.** No message, rule, or timing changes. The client draws every effect from events it already receives: `Damage`, `SkillUsed`, `AttackUsed`, its own actions, and C12's `MobAttacked` and `MobView.pose`.
- **Timing never shifts:**
  - Hit-stop holds a pose and then catches up, so ready still lands exactly on the cancel point, which is the game's only tell (C11).
  - The cancel hint shows only after a cancel.
- **Every effect is named and replaceable.**
  - A sprite animation, and a sound, can later be dropped in for any effect by adding files and a manifest line, with no code change.
  - Until then the drawn version plays, and no sound.
- **No screen shake and no combo counter,** as the user decided.

## 2. The change at a glance

| Part | Today | After |
| --- | --- | --- |
| Effects | Drawn in place by `skill-effect.ts` and `world.ts`: a box, a line, a ring, a slash arc, an arrow or an orb | An effect registry of named effects, each drawn or from an asset |
| Assets | Character, mob, tile, and background art only; no sound | `web/public/assets/effects/manifest.json`, listing art and sounds per effect; empty at first |
| Hits | A damage number | Hit-stop for the player's own hits, an impact spark, and a number that pops and stacks |
| Basic attacks | One slash arc for melee, an arrow or an orb | A look per class |
| Skills | One shape per kind, in the skill's colour | A look per skill, stronger at each evolution |
| Cancels | The new action replaces the old pose | Also a fading afterimage of the pose cut off, for the player's own cancels |

## 3. The effect registry

All of this lives in `web/src/game/effects/`.

- **An effect is a name and a drawn version:**

  ```ts
  type EffectId = `hit.${'physical' | 'magic' | 'crit' | 'miss'}`
    | `attack.${ClassId}` | `skill.${SkillId}` | 'cancel.afterimage' | `mob.${AttackId}`;
  ```

  - The drawn version is code: a function that builds Phaser objects and tweens for one play.
  - A rule test checks that every id has a drawn version.
- **Playing one:**

  ```ts
  play(scene, id, { x, y, facing, size?, tier?, beats?, targets?, copy? }): Stop
  ```

  - `size` scales a hit's spark to its target. An attack or a skill reads its own reach (the box, the range, the area) and its colour from the data.
  - `tier` is a skill's evolution stage, from 0.
  - `beats`, when given, times it to the action: full strength through the strike, gone by the cancel point. That's how skill shapes already work.
  - `targets` gives the bodies the action reached, where they are now: the foes it hit, the players a heal finds, a buff's caster.
  - `copy` is the sprite an afterimage copies.
  - `Stop` ends it early, when a cancel or a new action of the same caster replaces it, as `cutShort` does today.
- **The manifest** (`web/public/assets/effects/manifest.json`):

  ```json
  { "skill.spark": { "sheet": "effects/spark.png", "frameWidth": 128, "frameHeight": 128,
                     "frames": 8, "fps": 24, "anchor": [0.5, 0.5], "sound": "sfx/spark.ogg" } }
  ```

  - **Art:**
    - The preloader loads every sheet and sound the manifest lists.
    - `play` uses a listed sheet in place of the drawn version: one animation over the effect's duration, scaled by `ART_SCALE` like all the art, and flipped by `facing`.
    - A listed `sound` plays at the effect's start through Phaser's sound manager.
  - **Rules:**
    - A missing or broken file falls back to the drawn version and logs once. The game never stops for an asset.
    - A test refuses a manifest key that isn't a known id.
  - **It ships empty.** Adding art or a sound means adding the file and its line, with no code. A volume setting comes with the first sounds.
- **Existing drawings move into it.**
  - `skill-effect.ts`'s shapes and the melee slash become drawn versions of `skill.*` and `attack.*`.
  - The ranged shot becomes `attack.bowman` and `attack.magician`.
  - Once C12 PR 2 (#77) has merged, its mob attack drawings register as `mob.<attack>`, so they can be replaced the same way.

## 4. Hits

- **Hit-stop:**
  - **Trigger:** a `Damage` event whose source is the local player, and which isn't a MISS.
  - **What holds:** the player's pose tween and the target mob's sprite animation, for 50 ms. The mob's position keeps following the render clock, so nothing jumps afterwards.
  - **Timing:** the pose then catches up, ending its beats where the action timeline says, so ready still lands on the cancel point. At most one hit-stop starts per 100 ms, so a skill that hits three foes holds once.
  - **Others' hits** get sparks but no hit-stop.
- **Impact sparks:**
  - On every `Damage` event, a spark plays at the target as it's drawn now, sized to the target.
    - `hit.physical` or `hit.magic` for a hit;
    - `hit.crit` on top for a crit;
    - `hit.miss` for a MISS.
  - **The damage type** comes from the source's latest action, which the client already holds:
    - the player's own last attack or skill;
    - another player's last `AttackUsed` or `SkillUsed`;
    - a mob type's `damage`.

    With none of these known, it's physical.
- **Damage numbers** (`damage-number.ts`):
  - Each pops from 1.4× to 1× over 120 ms, easing back.
  - A crit's number is 1.3× bigger.
  - A number on a target that had one less than 300 ms ago starts one line above it, so a chain of hits stacks upward instead of overlapping.

## 5. Looks

All drawn now, each one replaceable through the manifest. They play from the action's start, sized to the real shape (the attack box, the range, the area), and run on its beats.

- **Basic attacks:**

  | Id | Look |
  | --- | --- |
  | `attack.beginner` | a short swing arc |
  | `attack.warrior` | a heavy, wide arc, thicker at its leading edge |
  | `attack.thief` | a quick, thin slash, short-lived |
  | `attack.bowman` | an arrow with a fading trail |
  | `attack.magician` | a glowing orb with a sparkle trail |

- **Skills,** with what each evolution adds:

  | Id | Look | Tier 1 | Tier 2 |
  | --- | --- | --- | --- |
  | `skill.strike` | a heavy overhead arc | wider and brighter | a crack along the ground |
  | `skill.throw` | a spinning stone in flight | a bigger stone, a dust trail | three stones (Volley) |
  | `skill.spark` | a short zig-zag bolt | a longer, thicker bolt | lightning that leaps between the foes hit (Arc) |
  | `skill.slash_blast` | a wide sweeping arc | | |
  | `skill.double_stab` | two quick flashes, one after the other | | |
  | `skill.power_shot` | a thick arrow with a long trail | | |
  | `skill.fire_arrow` | a burning arrow, bursting where it hits | | |
  | `skill.heal` | rising sparkles over each player healed (for another player's heal, over them alone: their party isn't known) | | |
  | `skill.iron_body`, `skill.haste`, `skill.focus` | a short aura in the buff's colour | | |

  A skill without evolutions draws tier 0 at every stage. The caster's flash at the strike stays (C11).
- **Hits within a skill:**
  - Arc's leaping lightning, Volley's three stones and Fire Arrow's burst go to the foes actually hit: the targets of the `Damage` events that follow the `SkillUsed`, from that caster, up to its cancel point. The player's own `Damage` comes a round trip after their beats start, so the strike alone is too short.
  - Before those arrive, the effect plays toward the facing.

## 6. The cancel hint

- **Trigger:** `ActionClock.begin` starting an action of the other kind while the current one is running, before its end. That's a cancel, and only the local player's clock knows it.
- **What shows:**
  - `cancel.afterimage` copies the local sprite's current frame and pose at its position.
  - It tints the copy white and fades it from 0.5 to 0 over 200 ms, drifting a few pixels back.
  - The new action's strike plays over it.
- **Never before:** nothing is drawn ahead of a cancel, so the hint confirms one without showing when to press.
- **Only the player sees their own,** since other players' cancels can't be told from their actions arriving.

## 7. Testing

- **Registry:**
  - every id has a drawn version;
  - the manifest refuses unknown ids;
  - a listed sheet is used and a missing one falls back;
  - a listed sound plays and an unlisted one doesn't;
  - `Stop` ends an effect.
- **Hit-stop:**
  - it holds 50 ms;
  - ready still lands on the cancel point;
  - only one hit-stop per 100 ms;
  - none for a MISS or for others' hits.
- **Sparks:** the type from the source's latest action, a crit on top, and a MISS puff.
- **Numbers:** the pop, the crit size, and stacking within 300 ms.
- **Looks:** each id draws, sized to its shape and timed to its beats, and each tier differs.
- **Cancel hint:** it shows on a cancel, and not on an action that follows another's end.

## 8. Build order

One PR, client only. Each step is one commit, green on `make test`, `make lint` and `make cover`:

1. **Registry and manifest:** the ids, `play`, the preloader, the fallback, and the existing drawings moved in.
2. **Hits:** hit-stop, sparks, and damage numbers.
3. **Looks:** the class attacks, the skills, and their tiers.
4. **The cancel hint.**
5. **Mob attacks** under the registry, once C12 PR 2 has merged.

## 9. Decisions

Agreed on 2026-11-13:

1. **No screen shake.**
2. **No combo counter.**
3. **A cancel hint, after the cancel only:** a fading afterimage of the pose cut off.
4. **Every effect named and replaceable** through a manifest, sounds included, so art and sound can be added later without code.
5. **The game's main experience** is grinding and the combat system, in which players chain attacks and skills with cancels for the most damage. Effects serve that.

## 10. Alternatives considered

- **Screen shake on skills and crits:** the user declined it.
- **A combo counter:** the user declined it.
- **Effects written straight into the entity code, as today:** art or sound would need a code change for each.
- **The server sending effect events:** every effect follows from events the client already has. The client computes and the server validates, so the server carries nothing more.
- **Hit-stop that pauses the action clock:** it would push the cancel point later on the player's screen than on the server's, and cancels would miss.
