// Aim (target-lock.md section 4): an action aims at the lock, a mob or an enemy, else along the heading; the world takes the aim as sent and doesn't check it.

/** A mob or an enemy player as drawn, which the lock may hold and an action aim at. */
export interface Mark {
  /** Its ref as the world reads a lock: "m:<id>" for a mob, "p:<id>" for a player. */
  readonly id: string;
  readonly x: number;
  readonly z: number;
  readonly radius: number;
  readonly hp: number;
  /** Its middle is on screen, inside the canvas less a margin: the lock may pick it (target-lock.md section 12). */
  readonly inView: boolean;
  /** Its middle is anywhere on the canvas: a lock on it holds. */
  readonly inFrame: boolean;
}

/** The yaw from feet (x, z) to mark m, or yaw itself with none, or with m right underfoot. */
export function aimYaw(x: number, z: number, yaw: number, m: Mark | null): number {
  if (!m || (m.x === x && m.z === z)) {
    return yaw;
  }
  return Math.atan2(m.x - x, m.z - z);
}
