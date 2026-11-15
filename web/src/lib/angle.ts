/** from turned toward to by share of the way, the short way round; radians. */
export function turn(from: number, to: number, share: number): number {
  const d = Math.atan2(Math.sin(to - from), Math.cos(to - from));
  return from + d * share;
}
