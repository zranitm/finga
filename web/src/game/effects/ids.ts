// Every effect by name, each replaceable through the effects manifest.
import { classes } from '@/gamedata/classes';
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

/** Every effect the game plays: the hits, each class's attack and each active skill's look. */
export const effectIds: readonly EffectId[] = [
  'hit.physical',
  'hit.magic',
  'hit.crit',
  'hit.miss',
  ...classes.map((c) => attackEffect(c.id)),
  ...skills.filter((s) => s.kind === 'active').map((s) => skillEffect(s.id)),
];
