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
    look: {sprite: alien_blue, tint: 0xff6640}
    attack: {kind: melee, width: 48, height: 48, length: 1050ms, damage: physical}
    base_stats: {str: 7, dex: 7, int: 7, agi: 7, vit: 7}
    skills: [strike, throw, spark, second_wind]
    starter_items: [{item: wooden_club, quantity: 1}, {item: red_potion, quantity: 5}]
  - id: warrior
    name: Warrior
    parent: beginner
    look: {sprite: alien_blue}
    attack: {kind: melee, width: 64, height: 56, length: 900ms, damage: physical}
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
    cooldown: 3s
    length: 1050ms                        # how long using it keeps the player busy (animation-cancel.md)
    cancelable: true                      # a basic attack may cut it short from its cancel point
    power: {base: 1.5, per_point: 0.05}   # × the matching attack
    stages:                               # evolution, all data
      - {at: 0, name: Strike, description: "A strong swing at whatever stands in front."}
      - {at: 10, name: Heavy Strike, description: "A wider, harder swing.", power: {base: 2.2}, shape: {kind: melee, width: 80, height: 56}}
      - {at: 25, name: Crushing Blow, description: "...", power: {base: 3.0}, targets: 3}
  - id: second_wind
    kind: passive
    effect: bonus
    bonus: {hp_regen: {base: 5, per_point: 1}, mp_regen: {base: 5, per_point: 1}}   # tenths of a point a second
    stages:
      - {at: 0, name: Second Wind, description: "..."}
      - {at: 10, name: Deep Breath, description: "...", bonus: {hp_regen: {base: 10}, mp_regen: {base: 10}}}
