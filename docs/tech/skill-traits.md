# Skill traits

> **Status: approved · 2026-11-17.** The high-level design for [C20. Skill traits](../product/features/combat-and-progression/c20-skill-traits.md). It builds on [A fuller skill roster](./skill-roster.md), whose fields the traits change, and on [Skill chains](./skill-chains.md) and [Abnormal statuses](./statuses.md). It also redesigns the skill window from [Classes, promotion, and skills](./classes-and-skills.md) section 9.

## 1. What has to hold

- **The user's words** (2026-11-17): "different traits add different effects to the skill - a skill can have maximum 2 traits, the default one and an extra one - only one trait can be chosen at a time - refactor the skill tree UI, the details only show when hovering on the skill".
- **Their choices:**
  - extra traits will come from items or quests, but for now every trait is open, once its skill is fully evolved (below);
  - a trait changes freely, any time out of combat;
  - evolutions stay, alongside traits;
  - every active skill gets an extra trait.
- **The extra opens at the last evolution** (the user, 2026-11-18): "traits only possible after the skill is in last evolved/doesnt have evolution". A skill with no evolution opens it with its first point.
- **Traits are data.** Skills already resolve stage by stage, each stage changing only the fields it names (classes-and-skills.md section 3). An extra trait is one more layer of the same changes, on top of the last stage. No field is new.
- **A trait trades, it doesn't add.** An extra trait that adds control or reach pays for it in cooldown, targets or time, so the default stays worth choosing.
- **The client computes, the server validates.** The client draws the window, predicts a use with its trait, and judges whether it's in combat. The server keeps the trait, refuses a change in combat, and resolves every use with the trait it was pressed with.

## 2. Data

`skills.yaml`, on each active skill that takes points:

```yaml
- id: war_cry
  # ...the skill's fields and stages, as today...
  traits:
    - {id: rally, name: Rally, description: "For the party within 8 m, for 30 s."}
    - id: fury
      name: Fury
      description: "For the Warrior alone: twice the attack, for 20 s."
      reach_party: false
      bonus: {physical_attack: {base: 16, per_point: 2}, magic_attack: {base: 16, per_point: 2}}
      duration: 20s
```

- **The first trait is the default:** an id, a name and a description, changing nothing. It is the skill as its fields and stages write it.
- **The second is the extra:** an id, a name, a description, and the fields it changes, any a stage may name.
- **It opens at the last stage.** Its points reach the last stage's `at`: 25 for Strike, Throw and Spark, 10 for the other skills that evolve, and the first point for a skill with one stage.
- **Resolving,** in the loader and in `gen-gamedata.ts` alike: the last stage as resolved today, then the extra's fields. That's one resolved stage per extra. It's checked as a stage is. Its name and description stay the last stage's; the trait has its own.
- **Merging is a stage's:**
  - a new status or move kind starts afresh;
  - a `shape` is replaced whole;
  - `power` and a bonus's stats may name `base` or `per_point` alone;
  - a bonus stat set to 0 drops it, as Blur drops Haste's speed;
  - `reach_party: false` turns a party buff into the caster's alone.

  A trait can't remove a field. None of the first traits needs to.
- **Validation** refuses:
  - more than two traits;
  - traits on a passive or a granted skill;
  - a default that changes anything;
  - two traits with one id, or a trait without a name or a description;
  - `stages` on a trait;
  - an extra whose resolved stage that fails the stage checks, such as a party buff with move speed, or a status skill with targets.
- **A real-data test** checks every active skill that takes points has two traits.
- **The type:** `gamedata.Skill` gains `Traits []Trait`, each with `ID`, `Name` and `Description`; the extra also has `Stage`, its resolved stage. `Skill.TraitOpen(points)` says whether the points reach the last stage. `Skill.Spec(points, trait)` gives the extra's stage when the trait is the extra and it's open, and otherwise the stage the points reach. An unknown or empty trait id is the default.
- **The generator** writes each skill's traits, the extra with its resolved stage, beside today's stages.
- **`actions.yaml`** gains `combat: 5s`, how long a player stays in combat after their last fight event (section 4).

## 3. The first traits

Twenty-nine extras, one for each active skill that takes points, each on top of its skill's last stage. "Cooldown" is the extra's; the default keeps the skill's.

