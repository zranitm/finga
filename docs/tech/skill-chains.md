# Skill chains

> **Status: approved · 2026-11-17.** The high-level design for [C18. Skill chains](../product/features/combat-and-progression/c18-skill-chains.md). It builds on [A fuller skill roster](./skill-roster.md), whose statuses, dodge, and lock it reads.

## 1. What has to hold

- **The user's words** (2026-11-17): "to avoid there are too many skills to use, we'll reuse the skill trigger mechanism of blade and soul, the next skill in the same skill slot will override if certain condition is met".
- **Their choices:**
  - chains fixed in game data per class;
  - all four conditions: right after a skill, target controlled, target near or far, right after a dodge;
  - roots dragged onto the six keys as now.
- **The client computes, the server validates.** The client decides which skill a key uses now, and draws it. The server checks the follow-up's condition, with slack, when it's used.
- **Chains are data.** Traits, which come next, can add or change follow-ups the same way.

## 2. Data

`classes.yaml`, under each class:

```yaml
chains:
  - root: strike
    follow:
      - {skill: ground_slam, when: {controlled: [stun, freeze]}}
      - {skill: charge, when: {far: 3}}
  - root: war_cry
    follow:
      - {skill: iron_body, when: {after: 3s}}
```

- **`when`** holds exactly one of:

  | Key | Holds while |
  | --- | --- |
  | `after: <time>` | Within `time` of the root's hit, on its key |
  | `controlled: [<status>, ...]` | The locked mob has one of these hard statuses from statuses.yaml (C19): stun, freeze, sleep, knockdown or pin. Each chain names what it waits for: one meant to follow Frost Nova lists `freeze` |
  | `far: <metres>` | The locked mob is farther than this across the ground |
  | `dodged: <time>` | Within `time` of the player's last dodge |
  | `controlled_self: true` | The player holds a hard status: stun, freeze, sleep, knockdown, or pin. Added on 2026-11-17 for the break-free skills ([Abnormal statuses](./statuses.md) section 13) |

- **A class's chains** are the only ones that apply to it.
  - A Warrior uses the Warrior's, which may root Beginner skills such as Strike.
  - A Beginner uses the Beginner's.
- **Validation** refuses:
  - a root or follow-up the class can't learn;
  - a passive in a chain;
  - a root used twice in a class's chains;
  - a follow-up that's its own root;
  - a `when` with no key or two keys;
  - a `controlled` naming no status, or one that isn't a hard status in statuses.yaml;
  - a follow-up listed twice in one chain;
  - `after` or `dodged` outside 0.2 to 5 s;
  - `far` at or below 0.
- **A follow-up may be listed in more than one chain.** It's one skill with one cooldown.
- **The generator** carries the chains to the client.

## 3. Which skill a key uses

On the client, `systems/chains.ts`, pure logic, decides each frame and as a key is pressed:
1. The key's root, from the skill bar.
2. Each of the root's follow-ups, in their listed order. The first that:
   - has at least one point;
   - is off cooldown;
   - and whose condition holds now,
   
   is the key's live skill.
3. Otherwise, the root.

What the client reads for each condition:

| Condition | Read from |
| --- | --- |
| `after` | The input the root was pressed during, plus its hit point in ticks, against the input now |
| `controlled` | `MobView.status` of the lock (skill-roster.md section 4) |
| `far` | The lock's drawn position against the player's feet |
| `dodged` | The input acked by the frame that carried the last "DODGE", against the input now |

- **A press** uses the live skill. `UseSkill` carries its id as today, and adds `chain_root`, the key's root.
- **Energy and cooldown** are the live skill's own, as a skill on its own key has them.

## 4. The world's check

When `UseSkill` names a skill that isn't the root, the world checks it, with slack:
- the class has a chain whose root is `chain_root`, and the skill is one of its follow-ups;
- the follow-up's condition held within the slack:

  | Condition | Server check | Slack |
  | --- | --- | --- |
  | `after` | The press's input is within `after` of the root's hit: the input the root was pressed during, plus its hit point | 150 ms either side |
  | `controlled` | The lock's status, from the lock id `UseSkill` already carries, holds or ended within the slack | 150 ms |
  | `far` | The lock is farther than `far` | 0.5 m |
  | `dodged` | The press's input is within `dodged` of the input the dodge's frame acked | +150 ms |