```

- **A stage overrides only the fields it names.** Everything else carries from the stage before. So an evolution can change anything, and the totals (`at`) are plain numbers to edit. The first stage is at 0, and the totals must rise.
  - Within `mp`, `power` and a bonus's stats, a stage may name only `base` or only `per_point`.
  - A `shape` is replaced whole, so a stage that changes one names its kind and every size.
- **Power:** `power` multiplies the caster's attack of the skill's damage type: (base + per_point × points). Heals use magic attack.
- **Bonuses** name stats from the stat system's list of 21, in the units `items.yaml` uses: rates in tenths of a percent, regen in tenths of a point a second. With points, each stat's value is rounded to a whole number, as an item's roll is.
- **Teaching:** each skill is taught by exactly one class, the one whose `skills` lists it. The loader sets the skill's class from that list.
- **`skill_points_per_level: 5`** goes in `stats.yaml`, beside `points_per_level`.
- **Validation** refuses:
  - a kind and effect that don't go together: an active skill damages, heals or buffs, and a passive one gives a bonus;
  - a first stage not at 0, totals that don't rise, or a stage without a name and a description;
  - an active skill without a cooldown, a length or an MP cost, or a passive one with any of them, a shape, or any other timing;
  - power on anything but a damage or heal skill, or power that isn't positive or falls with points;
  - targets below 1, or on a skill that doesn't hit;
  - a damage type on anything but a damage skill, and a duration on anything but a buff;
  - a buff or passive bonus without a bonus, a bonus on any other skill, a bonus on an unknown stat, and any bonus value below 0;
  - a shape whose kind is not melee, ranged or area, or that has sizes its kind doesn't use;
  - a skill taught by two classes, one no class teaches, and a class teaching a skill `skills.yaml` doesn't have.

## 4. Character state

- **The character service:**
  - `class` defaults to `beginner`;
  - `unspent_skill_points` is added, never below 0;
  - a new table `character_skills (character_id, skill_id, points)` holds the points invested, a row only for a skill with at least 1;
  - `skill_bar text[]`, six entries, holds the skill on each of keys 1 to 6, `''` for none;
  - a table `character_skill_timers (character_id, skill_id, kind, until, points)` (migration 00003) holds when each running cooldown and buff ends, with a buff's points.
- **Stored vs derived:** stages are never stored. A skill's stage is the last whose `at` is at or below its points, computed by whoever needs it.
- **Creating a character** (REST): `POST /api/characters` loses `class`. Every new character is a Beginner, with the Beginner's starter items granted as today. The item service's `GrantStarter` loses `class` too: the starter items are always the root's.
- **`Load` and `SaveCheckpoint`** carry the class, the unspent skill points, and every skill's points, like stat points. The checkpoint writes only the rows that changed, and refuses with `REASON_BAD_SKILL` a skill with no ID or listed twice, points below 1, or unspent skill points below 0.
  - **They carry the skill timers too:** every cooldown and buff still running, by skill and kind, each with when it ends, and a buff with the points it was used with. Each checkpoint writes the ones that changed and drops the ones it no longer lists. It refuses with `REASON_BAD_SKILL_TIMER` a timer with no skill, an unknown kind, one listed twice, or a buff without points.
- **The skill bar** is saved over REST, `PUT /api/characters/{id}/skill-bar` with `{slots}`, and listed with the character in `GET /api/characters`. It's a UI arrangement, not game state, and the world never reads it.
  - **Validation:** six keys, each empty or an active skill in the game data, whether or not the character has learned it yet. Anything else is `BAD_SKILL_BAR`. Another account's character is `CHARACTER_NOT_FOUND`.
    - It doesn't check the class, as the world refuses a skill the class can't learn when it's used.
    - Nor does it refuse one skill on two keys. The client never sends that, since a skill dropped on a key leaves any other.
  - **The client saves it** 800 ms after the player's last change, so a run of drags makes one call.
    - Saves go one at a time, each with the bar as it stood when due, so the last change lands last.
    - Leaving the world saves a change still waiting, and the character list loads only once that save answers, so a quick Back and Play shows it.
    - Closing the tab (`pagehide`) saves a change still waiting too. Saves are sent with `keepalive`, so one leaving as the page closes still arrives.
    - A failed save keeps the bar on screen and says so.

## 5. Spending skill points

- **Level-up:** each level adds `skill_points_per_level` to the unspent skill points, beside the stat points, and is saved at once as today.
- **`SpendSkillPoints{expected_unspent, points: {skill: n}}`:** the same compare-and-set as stat points. On the tick:
  1. **Refusals,** in this order:
     - `BAD_SKILL_POINTS` for any negative number;
     - `SKILL_NOT_LEARNABLE` for points in a skill outside the class and the classes above it, or one the data doesn't have;
     - `BAD_SKILL_POINTS` when the points add up to 0;
     - `SKILL_POINTS_CHANGED` when the unspent count isn't the expected one;
     - `NOT_ENOUGH_SKILL_POINTS` when the points add up to more than that.
  2. Otherwise the points are added, and the stats re-derive with the passives.
  3. A **`StatsChanged`** carries the new sheet, which holds the unspent count and every skill's points. The Response answers once it applies.
  4. The change is saved at once.
- **No message for skills of their own.** Skill points ride in the stat sheet, in `Welcome`, `LevelUp` and `StatsChanged`. Stages, MP costs and evolutions all follow from the points and the data, so the client works them out itself: it compares the points before and after a `StatsChanged` and announces an evolution in the combat channel. The server stays the judge of what a skill does when it's used.
- **No cap, no resets,** as the product pages say. A later cap is a data field.

## 6. Using skills

- **`UseSkill{skill}`** is sent on a skill-bar key and goes through the gateway to the world, with its own rate limit (5 a second, a burst of 6). **It's a request with a Response,** answered once the tick resolves it.
  - **Why a Response, not `SkillRefused`:** the approved design had a `SkillRefused{skill, reason}` message with its own reasons. A Response reuses the refusal path every other command has: area 33's codes, the gateway's error map and the client's words. A skill's cooldown is seconds long, so a reply per use costs little.
- **One action at a time,** on the timeline [Animation cancel](animation-cancel.md) describes. A basic attack and a skill are each an action with its own length.
  - A skill goes once the current action lets it: from a basic attack's cancel point, or at a skill's end. It has the same 150 ms slack as cooldowns, but its action and its cooldown count from that point, not from the early use.
  - Used before then, it waits and goes then, so it isn't refused.
  - Skills resolve before the attacks in the tick, so a skill goes ahead of a held attack. The attack waits for the skill's end, or its cancel point if the skill is cancelable.
  - One use waits per player. Another arriving meanwhile is refused with `SKILL_WAITING`, and one still waiting when the player leaves the map is answered as unavailable. The client never sends a second while its first waits, so this comes only when the two disagree on the turn. As the refused use never went, the client gives it its turn back and no cooldown.
  - Damage a second now follows the skills' lengths and the cancels as well as attack speed. [Animation cancel](animation-cancel.md#8-balance) says how the balance harness bounds it.
- **On the tick,** after movement, so a skill sees where everyone stands. The world refuses, in this order, at once on arrival and again when the turn comes:
  1. `CHARACTER_DEAD` while dead;
  2. `SKILL_NOT_LEARNED` for a skill with no points, one the data doesn't have, or one the class can't learn;
  3. `SKILL_PASSIVE` for a passive skill;
  4. `NOT_IN_TOWN` for a damaging skill in a town; a heal or a buff works there;
  5. `SKILL_COOLING_DOWN` during the skill's cooldown;
  6. `NOT_ENOUGH_MP` when MP is short of the stage's cost;
  7. `HP_FULL` for a heal with no one hurt to heal.
- **Resolving,** by the stage the skill's points reach:
  - **`damage`:** the stage's shape finds up to `targets` mobs, nearest first: a box in front of the feet, a line at half height along the facing, or a radius around the middle. Each is resolved with the stat system's `Resolve`, using the skill's damage type and the matching attack × power. Kills give EXP and loot as attacks do.
  - **`heal`:** up to `targets` below full HP, each by the caster's magic attack × power, up to their maximum HP. The caster comes first if hurt, then the nearest hurt party members alive on the map in its shape. Those at full HP take no place, so a full ally standing nearer never keeps a hurt one from the heal.
  - **`buff`:** the stage's bonus on the caster, at the points it was used with, until its duration ends. A buff used again starts over. The loader holds a buff's `targets` to 1.
  - Then MP is spent, the cooldown starts, and a `StatsChanged` carries the MP left. A buff's carries the new sheet.
- **Cooldowns are per skill per player, in ticks:** *⌈cooldown × (1 − cooldown speed) ÷ tick⌉*, so the stat system's cooldown speed applies.
  - **A little early is let through.** A skill used up to 150 ms before its cooldown ends is used, and the next cooldown counts from the end, not the early use. A client timing the cooldown itself can be a tick ahead of the server; this way it's never refused for it, and gains nothing.
  - **The client counts them itself,** from the data and the sheet's cooldown speed, with the same formula. The sheet carries no skill cooldowns.
- **Everyone sees it:** a **`SkillUsed{caster, skill, stage, facing}`** event goes to the map, for drawing. Its damage goes out as `Damage`, as attacks' does.
- **Buffs end at death.** The world restats, and the sheet goes out without them.
- **Buffs and cooldowns outlive the map:** a map change, through a portal or to the town after dying (whose buffs have ended), and a fresh join, after a reload, a reconnect or a world restart.
  - Each map counts them in its own ticks, and maps may tick at different rates. So the handoff and the checkpoint keep when each ends on the wall clock, with a buff's points, and a map counts each on from its own tick. A buff's bonus comes from the data again, at those points; a skill the data no longer has ends its buff.
  - **A fresh join** reads them from the last checkpoint, dropping any that ended meanwhile. A running buff counts in the joining sheet.
  - **The client keeps its own** across a map change. A `Welcome` sets them to the world's, from the time each has left. A key shows its cooldown where it stands, against its full length at the character's cooldown speed, drains on from there, and ends at its time.
  - A crash loses at most one checkpoint interval of them, as it does of everything else.
- **Passive skills and buffs feed the stats.** `DeriveStats` takes both as the skills' bonuses, added like gear, with the limits still the ceiling.
  - **Passives:** a passive with at least one point adds its stage's bonus.
  - **The sheet's shares:** the sheet's equipment share stays separate. `primary_skills` and `derived_skills` are the skills' share, passives and buffs together: the totals less the totals without them. The character window shows both shares in one bracket after the total, without the unit: the equipment's in green, then the skills' in blue.
  - **Requirements ignore them:** item requirements read own stats, which leave out the skills' share as they leave out the gear's.
- **The client's copy follows.** The character window's preview runs a TypeScript port of `DeriveStats` (`web/src/progression/stats.ts`), and `web/src/progression/skills.ts` ports the passives and buffs. The client times its own buffs from its `SkillUsed`, so a preview made during one keeps it. The shared stat vectors in `server/testdata/stats/` carry each case's skill points, and both sides replay them.
- **The basic attack switches with the class,** at once on promotion.

## 7. Promotion

- **The NPC dialog** (from the NPCs design) shows a trainer's promotion: the class it trains, if its parent is the character's class, and each condition with what it needs and what the character has. The world computes it from live state, and checks a `Promote` with the same rules:
  - the level;
  - own stats, the stat system's points plus starting stats, without gear;
  - points in the named skills;
  - finished quests, from the quest log.
- **`Promote{npc_id, class}`** goes through the gateway with a Response.
  1. **Checks, on the tick:** the character must be alive, the NPC in reach, and the class the one it offers. Every condition must be met. Otherwise the request is refused with `NPC_OUT_OF_REACH`, `NOT_YOUR_NEXT_CLASS` or `PROMOTION_CONDITIONS_UNMET`. One promotion goes at a time; a second while one is in flight is refused as unavailable.
  2. **The trainer's weapon first.** A worker asks the item service to add the class's `promotion.weapon`, with cause `promotion`, as hand-ins do, retrying while it's unavailable.
     - **Its key is derived from the character and the class** (`uuidv7.Derived`), and each roll is the middle of its range. So a retry, or promoting again after a lost answer, gives the weapon once, while the item service keeps the key (`idempotency_keys.keep`, 24 h). A promotion tried again after that gives a second weapon.
     - **A refusal,** such as a full bag, changes nothing, and the player sees its words.
  3. **Apply, on the tick once the weapon is given:**
     - The character must still be alive and meet every condition; a stat reset in the meantime refuses it. They may have stepped away from the trainer. A refusal here keeps the weapon given, and promoting again within the key's 24 h gives no second one.
     - The class changes, with the basic attack and the base values. The stats re-derive, and HP and MP are restored to the new maximums, as at a level-up.
     - Worn weapons need no check: a Beginner weapon fits every class.
     - A checkpoint saves it at once. The checkpoint carries the class, and the character service refuses one that isn't in the data.
  4. **Tell everyone:** the player gets an `InventoryChanged` with the weapon, and a **`Promoted`** with the whole character view, as `LevelUp` has. The map sees the class in `PlayerView`, and the party in `PartyMember`.
- **Durability:** a promotion is saved like a level-up, at once. A crash before the save means doing it again: the conditions are still met, and within the key's 24 h no second weapon comes.

## 8. Protocol

### Client protocol, all additive

| Message | Direction | Change |
| --- | --- | --- |
| `Welcome`, `LevelUp`, `StatsChanged` | server | `StatSheet` gains `skills` (the unspent skill points and each skill's points) and the skills' share, `primary_skills` and `derived_skills` |
| `Welcome` | server | Gains `skill_timers`: each running cooldown and buff, with the time left and a buff's points |
| `SpendSkillPoints` | client | New: `expected_unspent`, `points` per skill; Response |
| `UseSkill` | client | New: `skill`; Response |
| `SkillUsed` | server | New, to the map: caster, skill, stage, facing |
| `NpcDialog` | server | Gains `promotion`: the class, and each condition's kind, ID, need and have |
| `Promote` | client | New: `npc_id`, `class`; Response |
| `Promoted` | server | New: the character view, with the new class, sheet, HP and MP |
| `PartyMember` | server | Gains `class` |

- **New error codes**, each the next number in its area (33, the world):
  - `NOT_ENOUGH_SKILL_POINTS`;
  - `SKILL_POINTS_CHANGED`;
  - `SKILL_NOT_LEARNABLE`;
  - `BAD_SKILL_POINTS`: a negative number, or no points at all;
  - `SKILL_NOT_LEARNED`, `SKILL_PASSIVE`, `NOT_IN_TOWN`, `SKILL_COOLING_DOWN` and `NOT_ENOUGH_MP`, a used skill's refusals (330019 to 330023), and `SKILL_WAITING` (330024), another use already waiting for the turn;
  - `PROMOTION_CONDITIONS_UNMET` (330025): "You don't meet every condition yet.";
  - `NOT_YOUR_NEXT_CLASS` (330026): the trainer doesn't train that class, or it isn't the next one for the character's.
  - In area 12, the characters: `BAD_SKILL_BAR` (120007).
- **REST:**
  - `POST /api/characters` without `class`;
  - `PUT /api/characters/{id}/skill-bar` with `{slots}`, six skill IDs, answered with the bar as saved;
  - the character list shows the class, Beginner included, and the skill bar.
- **Gateway routes:**
  - `UseSkill` beside `Attack`, with its own limit, and a Response;
  - `SpendSkillPoints` and `Promote` with a Response.

### Internal contracts

- **`world.v1`:** `SpendSkillPoints`, `UseSkill`, and `Promote`; `SkillUsed` and `Promoted` on `Deliver`, and the promotion in the NPC dialog; the skill timers left in `JoinResponse`.
- **`character.v1`:** class, skills, skill points and skill timers in `Load` and the checkpoint, and `REASON_BAD_SKILL_TIMER`; `SetSkillBar`, and the skill bar in `Character`.
- **`item.v1`:** `CAUSE_TYPE_PROMOTION`, for the trainer's weapon.
- **`social.v1`:** a member's class in its updates.

## 9. Client

- **features/characters:** creation asks for a name only.
- **features/skills, the skill window (K):**
  - the skills of the class and of every class above it, listed by class;
  - each skill's points, what it does at its stage, what a point adds, and its next stage at its total;
    - What it does reads by its effect: an attack's power, cost, reach and foes; a buff's bonus on the caster and how long it lasts; a heal's reach, as the world picks who it heals, and its amount; a passive's bonus.
  - **+**, Apply, and Undo, as for stats, with a preview of the staged points and any evolution they reach. A skill with no points says it's usable once it has one, and with points staged, what that many points make it.
  - An active skill is dragged onto the skill bar.
- **features/hud:**
  - **the skill bar,** under the HUD's bars: keys 1 to 6, each with its skill's stage name.
    - A key greys out when MP is short or the skill has no points, and drains from the top during its cooldown.
    - Dragging a key onto another swaps them; dropping it anywhere off the bar empties it, and a drag cancelled with Esc keeps it.
    - A key answers a skill from the skill window with the copy effect its row allows, and a dragged key with move, since a browser drops nothing with an effect the drag doesn't allow.
  - the unspent skill-points badge, which opens the skill window.
- **The combat channel** says "Strike evolved into Heavy Strike!" when a `StatsChanged` moves a skill to a later stage.
- **input:** six `skill1`–`skill6` actions in both presets (keys 1 to 6), and a `skills` action (K).
- **game:**
  - **a skill key** (`SkillKeys`, beside the potion keys) checks what the client knows: points, a passive, damage in a town, its own cooldown count, and MP. Each says why the key did nothing: a press during a cooldown once a cooldown, and one short of MP once until a press finds enough.
    - **The action clock** (`game/systems/actions.ts`, from [Animation cancel](animation-cancel.md#7-client)) says when a skill may start. A key pressed before then is held, and checked again when its moment comes. A newer press takes its place, and the attack key, held or pressed, holds back while one waits.
    - Then it sends `UseSkill`, and starts the skill's action and cooldown and draws its shape, all at once.
    - **A refusal** gives the action back and shows the client's words for the code. It undoes the cooldown too, except for `SKILL_COOLING_DOWN`: the world says the skill isn't ready, so it doesn't look ready.
    - **No answer** keeps both, as the use may have landed.
  - skill effects are drawn as simple shapes, until there's art: the box a melee stage swings through, the line a ranged one flies along, with a dark edge so it shows on pale ground, or a ring around the caster. The player's own show as they're sent; others' come from `SkillUsed`.
- **The NPC dialog** (from the NPCs design) shows a trainer's promotion: "Become a Warrior", the class's description, and each condition, with what the character has while it falls short. Its button waits for every one.
  - **The counts follow live state.** The dialog's needs come from the world. What the character has is counted again from the client's own state, as the world counts it (`features/npc/promotion.ts`), so spends, level-ups and quests show without talking again. The world checks again anyway.
  - **It asks first:** a promotion is for good, and the trainer hands over the first weapon. Then it sends `Promote`, and closes once it's done.
- **On `Promoted`,** the character's look, basic attack and learnable skills switch at once, and the trainer's dialog closes, even if the request's answer was lost. Others see the class from the next `Delta`. A toast and the combat channel say "You became a Warrior!".
- **Auto-assign** for stat points is hidden while the character is a Beginner.

## 10. When things fail

| Failure | What the player sees | Why it is safe |
| --- | --- | --- |
| `SpendSkillPoints` reply lost; retried | Nothing unusual | Compare-and-set: the retry is refused with `SKILL_POINTS_CHANGED`, and its `StatsChanged` already arrived |
| World crash between a spend or promotion and its save | After the rejoin, the points are unspent or the character is a Beginner again | Same window as a level-up; doing it again is allowed, and nothing is gained twice |
| An evolution total changes in the data | Characters' skills re-form at their next join | Stages are derived from points, never stored |
| A class is added to the data | It appears in its trainer's dialog | Classes are IDs checked against data, not a list in code |
| Client and server disagree on a cooldown by a tick | Nothing: the use goes through | The server lets a use up to 150 ms early through and counts the next cooldown from the end |
| Client and server disagree on a cooldown by more | "That skill isn't ready yet.", and the key keeps its cooldown until the client's count ends | The client only gates; the server counts the ticks |
| A skill bar save fails | "Could not save the skill bar." or the refusal's words; the bar stays as arranged | The next change saves the whole bar again |
| A map change during a cooldown or a buff | Both carry on | The handoff carries the time each has left, and the next map counts it in its own ticks |
| A reload or a reconnect during a cooldown or a buff | Both carry on | The checkpoint keeps when each ends, and the join counts them on and tells the client |
| The item service refuses the trainer's weapon, as for a full bag | The refusal's words; still the old class | Nothing changed anywhere |
| A promotion's answer is lost after the weapon is given | Promoting again works | The weapon's key is the character's and the class's, so within the item service's 24 h key retention no second one comes |
| A weapon's roll ranges change in the data between a gift and a retry within 24 h | The promotion is refused until the key expires | The retry's roll differs, so the item service refuses the reused key rather than giving another weapon |

## 11. Testing

- **Game data:** the tree, skills, and stages validation rows.
- **Stages:** the stage for a given number of points; a stage overriding only what it names; re-staging after a data change.
- **Character service:** a Beginner by default; creation without a class; skills and skill points stored and loaded; the skill bar over REST.
- **World:**
  - level-up skill points;
  - `SpendSkillPoints`: once only; stale; too many; not learnable; negative or none; passives in the sheet;
  - `UseSkill`:
    - each refusal, and their order;
    - damage by shape and targets, with the damage type and power;
    - a heal on the caster and the nearest hurt party member in reach, not a farther one or a stranger; a full member nearer than the hurt ones, and a full caster, take no place;
    - a buff in the sheet until it ends, and ending at death;
    - cooldown ticks following cooldown speed, the early use let through, and the next cooldown from the end;
    - one action at a time: a skill sent with an attack goes first; one used during an action waits; one waiting at a leave is answered. The cancels are in [Animation cancel](animation-cancel.md#11-testing);
  - `Promote`:
    - the trainer's dialog offering it, with what the character has;
    - each condition, and each refusal;
    - the weapon's call, key and roll; a promotion changing the attack and base values, and saving the class at once;
    - Beginner skills kept;
    - the party and the map seeing the class;
    - the weapon refused; a second promotion while one is in flight; a condition lost before it applies;
  - a map change carrying a cooldown and a buff;
  - a checkpoint keeping them, and a fresh join counting them on, an ended one dropped and a running buff in the joining sheet.
- **End to end:** a Beginner meeting every condition talks to the trainer, is refused a class it doesn't train, promotes, finds the weapon in the bag, is refused a second promotion, and logs in again as a Warrior. A Warrior who used Iron Body logs in again with its cooldown and buff still running, in the Welcome and in the world. Spending and using skills have their own rows.
- **Client:**
  - Vitest for the skill window's staging, the skill keys' checks and cooldowns, the bar's saving, and the effects' shapes;
  - Testing Library for creation, the skill window, the skill bar's drags, the preview with a buff, and the promotion confirmation.

## 12. Build order

After [NPCs and task quests](./npcs-and-quests.md) is merged, on its own branch and PR. Each step is one commit, green on `make test`, `make lint`, `make cover`, and `make breaking`:

1. **Game data:**
   - the class tree, `skills.yaml` with stages, and Beginner base values and starting stats;
   - `kernel.Class` as an ID checked against data;
   - weapons fitting a class and the classes below it;
   - validation and the client generator.
2. **Character service:** Beginner by default, creation without a class, skill points and `character_skills`, the skill bar over REST, and `Load` and the checkpoint.
3. **World, skill points:** level-up points, `SpendSkillPoints`, stages, and passives in the sheet.
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
