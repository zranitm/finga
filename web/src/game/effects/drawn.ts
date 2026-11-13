// Each effect's drawn version, by id: what plays wherever the manifest names no art.
import { classes } from '@/gamedata/classes';
import { skillById } from '@/progression/skills';
import type { Skill } from '@/progression/skills';

import { attackEffect, effectIds, skillEffect } from './ids';
import type { EffectId } from './ids';
import { colorOf, drawShape, drawShot, shapeAt, slashAt, slashColors } from './shapes';
import type { Drawn, Stop } from './types';

const none: Stop = () => undefined;

type Attack = (typeof classes)[number]['attack'];

/** A class's basic attack: a melee slash through its box, timed to its beats, or a shot that flies its range. */
function attacking(a: Attack): Drawn {
  if (a.kind === 'ranged') {
    return (scene, o) => drawShot(scene, o, a.range, a.damage === 'magic');
  }
  return (scene, o, now) =>
    o.beats ? drawShape(scene, slashAt(o, a, slashColors[a.damage]), o.beats, now) : none;
}

/** A skill's shape at the tier's stage, timed to its beats. */
function shaping(skill: Skill): Drawn {
  return (scene, o, now) => {
    const stage = skill.stages[o.tier ?? 0] ?? skill.stages[0];
    const e = stage && shapeAt(stage.shape, o, colorOf(skill, stage));
    return e && o.beats ? drawShape(scene, e, o.beats, now) : none;
  };
}

const drawn = new Map<EffectId, Drawn>();
for (const c of classes) {
  drawn.set(attackEffect(c.id), attacking(c.attack));
}
for (const id of effectIds) {
  const skill = id.startsWith('skill.') ? skillById(id.slice('skill.'.length)) : undefined;
  if (skill) {
    drawn.set(skillEffect(skill.id), shaping(skill));
  }
}

export function drawnVersion(id: EffectId): Drawn | undefined {
  return drawn.get(id);
}
