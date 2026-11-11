# Classes, promotion, and skills

> **Status: approved · 2026-11-10.** The high-level design for [C8. Class promotion](../product/features/combat-and-progression/c8-class-promotion.md), [C5. Skills](../product/features/combat-and-progression/c5-skills.md), the new [A2](../product/features/account-and-character/a2-create-a-character.md), and [Systems → Classes and skills](../product/systems/classes-and-skills.md). It builds on `main`, after the [stat system](./stat-system.md) (PR #5 and #6), plus [NPCs and task quests](./npcs-and-quests.md), which comes first.

## 1. What has to hold

From the product pages:

- **Classes:**
  - Everyone starts as a Beginner; character creation asks for a name only.
  - Classes form a tree (Beginner → Warrior, Thief, Bowman, Magician), and more branches come later.
  - A class decides the basic attack, the base values, and which skills can be learned.
- **Promotion:**
  - It happens at the class's trainer NPC, when every condition is met: level, own stats, skill points in a named skill, and finished quests.
  - It is permanent. A player can stay a Beginner forever.
  - Beginner skills and every point carry over.
- **Skills:**
  - 5 skill points per level, spent like stat points (staged, then applied), into the skills of the character's class or any class below it in the tree.
  - Each point strengthens a skill, and set totals evolve it into a new form. **The totals and forms change often, so they must be data only.**
  - Beginner and class skills have no cap, and there are no resets.
  - Active skills go on a 1–6 skill bar and cost MP, with cooldowns. Passive skills are always on.
  - Damaging skills follow the combat rules and can't be used in towns.
- **Beginner:** the same starting stats and gear for everyone. Beginner weapons fit every class.

From the code, the rules this design keeps:

- **The server decides.** Skills are resolved in the tick, like attacks. The client only gates keys and draws.
- **No closed lists in code for content.** Today `kernel.Class` names four classes in Go. Classes, like items and mobs, become IDs checked against game data, so a fifth class is data, not code.
- **Points are stored, everything else is derived.** As in the stat system, a skill's form is computed from its points and the data, never stored. Changing an evolution total in the data re-forms every character's skills at their next join, and nothing needs converting.

## 2. The change at a glance

| Part | Today | After |
| --- | --- | --- |
| `data/classes.yaml` | Warrior and Bowman, flat, each with the same starting stats; chosen at creation | A tree rooted at `beginner` with Thief and Magician added: parent, attack, skills, promotion conditions, auto-assign split (none for Beginner); starting stats and starter items on Beginner only |
| `data/skills.yaml` | — | New: every skill, with its kind, cost, cooldown, shape, power, and its evolution stages |
| `kernel.Class` | A closed enum of four | An ID checked against `classes.yaml` |
| Character service | `class` chosen at creation | `class` starts as `beginner`; adds unspent skill points, a `character_skills` table, and the skill bar |
| World | Basic attack per class | Also spending skill points, using skills (damage, heal, buff, passive), skill cooldowns, and promotion at a trainer |
| `DeriveStats` (stat system), and its client copy | Own stats and gear | Also passive skills and active buffs, as two more sources of bonuses |
| Client | Class picker at creation; no skills | Name-only creation; the skill window (K); the skill bar (1–6); skill effects; promotion in the NPC dialog |

## 3. Game data

### `data/classes.yaml`

```yaml
classes:
  - id: beginner
    name: Beginner
    look: {sprite: alien_blue, tint: 0xd8c8a8}
    attack: {kind: melee, width: 48, height: 48, cooldown: 700ms, damage: physical}
    base_stats: {str: 7, dex: 7, int: 7, agi: 7, vit: 7}
    skills: [strike, throw, spark, second_wind]
    starter_items: [{item: wooden_club, quantity: 1}, {item: red_potion, quantity: 5}]
  - id: warrior
    name: Warrior
    parent: beginner
    look: {sprite: alien_blue}
    attack: {kind: melee, width: 64, height: 56, cooldown: 600ms, damage: physical}
    skills: [...]
    auto_assign: {str: 3, vit: 2}
    promotion:
      trainer: warrior_trainer            # an NPC (npcs.yaml)
      level: 10
      stats: {str: 30}                    # own stats, without gear
      skills: {strike: 10}                # points invested
      quests: [warriors_trial]
```

- **Starting stats** (`base_stats`) move to `beginner` alone. Every character starts as one, and a promotion doesn't change primary stats.
- **Base values** for each class, Beginner included, stay in `stats.yaml` (stat system).
- **Thief and Magician** join the data as first classes. Their attacks follow the product page: the Thief's is fast with a short reach, and the Magician's is a ranged spell dealing magic damage. Both fit the attack kinds and damage types the world already has. Each also gets base values in `stats.yaml`, an auto-assign split (Progression), and a weapon line: a dagger or a wand at each level the sword and bow have, dropping where they drop.
- **Look:** the client's art for the class, a player sprite and an optional tint over its grey copy, as mobs have.
- **Weapons:** a weapon's `class` now means "this class and every class below it in the tree". A weapon with no class, such as a Beginner weapon, fits everyone.
- **Validation** refuses:
  - a first class with a parent, or any later class without one or whose parent isn't above it in the file, so the tree has one root and no cycle;
  - a look without a sprite, or with a tint past RGB;
  - a skill listed by two classes;
  - promotion conditions that name an unknown NPC, quest, or stat, or a skill the class below can't learn;
  - a trainer NPC whose `trainer:` doesn't match the class;
  - starting stats or starter items on any class but `beginner`;
  - an auto-assign split on `beginner`, or none on any other class.

### `data/skills.yaml`

```yaml
skills:
  - id: strike
    kind: active                          # active | passive
    effect: damage                        # damage | heal | buff (active); bonus (passive)
    damage: physical
    shape: {kind: melee, width: 64, height: 56}   # melee | ranged (range) | area (radius)
    mp: {base: 4, per_point: 0.2}
    cooldown: 2s
    power: {base: 1.5, per_point: 0.05}   # × the matching attack
    stages:                               # evolution, all data
      - {at: 0, name: Strike}
      - {at: 10, name: Heavy Strike, power: {base: 2.2}, shape: {width: 80}}
      - {at: 25, name: Crushing Blow, power: {base: 3.0}, targets: 3}
  - id: second_wind
    kind: passive
    effect: bonus
    bonus: {hp_regen: {base: 0.5, per_point: 0.05}, mp_regen: {base: 0.5, per_point: 0.05}}
    stages:
      - {at: 0, name: Second Wind}
      - {at: 10, name: Deep Breath, bonus: {hp_regen: {base: 1.0}}}
```

- **A stage overrides only the fields it names.** Everything else carries from the stage before. So an evolution can change anything, and the totals (`at`) are plain numbers to edit. The first stage is at 0, and the totals must rise.
- **Power:** `power` multiplies the caster's attack of the skill's damage type: (base + per_point × points). Heals use magic attack. Buffs and bonuses name stats from the stat system's list of 21.
- **`skill_points_per_level: 5`** goes in `stats.yaml`, beside `points_per_level`.
- **Validation** refuses:
  - stages out of order;
  - a stage overriding a field its kind doesn't have;
  - a bonus or buff on an unknown stat;
  - an active skill without a cost, cooldown, or shape.

## 4. Character state

- **The character service:**
  - `class` defaults to `beginner`;
  - `skill_points_unspent` is added;
  - a new table `character_skills (character_id, skill_id, points)` holds the points invested;
  - `skill_bar int[6]`, or a small table, holds the skill IDs on keys 1 to 6.
- **Stored vs derived:** stages are never stored. A skill's stage is the last whose `at` is at or below its points, computed by whoever needs it.
- **Creating a character** (REST): `POST /api/characters` loses `class`. Every new character is a Beginner, with the Beginner's starter items granted as today. The item service's `GrantStarter` loses `class` too: the starter items are always the root's.
- **`Load` and `SaveCheckpoint`** carry the class, the unspent skill points, and the changed skill rows, like stat points.
- **The skill bar** is saved over REST: `PUT /api/characters/{id}/skill-bar`. It's a UI arrangement, not game state, and the world doesn't need it.

## 5. Spending skill points

- **Level-up:** each level adds `skill_points_per_level` to the unspent skill points, beside the stat points, and is saved at once as today.
- **`SpendSkillPoints{expected_unspent, points: {skill: n}}`:** the same compare-and-set as stat points. On the tick:
  1. **Refusals:**
     - `SKILL_POINTS_CHANGED` when the unspent count isn't the expected one;
     - `NOT_ENOUGH_SKILL_POINTS` when the points add up to more than that;
     - `SKILL_NOT_LEARNABLE` for a skill outside the class and the classes below it;
     - a refusal for any negative number.
  2. Otherwise the points are added.
  3. For any skill whose stage changed, a **`SkillEvolved`** goes to the player (and the combat channel), and passive bonuses re-derive.
  4. The change is saved at once.
- **No cap, no resets,** as the product pages say. A later cap is a data field.

## 6. Using skills

- **`UseSkill{skill, seq}`** is sent on a skill-bar key and travels like `Attack`: through the gateway to the world, rate-limited, and answered with no Response.
- **On the tick:** a refusal sends the player a **`SkillRefused{skill, reason}`**, which the client shows as a short notice. The world refuses:
  - while dead;
  - a skill with no points, or not learnable;
  - a damaging skill in a town;
  - during the skill's cooldown;
  - when MP is too low.
- **Resolving:**
  - **`damage`:** the stage's shape finds its targets (a box in front, a line along the facing, or a radius), up to the stage's `targets`. Each target is resolved with the stat system's `Resolve`, using the skill's damage type and the attack × power.
  - **`heal`:** the caster, or allies in its shape, by magic attack × power.
  - **`buff`:** a timed bonus to named stats on the caster (or allies), held by the world until it ends.
  - Then MP is spent, and the cooldown starts.
- **Cooldowns are per skill per player, in ticks:** *⌈cooldown × (1 − cooldown speed) ÷ tick⌉*, so the stat system's cooldown speed applies. They're shown to the client the way `attack_cooldown_ticks` is.
- **Everyone sees it:** a **`SkillUsed{caster, skill, stage, facing}`** event goes to the map for drawing. The damage numbers already exist.
- **Passive skills and buffs feed the stats.** `DeriveStats` gains two more sources of bonuses: passives (from points and stage) and active buffs. They add like gear, and the limits stay the ceiling. The sheet's share for equipment stays separate. The window shows buffs and passives as their own share (proposal).
- **The client's copy follows.** The character window's preview runs a TypeScript port of `DeriveStats` (`web/src/progression/stats.ts`). It gains the same two sources, and the shared stat vectors in `server/testdata/stats/` gain cases with passives and buffs, which both sides replay.
- **The basic attack switches with the class,** at once on promotion.

## 7. Promotion

- **The NPC dialog** (from the NPCs design) shows a trainer's promotion, each condition with *met* or *not met*. The world computes it from live state:
  - the level;
  - own stats, the stat system's points plus starting stats, without gear;
  - points in the named skills;
  - finished quests, from the quest log.
- **`Promote{npc, class}`** goes through the gateway with a Response. On the tick:
  1. **Checks:** the NPC must be in reach and train that class, the class's parent must be the character's class, and every condition must still be met. Otherwise the request is refused with `PROMOTION_CONDITIONS_UNMET`, `NPC_OUT_OF_REACH`, or `NOT_YOUR_NEXT_CLASS`.
  2. **Apply:**
     - the class changes, with the basic attack and the base values;
     - the stats re-derive, and HP and MP are restored to the new maximums (open question 1);
     - worn weapons are re-checked: a Beginner weapon still fits;
     - the change is saved at once.
  3. **Tell everyone:** the player gets a **`Promoted{class}`** (a toast, and a combat-channel line). The map sees the class in `PlayerView`, and the party in `PartyMember`, which gains `class`.
- **Durability:** a promotion is saved like a level-up, at once. A crash before the save means doing it again, and the conditions are still met.

## 8. Protocol

### Client protocol, all additive

| Message | Direction | Change |
| --- | --- | --- |
| `Welcome`, `LevelUp` | server | `CharacterView` gains the unspent skill points and the skills: each with its points, stage, MP cost, and cooldown ticks |
| `SpendSkillPoints` | client | New: `expected_unspent`, `points` per skill; Response |
| `UseSkill` | client | New: `skill`, `seq` |
| `SkillsChanged` | server | New: the skills after a spend, a promotion, or a stat change that moves costs or cooldowns |
| `SkillEvolved` | server | New: skill, new stage |
| `SkillUsed` | server | New, to the map: caster, skill, stage, facing |
| `SkillRefused` | server | New: skill, reason |
| `Promote` | client | New: `npc`, `class`; Response |
| `Promoted` | server | New: the new class |
| `PartyMember` | server | Gains `class` |

- **New error codes**, each the next number in its area (33, the world):
  - `SKILL_POINTS_CHANGED`;
  - `NOT_ENOUGH_SKILL_POINTS`;
  - `SKILL_NOT_LEARNABLE`;
  - `PROMOTION_CONDITIONS_UNMET`: "You don't meet every condition yet.";
  - `NOT_YOUR_NEXT_CLASS`.
  - `SkillRefused` reasons: `no_mp`, `on_cooldown`, `not_in_town`, `dead`, `not_learned`.
- **REST:**
  - `POST /api/characters` without `class`;
  - `PUT /api/characters/{id}/skill-bar`;
  - the character list shows the class, Beginner included.
- **Gateway routes:**
  - `UseSkill` beside `Attack`, with its own limit;
  - `SpendSkillPoints` and `Promote` with a Response.

### Internal contracts

- **`world.v1`:** `SpendSkillPoints`, `UseSkill`, and `Promote`.
- **`character.v1`:** class, skills, and skill points in `Load` and the checkpoint.
- **`social.v1`:** a member's class in its updates.

## 9. Client

- **features/characters:** creation asks for a name only.
- **features/skills, the skill window (K):**
  - the class's and the lower classes' skills;
  - each skill's points, what the next point adds, and its next stage at its total;
  - **+**, Apply, and Undo, as for stats.
  - Dragging an active skill to the skill bar saves the bar over REST.
- **features/hud:**
  - the skill bar, with keys, cooldown sweeps, and greying when MP is short;
  - the unspent skill-points badge.
- **input:** six `skill1`–`skill6` actions in both presets (keys 1 to 6), and a `skills` action (K).
- **game:**
  - a skill key sends `UseSkill`, gated by the skill's cooldown ticks and MP from the server;
  - skill effects are drawn from `SkillUsed`, as simple shapes, until there's art.
- **The NPC dialog** (from the NPCs design) shows promotions and sends `Promote` with a confirmation that says it's permanent.
- **Auto-assign** for stat points is hidden while the character is a Beginner.

## 10. When things fail

| Failure | What the player sees | Why it is safe |
| --- | --- | --- |
| `SpendSkillPoints` reply lost; retried | Nothing unusual | Compare-and-set: the retry is refused with `SKILL_POINTS_CHANGED`, and `SkillsChanged` already arrived |
| World crash between a spend or promotion and its save | After the rejoin, the points are unspent or the character is a Beginner again | Same window as a level-up; doing it again is allowed, and nothing is gained twice |
| An evolution total changes in the data | Characters' skills re-form at their next join | Stages are derived from points, never stored |
| A class is added to the data | It appears in its trainer's dialog | Classes are IDs checked against data, not a list in code |
| Client and server disagree on a cooldown | The server refuses the early use with `on_cooldown` | The client only gates; the server counts the ticks |

## 11. Testing

- **Game data:** the tree, skills, and stages validation rows.
- **Stages:** the stage for a given number of points; a stage overriding only what it names; re-staging after a data change.
- **Character service:** a Beginner by default; creation without a class; skills and skill points stored and loaded; the skill bar over REST.
- **World:**
  - level-up skill points;
  - `SpendSkillPoints`: once only; stale; too many; not learnable; evolution events;
  - `UseSkill`:
    - each refusal reason;
    - damage by shape and targets, with the damage type and power;
    - heal and buff, and buffs ending;
    - passives in the sheet, and the shared stat vectors with passives and buffs;
    - cooldown ticks following cooldown speed;
  - `Promote`:
    - each condition, and each refusal;
    - a promotion changing the attack and base values;
    - Beginner skills kept;
    - the party seeing the class.
- **End to end:** create (a Beginner), level, spend skill points into Strike until it evolves, use it on a mob, finish the trial quest, promote at the trainer, attack as a Warrior.
- **Client:** Vitest for the skill window's staging and the skill bar gating; Testing Library for creation, the skill window, and the promotion confirmation.

## 12. Build order

After [NPCs and task quests](./npcs-and-quests.md) is merged, on its own branch and PR. Each step is one commit, green on `make test`, `make lint`, `make cover`, and `make breaking`:

1. **Game data:**
   - the class tree, `skills.yaml` with stages, and Beginner base values and starting stats;
   - `kernel.Class` as an ID checked against data;
   - weapons fitting a class and the classes below it;
   - validation and the client generator.
2. **Character service:** Beginner by default, creation without a class, skill points and `character_skills`, the skill bar over REST, and `Load` and the checkpoint.
3. **World, skill points:** level-up points, `SpendSkillPoints`, stages, and evolution events.
4. **World, using skills:** shapes, damage, heal, buff, passives and buffs in `DeriveStats` and its client copy, cooldowns, and `SkillUsed`.
5. **World, promotion:** conditions in the trainer's dialog, `Promote`, and the class in views and in the party.
6. **Gateway and protocol:** routes, messages, error codes, REST, and contracts.
7. **Client:** creation, the skill window, the skill bar, skill effects, promotion in the NPC dialog, and auto-assign hidden for Beginners.
8. **Content and balance:**
   - Beginner skills and the Wooden Club;
   - Thief and Magician: attacks, base values, auto-assign splits, and a first weapon each (a dagger and a wand), with requirements like the other classes' weapons;
   - two placeholder skills per first class;
   - the four promotions' conditions (placeholders, as the product page says);
   - a `make bots` run with bots that promote.

## 13. Decisions

Agreed on 2026-11-10:

1. **HP and MP are restored to full at promotion,** like a level-up, since a promotion is a milestone.
2. **The skill bar is saved over REST** in the character service, as a UI arrangement, not through the world.
3. **A weapon's class covers that class and every class below it in the tree,** so later second-tier classes keep their first class's weapons.
4. **Stages are derived from points and never stored,** which is what makes evolutions data only.
5. **Each first class ships with two placeholder skills,** so promotion leads somewhere, until the real sets are designed.
6. **Buffs and passives show as their own share** in the character window, beside equipment's.

## 14. Alternatives considered

- **Store each skill's stage:** simpler reads, but every evolution change in the data would need a conversion. Derived stages make the data the single source.
- **Classes stay a closed list in Go:** fine for four, but "four isn't the final set". IDs checked against data cost one validation.
- **Skill effects as code per skill:** maximum freedom, but every new skill or evolution would need a release. A few effect kinds (damage, heal, buff, bonus) with data parameters cover the planned skills. A new kind is a code change, made only when content needs one.
- **Promotion stored as a separate record with its own key:** a promotion has no economic effect, so it is saved like a level-up, and doing it again after a crash is harmless.
