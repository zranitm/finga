// A basic attack's paler slash; a skill's colour is colorOf in progression/skills, which its icon shares.
export const slashColors = { physical: 0xfff6d8, magic: 0xd0a0ff } as const;

/** A dark edge under a stroke, so a pale one shows on pale ground too. */
export const EDGE = 0x1d1a16;

/** color mixed toward white by share. */
export function brighter(color: number, share: number): number {
  const ch = (shift: number) => {
    const c = (color >> shift) & 0xff;
    return Math.round(c + (0xff - c) * share) << shift;
  };
  return ch(16) | ch(8) | ch(0);
}