| Skill | Default | Extra | What the extra changes |
| --- | --- | --- | --- |
| Strike | Swift | Concussion | `status: {id: stun, time: 1s}`, cooldown 7s. Crushing Blow's three targets are all stunned |
| Throw | Stone | Bola | `status: {id: slow, time: 3s}`, cooldown 6s |
| Spark | Crackle | Static | `status: {id: weaken, time: 4s}`, cooldown 7s |
| Lunge | Quick Lunge | Slip In | `dodge: 300ms`, covering the 200 ms dash and the swing; cooldown 8s |
| Roll Back | Retreat | Dive | `move: {toward: heading}` |
| Slash Blast | Sweep | Whirlwind | `shape: {kind: area, radius: 3}`, targets 5, cooldown 9s. Rending Blast's armour break stays |
| Iron Body | Steadfast | Iron Wall | `reach_party: true`, `shape: {kind: area, radius: 6}`, duration 15s |
| Charge | Bull Rush | Trample | targets 3, `status: {id: knockdown}` |
| Ground Slam | Shockwave | Fissure | `shape: {kind: shot, range: 7, width: 2}` |
| War Cry | Rally | Fury | as above: the Warrior alone, double the bonus, 20 s against Battle Roar's 40 |
| Taunt | Roar | Call Out | `shape: {kind: shot, range: 12, width: 1}`, `status: {time: 9500ms}` |
| Double Stab | Twin Stab | Puncture | `status: {id: armor_break, time: 5s}`, cooldown 5s. Replaces Bleeding Stab's bleed |
| Haste | Fleet | Blur | `bonus: {move_speed: {base: 0, per_point: 0}, evasion: {base: 30, per_point: 3}}` |
| Shadow Step | Ambush | Sap | `status: {id: sleep, time: 4s}`, cooldown 14s. The strike's damage lands first, so the sleep holds |
| Smoke Bomb | Smoke | Flash Powder | `status: {id: blind, time: 5s}` |
| Flurry | Five Stabs | Fan of Knives | `shape: {kind: area, radius: 3}`, targets 4, `hits: {count: 3}`, cooldown 10s |
| Poison Blade | Venom | Numbing Edge | `on_hit: {status: weaken, time: 3s}` |
| Power Shot | Heavy Shot | Piercing Shot | targets 3, cooldown 6s. Blinding Shot's blind reaches all three |
| Focus | Precision | Expose | `bonus: {crit_rate: {base: 0, per_point: 0}}`, `on_hit: {status: armor_break, time: 4s}` |
| Disengage | Kick Off | Net | `status: {id: pin, time: 2s}`, cooldown 16s. The push stays, so the mob is pinned where it lands |
| Pinning Shot | Pin | Barbed Shot | `status: {id: bleed, time: 5s, power: {base: 0.2}}` |
| Arrow Rain | Downpour | Thorn Rain | `status: {id: slow, time: 2s}`, cooldown 15s |
| Eagle Eye | Lone Hunter | Spotter | `reach_party: true`, `shape: {kind: area, radius: 8}`, duration 25s |
| Fire Arrow | Flame | Ice Arrow | `status: {id: slow, time: 4s}`, cooldown 6s. Replaces Searing Arrow's burn |
| Heal | Mend | Greater Heal | targets 1, `power: {base: 2.55, per_point: 0.085}`, cooldown 6s |
| Blink | Stride | Recede | `move: {toward: back}`, `dodge: 500ms` |
| Frost Nova | Nova | Ice Prison | `shape: {kind: area, radius: 3, at: lock, reach: 10}` |
| Barrier | Bulwark | Sanctuary | `reach_party: true`, `shape: {kind: area, radius: 6}`, `shield: {power: {base: 1.6, per_point: 0.06}}`, about 60% of Aegis's |
| Meteor | Impact | Firestorm | `status: {id: burn, time: 3s, power: {base: 0.3}}` |

- **Descriptions** are C20's table, written out: a default says what it keeps, such as "Ready again in 4.5 s"; an extra says what changes and what it costs.
- **Chains** (skill-chains.md section 6) stay as they are. Some extras feed them and some leave them:
  - Concussion stuns, so a Warrior's Strike key offers Ground Slam after it.
  - Net pins, so Power Shot's key offers Arrow Rain.
  - Ice Prison freezes, so Frost Nova's key still offers Meteor.
  - Trample, Firestorm and Ice Arrow give up the stun, knockdown or burn their chains or their evolutions brought. That's their price.
- **Super armour** (statuses.md) shapes the choice: Firestorm's burn and Barbed Shot's bleed take on elites and bosses, at half the time, where Impact's knockdown and Pin's pin don't.

