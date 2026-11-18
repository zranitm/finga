// Each mob attack's look, by its name (3d-world.md section 10): a boss's area on the ground filling over the windup, red if a block doesn't stop it and yellow if it does (blocking.md section 2), in the attack's shape as the world fixed it then, held till the world says it acted; then its impact, a charge's dust, and a spit's balls in flight. Other mobs draw no area: their attacks are read from their bodies. The area and the balls, its warnings, show faintly where the place hides them.
import { Vector3 } from '@babylonjs/core/Maths/math.vector';
import type { Mesh } from '@babylonjs/core/Meshes/mesh';

import { mobs } from '@/gamedata/mobs';

import { groundUnder } from '../world3d/map';

import { animate, Band, ball, beatsOf, clamp01, Line, Patch, ring } from './draw';
import type { Fx } from './fx';
import { mobEffect } from './ids';
import type { EffectId } from './ids';
import { stream } from './particles';
import type { Drawn, PlayOptions, Point } from './types';

/** Each area's pale colour over the whole of it and its fill growing over that, each glowing on the ground so it lightens it as it colours it, the fill far enough from the area to follow on any ground; its rim, strong enough to hold on snow and sand; and where a status's edge runs, in metres out from the rim: a red area's inside it, as its outermost line is always its red rim and the yellow family never borders it. */
const AREAS = {
  red: { zone: 0xff8a7a, fill: 0xff5040, fillAlpha: 0.85, rim: 0xff3a2a, edgeOut: -0.3 },
  yellow: { zone: 0xffe870, fill: 0xffd43a, fillAlpha: 0.75, rim: 0xffc21a, edgeOut: 0.12 },
} as const;
const ZONE_ALPHA = 0.4;
const RIM_R = 0.065;
const FX = 0xfff4d6;
const SPIT = 0xc8e6a0; // an untinted mob's spit
const FRONT = (90 * Math.PI) / 180; // a bite's arc
const EDGE_R = 0.04; // a status's edge
const EDGE_LIFT_M = 0.05;

type Kind = (typeof mobs)[number]['attacks'][number]['kind'];

/** The attack's area on the ground's plane at (u, v), each from 0 to 1: u across, v outward or along, each of its columns straight; grown by `grow` metres all round, or shrunk where it's negative. */
function areaOf(
  kind: Kind,
  o: PlayOptions,
  grow = 0,
): (u: number, v: number) => { x: number; z: number } {
  const r = o.size?.radius ?? 0.5;
  const reach = r + (o.reach ?? 0) + grow;
  const at = o.to && kind === 'leap' ? o.to : o;
  const fwd = { x: Math.sin(o.yaw), z: Math.cos(o.yaw) };
  const circle =
    (from: number, span: number, inner = 0) =>
    (u: number, v: number) => {
      const wide = span < 2 * Math.PI ? grow / reach : 0;
      const a = from - wide + (span + 2 * wide) * u;
      const d = Math.max(0, inner - grow) + (reach - Math.max(0, inner - grow)) * v;
      return { x: at.x + Math.sin(a) * d, z: at.z + Math.cos(a) * d };
    };
  const lane = (length: number, width: number) => (u: number, v: number) => {
    const d = -grow + (length + 2 * grow) * v;
    const side = (u - 0.5) * (width + 2 * grow);
    return { x: at.x + fwd.x * d + fwd.z * side, z: at.z + fwd.z * d - fwd.x * side };
  };
  switch (kind) {
    case 'front': // from the body's edge, as it hides what's inside
      return circle(o.yaw - FRONT / 2, FRONT, r);
    case 'rush': {
      const to = o.to ?? o;
      return lane(Math.hypot(to.x - o.x, to.z - o.z) + r, o.width ?? 2 * r);
    }
    case 'shot': // to where its ball stops, the lane's tip at the ball's front
      return lane((o.reach ?? 0) + (o.width ?? 0) / 2, o.width ?? 0.4);
    default: // a leap's landing and a slam's circle
      return circle(0, 2 * Math.PI);
  }
}

