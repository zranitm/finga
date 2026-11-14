# Energy

> **Status: approved · 2026-11-13, built · 2026-11-14.** The high-level design for [C14. Energy](../product/features/combat-and-progression/c14-energy.md) and [Systems → Combat → Energy](../product/systems/combat.md). It describes the change as built, in two PRs: energy in place of MP, and no attack speed. "Today" below is the code before it, after C12 and C13.

## 1. What has to hold

- **The server counts energy.** It grants every gain and checks and spends every cost. The client never grants itself energy.
- **A cancel never waits on energy it has earned.** A skill pressed at an attack's cancel point must find that attack's energy counted on the client as well as the server.
- **A spend shows at once and never flickers back.** The pips drop when the player presses a skill, and a late update from before the spend doesn't refill them.
- **No MP and no attack speed remain:** not in game data, the wire, the database, the client, or the docs.

## 2. The change at a glance

| Part | Today | After |
| --- | --- | --- |
| Resource | MP: a max from class, level, INT, and gear; regen; potions | Energy: 0 to 10, from basic attacks that reach a mob |
| Skill cost | `mp: {base, per_point}`, growing with points | `energy: 2` or `3`, flat |
| Potions | Red, Orange, and White for HP; Blue and Mana Elixir for MP | HP only |
| INT | Magic attack, magic defense, cooldown speed, max MP, MP regen | Magic attack, magic defense, cooldown speed |
| Attack speed | A rate from AGI and gear, shortening basic attacks | Gone; a basic attack is its class's length |
| Haste | Attack speed and evasion | Move speed and evasion |
| Wire | `mp`/`max_mp` in `CharacterView` and `StatsChanged`; MP and attack speed in `DerivedStats` and `ItemStats` | `energy` in `CharacterView` and in the recipient's `Delta`, beside `ack_seq` |
| Database | `characters.mp`; `item_rolls` MP and attack speed columns | `characters.energy`; those columns gone |

## 3. Game data

**`data/actions.yaml`** gains:

```yaml
energy: {max: 10, per_attack: 2}  # what skills cost: a basic attack that reaches a mob gives per_attack, up to max
```

**`data/skills.yaml`:**
- Each active skill's `mp: {base, per_point}` becomes `energy`, a whole number. A stage may name its own, like any value, though none does yet:

  | `energy` | Skills |
  | --- | --- |
  | 2 | `strike`, `throw`, `double_stab`, `power_shot`, `spark`, `fire_arrow` |
  | 3 | `slash_blast`, `heal`, `iron_body`, `haste`, `focus` |

- `second_wind` and Deep Breath lose `mp_regen` and keep `hp_regen`. The description becomes "more HP back every second".
- `haste`'s `attack_speed` bonus becomes `move_speed`, with the same values: base 80 and 5 a point, in tenths of a percent. Its description becomes "Moves quicker: faster on foot and more evasion for a while."

**`data/stats.yaml`:**
- INT loses `max_mp` and `mp_regen`.
- Each class loses `max_mp`.
- The `attack_speed` curve goes. AGI keeps evasion and move speed.

**`data/items.yaml`:**
- `blue_potion` and `mana_elixir` go, and with them the `heal_mp` field. HP potions keep `heal_hp`.
- `runed_wand`, `gold_pendant`, and `starlight_pendant` lose `max_mp` and `mp_regen`.
- `hunters_gloves`, `plate_gauntlets`, `ranger_gloves`, and `whisper_edge` lose `attack_speed`.
- No other stat changes.

**`data/mobs.yaml`:** every `blue_potion` and `mana_elixir` drop line goes, from normal mobs and bosses alike. Nothing replaces them.

**`data/quests.yaml`:** the three `blue_potion` rewards become `red_potion` in the same quantities.

**`data/classes.yaml`:** the Magician's description loses "paid for in MP" and becomes "Fights from afar with magic: strong spells that hit hard." The header comment no longer says attack speed shortens the attack.

**Validation** refuses:
- an `energy` block without a positive `max`, or with `per_attack` outside 1 to `max`;
- an active skill, or a stage, whose `energy` is outside 1 to `max`;
- any `mp`, `max_mp`, `mp_regen`, `heal_mp`, or `attack_speed` key, so none creeps back.