## 4. Choosing a trait

- **`ChooseTrait{skill, trait}`** goes through the gateway to the world, with a Response and the skill points' rate limit. On the tick, it is refused, in this order:
  1. `CHARACTER_DEAD` while dead;
  2. `SKILL_NOT_LEARNED` for a skill with no points, one the data doesn't have, or one the class can't learn;
  3. `TRAIT_UNKNOWN` for a trait the skill doesn't have, a passive's or a granted skill's included;
  4. `TRAIT_LOCKED` for the extra while the skill's points are short of its last stage;
  5. `IN_COMBAT` while the player is in combat.
- **Otherwise** the world sets the trait, sends a `StatsChanged` with the sheet, and saves it at once, as spent skill points are. Choosing the trait already chosen answers OK and changes nothing.
- **In combat:**
  - The world keeps, per player, the tick of their last fight event: each `Damage` naming them as source or target. That's a hit, a miss, a dodge, or a damage-over-time tick, dealt or taken.
  - Every `Damage` already goes out through the tick's events, so the world stamps both sides there, in one place.
  - The player is in combat until `combat` (5 s) after it.
  - A town has no fights, so its players are never in combat. A new map or a rejoin starts out of combat, as no fight crosses a map.
- **Running cooldowns and buffs keep going as they were.** A cooldown counts on to its end, and the new trait's cooldown applies from the next use. A buff keeps the trait it was used with (section 5).
- **Stored with the points:**
  - `character_skills` gains `trait text not null default ''`, `''` being the default. A skill needs a point to have a row, so it needs one to choose.
  - `Load` and `SaveCheckpoint` carry it. The checkpoint refuses with `REASON_BAD_SKILL` a trait the skill doesn't have.
  - The dev database is wiped as usual.
- **The sheet:** `SkillPoints` gains `traits`, each skill's chosen trait by id, a default left out. It rides in `Welcome`, `LevelUp` and `StatsChanged` with the points.

## 5. Using a skill with its trait

- **The world resolves a use** with `Skill.Spec(points, trait)` where it used the stage before: shape, targets, power, status, move, dodge, cooldown, energy, buff and all.
- **A use waiting for its turn** keeps the trait it arrived with, as it keeps its stage today. A change in between can't alter a use the client already drew.
- **A buff keeps its trait:**
  - The buff carries its trait beside its points, on the caster and on each party member it reaches.
  - `on_hit` reads the buff's own trait, as it reads its points.
  - It crosses maps and checkpoints with it: `character_skill_timers` gains `trait text not null default ''`, and the handoff carries it.
- **`SkillUsed`** gains `trait`, the id, empty for the default, so others draw the shape the trait gives, such as Whirlwind's circle or Fissure's line.
- **Chains** check a follow-up's condition as today. Its trait changes what it does, not when the key offers it.
- **Bots** use the default traits.

## 6. Protocol

| Message | Direction | Change |
| --- | --- | --- |
| `ChooseTrait` | client | New: `skill`, `trait`; Response |
| `SkillPoints` (in the sheet) | server | Gains `map<string, string> traits` |
| `SkillUsed` | server | Gains `trait` |
| `Welcome` | server | `skill_timers` gain `trait` |

- **New error codes** in area 33:
  - `TRAIT_UNKNOWN` (330036), a 400: "That skill has no such trait."
  - `IN_COMBAT` (330037), a 409: "You can't change traits in combat."
  - `TRAIT_LOCKED` (330038), a 409: "That trait opens once the skill reaches its last evolution."
- **Internal contracts:**
  - `world.v1`: `ChooseTrait`, and the trait in `SkillUsed` and in the skill timers of `JoinResponse` and the handoff;
  - `character.v1`: each skill's trait in `Load` and the checkpoint, and each timer's.
- **Gateway:** `ChooseTrait` routed with a Response, under the skill points' limit.

## 7. Client

### The skill window

```
┌ Skills ─────────────────────────────────────────── × ┐
│ Skill points: 7                        [Undo] [Apply] │
│ BEGINNER                                              │
│ ┌────┐ ┌────┐ ┌────┐ ┌────┐ ┌────┐ ┌────┐ ┌────┐      │
│ │icon│ │icon│ │icon│ │icon│ │icon│ │icon│ │icon│      │
│ │ 12+│ │  3+│ │  0+│ │  5+│ │    │ │Lv 3│ │  1+│      │
│ └────┘ └────┘ └────┘ └────┘ └────┘ └────┘ └────┘      │
│ Heavy… Throw  Spark  Second Break… Lunge  Roll…       │
│  ◆ ◇    ◇ ◆    ◆ ◇                  ◆ ◇    ◆ ◇        │
│ WARRIOR                                               │
│ ...                                                   │
└───────────────────────────────────────────────────────┘
```

