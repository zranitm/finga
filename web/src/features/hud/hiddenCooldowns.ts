// The follow-up cooldowns the keys hide (frame-icons.md section 3), worked out from the bar as it stands, with no state of their own.
import { chainsOf } from '@/progression/chains';
import type { ChainCondition } from '@/progression/chains';
import { pointsIn, skillById } from '@/progression/skills';
import type { Cooldown } from '@/state/skill-bar';

/** A follow-up cooling while no key shows it: the keys it comes up on, each root with its condition, and its cooldown. */
export interface HiddenCooldown {
  readonly skill: string;
  readonly on: readonly { readonly root: string; readonly when: ChainCondition }[];
  readonly cooldown: Cooldown;
}

/** cls's follow-ups of chains rooted on a key, with a point or granted, cooling, and on no key now, neither standing on one nor offered by one; soonest ready first. */
export function hiddenCooldowns(
  cls: string,
  slots: readonly string[],
  live: readonly string[],
  cooldowns: Readonly<Record<string, Cooldown>>,
  invested: Readonly<Record<string, number>>,
): HiddenCooldown[] {
  const found = new Map<string, HiddenCooldown>();
  for (const c of chainsOf(cls)) {
    if (!slots.includes(c.root)) {
      continue;
    }
    for (const f of c.follow) {
      const s = skillById(f.skill);
      const cooldown = cooldowns[f.skill];
      if (
        !s ||
        !cooldown ||
        pointsIn(s, invested) < 1 ||
        slots.includes(f.skill) ||
        live.includes(f.skill)
      ) {
        continue;
      }
      const on = [...(found.get(f.skill)?.on ?? []), { root: c.root, when: f.when }];
      found.set(f.skill, { skill: f.skill, on, cooldown });
    }
  }
  return [...found.values()].sort((a, b) => a.cooldown.until - b.cooldown.until);
}
