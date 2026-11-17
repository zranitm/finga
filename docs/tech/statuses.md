# Abnormal statuses

> **Status: approved · 2026-11-17.** The high-level design for [C19. Abnormal statuses](../product/features/combat-and-progression/c19-statuses.md). It replaces [A fuller skill roster](./skill-roster.md) section 4 ("Control on mobs" and "Poison") with one system for mobs and players, and takes the place of C17's PR 3.

## 1. What has to hold

- **The user's words** (2026-11-17): "we'll need to define a set of abnormal status, any suggestions?".
- **Their choices:**
  - all four groups: hard control, soft control, damage over time, and weakened;
  - mobs inflict a few on players;
  - a brief immunity after a hard control.
- **The server applies and decides everything:** a status starts, ticks, and ends on the world's ticks. The client draws marks and timers, and predicts nothing it can't know.
- **Statuses are data.** Their numbers live in `statuses.yaml`, and skills and mobs name them by id, so traits can swap one status for another.

## 2. Data

`server/data/statuses.yaml` (new):

```yaml
hard_immunity: 2s            # after a hard status ends, no hard status starts on that body for this long
super_armor: {hard: immune, others: 0.5}   # elites and bosses: hard statuses don't take; the rest last half as long
statuses:
  - {id: stun,      group: hard,   blocks: [move, act]}
  - {id: freeze,    group: hard,   blocks: [move, act], shatter: 1.5}     # the next hit ends it, ×1.5
  - {id: sleep,     group: hard,   blocks: [move, act], wakes: true}      # any damage ends it
  - {id: knockdown, group: hard,   blocks: [move, act]}                   # the down pose; rising armour as today
  - {id: pin,       group: hard,   blocks: [move]}
  - {id: slow,      group: soft,   move_speed: 0.6}                       # a factor
  - {id: silence,   group: soft,   blocks: [skills]}
  - {id: blind,     group: soft,   accuracy: 0.5}
  - {id: poison,    group: dot,    every: 1s}
  - {id: burn,      group: dot,    every: 500ms}
  - {id: bleed,     group: dot,    every: 1s, moving: 2.0}
  - {id: armor_break, group: weaken, physical_defense: 0.7, magic_defense: 0.7}
  - {id: weaken,    group: weaken, attack: 0.75}
  - {id: taunt,     group: aggro}                                         # mobs only
  - {id: lose_sight, group: aggro}                                        # mobs only
```

- **A status's time and power come from what inflicts it,** not from the status. `power` is for damage over time: each tick deals power × the inflicter's attack of its damage type at the time it was inflicted.
- **Skills** (`skills.yaml`):
  - C17 PR 1's `control` becomes `status: {id, time, power}`, on what the skill hits.
  - `push` becomes its own field, `push: {distance}`, since it moves rather than lasts.
  - C17's `poison` buff becomes `on_hit: {status, time, power}`: while the buff lasts, each of the user's hits inflicts it.
  - A stage may change any of them, as now.
- **Mob attacks** (`mobs.yaml`) gain an optional `status: {id, time, power}` on what the attack hits. The first are section 7's.
- **Validation** refuses:
  - an unknown id;
  - `taunt` or `lose_sight` on a mob's attack;
  - a time at or below 0;
  - a damage-over-time status without power, or power on any other.

## 3. The world

### One status list per body

Mobs (`application/mobs/`) and players (`instance/`) each hold their statuses. Each has:
- an id;
- an end tick;
- for damage over time, the inflicter, its attack, its damage type, its power, and its next tick.

C17's mob-only poses and statuses fold into this list. The flinch, and the knockdown's rising armour, stay poses.

### Inflicting

When a hit lands and names a status, in order:
1. **Super armor:** a hard status doesn't take. Any other's time is halved.
2. **Hard immunity:** a hard status doesn't take while the body's immunity runs.
3. **Same id:**
   - a status already running restarts with the new time;
   - damage over time is the exception: it restarts per inflicter, and runs beside others' of the same id.