- **`features/skills/SkillWindow.tsx`** becomes a grid of tiles, one section per class (Beginner first), each skill in the order its `level` opens it, then file order.
- **A tile:**
  - the skill's icon at its stage (C16), greyed with no points;
  - its points in a corner, any staged as "+n" in green;
  - **+** while points are left and the skill isn't locked;
  - "Lv N" over the icon while locked;
  - its stage name under it, cut short to fit;
  - a chain mark in a corner on a follow-up that can't stand on a key. Such a tile isn't draggable.
  - An active tile that can stand on a key drags onto the bar as today.
- **The trait buttons:** two small diamonds under an active tile with traits, the chosen one filled in the skill's colour.
  - Hovering one shows its trait card. Clicking the other sends `ChooseTrait` at once, with no staging; the diamond pulses until the answer.
  - Until the skill reaches its last evolution, the extra's diamond is an outline with a small lock, and doesn't send. Staged points don't open it; applied ones do.
  - A refusal leaves the chosen one as it was and shows the world's words in the window's notice, as spending does.
  - While the player is in combat they're dimmed and don't send. The header says "In combat: traits change once out of it."
- **The skill card,** beside the pointer as an item's card is (`lib/placement`'s `besidePointer`), on hover or keyboard focus, hidden as a drag starts:
  - the icon, the stage name and any evolution the staged points reach;
  - the class, active or passive, and the points;
  - the stage's description;
  - what it does now, and with staged points ("→");
  - its cost, cooldown and length;
  - what each point adds, and its next evolution;
  - its chains, as the window lists them today: on a root, the follow-ups its key turns into; on a follow-up, on whose keys it comes up;
  - both traits, each a line, the chosen one marked;
  - "Lv N" for a locked skill.
- **The trait card:** its name, "Default" or "Extra", its description, and what the skill does with it, from that trait's resolved stage, so the numbers compare. The extra's numbers are at the last stage, with the skill's points or that stage's total, whichever is more. Then "Chosen", "Click to choose", the in-combat line, or, while locked, "Opens at 25 points, as Crushing Blow".
- **Everything the window shows today** moves into the card; nothing is dropped.

### The chains panel

The user's words (2026-11-18): "skill tree in K should show the link of how the chain work". They chose a panel under the grid over lines drawn on hover or a grid laid out as a tree.

```
│ CHAINS: what each key turns into                    ▾ │
│ [Strike] ──── stunned or frozen ──▶ [Ground Slam]     │
│          └─── over 3 m ───────────▶ [Charge]          │
│ [Roll Back] ─ while held ─────────▶ [Unyielding]      │
│          └─── after a dodge ──────▶ [Lunge]           │
│ [War Cry] ─── right after ────────▶ [Iron Body]       │
```

