# Buffs and hidden cooldowns on the frame

> **Status: approved · 2026-11-18.** The high-level design for [C22. Buffs and hidden cooldowns on the frame](../product/features/combat-and-progression/c22-frame-icons.md). It extends the status icons of [Abnormal statuses](./statuses.md) section 6 on the player's frame, and reads the chains of [Skill chains](./skill-chains.md).

> **Changed by [Large regions](./large-regions.md) section 7:** while walk mode is on, a walk icon comes first in the player's own row, before the buffs.

## 1. What has to hold

- **The user's words** (2026-11-18): "the cooldown of chained skills (hidden after normal circumstance) should show as buff/debuff so that player can monitor the cd of them".
- **Their choice:** the player's own buffs show in the same row, since they show nowhere today.
- **The client computes, the server validates.** The client already counts every cooldown it starts, for its keys. It can't know a buff a party member's skill put on it, so the server lists the buffs on each player.
- **One row, three kinds,** each with its own look, beside the statuses the frames already show.

## 2. The row

- **`features/hud/StatusIcons.tsx`** becomes the frame's icon row, `FrameIcons`. On the player's own frame it shows three kinds, in order: buffs, statuses, then hidden cooldowns. Party frames and the target window keep statuses alone.
- **Each kind:**

  | Kind | Tile | Edge | Count under it | Sweep |
  | --- | --- | --- | --- | --- |
  | Buff | the skill's icon at its stage, in its colour (C16) | the ally green | seconds left | drains as it runs out |
  | Status | as today | its status's colour | seconds left | as today |
  | Cooldown | the skill's icon, dimmed | grey | seconds to ready | the skill bar's dark sweep |
  | Walk mode (large-regions.md section 7) | the walk icon on slate, first while walk mode is on | the text colour | none | none |

