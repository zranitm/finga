// A moving skill's way (skill-roster.md section 3), worked out as the client starts the skill; the world takes it as sent, its length held to the skill's distance.
import { movement } from '@/gamedata/movement';
import type { Move } from '@/progression/skills';
import type { Input } from '@/sim/input';
import { blink, dash, DIAGONAL } from '@/sim/step';
import type { Mobs, State, Way } from '@/sim/step';

import type { Mark } from './aim';
import type { Mark as MoveMark } from './prediction';

interface Unit {
  readonly x: number;
  readonly z: number;
}

/** How far past the lock's far contact ring a blink at it aims, as the world aims it, so it lands clear of the body however the sums round. */
const PAST_RING = 0.05;

/** The unit way from the feet to the lock's middle, and how far it is; null with none, or with it underfoot. */
function toLock(s: State, lock: Mark | null): (Unit & { readonly d: number }) | null {
  if (!lock) {
    return null;
  }
  const dx = lock.x - s.x;
  const dz = lock.z - s.z;
  const d = Math.hypot(dx, dz);
  return d > 1e-9 ? { x: dx / d, z: dz / d, d } : null;
}

/** The way the held keys walk, as the sim heads them; null with none. */
function keysWay(keys: Input | null): Unit | null {
  if (!keys) {
    return null;
  }
  let x = (keys.right ? 1 : 0) - (keys.left ? 1 : 0);
  let z = (keys.up ? 1 : 0) - (keys.down ? 1 : 0);
  if (x !== 0 && z !== 0) {
    x = x * DIAGONAL;
    z = z * DIAGONAL;
  }
  return x !== 0 || z !== 0 ? { x, z } : null;
}

/** Where a moving skill goes from s, with the keys held and the locked mob: at the lock to its near contact ring, or for a blink just past its far one, beyond the distance if the lock is within it; away from it, or back from the heading with none; or along the keys, back from the heading with none held. A lock skill with no lock goes along the heading. */
export function wayFor(move: Move, s: State, keys: Input | null, lock: Mark | null): Way {
  const at = toLock(s, lock);
  const back = { x: -s.hx, z: -s.hz };
  const along = (u: Unit, length: number): Way => ({
    x: u.x,
    z: u.z,
    length: Math.min(length, move.distance),
  });
  switch (move.toward) {
    case 'lock': {
      if (!at || !lock) {
        return along({ x: s.hx, z: s.hz }, move.distance);
      }
      const ring = lock.radius + movement.radius;
      if (move.kind === 'blink') {
        return at.d <= move.distance
          ? { x: at.x, z: at.z, length: at.d + ring + PAST_RING }
          : along(at, move.distance);
      }
      return along(at, Math.max(0, at.d - ring));
    }
    case 'back':
      return along(at ? { x: -at.x, z: -at.z } : back, move.distance);
    default:
      return along(keysWay(keys) ?? back, move.distance);
  }
}

/** The move as the predictor starts it, from the input after the press: a blink at once, or a dash or leap over its ticks' sub-steps. */
export function moveStart(move: Move, way: Way, steps: number): MoveMark['start'] {
  return move.kind === 'blink'
    ? (s, g, p) => blink(s, way, g, p)
    : (s, _g, p) => dash(s, way, steps, mobsOf(move), move.kind === 'leap', p);
}

/** How move meets mobs' bodies. */
function mobsOf(move: Move): Mobs {
  return move.through ? 'through' : move.stop ? 'stop' : 'slide';
}