/** The area's outline on the ground, nu by nv cells of place: a circle's rim, or round the whole of any other shape. */
function rimOf(
  fx: Fx,
  place: (u: number, v: number) => { readonly x: number; readonly z: number },
  [nu, nv]: readonly [number, number],
  circle: boolean,
  y: number,
): Point[] {
  const at = (i: number, j: number): Point => {
    const p = place(i / nu, j / nv);
    return { x: p.x, y: groundUnder(fx.map, p.x, p.z, y) + EDGE_LIFT_M, z: p.z };
  };
  if (circle) {
    return Array.from({ length: nu + 1 }, (_, i) => at(i, nv));
  }
  const rim: Point[] = [];
  for (let j = 0; j <= nv; j++) {
    rim.push(at(0, j));
  }
  for (let i = 1; i <= nu; i++) {
    rim.push(at(i, nv));
  }
  for (let j = nv - 1; j >= 0; j--) {
    rim.push(at(nu, j));
  }
  for (let i = nu - 1; i >= 0; i--) {
    rim.push(at(i, 0));
  }
  return rim;
}

/** A mob attack of kind, round the mob: a boss's area through the windup, rimmed in its colour and edged by the rim in the colour of the status it inflicts, its impact after its travel, and a spit's balls while they fly. */
function mobLook(kind: Kind): Drawn {
  return (fx, o, now) => {
    const b = beatsOf(o, now);
    const r = o.size?.radius ?? 0.5;
    const reach = r + (o.reach ?? 0);
    const round = kind === 'front' || kind === 'leap' || kind === 'around';
    const [nu, nv] = round ? [kind === 'front' ? 12 : 32, 6] : [2, 16];
    const place = areaOf(kind, o);
    const shade = o.area && AREAS[o.area];
    const area = shade && {
      zone: new Patch(fx, shade.zone, nu, nv, 'glow', ZONE_ALPHA), // the whole area through the windup
      fill: new Patch(fx, shade.fill, nu, nv, 'glow', shade.fillAlpha),
    };
    area?.zone.lay(place, o.y);
    const closed = kind === 'leap' || kind === 'around';
    const rim = shade ? rimOf(fx, place, [nu, nv], closed, o.y) : [];
    const rimLine = shade ? new Line(fx, shade.rim, rim.length, RIM_R, 'over', true) : null;
    const edged = shade && o.edge !== undefined;
    const by = edged ? rimOf(fx, areaOf(kind, o, shade.edgeOut), [nu, nv], closed, o.y) : [];
    const edge = edged ? new Line(fx, o.edge, by.length, EDGE_R, 'over', true) : null;
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
      ...(area ? [area.zone, area.fill] : []),
      ...(impact ? [impact.mesh] : []),
      ...(bite ? [bite] : []),
      ...(rimLine ? [rimLine] : []),
      ...(edge ? [edge] : []),
      { dispose: () => dust?.() },
      { dispose: () => balls.forEach((m) => m.dispose()) },
    ];
    return animate(fx, now, b.end, parts, (t) => {
      const landed = o.landed ? (o.landed() ?? Infinity) : b.strikeEnd;
      const warning = t < (kind === 'shot' ? b.hit : landed); // a spit's ball is the warning once thrown
      area?.zone.show(warning);
      area?.fill.show(warning);
      rimLine?.draw(rim, warning ? 1 : 0);
      edge?.draw(by, warning ? 1 : 0);
      if (warning && area) {
        const k = Math.max(0.02, clamp01((t - b.start) / Math.max(1, b.hit - b.start)));
        area.fill.cover(area.zone, k); // full from the hit, held till its result
      }
      const travelling = t >= b.hit && t < b.strikeEnd;
      const body = o.follow?.();
      if (dust && body) {
        dustAt.set(body.x, body.y, body.z);
      }
      if (dust && !travelling && t >= b.strikeEnd) {
        dust(); // the rush is over: its dust goes
      }
      const p = (t - landed) / Math.max(1, b.readyAt - b.strikeEnd);
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
        const b = ball(fx, o.color ?? SPIT, 1, 'over');
        fx.ghost(b, o.color ?? SPIT, 'over'); // a thrown spit's warning
        balls.push(b);
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