- **A failed check** is refused with `SKILL_CHAIN_NOT_READY`. The client gives back the action, cooldown, and energy, as for any refusal.
- **The world keeps** the tick of each player's last hit per skill, and of their last dodge. Statuses and the lock are already kept.
- **A follow-up has no hidden power:** a finisher is a skill as it is. Its condition only decides when the key offers it.

### As built (server half)

- **Windows count inputs, not the world's ticks.**
  - `after` runs from the input the root was pressed during, plus its hit point. `dodged` runs from the input acked by the frame that carried the dodge.
  - The client counts the same two numbers exactly, so latency drops out. Against the world's tick at arrival, a `dodged` press near the end of the client's window would be refused once round trip plus input buffer passed about 200 ms: the client sees the dodge a round trip late, and its press arrives later still.
  - The press's input is held between the world's latest and two past the newest it has (`pressedDuring`), so a client can't backdate a press.
  - The 150 ms slack stays, for rounding. `after` also allows it before the hit: a root whose cancel point is its hit point can be followed by a press that reaches the world up to 150 ms early.
- **The condition counts at the press.** A follow-up waiting for its turn isn't checked again as its turn comes.
- **A follow-up from its own key is refused** with `SKILL_CHAIN_NOT_READY`, so a client can't skip a condition by putting a follow-up on a key. A skill may sit on its own key when it roots one of its class's chains or is in none of them. An empty `chain_root` counts as the skill's own key.
- **`controlled`** reads the lock's statuses. Statuses keep the tick each one last ended, set wherever a status ends (its time, a shatter, a waking), for the slack. A death clears them.
- **`SKILL_CHAIN_NOT_READY`** is 330034, a 409.
- **The world takes one `SkillUse`** (the skill, `Root`, the aim, the lock and the way) from the gateway down to the instance.
- **The generator** writes each class's `chains`, with `after_ms`, `controlled`, `far` and `dodged_ms`, each null when absent.

## 5. The skill bar and window

- **`state/skill-bar.ts`** holds roots only. Dragging a follow-up onto a key is refused, and the key flashes red.
- **A key whose live skill is a follow-up:**
  - shows that skill's icon (C16), with a glowing frame in the skill's colour;
  - its cooldown sweep and count are that skill's;
  - its tooltip names it and its condition.
- **On promotion,** the class's chains replace the Beginner's. Keys holding a Beginner root keep it, and its follow-ups become the new class's.
- **The skill window** shows chains: each root, with its follow-ups and conditions in a list under it. Since traits, the window is a grid, each skill's card lists its chains, and a chains panel under the grid draws them with their conditions ([Skill traits](./skill-traits.md) section 7).

### As built (client half)

- **Two files.** `progression/chains.ts` reads the data: each class's chains, which skills may stand on a key of their own, and how a condition reads. `game/systems/chains.ts` judges a key now: `liveSkill`, `holds`, and `ChainMemory`. The features layer may not import from `game`, so the bar and the window read the first.
- **What the client counts, as the world does:**
  - `after`: each skill's last hit, noted at the press as the input the use goes with plus its hit point in ticks. A refusal forgets it; no answer keeps it, as it may have landed.
  - `dodged`: the input the newest frame acked when the player's own DODGE arrives. The world sends a tick's frame before its events, so that frame is the dodge's.
  - `controlled`: the lock's statuses as of the newest frame, running past its tick. `MobTracker` keeps them with `statusesOf`, shared with C19's B2.
  - `far`: the lock's drawn centre from the predicted feet, past the distance, with no slack.
  - A new map or a rejoin forgets the hits and the dodge, as the world does; a death doesn't.