- **Where:** under the class sections, in the same scroll. Its header, "Chains: what each key turns into", folds it away and back. The choice is kept per browser, in `localStorage`, guarded as the other window choices are; it starts open.
- **What:** the character's class's chains (`chainsOf`), in the class's order. Skills in no chain aren't listed.
- **A chain:**
  - the root, a small icon (`SkillIcon`'s `grid` size or smaller) with its stage name;
  - then each follow-up in its listed order, one line each: a connector (`─` for the first, `└` after), an arrow with its condition's short words, and the follow-up's icon and stage name.
  - The order reads as the rule: the first follow-up whose condition holds is the one the key offers.
- **The short words:**

  | Condition | On the arrow |
  | --- | --- |
  | `after` | right after |
  | `dodged` | after a dodge |
  | `controlled` | the statuses as words, "stunned or frozen"; four or more read "controlled" |
  | `controlled_self` | while held |
  | `far` | over N m |

  Hovering the words shows the condition in full, as `conditionText` writes it.
- **An icon in the panel** is the skill's:
  - greyed with no points, since a follow-up comes up only with one;
  - hovering or focusing it shows the skill's card;
  - a root drags onto the bar as its tile does, and a follow-up doesn't.
- **A skill in two chains** appears in each, as Meteor, Shadow Step and Lunge can. A skill that is a root and a follow-up, as Fire Arrow and Power Shot are, appears as both. The panel is a map of the keys, not a place to spend points, so repeats don't confuse the points.
- **The cards keep their chain lines,** for a player reading one skill.

### Elsewhere

- **`progression/skills.ts`** reads each skill's traits and gives `specAt(skill, points, trait)`. Everything that read a stage reads it: the skill keys' energy and cooldown, the move's prediction (Dive and Recede go their own way), the shape drawn at a press, the effect text and the chains' "has a point, off cooldown" check.
- **The in-combat clock:** a small store, `state/combat.ts`, notes when a `Damage` names the player, either way. Game systems feed it; the window reads it. The client hears a fight event after the world's tick, so its five seconds end after the world's, and it never offers a change the world would refuse.
- **The sheet's `traits`** come into the character store with the points.
- **The skill bar:** a key whose skill uses its extra trait shows a small diamond in the skill's colour in a corner. Its tooltip names the trait.
- **Others' skills** draw with the `SkillUsed` trait's resolved stage.

## 8. Balance

- **The harness** (animation-cancel.md section 8) runs each class twice, with the defaults and with every extra, and reports both. An extra that trades damage for control or reach should deal no more than its default over a minute on one target.
- **Numbers** are first guesses, in data, for the live pass to tune.

## 9. Testing

- **Data:**
  - each refusal in section 2;
  - resolution: the extra on top of the last stage, such as Concussion stunning Crushing Blow's three targets;
  - `TraitOpen` and `Spec` at one point short of the last stage and at it, and from the first point for a skill with one stage;
  - Puncture replacing Bleeding Stab's bleed;
  - Blur dropping Haste's speed;
  - Iron Wall turning Iron Body into a party buff;
  - the generator's traits equal to the loader's, as its stages are.
- **World:**
  - `ChooseTrait`'s refusals, in order;
  - in combat after each kind of fight event, both ways, and out again 5 s later;
  - never in a town;
  - a use resolved with its trait: Concussion stuns, Whirlwind hits behind, Dive rolls along the heading;
  - a waiting use keeping its trait;
  - a buff keeping its trait across a change, a map change and a checkpoint;
  - `SkillUsed` carrying the trait.
- **Character:** the trait's round trip in `Load` and the checkpoint, with `REASON_BAD_SKILL` for an unknown one, and a timer's trait.
- **Client:**
  - the grid's order and sections;
  - the card on hover and focus, hidden on a drag;
  - the trait diamonds: choosing, pending, a refusal, and dimmed in combat;
  - the in-combat clock;
  - the key's mark and tooltip;
  - a press predicting with its trait: energy, cooldown, move and shape.
- **End to end:**
  - a Beginner with 24 points in Strike is refused Concussion with `TRAIT_LOCKED`; with 25 they choose it, and Crushing Blow stuns a slime;
  - a change during a fight is refused with `IN_COMBAT`, and goes through once the fight is 5 s past, judged by the world's ticks, not the wall clock.
- **Live:** every class tries both traits of each skill, and the window's card reads true against what the skill does.

## 10. Build

Three PRs, and the chains panel on its own:
1. **The window,** client only, with today's data: the grid, the tiles, the card, the chain marks. It replaces the list as it stands, with no traits yet. Merged as #250.
- **The chains panel** (section 7), client only, any time after PR 1. It doesn't wait for traits.
2. **Traits, server side:**
   - the data, validation and generator, with the twenty-nine traits;
   - the character module's columns;
   - the world's `ChooseTrait`, in-combat clock, uses, buffs and `SkillUsed`;
   - the protocol and gateway.
3. **Traits, client side:**
   - the sheet's traits;
   - the diamonds and the trait card;
   - the in-combat store;
   - uses and drawing with the trait;
   - the key's mark.

### As built: PR 1, the window

- **Files:** `features/skills/SkillWindow.tsx` draws the grid and `SkillCard.tsx` the card. `features/shared/BesidePointer.tsx` places it, and the item cards too, measured before paint.
- **Sections:** the Beginner first, then each class it led to; within one, by the level a skill opens at, then the class's order.
- **A tile** (seven to a row):
  - the 40 px icon (`SkillIcon`'s `grid` size), with the skill bar's cooldown sweep and count;
  - its points under it, any staged as "+n" in green, then **+**;
  - "Lv N" over a locked icon;
  - a chain mark, two links, in the top-left corner of a follow-up that can't stand on a key; that tile doesn't drag, and the bar's red flash stays for a key saved before the chains;
  - its name under it, on up to two lines broken only between words, so names that share a first word, such as Arcane Mind and Arcane Release, still differ. A word too long for the tile, such as Marksmanship, ends in "…" rather than splitting; the card and the hover give it whole. Browsers don't hyphenate it reliably, headless Chrome not at all.
- **The card** (the item cards' look, 280 px):
  - follows the pointer on hover, and sits beside a tile that takes keyboard focus;
  - hides when the pointer leaves, focus goes, or a drag starts;
  - is a `tooltip` the tile's `aria-describedby` names;
  - holds this section's list without the traits, and "Drag onto the skill bar to use it with keys 1 to 6." for a skill that can stand on a key.
- **No room for the diamonds yet:** PR 3 adds them under the active tiles.

### As built: the chains panel

- **Files:** `features/skills/ChainsPanel.tsx`. `cardTarget.ts` gives a tile and a panel icon the same hover, focus and drag. `shortCondition` sits beside `conditionText` in `progression/chains.ts`.
- **The fold** is kept under `finga.skills.chains` through `lib/storage`, the guard the controls preset's cache now shares: a blocked or missing storage reads as open and drops the write.
- **A chain:**
  - the root's 32 px icon and stage name in a column of their own;
  - then a line per follow-up: `─` or `└`, the short words on a wire with the full `conditionText` as its title, `▶`, and the follow-up's icon and name, in a column that lines up.
  - Each skill's box is 128 px, which fits the longest stage name, Arcane Release. The window is 400 px wide so that two boxes and the longest words, "stunned or frozen", share a line. The grid's columns are 6 px apart to fill it.
- **The card** shows from a panel icon as from a tile. The window notes which element shows it, so only the icon under the pointer names the card, not the same skill's tile.
- **A root drags** if it's active, as its tile does. A follow-up never drags, even one that roots a chain elsewhere, such as Fire Arrow under Blink.

## 11. Later

- **Unlocking by item or quest:** an extra trait gains an `unlock` in data, an item or a quest, on top of the last evolution. The character keeps the traits they've unlocked, and the world refuses a locked one with the same `TRAIT_LOCKED`. The locked diamond's card says what opens it.
- **Skill resets:** there are none. If one comes, a skill that falls short of its last evolution goes back to its default.
- **Traits that change chains:** a trait naming follow-ups of its own, read where the class's chains are.
- **An icon per trait,** if the corner diamond turns out too quiet.
- **The skill card on the bar's keys,** in place of their tooltip.

## 12. Decisions

The user's choices are in section 1. These calls were made in planning and logged for the user to push back on:

1. **The default trait is the skill as it is,** named in data, so the window can offer two named choices.
2. **The extra sits on top of the last stage only,** since it opens there (the user's call of 2026-11-18). Traits need no changes of their own at evolution totals, and each extra resolves to one stage.
3. **In combat lasts 5 s** after any hit, miss, dodge or damage-over-time tick, dealt or taken, and never in towns. It's the simplest rule both sides count the same way.
4. **A change is instant and free,** with no Apply: it costs nothing and can be undone.
5. **The extra opens with applied points, not staged ones.** A skill with one stage opens it with its first point. Its trait lives on its points' row.
6. **Running cooldowns and buffs are untouched** by a change, so changing never resets a cooldown.
7. **No icon per trait for now:** a corner diamond marks the extra on the bar and the window, which saves twenty-nine new icons.
8. **The first traits use only existing fields:** no new mechanics, and none that removes a field.
9. **The window is a grid, with details in a card on hover,** as the user asked, in the same card style as items. Chains are in the card, and in the chains panel the user asked for on 2026-11-18.
10. **Bots keep the defaults,** and the harness reports extras beside them.

## 13. Alternatives considered

- **Traits replacing evolutions:** the user kept evolutions.
- **Traits changed only at a trainer, or anywhere:** the user chose any time out of combat.
- **Staging trait changes with Apply:** an extra step for a free change.
- **In combat as long as any mob chases the player:** the client doesn't know mobs' targets, and the two sides would disagree.
- **A trait per evolution, as tiers to pick from:** more choices than the user's two per skill.
- **The grid laid out as a tree,** a row per key with arrows to its follow-ups: offered to the user on 2026-11-18, who chose the panel. A skill in two chains, such as Meteor, would show twice where its points are spent, and chains cross classes.
- **Lines drawn in the grid on hover:** no space taken, but the links stay hidden until a hover. The user chose the panel.
