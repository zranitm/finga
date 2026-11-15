// Auto-aim (3d-world.md section 6): an action turns to the nearest living mob within its reach and the cone ahead; the world takes the aim as sent and doesn't check the pick.
import { actions } from '@/gamedata/actions';

const CONE = (actions.aim_cone * Math.PI) / 180;

/** A mob as drawn, which an action may aim at. */
export interface Mark {
  readonly id: number;
  readonly x: number;
  readonly z: number;
  readonly radius: number;
  readonly hp: number;
}

/** What an action reaches, in metres: an arc's radius, a shot's range or an area's radius; nothing for a heal, which aims at no one. */
export function reachOf(
  shape: { readonly kind: string; readonly radius?: number; readonly range?: number } | null,
  heals = false,
): number {
  if (!shape || heals) {
    return 0;
  }
  return (shape.kind === 'shot' ? shape.range : shape.radius) ?? 0;
}

/** The mob an action reaching `reach` from feet (x, z), heading along yaw, aims at: the nearest living one whose body it reaches, within the cone either side of the heading; a tie goes to the smaller angle. */
export function aimAt(
  x: number,
  z: number,
  yaw: number,
  reach: number,
  mobs: Iterable<Mark>,
): Mark | null {
  let best: Mark | null = null;
  let bestD = Infinity;
  let bestA = Infinity;
  for (const m of mobs) {
    const dx = m.x - x;
    const dz = m.z - z;
    const d = Math.hypot(dx, dz);
    if (m.hp <= 0 || d > reach + m.radius) {
      continue;
    }
    const to = Math.atan2(dx, dz);
    const a = d === 0 ? 0 : Math.abs(Math.atan2(Math.sin(to - yaw), Math.cos(to - yaw)));
    if (a > CONE) {
      continue;
    }
    if (d < bestD || (d === bestD && a < bestA)) {
      best = m;
      bestD = d;
      bestA = a;
    }
  }
  return best;
}

/** The yaw from feet (x, z) to mark m, or yaw itself with none, or with m right underfoot. */
export function aimYaw(x: number, z: number, yaw: number, m: Mark | null): number {
  if (!m || (m.x === x && m.z === z)) {
    return yaw;
  }
  return Math.atan2(m.x - x, m.z - z);
}