The web generator carries `energy` and each skill's cost, and drops the rest.

## 4. The server

**The player** holds `Energy`, from 0 to `max`, in place of `MP` and its regen carry.

**Gaining:**
- A basic attack's hit lands at its start, when `resolveAttacks` begins it: no class names a later hit share.
- When that hit's `strike` reaches at least one mob, hit or MISS, the player gains `per_attack`, up to `max`.
- A skill's strike gains nothing, and a basic attack that reaches nothing gains nothing.

**Spending:**
- A skill press is refused with `ErrNotEnoughEnergy` when the player's energy is below the skill's cost, where it was refused for MP.
- The cost is spent when the skill starts, in `cast`, as MP was. `cast` checks the count again first, since a death or another spend can come between the press and the start. A skill short then doesn't start.
- A refusal spends nothing.

**Keeping:**
- **Death:** energy goes to 0 on death, and the respawn checkpoint carries 0.
- **Saving and loading:**
  - The checkpoint carries energy as it carried MP: on a map change, a disconnect, and the periodic save.
  - Join loads it, clamped to `max`.
- **Level-ups and promotions** restore HP only, and leave energy as it is. The level-up save stops sending MP.
- **No longer needed:**
  - The once-a-second regen tick handles HP only.
  - A change of gear or points no longer clamps MP.
- **Potions:** `use` restores HP only. `ErrMPFull` goes.

**Attack speed goes:**
- A basic attack lasts its class's length in ticks, `TicksIn` as a skill's does. `AttackLengthTicks` and `SkillLengthTicks` go.
- `AttackUsed.Length` stays, and is now the class's length in ticks.
- `progression` drops `AttackSpeed` from the derived stats, the worn stats, and the rates.
- **Three attacks are re-paced,** since their lengths counted their class's attack speed in (section 7): the Thief's 650 ms, cancelable from 0.8 of it, the Bowman's 1050 ms, and the Magician's 1250 ms.

**Haste's move speed** needs no new code on the server. A buff's `move_speed` already reaches the derived stats, and the movement sim reads move speed from them.

## 5. The wire and the database

**`client.proto`:**
- `CharacterView`: `mp` and `max_mp` become `energy`. The max comes from game data.
- `Delta`: gains `optional uint32 energy`. It is the recipient's energy as of `ack_seq`, set on the ticks it changes and on the first `Delta` after join.
  - "As of `ack_seq`" is as the client counts it. A spend for a skill pressed during a later input is added back, since the client still takes it off itself. A skill pressed by then and still waiting for its turn is taken off, since the client already took it off.
  - A press's input is the client's word, so the world holds it between the last input it stepped and two past the newest it received, as it does for casting. A made-up one can't keep a spend out of the count for long.
- `StatsChanged`: loses `mp` and `max_mp`, and is about HP and the sheet only.
- `DerivedStats`: loses `max_mp`, `mp_regen`, and `attack_speed`.
- `ItemStats`: loses `max_mp`, `mp_regen`, and `attack_speed`.

**Errors:**
- `REASON_NOT_ENOUGH_MP` becomes `REASON_NOT_ENOUGH_ENERGY`, and the gateway's `NOT_ENOUGH_MP` becomes `NOT_ENOUGH_ENERGY`.
- `REASON_MP_FULL` and `MP_FULL` go. The world's codes after `HP_FULL` move down one, from 330014, since an area's codes count up with no gap.

**Module contracts:**
- The character module: `Character` and `Checkpoint` carry `Energy`; `LevelUp` carries none.
- The world and item modules: the same changes as the client messages.

**Database** (each change is a migration, though a fresh database is fine before go-live):
- `characters.mp` goes, and `energy smallint` comes, never below 0. Its most is game data, which the world holds it to, so raising the max needs no migration. Every character starts at 0: nothing carries over from MP.
- `item_rolls` drops `max_mp`, `mp_regen`, and `attack_speed`.

The contract fixtures, the stat goldens, and the error codes are made again.

## 6. The client

