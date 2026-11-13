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

/** Every effect the game plays: each class's attack and each active skill's look. */
export const effectIds: readonly EffectId[] = [
  ...classes.map((c) => attackEffect(c.id)),
  ...skills.filter((s) => s.kind === 'active').map((s) => skillEffect(s.id)),
];