4. **Otherwise** it starts.

### Effects, each tick

| Effect | On a mob | On a player |
| --- | --- | --- |
| `blocks: move` | No walking, chasing, or moves | The sim's `State.Held` (section 4) |
| `blocks: act` | No windups, and a windup in progress is cancelled | No attack or skill; a press is refused with `CANT_ACT`, an action in progress ends as at a death |
| `blocks: skills` | — (mobs have no skills) | Skills refused with `SILENCED`; basic attacks go |
| `move_speed` | Speed × factor | The sim's `State.Slow` (section 4) |
| `accuracy`, `attack`, defenses | × factor, in `Resolve` | × factor, in `Resolve` |
| `shatter` | The next hit that lands ends it and is × shatter | The same |
| `wakes` | Any damage ends it | The same |
| damage over time | Each `every`, the tick's damage, credited to its inflicter, never a MISS or a crit | The same, credited to the mob |
| `moving` (bleed) | × moving if the mob moved since the last tick | × moving if the player moved since |
| `taunt` | Its provoker is the inflicter (C17) | — |
| `lose_sight` | Drops the inflicter, and can't see them for the time (C17) | — |

- **Ending:** when a hard status ends by time, by a shatter, or by waking, the body's hard immunity runs for `hard_immunity`.
- **Knockdown** keeps today's down pose, its rising, and its brief armour. On a player, it's the same: 1 s down, then up.
- **Death** clears every status. So does a map change, for a player.

## 4. Players' movement under a status

The server applies statuses on its ticks, and the client can't predict them. The sims carry them as state, so a reconcile steps them the same:
- **`State.Held`:** sub-steps left of no walking and no jumping. The heading is held, and gravity runs.
- **`State.Slow`:** sub-steps left, and a factor on the walk speed.
- **Starting:** the world sets them from the status's end tick, for the input it steps next.
- **`PlayerView`** carries both, so the client's reconcile resets to them.
- **The cost:** a player stunned by a mob is corrected once on their own screen, as a knockback already is today.
- **Vectors** in `server/testdata/movement`:
  - held, with keys and jump pressed;
  - held in mid-air, falling straight;
  - slow at its factor;
  - both running out mid-press.

## 5. Protocol