- **Order within a kind:** soonest to end first. Statuses keep hard control first.
- **Room:** up to 12 of each kind, then "+N" for that kind (the user's call of 2026-11-18). The row wraps onto more lines inside the frame's width. Party frames and the target window keep statuses to six.
- **Hover** shows a small tooltip, as statuses have:
  - a buff: "Iron Body: 12 s left", with "from <name>" when a party member gave it;
  - a cooldown: "Ground Slam: ready in 7 s. On Strike's key while the target is stunned or frozen.", the condition in `conditionText`'s words.

## 3. Which cooldowns show

A skill shows as a hidden cooldown while all of these hold:
- it is a follow-up in one of the class's chains (`chainsOf`) whose root sits on one of the skill bar's keys;
- it has at least one point, or is granted;
- its cooldown is running, in the skill bar store's `cooldowns`;
- no key shows it now: neither as a root on its own key, nor as what a key offers (the store's `live`).

So:
- **A break-free skill** shows through its 32 s. While a hard status stops the player acting, its key shows it cooling (skill-chains.md section 3), so it leaves the row then.
- **A block's counter** shows, as does any other follow-up.
- **A skill that roots a chain and is a follow-up too,** such as Power Shot, shows on its own key if it sits on one, and in the row only if it doesn't.
- It leaves the row as its cooldown ends.
- It's worked out each frame from the bar store, with no new state.

## 4. Buffs on the sheet

- **`StatSheet` gains `repeated BuffView buffs`:** each buff running on the player, with `skill`, `left_ms`, `points`, and `from`, the caster's character id, empty for the player's own.
  - The world fills it from the player's buffs wherever it builds a sheet: `Welcome`, `LevelUp`, `StatsChanged`.
  - A buff's start and end already restat the player and send the sheet (`restat`, `expireBuffs`), so the list is always current, a shield-only buff included.
- **The world's `buff`** gains `from`, set in `cast` for every player a buff reaches.
- **Across a join:** a map change hands the giver on with the buff's timer, as it does a shield's pool. A checkpoint saves neither, so after a relog a party member's buff shows with no giver, as the player's own does. A party buff lasts seconds, so a column for a tooltip's name isn't worth it.
- **The client** keeps the list in the character store, each with when it ends by the local clock: `left_ms` from the sheet's arrival. The row counts down from there. A buff used again comes with a new sheet, so it starts over.
- **The client's own buff timing** for the stat preview (classes-and-skills.md section 6) stays as it is. The row reads the sheet's list, so a player's own buff shows a round trip after the press, as the world confirms it.
- **Names** for `from` come from the party list. One not in the party anymore reads "an ally".

### As built

- **The world:**
  - `buff.from` is set in `cast`. It rides on `BuffTimer.From` through `timersOf` and `startTimers`, so a map change keeps it.
  - `Instance.stats` lists the buffs on every sheet it builds, through `BuffViewsOf`. The join's Welcome lists them the same way, from the timers it joined with.
  - A giver equal to the player is sent empty.
- **The wire:**
  - `BuffView.left_ms` is an `int64`, as `SkillTimer`'s is.
  - The gateway model keeps `Left` as a duration and turns it into milliseconds at the wire, as it does for skill timers.
- **The client:**
  - **The store:** `character.buffs` holds `FrameBuff`s from each sheet: `buffsOf` in `state/character.ts`.
    - A buff the list had, from the same giver and points and ending within 300 ms of the new end, keeps its times. So a sheet sent for anything else, such as gear, leaves its sweep alone.
    - A new one sweeps from its stage's whole duration, so one joined with part-way shows part-way.
  - **The rule:** `features/hud/hiddenCooldowns.ts` works out section 3 on each render from the bar store's slots, `live` and cooldowns. A follow-up that two keys' chains share shows once, and its tooltip names both keys.
  - **The row:** `FrameIcons` takes `own`, the player's buffs and hidden cooldowns, on their frame alone.
    - **Room:** with `own`, the row shows up to twelve of each kind, the user's call of 2026-11-18. Other frames show six statuses.
    - **Names:** the player's row is "Buffs, statuses and cooldowns", and another frame's is "Statuses". Each kind's "+N" on the player's row says "N more buffs", "N more statuses" or "N more cooldowns".
  - **The tooltips:** "War Cry: 8 s left, from Robin" and "Ground Slam: ready in 7 s. On Strike's key while the target is stunned or frozen." Each counts its seconds as the pointer comes onto the tile.
  - **The looks:**
    - a buff's edge is 2 px of the ally green;
    - a cooldown's is 2 px of the muted grey, its face greyed to 70% and darkened, with the bar's 60% sweep.
- **The real render,** in headless Chrome:
  - a Warrior's own frame with two buffs, two statuses and three hidden cooldowns: Charge and Ground Slam off Strike's key, and Unyielding through its 30 s;
  - the same with fourteen buffs and nine statuses: twelve buffs and "+2", every status, then the cooldowns, wrapping over three lines inside the frame.

## 5. Protocol

| Message | Direction | Change |
| --- | --- | --- |
| `StatSheet` | server | Gains `buffs`: `BuffView{skill, left_ms, points, from}` |

## 6. Testing

- **World:** the sheet lists the player's own buff and a party member's War Cry with its `from`; the list after a buff ends; a shield-only Barrier listed.
- **Client:**
  - the row's order and three looks;
  - "+N" per kind and the wrap;
  - a hidden cooldown appearing as its follow-up is used, gone when it ends, and not shown while a key shows the skill;
  - a break-free shown through its cooldown and gone from the row while its key shows it;
  - a buff from the sheet counting down, with its tooltip and giver.
- **Live:** a Warrior chains Strike into Ground Slam and watches Ground Slam's cooldown in the row until Strike's key offers it again; a party's War Cry shows on each member's frame.

## 7. Build

One PR: the sheet's `buffs` and the world's `from`, then the client's row.

## 8. Decisions

The user's choices are in section 1. These calls were made in planning and logged for the user to push back on:

1. **The row is on the player's own frame only.** Party frames and the target window keep statuses alone.
2. **Buffs come from the server's sheet,** not the client's own timing, so a party member's buff shows too.
3. **Only follow-ups of the keys' chains show as cooldowns,** and only while no key shows them: everything else already shows on a key or doesn't matter to the bar.
4. **Party frames and the target window keep six statuses,** as they're smaller. The player's own row takes 12 of each kind, as the user asked.
5. **Order:** buffs, then statuses, then cooldowns, each soonest to end first.

## 9. Alternatives considered

- **A cooldown strip above the skill bar:** the user asked for the cooldowns to show as buffs and debuffs do.
- **Small cooldown dots on the root's key:** six keys with up to three follow-ups each would crowd the bar.
- **The client tracking party buffs from `SkillUsed`:** it can't know whom a buff reached; the world decides that.
