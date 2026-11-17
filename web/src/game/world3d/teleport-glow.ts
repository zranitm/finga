// The teleport channel's glow (teleport.md section 4): two rings in the portals' blue rising from the player's feet, narrowing and fading, one after the other, while the channel runs.
import { animate, ring } from '../effects/draw';
import type { Fx } from '../effects/fx';
import type { Point, Stop } from '../effects/types';

const COLOR = 0x7ac7ff; // the portals' blue
const RISE_MS = 900; // a ring's climb
const RISE_M = 1.8;
const RADIUS_M = 0.75;

/** Glows round feet() till until, or till the Stop it returns; nothing shows while feet() has no place. */
export function teleportGlow(fx: Fx, until: number, feet: () => Point | undefined): Stop {
  const from = fx.now();
  const rings = [ring(fx, COLOR, 0.07), ring(fx, COLOR, 0.05)];
  return animate(fx, from, until, rings, (t) => {
    const at = feet();
    rings.forEach((m, i) => {
      const k = ((t - from) / RISE_MS + i / rings.length) % 1;
      if (!at) {
        m.visibility = 0;
        return;
      }
      const r = RADIUS_M * (1 - 0.4 * k);
      m.position.set(at.x, at.y + 0.05 + k * RISE_M, at.z);
      m.scaling.set(r, 1, r);
      m.visibility = 0.9 * (1 - k);
    });
  });
}
