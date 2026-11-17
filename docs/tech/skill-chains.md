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
      - {skill: ground_slam, when: {controlled: [stun]}}
      - {skill: charge, when: {far: 3}}
  - root: war_cry
    follow:
      - {skill: iron_body, when: {after: 3s}}
```

- **`when`** holds exactly one of:

  | Key | Holds while |
  | --- | --- |
  | `after: <time>` | Within `time` of the root's hit, on its key |
  | `controlled: [stun \| root \| knockdown]` | The locked mob has one of these statuses. Frost Nova's freeze is a `stun` drawn as ice |
  | `far: <metres>` | The locked mob is farther than this across the ground |
  | `dodged: <time>` | Within `time` of the player's last dodge |

- **A class's chains** are the only ones that apply to it.
  - A Warrior uses the Warrior's, which may root Beginner skills such as Strike.
  - A Beginner uses the Beginner's.
- **Validation** refuses:
  - a root or follow-up the class can't learn;
  - a passive in a chain;
  - a root used twice in a class's chains;
  - a follow-up that's its own root;
  - a `when` with no key or two keys;
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
| `after` | The time of the root's last hit on the client's `ActionClock` |
| `controlled` | `MobView.status` of the lock (skill-roster.md section 4) |
| `far` | The lock's drawn position against the player's feet |
| `dodged` | The last "DODGE" the player's own client showed |

- **A press** uses the live skill. `UseSkill` carries its id as today, and adds `chain_root`, the key's root.
- **Energy and cooldown** are the live skill's own, as a skill on its own key has them.

## 4. The world's check

When `UseSkill` names a skill that isn't the root, the world checks it, with slack:
- the class has a chain whose root is `chain_root`, and the skill is one of its follow-ups;
- the follow-up's condition held within the slack:

  | Condition | Server check | Slack |
  | --- | --- | --- |
  | `after` | The player's last hit of `chain_root` was within `after` | +150 ms |
  | `controlled` | The lock's status, from the lock id `UseSkill` already carries, holds or ended within the slack | 150 ms |
  | `far` | The lock is farther than `far` | 0.5 m |
  | `dodged` | The player's last dodge was within `dodged` | +150 ms |

- **A failed check** is refused with `SKILL_CHAIN_NOT_READY`. The client gives back the action, cooldown, and energy, as for any refusal.
- **The world keeps** the tick of each player's last hit per skill, and of their last dodge. Statuses and the lock are already kept.
- **A follow-up has no hidden power:** a finisher is a skill as it is. Its condition only decides when the key offers it.

## 5. The skill bar and window

- **`state/skill-bar.ts`** holds roots only. Dragging a follow-up onto a key is refused, and the key flashes red.
- **A key whose live skill is a follow-up:**
  - shows that skill's icon (C16), with a glowing frame in the skill's colour;
  - its cooldown sweep and count are that skill's;
  - its tooltip names it and its condition.
- **On promotion,** the class's chains replace the Beginner's. Keys holding a Beginner root keep it, and its follow-ups become the new class's.
- **The skill window** shows chains: each root, with its follow-ups and conditions in a list under it. The skill window's redesign, which comes with traits, keeps this grouping.

## 6. The first chains

They're C18's table, as data:
- the `after` windows are 3 s for buffs and 2 s for combos;
- `dodged` is 1.5 s;
- `far` is 3 m for melee roots and 10 m for Power Shot and Fire Arrow, with Lunge, Charge, and Shadow Step taking over beyond it;
- `controlled` is `[stun]` for Ground Slam and Frost Nova's Meteor, and `[stun, root, knockdown]` for Arrow Rain and Fire Arrow's Meteor.

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
