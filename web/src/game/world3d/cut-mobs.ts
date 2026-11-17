// The mobs the place opens round besides the player (3d-world.md section 10), so a bank between the camera and them doesn't hide them or their rings.
import type { Mark } from '../systems/aim';

/** How many mobs the place opens round at once: the place's materials have a slot for each. */
export const CUT_MOBS = 4;
const ATTACK_M = 8; // an attacking mob this near the feet, past a spitter's 7.5 m reach
const NEAR_M = 4; // and any mob this near

/** The living mobs to open the place round, seen from feet (x, z): the locked one, then those attacking within ATTACK_M, then any within NEAR_M, nearest first in each, a tie to the lower id; CUT_MOBS at most. */
export function cutMobs(
  x: number,
  z: number,
  locked: number | null,
  attacking: (id: number) => boolean,
  mobs: readonly Mark[],
): Mark[] {
  const away = (m: Mark) => Math.hypot(m.x - x, m.z - z);
  const rank = (m: Mark) =>
    m.id === locked ? 0 : attacking(m.id) && away(m) <= ATTACK_M ? 1 : away(m) <= NEAR_M ? 2 : 3;
  return mobs
    .filter((m) => m.hp > 0 && rank(m) < 3)
    .sort((a, b) => rank(a) - rank(b) || away(a) - away(b) || a.id - b.id)
    .slice(0, CUT_MOBS);
}