**Its count of energy:**
- Energy comes from the server's count in each `Delta`, as of that frame's `ack_seq`.
- It shows that count less the cost of each of the player's own skill presses with a `seq` after `ack_seq`.
  - So a spend shows the moment it's pressed.
  - A `Delta` from before the spend can't refill the pips, since the press is still after its `ack_seq`.
  - Once a `Delta` acks the press, its count includes the spend, or doesn't if the server refused.
  - A refusal gives the press's energy back at once. A press with no answer stays taken off until a `Delta` acks it.
  - A `Welcome` sets the count. A new map's `Snapshot` drops the presses before it, since that map's frames count them.
- **Gains are not predicted.**
  - The hit lands at the attack's start, so the server's count reaches the client one round trip after the attack begins.
  - The earliest cancel is at least 400 ms after it starts: the Thief's 650 ms attack is 13 ticks, its cancel point is tick 10 (500 ms), and the early press is 100 ms.
  - So the gain is counted in time unless the round trip passes about 400 ms.
  - Predicting gains would need the client's own reach check against drawn mobs, and a rollback when the server disagreed.

**Using a skill:**
- `skill-keys` checks the shown count against the skill's cost, in place of MP. Short, it shows "Not enough energy" once per stretch, as it did for MP.
- It checks at the press's moment: a press held for an attack's cancel point is checked then, so it counts the energy that attack gave.
- A refusal from the server gives back the action and the energy.

**The HUD:**
- The MP bar becomes 10 pips under the HP bar, filled to the shown count.
- The skill bar labels each key with its cost and dims it when the count is short.

**Haste's move speed:**
- The client switches its predicted move speed at its own Haste's hit and back at the buff's end, as the server does: fast from the first input after the press, and back from the 401st.
- The end's speed is read as it applies, from the sheet the client has then, less the skills' share, plus what passives and other buffs add. Gear changed during Haste counts.
- A one-tick difference moves the player under 2 px, inside the 4 px reconcile tolerance.
- Buffs reach only their caster, so no other player's prediction changes.

**Gone:**
- The MP potion key (`mpPotion`, S in Classic and F in WASD) and its path in `potion-keys` and `potions.ts`.
- "Restores N MP" on item cards.
- The Max MP, MP regen, and attack speed rows in the character window and item stat lines.
- Attack speed in the action clock's attack length.
- MP in `stats.ts`.

## 7. Bots, testers, and balance

- **Bots** read energy from their `Delta` and use a skill when energy covers its cost. They already attack between skills.
- **Testers** get a stack of White Potions only. The README line about Mana Elixirs changes.
- **The balance harness** models HP and potions, not MP.
  - It ran again without attack speed. The Thief fought about 15% slower, the Bowman 10%, and the Magician 4%: seven rows past their bands, and four bosses cost the Thief 32 or 33 potions.
  - Re-paced (section 4), every class is back at its old mean hits at the reference pace, and every band and boss bound passes.
  - **Its skill rotation counts energy,** from none: a skill waits for its cooldown and its cost, and each basic attack gives 2. Warrior, Thief and Bowman skills wait on cooldowns, so their numbers don't move.
  - **The Magician's waited on energy** at 3 a skill: about 27% less damage without cancels, and a perfect chain adding 27–36%, past the 22% bound. At 2 (section 11), its chain adds 10–11%, and its damage with skills is 460 at L25 and 540 at L28.

## 8. Edge cases

| Case | What happens | Why it's fine |
| --- | --- | --- |
| An attack reaches several mobs | One gain of `per_attack` | It's per attack; a basic attack reaches one mob today anyway |
| An attack MISSes | It gains | Evasion is the mob's luck, not a whiff |
| A skill is pressed early, before the cancel point | Checked on arrival, and again at its start, where it's spent | The attack's gain landed at its start, before any press can cancel it |
| A round trip over 400 ms | A cancel can find the gain not yet counted on the client, and the key says not enough | Rare; the press works once the count arrives |
| The player dies with a skill pressed | The skill doesn't start; energy is 0 | Death ends actions as today |
| A map change mid-fight | Energy goes with the checkpoint | Players open the next map's pack with it |

## 9. Testing