- **`MobView.status`** (C17's bit set) and a new **`PlayerView.statuses`** become a list of `{id, ends}`, the end as a tick, sent when it changes. The client counts the time left from the tick.
- **`Damage`** gains `status_id`, set on a damage-over-time tick, so its number draws in that status's colour.
- **New codes:** `CANT_ACT` and `SILENCED`, public, as other refusals.
- **All additive,** so `make breaking` passes. C17's `status` bit set is reserved.

## 6. Client

- **Refusals:** the action clock refuses an attack or skill while the player holds a hard status, and a skill while silenced, before sending, with the same messages. The server is the check.
- **Marks** on units, one per status, with its effect id (C13):
  - stars;
  - an ice shell;
  - "Zzz";
  - roots at the feet;
  - a blue trail;
  - a crossed-out mark;
  - a dark haze;
  - green bubbles;
  - flames;
  - red drips;
  - a cracked shield;
  - a drooping sword;
  - a red mark for taunt, and smoke for lose sight.
  - Hard immunity shows as a faint white outline.
- **Icons:** a game-icons.net icon per status (C16), shown with its time left:
  - on the target window (locked mob), beside the stats line;
  - on the player's own frame;
  - on the party's frames.
  - Up to six show; the rest as "+N".
- **Damage-over-time numbers** are small, in the status's colour.
- **Telegraphs:** a mob attack that inflicts a status tints its red area by the status, a blue edge for slow for example, so the player knows what it does before it lands.

## 7. First uses

**Skills,** in C17's terms:

| Skill | Status |
| --- | --- |
| Charge | stun 1.5 s |
| Frost Nova | freeze 2 s |
| Pinning Shot | pin 3 s |
| Ground Slam, Meteor | knockdown |
| Taunt | taunt 5 s |
| Smoke Bomb | lose_sight 2 s |
| Poison Blade | `on_hit: poison 4 s, power 0.2` |
| Fire Arrow's evolution | burn 2 s, power 0.3 |
| Double Stab's evolution | bleed 4 s, power 0.15 |
| Power Shot's evolution | blind 3 s |
| Slash Blast's evolution | armor_break 5 s |

Weaken has no skill yet. It's ready for traits.

**Mobs:** each family below gets a status on one of its attacks, as data. The build picks the attack per family, and each stays under the field bosses' 30-potion bound:

| Mobs | Status |
| --- | --- |
| Every field boss's slam | stun 1 s |
| The icy maps' spitters (the icewing and its kin) | slow 2 s |
| The poisonous: the mushrooms' and moths' bites | poison 4 s, power 0.15 |
| The desert's chargers (the scarab and its kin) | bleed 3 s, power 0.15 |
| The keep's shades | silence 2 s |

## 8. Balance

- **The rotation harness** counts damage over time for the skills that inflict it, and shatter for a freeze followed by a hit. Control and weakening aren't modelled; the playtest judges them.
- **The bands** don't move. The boss potion bound is run again with the bosses' 1 s stun, and must still pass.

## 9. Testing

- **Data:** each refusal; stage overrides of `status` and `on_hit`.
- **World:**
  - each effect of section 3's table, on a mob and a player;
  - super armor;
  - hard immunity after each way a hard status ends;
  - restarting, and damage over time per inflicter;
  - credit, kills, and boss shares;
  - shatter and waking;
  - bleed while moving;
  - death and map change clearing;
  - `CANT_ACT` and `SILENCED`.
- **Sim:** section 4's vectors, on Go and TypeScript, on both machines.
- **Client:**
  - marks and icons with timers;
  - "+N";
  - damage-over-time numbers;
  - tinted telegraphs;
  - the client's own refusals.
- **End to end:**
  - a boss's slam stuns a player, who's held and refused, then immune for 2 s;
  - Frost Nova freezes, and the next hit shatters for 1.5×.
- **Live:** each status, from a skill and from its mobs, and the boss bound.

## 10. Build

It replaces C17's PR 3, and can go beside C17's PR 2 and PR 4. C17's PR 5 (the 26 skills) comes after it.

1. **Data:** `statuses.yaml`, `status`, `push`, and `on_hit` in place of `control` and `poison`, the mobs' `status`, and validation.
2. **World:** the status list, inflicting, effects, immunity, super armor, and damage over time.
3. **Players' movement:** `State.Held`, `State.Slow`, their vectors, and `PlayerView`.
4. **Protocol and client:** the lists, `Damage.status_id`, the codes, marks, icons, timers, and tinted telegraphs.

**Docs the build keeps true:**
- this page;
- [A fuller skill roster](./skill-roster.md), whose section 4 points here;
- Systems → Combat: the statuses' rules.

## 11. Decisions

The user's choices are in section 1. These calls were made in planning and logged for the user to push back on:

1. **The set and numbers** of section 2: slow 40%, blind halves accuracy, armour break −30%, weaken −25%, and freeze shatters for 1.5×.
2. **Pin is hard control:** elites and bosses shrug it off, and it starts the immunity.
3. **Super armor:** immune to hard control, with the rest at half time.
4. **The mobs' first statuses** are section 7's, and keep the bosses' potion bound.
5. **The skills' first statuses** are section 7's, with four skills gaining one at their evolution.
6. **Statuses on players are corrected, not predicted,** as knockback is.

## 12. Alternatives considered

- **Statuses stacking in strength:** hard to read and to balance. A restart is clear.
- **Diminishing durations instead of immunity:** a second stun at half time still chains. A clean 2 s window is easier to read.
- **Bosses taking hard control at reduced time:** a party could chain them. Immunity keeps boss fights about dodging.
- **The client predicting statuses on the player:** it can't know them before the server, and a wrong guess would be worse than one correction.
