// Each mob attack's look, by its name (3d-world.md section 10): a red area on the ground filling over the windup, in the attack's shape as the world fixed it then; then its kind's travel and impact, and a spit's balls in flight.
import { Vector3 } from '@babylonjs/core/Maths/math.vector';
import type { Mesh } from '@babylonjs/core/Meshes/mesh';

import { mobs } from '@/gamedata/mobs';

import { groundUnder } from '../world3d/map';

import { animate, Band, ball, beatsOf, clamp01, Patch, ring } from './draw';
import { mobEffect } from './ids';
import type { EffectId } from './ids';
import { stream } from './particles';
import type { Drawn, PlayOptions, Point } from './types';

const RED = 0xff3b30;
const FX = 0xfff4d6;
const SPIT = 0xc8e6a0; // an untinted mob's spit
const FRONT = (90 * Math.PI) / 180; // a bite's arc
const ZONE_ALPHA = 0.22; // the whole area through the windup
const FILL_ALPHA = 0.45; // what has filled of it

type Kind = (typeof mobs)[number]['attacks'][number]['kind'];

/** The attack's area on the ground's plane at (u, v), each from 0 to 1, filled out to `fill` of it: u across, v outward or along. */
function areaOf(
  kind: Kind,
  o: PlayOptions,
): (u: number, v: number, fill: number) => { x: number; z: number } {
  const r = o.size?.radius ?? 0.5;
  const reach = r + (o.reach ?? 0);
  const at = o.to && kind === 'leap' ? o.to : o;
  const fwd = { x: Math.sin(o.yaw), z: Math.cos(o.yaw) };
  const circle = (from: number, span: number) => (u: number, v: number, fill: number) => {
    const a = from + span * u;
    const d = reach * v * fill;
    return { x: at.x + Math.sin(a) * d, z: at.z + Math.cos(a) * d };
  };
  const lane = (length: number, width: number) => (u: number, v: number, fill: number) => {
    const d = length * v * fill;
    const side = (u - 0.5) * width;
    return { x: at.x + fwd.x * d + fwd.z * side, z: at.z + fwd.z * d - fwd.x * side };
  };
  switch (kind) {
    case 'front':
      return circle(o.yaw - FRONT / 2, FRONT);
    case 'rush': {
      const to = o.to ?? o;
      return lane(Math.hypot(to.x - o.x, to.z - o.z) + r, o.width ?? 2 * r);
    }
    case 'shot':
      return lane(o.reach ?? 0, o.width ?? 0.4);
    default: // a leap's landing and a slam's circle
      return circle(0, 2 * Math.PI);
  }
}

/** A mob attack of kind, round the mob: its area through the windup, its impact after its travel, and a spit's balls while they fly. */
function mobLook(kind: Kind): Drawn {
  return (fx, o, now) => {
    const b = beatsOf(o, now);
    const r = o.size?.radius ?? 0.5;
    const reach = r + (o.reach ?? 0);
    const round = kind === 'front' || kind === 'leap' || kind === 'around';
    const [nu, nv] = round ? [kind === 'front' ? 12 : 32, 6] : [2, 16];
    const place = areaOf(kind, o);
    const zone = new Patch(fx, RED, nu, nv);
    const fill = new Patch(fx, RED, nu, nv);
    zone.lay((u, v) => place(u, v, 1), o.y);
    const impact: { mesh: Mesh; at: Point } | null =
      kind === 'leap' || kind === 'around'
        ? { mesh: ring(fx, FX, 0.06), at: kind === 'leap' && o.to ? o.to : o }
        : null;
    const bite = kind === 'front' ? new Band(fx, FX) : null;
    const dustAt = new Vector3(o.x, o.y, o.z);
    const dust =
      kind === 'rush'
        ? stream(fx, dustAt, {
            rate: 50,
            color: FX,
            size: [0.3, 0.5],
            life: [0.25, 0.4],
            speed: [0.2, 0.6],
            rise: 0.5,
            blend: 'over',
          })
        : null;
    const balls: Mesh[] = [];
    const parts = [
      zone,
      fill,
      ...(impact ? [impact.mesh] : []),
      ...(bite ? [bite] : []),
      { dispose: () => dust?.() },
      { dispose: () => balls.forEach((m) => m.dispose()) },
    ];
    return animate(fx, now, b.end, parts, (t) => {
      const winding = t < b.hit;
      zone.mesh.visibility = winding ? ZONE_ALPHA : 0;
      fill.mesh.visibility = winding ? FILL_ALPHA : 0;
      if (winding) {
        const k = Math.max(0.02, clamp01((t - b.start) / Math.max(1, b.hit - b.start)));
        fill.lay((u, v) => place(u, v, k), o.y);
      }
      const travelling = t >= b.hit && t < b.strikeEnd;
      const body = o.follow?.();
      if (dust && body) {
        dustAt.set(body.x, body.y, body.z);
      }
      if (dust && !travelling && t >= b.strikeEnd) {
        dust(); // the rush is over: its dust goes
      }
      const p = (t - b.strikeEnd) / Math.max(1, b.readyAt - b.strikeEnd);
      const hitting = p >= 0 && p < 1;
      if (impact) {
        const s = reach * (kind === 'leap' ? 0.6 + 0.4 * p : 0.5 + 0.5 * p);
        impact.mesh.position.set(
          impact.at.x,
          groundUnder(fx.map, impact.at.x, impact.at.z, o.y) + 0.05,
          impact.at.z,
        );
        impact.mesh.scaling.set(s, 1, s);
        impact.mesh.visibility = hitting ? 1 - p : 0;
      }
      if (bite) {
        bite.root.position.set(o.x, o.y + (o.size?.height ?? 1) / 2, o.z);
        bite.root.rotation.set(0, o.yaw, 0);
        bite.draw(-FRONT / 2, FRONT / 2, reach, () => 0.18, hitting ? 1 - p : 0);
      }
      const shots = o.shots?.() ?? [];
      while (balls.length < shots.length) {
        balls.push(ball(fx, o.color ?? SPIT, 1, 'over'));
      }
      balls.forEach((m, i) => {
        const s = shots[i];
        m.setEnabled(s !== undefined);
        if (s) {
          m.position.set(s.x, s.y, s.z);
          m.scaling.setAll(s.size);
        }
      });
    });
  };
}

/** Every mob attack's look, by name, drawn as its kind. */
export function mobLooks(): [EffectId, Drawn][] {
  const kinds = new Map<string, Kind>();
  for (const m of mobs) {
    for (const a of m.attacks) {
      kinds.set(a.name, a.kind);
    }
  }
  return [...kinds].map(([name, kind]) => [mobEffect(name), mobLook(kind)]);
}
