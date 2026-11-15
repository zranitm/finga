// Each look's colour: a skill's, and a basic attack's paler slash.
import type { Skill, Stage } from '@/progression/skills';

// Each apart from the slash's pale cream and from the forest's grass.
const colors = { physical: 0xff6a3d, magic: 0xb070f0, heal: 0x4fd8ff, buff: 0xf0b23a } as const;
export const slashColors = { physical: 0xfff6d8, magic: 0xd0a0ff } as const;

/** A dark edge under a stroke, so a pale one shows on pale ground too. */
export const EDGE = 0x1d1a16;

export function colorOf(skill: Skill, stage: Stage): number {
  if (skill.effect === 'heal' || skill.effect === 'buff') {
    return colors[skill.effect];
  }
  return stage.damage === 'magic' ? colors.magic : colors.physical;
}

/** color mixed toward white by share. */
export function brighter(color: number, share: number): number {
  const ch = (shift: number) => {
    const c = (color >> shift) & 0xff;
    return Math.round(c + (0xff - c) * share) << shift;
  };
  return ch(16) | ch(8) | ch(0);
}
