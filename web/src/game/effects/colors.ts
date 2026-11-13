// Each look's colour: a skill's, which is also the caster's flash at its strike, and a basic attack's brighter slash.
import type { Skill, Stage } from '@/progression/skills';

// Each apart from the slash's pale cream and from the forest's grass.
const colors = { physical: 0xff6a3d, magic: 0xb070f0, heal: 0x4fd8ff, buff: 0xf0b23a } as const;
export const slashColors = { physical: 0xfff6d8, magic: 0xd0a0ff } as const; // brighter: a basic attack shows no box

export function colorOf(skill: Skill, stage: Stage): number {
  if (skill.effect === 'heal' || skill.effect === 'buff') {
    return colors[skill.effect];
  }
  return stage.damage === 'magic' ? colors.magic : colors.physical;
}
