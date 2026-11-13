// Every effect by name, each replaceable through the effects manifest.
import { classes } from '@/gamedata/classes';
import { mobs } from '@/gamedata/mobs';
import { skills } from '@/gamedata/skills';

export type EffectId =
  | `hit.${'physical' | 'magic' | 'crit' | 'miss'}`
  | `attack.${string}`
  | `skill.${string}`
  | 'cancel.afterimage'
  | `mob.${string}`;

/** A class's basic attack; a class this client doesn't know draws as the first one's, as its timing does. */
export function attackEffect(cls: string): EffectId {
  return `attack.${(classes.find((c) => c.id === cls) ?? classes[0]).id}`;
}

export function skillEffect(skill: string): EffectId {
  return `skill.${skill}`;
}

/** A mob attack's look, by the attack's name in mobs.yaml; a mob may change its values but not its kind. */
export function mobEffect(attack: string): EffectId {
  return `mob.${attack}`;
}

export type Damage = 'physical' | 'magic';

/** A data file's damage type; anything but magic hits as physical. */
export function damageType(d: string | null): Damage {
  return d === 'magic' ? 'magic' : 'physical';
}

/** What a Damage event plays on its target: a puff for a miss, else a spark of the source's damage type, a crit's on top. */
export function hitEffects(
  damage: Damage,
  d: { readonly crit?: boolean; readonly miss?: boolean },
): EffectId[] {
  if (d.miss) {
    return ['hit.miss'];
  }
  return d.crit ? [`hit.${damage}`, 'hit.crit'] : [`hit.${damage}`];
}

/** Every effect the game plays: the hits, a cancel's afterimage, each class's attack, each active skill's and each mob attack's look. */
export const effectIds: readonly EffectId[] = [
  'hit.physical',
  'hit.magic',
  'hit.crit',
  'hit.miss',
  'cancel.afterimage',
  ...classes.map((c) => attackEffect(c.id)),
  ...skills.filter((s) => s.kind === 'active').map((s) => skillEffect(s.id)),
  ...new Set(mobs.flatMap((m) => m.attacks.map((a) => mobEffect(a.name)))),
];