- **A key's live skill** is judged every frame for the bar, on the lock as drawn. At a press it's judged on the lock the press will aim at. The bar store holds what each key offers now, beside its slots.
- **A follow-up on a key of its own** is refused before anything starts, with the world's words. A key can still hold one from a bar saved before the chains.
- **The bar:** a live follow-up shows its icon and its own cooldown, in a frame glowing its colour, and its tooltip says on whose key it comes and while what ("on Heavy Strike's key while the target is over 3 m away"). A follow-up dropped on a key flashes it red and leaves it as it was.
- **The window:** a root's card lists its follow-ups in order with their conditions (a list under its row until C20 PR 1). A follow-up's card says on whose keys it comes up, and that it can't go on a key of its own; one that roots a chain too says "Also on Blink's key right after a dodge." A tile that can't go on a key carries a chain mark and doesn't drag.
- **The first chains** are section 6's, but the Beginner's Roll Back gives Lunge, not Spark (decided 11-17): Spark is the Magician's promotion skill and would have been dodge-only. A real-data test checks each promotion's skills stand on a Beginner's key.
- **The bots** fight with their main skill only when their class lets it stand on a key, and send it as its own `chain_root`.
- **The harness** carries six keys per class: each a skill its class lets stand there, with its `after` follow-ups, the six whose strongest use deals the most. A far lock, a control or a dodge isn't modelled, so follow-ups waiting on one aren't counted. A follow-up goes only within its window from its root's hit, in both rotations. It plans points as a player building toward the promotion spends them, not the bots' point in every skill, which would spend energy on 1-point skills (skill-roster.md, as built for 5a).
- **End to end:**
  - a Warrior's Strike key gives Ground Slam only on a stunned lock, Charge at a lock 6 m off, then Ground Slam once Charge's stun shows;
  - a Bowman's Disengage key gives Power Shot only after a slime's bite passes through the dodge.
  - The tests pin hits and Charge's hit late in test data, so a slow runner's lagging inputs can't make Charge miss.

## 6. The first chains

They're C18's table, as data:
- the `after` windows are 3 s for buffs and 2 s for combos;
- `dodged` is 1.5 s;
- `far` is 3 m for melee roots and 10 m for Power Shot and Fire Arrow, with Lunge, Charge, and Shadow Step taking over beyond it;
- `controlled` is `[stun, freeze]` for Ground Slam and Frost Nova's Meteor, and `[stun, freeze, pin, knockdown]` for Arrow Rain and Fire Arrow's Meteor. Sleep is left out: a follow-up's first blow would wake it.

## 7. Testing

- **Data:** each refusal; a follow-up in two chains; the class's chains replacing the Beginner's.
- **`chains.ts`:**
  - each condition holding and not;
  - the listed order;
  - a follow-up with no points or on cooldown leaving the root;
  - the key going back when the condition ends.
- **World:**
  - each condition's check, at and just past its slack;
  - `SKILL_CHAIN_NOT_READY`;
  - a follow-up from the wrong root refused.
- **Client:**
  - the glowing frame and icon swap;
  - the tooltip;
  - refusing a follow-up dragged to a key;
  - promotion moving chains;
  - the window's grouping.
- **End to end:**
  - a Warrior's Strike key becomes Charge at a far lock, and Ground Slam on a stunned one;
  - a Bowman's Disengage key becomes Power Shot after a dodge.
- **Live:** every chain, for each class, takes over and goes back as listed.

## 8. Build

One PR after C17's mechanics (its PRs 1 to 4), before or with C17's roster PR, which can carry the chain data:
1. The `chains` data, its validation, and the generator.
2. The world's check and `chain_root`.
3. `chains.ts`, the skill bar and window, and the glowing frame.

## 9. Decisions

The user's choices are in section 1. These calls were made in planning and logged for the user to push back on:

1. **Chains are per class.** A promoted class's chains replace the Beginner's, so a Warrior's Strike key turns into Charge, where a Beginner's turns into Lunge.
2. **The first chains** are C18's table: they reuse C17's skills, and add none.
3. **A follow-up comes up only with a point and off cooldown.** A key never shows a skill it can't use.
4. **A follow-up may appear in more than one chain,** with one cooldown.
5. **The windows:** 2 s for combos, 3 s for buffs, and 1.5 s after a dodge, with 150 ms and 0.5 m of slack on the server.
6. **The live key gets a glowing frame,** since the user's taste is for no cues before an action, but a key that changed has to be seen to be used.

## 10. Alternatives considered

- **Players building chains:** the user chose fixed chains.
- **Fixed bars per class:** the user kept dragging roots onto keys.
- **The server choosing the live skill:** a round trip on every press, and a key that changes late. The client decides, and the server checks.
- **Follow-ups stronger on their condition** (a bonus while the target is controlled): left for traits, which can add such effects.
