# Combat effects

> **Status: approved · 2026-11-13.** The high-level design for [C13. Combat effects](../product/features/combat-and-progression/c13-combat-effects.md) and [Systems → Combat → What players see](../product/systems/combat.md). It describes the client as it stands on `main` (after C11's beats in #61, #63 and #65) and the change it plans.

## 1. What has to hold

- **Drawing only.** No message, rule, or timing changes. The one server change is order: an action is told before the hits it causes (section 4). The client draws every effect from events it already receives: `Damage`, `SkillUsed`, `AttackUsed`, its own actions, and C12's `MobAttacked` and `MobView.pose`.
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
  play(scene, id, { x, y, facing, size?, tier?, beats?, targets?, follow?, reach?, shots?, color?, copy? }): Stop
  ```

  - `size` scales a hit's spark to its target. An attack or a skill reads its own reach (the box, the range, the area) and its colour from the data.
  - `tier` is a skill's evolution stage, from 0.
  - `beats`, when given, times it to the action: full strength through the strike, gone by the cancel point. That's how skill shapes already work.
  - `targets` gives the bodies the action reached, where they are now: the foes it hit, the players it healed.
  - `follow` gives the body a look stays on as it moves, where it is now: a buff's caster, a mob through its attack.
  - `reach`, `shots` and `color` are a mob attack's: mob types share attack names but not their reach or colour.
    - `reach` is how far the attack reaches past the mob: a bite's reach, a slam's or a hop's spread.
    - `shots` gives a spit's balls in flight, where they are now.
    - `color` is the mob's tint, for its spit.
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
  - C12's mob attack drawings register as `mob.<attack>`, so they can be replaced the same way. `MobView` keeps only the body's motion: the squash, lean, leap, lunge and recoil.

## 4. Hits

- **Hit-stop:**
  - **Trigger:** a `Damage` event whose source is the local player, and which isn't a MISS. A shot's waits for the shot to land (below), and holds only the action that fired it: once the player has started another, there's none.
  - **What holds:** the player's pose tween and the target mob's sprite animation, for 50 ms. The mob's position keeps following the render clock, so nothing jumps afterwards.
  - **Timing:** the pose then catches up, ending its beats where the action timeline says, so ready still lands on the cancel point. At most one hit-stop starts per 100 ms, so a skill that hits three foes holds once.
  - **Others' hits** get sparks but no hit-stop.
- **A drawn shot shows its hit where and when it visibly lands.**
  - The shots are the ranged basic attacks (the Bowman's arrow, the Magician's orb), Throw and Volley, Power Shot, Fire Arrow, and a ranged skill without its own look. Spark's bolt and Arc's lightning strike at once, so their hits show at once, as melee hits and mobs' do.
  - A shot's `Damage` waits until the shot drawn from that player's latest action reaches the target's middle, as Fire Arrow bursts: from its hit at the shots' 1.4 px/ms, no further than its range. Then its spark, its number and the player's own hit-stop come. One whose shot is already there shows at once.
  - The server's hit is unchanged: it lands as the action starts, as before. Only the drawing waits, up to about 330 ms for a full-range Power Shot. Before this, a ranged hit's number showed when its `Damage` arrived, before the arrow drawn to it, about 35 ms early at 113 px and 300 ms at full range.
  - A target that dies or leaves meanwhile gets its spark and number where it was last drawn.
  - A `Damage` is matched to its source's latest action, so one arriving after its shooter started another is timed by the newer one. That needs a round trip longer than the time to the next action, and changes only when the number shows.
  - So the server tells an action before its hits: `AttackUsed` and `SkillUsed` go out before the `Damage` they cause in the same tick. A basic attack's `Damage` used to come first, so an onlooker timed another player's arrow or orb by their previous shot and showed its number at once.
- **Impact sparks:**
  - On every `Damage` event, a spark plays at the target as it's drawn when the hit shows, sized to the target.
    - `hit.physical` or `hit.magic` for a hit;
    - `hit.crit` on top for a crit;
    - `hit.miss` for a MISS.
  - **The damage type** comes from the source's latest action, which the client already holds:
    - the player's own last attack or skill;
    - another player's last `AttackUsed` or `SkillUsed`;
    - a mob type's `damage`.

    With none of these known, it's physical.
- **Damage numbers** (`damage-number.ts`):
  - Each pops from 1.4× to 1× over 120 ms, easing evenly. Its first drawn frame shows the full 1.4×, so the pop shows whenever the hit lands within a frame.
  - A crit's number is 1.3× bigger.
  - Numbers stack by place, not by target. A number starting within 24 px across and 2 lines up or down of any number drawn less than 300 ms ago starts one line above the highest of them. A shot's number counts from when it shows. Each is measured from where the number was asked to start, before stacking, so a long chain keeps stacking, and a foe's number on a platform stays apart from one beneath it. So a chain of hits stacks upward instead of overlapping, whether it lands on one foe or on several piled together, as when Arc or Crushing Blow hits a pile of slimes.

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
- **Mob attacks,** one per attack name, drawn by its kind. Each follows its mob through the windup, which a pulsing ring marks, and ends with the attack:

  | Id | Kind | Look |
  | --- | --- | --- |
  | `mob.bite` | front | an arc through its front, as far as the bite reaches |
  | `mob.hop` | leap | a ring where its hit comes down, spreading to the hop's reach |
  | `mob.charge` | rush | dust trailing behind its hit as it runs, to the first player it meets |
  | `mob.spit` | shot | its balls in flight, in the mob's colour; the look lasts until they land |
  | `mob.slam` | around | a ring spreading around it to the slam's reach |

  A blow that breaks an attack off ends its look, and a map change ends them all.
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
  - none for a MISS or for others' hits;
  - a shot's only for the action that fired it, and one starting just before the cancel point ends on it.
- **Shots:**
  - the player's own arrow and another player's show their hit as the drawn shot arrives, the other's with its `AttackUsed` and `Damage` in one frame;
  - the world sends an attack before its `Damage`, for a shot and a swing;
  - a full-range Power Shot's hold ends by its cancel point;
  - a target gone by then gets its number where it was last drawn.
- **Sparks:** the type from the source's latest action, a crit on top, and a MISS puff.
- **Numbers:** the pop at 1.4× on the first frame, the crit size, and stacking within 24 px, 2 lines and 300 ms, on one target and across piled targets, but not between a platform and the ground beneath.
- **Looks:** each id draws, sized to its shape and timed to its beats, and each tier differs.
- **Mob attacks:** each follows its mob and reaches as far as the attack; a look plays once per attack, and a spit's lasts while its ball flies.
- **Cancel hint:** it shows on a cancel, and not on an action that follows another's end.

## 8. Build order

Client only. Each step is one commit, green on `make test`, `make lint` and `make cover`. Steps 1 to 4 are one PR (#80):

1. **Registry and manifest:** the ids, `play`, the preloader, the fallback, and the existing drawings moved in.
2. **Hits:** hit-stop, sparks, and damage numbers.
3. **Looks:** the class attacks, the skills, and their tiers.
4. **The cancel hint.**

Step 5 is its own PR, after #80:

5. **Mob attacks** under the registry.

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