- **Game data:**
  - the `energy` block and each refusal;
  - every active skill's cost;
  - a stage overriding a cost;
  - no MP or attack speed key accepted anywhere.
- **World:**
  - a gain from a hit and from a MISS, and none from a whiff or a skill;
  - the cap at `max`;
  - a refusal without enough, spending nothing;
  - the spend at the skill's start;
  - 0 on death, through respawn;
  - energy kept over a map change and a rejoin;
  - level-up and promotion leaving it alone;
  - an attack's length without attack speed;
  - Haste raising move speed.
- **Client:**
  - the pips;
  - the skill bar's cost and dimming;
  - a spend shown at once and not refilled by an older `Delta`, then confirmed;
  - a refused skill giving its energy back once acked;
  - Haste's predicted move speed.
- **End to end:**
  - a new character joining with none;
  - a skill refused at 0;
  - an attack filling energy and a skill spending it;
  - energy kept over a rejoin.

## 10. Build order

Two PRs, each step one commit, green on `make test`, `make lint`, `make cover`, and `make breaking`:

**PR 1, no attack speed:**
1. **Data and stats:** the curve, the gear lines, Haste's move speed, and the generator.
2. **Server, wire, and database:** attack length without attack speed, the `DerivedStats` and `ItemStats` fields, and the `item_rolls` column.
3. **Client:** the action clock, the stat rows, and Haste's predicted move speed.

**PR 2, energy:**
4. **Data:** the `energy` block and costs, with MP out of stats, items, drops, quests, and skills, and its validation and generator.
5. **Server, wire, and database:**
   - gaining, spending, and death;
   - the checkpoint and join;
   - the protos, `characters.energy`, and the error codes;
   - MP regen, potions, and clamps out.
6. **Client:** the count, the pips, the skill bar and keys, and the MP potion key out.
7. **Bots, testers, balance, and end to end.**

**Docs the build keeps true:** `stat-system.md` (INT, class bases, attack speed), `classes-and-skills.md` (`second_wind`), `equipment-and-item-details.md` (the rollable stats), `animation-cancel.md` and `combat-feel.md` (the attack length), and the README.

## 11. Decisions

Agreed with the user on 2026-11-13:

1. **Energy replaces MP.** It runs from 0 to 10, and nothing raises the 10.
2. **Basic attacks that reach a mob give 2 energy,** for now. A swing at nothing gives none.
3. **Skills cost 2 or 3:** 2 for the quick single-target skills, 3 for the rest. The cost doesn't grow with points.
4. **Energy is kept between fights and maps, and lost on death.**
5. **Cooldowns stay,** to tune after a playtest.
6. **INT keeps magic attack, magic defense, and cooldown speed.** MP gear lines and MP regen go, and Second Wind gives HP regen only.
7. **MP potions go.** Their drops go with nothing in their place, and the quests that gave them give Red Potions.
8. **Attack speed goes.** Haste gives move speed instead.
9. **The Thief's, Bowman's, and Magician's attacks are re-paced** to 650, 1050, and 1250 ms, so each class keeps its pace without attack speed. The Thief's cancels at 0.8 of its length, keeping a 150 ms window ([Animation cancel](./animation-cancel.md), section 13).
10. **Spark and Fire Arrow cost 2** (2026-11-14), so the Magician's 1:1 rate keeps cancel gain under the bound.

## 12. Alternatives considered

- **1 energy an attack** (the first proposal): the user chose 2 for now.
- **1 or 2 an attack, at random, or 2 on a crit:** the user chose a flat 2 for now.
- **Energy from any swing:** a player could fill up swinging at the air before a fight.
- **Energy draining out of combat:** more to explain, and opening a pack with a skill is part of the rhythm.
- **Keeping MP beside energy:** two resources for one choice, and potions back in the loop.
- **Haste giving stronger evasion only:** the user chose move speed, which fits its name.
- **Keeping the attack lengths:** the three classes that had attack speed would fight 4–15% slower, past their bands.
- **The client predicting gains:** it would need its own reach check and a rollback, while the server's count already arrives before the earliest cancel.
- **Energy in `StatsChanged`:** it carries no `ack_seq`, so a spend shown at once could be refilled by an update from before it.
