// Each status's mark (statuses.md section 6): drawn on the unit it runs on, following its body, in the status's colour, until the status ends and the mark is stopped.
import { Vector3 } from '@babylonjs/core/Maths/math.vector';

import { statuses } from '@/gamedata/statuses';
import { statusColor } from '@/progression/statuses';

import { animate, Band, ball, Line, ring } from './draw';
import type { Fx } from './fx';
import { statusEffect } from './ids';
import type { EffectId } from './ids';
import { stream } from './particles';
import type { Stream } from './particles';
import type { Body, Drawn, PlayOptions, Point } from './types';

const OVER_HEAD_M = 0.3; // a mark over the head, above the body's top
const forever = Infinity; // a mark runs till its status ends and it's stopped

/** The body a mark is on, as drawn now. */
function bodyOf(o: PlayOptions): Body {
  return o.follow?.() ?? { ...o, radius: o.size?.radius ?? 0.5, height: o.size?.height ?? 1 };
}

/** The flat way to the screen's right at p, and the yaw that turns a standing mark there to face the camera. */
function facing(
  fx: Fx,
  p: Point,
): { readonly x: number; readonly z: number; readonly yaw: number } {
  const c = fx.scene.activeCamera?.position;
  const [dx, dz] = c ? [c.x - p.x, c.z - p.z] : [0, -1];
  const d = Math.hypot(dx, dz) || 1;
  return { x: -dz / d, z: dx / d, yaw: Math.atan2(dx, dz) };
}

/** A point in the camera's plane through c: `right` across the screen, `up` metres up. */
const inPlane = (c: Point, f: { x: number; z: number }, right: number, up: number): Point => ({
  x: c.x + f.x * right,
  y: c.y + up,
  z: c.z + f.z * right,
});

const overHead = (b: Body): Point => ({ x: b.x, y: b.y + b.height + OVER_HEAD_M, z: b.z });

/** Stars circling over the head: a stun, or a knockdown's daze. */
function stars(color: number): Drawn {
  return (fx, o, now) => {
    const balls = [0, 1, 2].map(() => ball(fx, color, 0.12));
    return animate(fx, now, forever, balls, (t) => {
      const b = bodyOf(o);
      const r = Math.max(0.25, b.radius * 0.7);
      balls.forEach((m, i) => {
        const a = ((t - now) / 1200) * 2 * Math.PI + (i * 2 * Math.PI) / 3;
        m.position.set(
          b.x + Math.sin(a) * r,
          b.y + b.height + 0.15 + 0.04 * Math.sin(2 * a),
          b.z + Math.cos(a) * r,
        );
      });
    });
  };
}

/** A pale shell of ice round the body. */
function ice(color: number): Drawn {
  return (fx, o, now) => {
    const shell = ball(fx, color, 1, 'over');
    return animate(fx, now, forever, [shell], (t) => {
      const b = bodyOf(o);
      shell.position.set(b.x, b.y + b.height / 2, b.z);
      shell.scaling.set(b.radius * 2.6, b.height * 1.15, b.radius * 2.6);
      shell.visibility = 0.35 + 0.05 * Math.sin((t - now) / 300);
    });
  };
}

/** "Zzz": three Zs rising in turn over the head, facing the camera. */
function zzz(color: number): Drawn {
  return (fx, o, now) => {
    const zs = [0, 1, 2].map(() => new Line(fx, color, 4, 0.018));
    return animate(fx, now, forever, zs, (t) => {
      const top = overHead(bodyOf(o));
      const f = facing(fx, top);
      zs.forEach((z, i) => {
        const k = ((t - now) / 1500 + i / 3) % 1;
        const s = 0.06 + 0.06 * k; // half its size
        const c = inPlane(top, f, 0.1 + 0.2 * k, 0.4 * k);
        const p = (u: number, v: number) => inPlane(c, f, u * s, v * s);
        z.draw([p(-1, 1), p(1, 1), p(-1, -1), p(1, -1)], k < 0.8 ? 1 : (1 - k) / 0.2);
      });
    });
  };
}

/** Roots growing up round the feet. */
function roots(color: number): Drawn {
  return (fx, o, now) => {
    const n = 5;
    const lines = Array.from({ length: n }, () => new Line(fx, color, 3, 0.03, 'over'));
    const base = ring(fx, color, 0.08, 'over');
    return animate(fx, now, forever, [...lines, base], (t) => {
      const b = bodyOf(o);
      const grow = Math.min(1, (t - now) / 250);
      const r = b.radius + 0.1;
      base.position.set(b.x, b.y + 0.04, b.z);
      base.scaling.set(r, 1, r);
      lines.forEach((l, i) => {
        const a = (i / n) * 2 * Math.PI + 0.4;
        const at = (d: number, h: number) => ({
          x: b.x + Math.sin(a) * d,
          y: b.y + h * grow,
          z: b.z + Math.cos(a) * d,
        });
        l.draw([at(r + 0.15, 0), at(r + 0.05, 0.25), at(r - 0.08, 0.45)], 1);
      });
    });
  };
}

/** Particles given off from a point that follows the body: where(b) puts it. */
function streaming(color: number, s: Omit<Stream, 'color'>, where: (b: Body) => Point): Drawn {
  return (fx, o, now) => {
    const b0 = bodyOf(o);
    const p0 = where(b0);
    const at = new Vector3(p0.x, p0.y, p0.z);
    const stop = stream(fx, at, {
      ...s,
      color,
      ...(s.around && {
        around: { radius: b0.radius * s.around.radius, height: b0.height * s.around.height },
      }),
    });
    return animate(fx, now, forever, [{ dispose: stop }], () => {
      const p = where(bodyOf(o));
      at.set(p.x, p.y, p.z);
    });
  };
}

const middle = (b: Body): Point => ({ x: b.x, y: b.y + b.height / 2, z: b.z });
const head = (b: Body): Point => ({ x: b.x, y: b.y + b.height * 0.85, z: b.z });

/** A blue trail left behind at the feet, and a faint ring round them. */
function trail(color: number): Drawn {
  const dust = streaming(
    color,
    { rate: 20, size: [0.12, 0.22], speed: [0, 0.1], life: [0.5, 0.9], spread: 0.25 },
    (b) => ({ x: b.x, y: b.y + 0.1, z: b.z }),
  );
  return (fx, o, now) => {
    const stopDust = dust(fx, o, now);
    const base = ring(fx, color, 0.05);
    return animate(fx, now, forever, [base, { dispose: stopDust }], () => {
      const b = bodyOf(o);
      const s = b.radius + 0.15;
      base.position.set(b.x, b.y + 0.05, b.z);
      base.scaling.set(s, 1, s);
      base.visibility = 0.6;
    });
  };
}

/** A crossed-out circle over the head, facing the camera: no skills. */
function crossed(color: number): Drawn {
  return (fx, o, now) => {
    const circle = ring(fx, color, 0.3);
    const bar = new Line(fx, color, 2, 0.022);
    return animate(fx, now, forever, [circle, bar], () => {
      const c = overHead(bodyOf(o));
      const f = facing(fx, c);
      const s = 0.14;
      circle.position.set(c.x, c.y, c.z);
      circle.rotation.set(Math.PI / 2, f.yaw, 0); // stood up, facing the camera
      circle.scaling.setAll(s);
      bar.draw([inPlane(c, f, -s * 0.7, s * 0.7), inPlane(c, f, s * 0.7, -s * 0.7)], 1);
    });
  };
}

/** A broken hoop of armour round the chest, turning slowly. */
function cracked(color: number): Drawn {
  return (fx, o, now) => {
    const pieces = [0, 1, 2].map(() => new Band(fx, color, 6));
    return animate(fx, now, forever, pieces, (t) => {
      const b = bodyOf(o);
      pieces.forEach((p, i) => {
        p.root.position.set(b.x, b.y + b.height * 0.6, b.z);
        p.root.rotation.set(0, (t - now) / 1500, 0);
        const a = (i * 2 * Math.PI) / 3;
        p.draw(a + 0.25, a + (2 * Math.PI) / 3 - 0.25, b.radius + 0.2, () => 0.07, 0.85);
      });
    });
  };
}

/** A sword drooping over the head, facing the camera: a weaker attack. */
function drooping(color: number): Drawn {
  return (fx, o, now) => {
    const blade = new Line(fx, color, 2, 0.025);
    const guard = new Line(fx, color, 2, 0.02);
    return animate(fx, now, forever, [blade, guard], (t) => {
      const c = overHead(bodyOf(o));
      const f = facing(fx, c);
      const d = 0.6 + 0.12 * Math.sin((t - now) / 400); // its tilt from straight down
      const along = (k: number) => inPlane(c, f, Math.sin(d) * k, -Math.cos(d) * k);
      const hilt = along(-0.15);
      blade.draw([hilt, along(0.2)], 1);
      const g = along(-0.11);
      const across = (k: number) => inPlane(g, f, Math.cos(d) * k, Math.sin(d) * k);
      guard.draw([across(-0.07), across(0.07)], 1);
    });
  };
}

/** A red "!" over the head, pulsing: it's after its taunter. */
function warned(color: number): Drawn {
  return (fx, o, now) => {
    const bar = new Line(fx, color, 2, 0.03);
    const dot = ball(fx, color, 0.07);
    return animate(fx, now, forever, [bar, dot], (t) => {
      const c = overHead(bodyOf(o));
      const s = 1 + 0.15 * Math.sin((t - now) / 150);
      bar.draw(
        [
          { ...c, y: c.y + 0.22 * s },
          { ...c, y: c.y + 0.04 * s },
        ],
        1,
      );
      dot.position.set(c.x, c.y - 0.04 * s, c.z);
      dot.scaling.setAll(s);
    });
  };
}

/** A faint ring over the head: a status with no mark of its own. */
function halo(color: number): Drawn {
  return (fx, o, now) => {
    const r = ring(fx, color, 0.06);
    return animate(fx, now, forever, [r], () => {
      const c = overHead(bodyOf(o));
      r.position.set(c.x, c.y, c.z);
      r.scaling.setAll(0.2);
    });
  };
}

const marks: Readonly<Record<string, (color: number) => Drawn>> = {
  stun: stars,
  freeze: ice,
  sleep: zzz,
  knockdown: stars, // dazed on the ground, as its down pose shows
  pin: roots,
  slow: trail,
  silence: crossed,
  blind: (c) =>
    streaming(
      c,
      {
        rate: 16,
        size: [0.25, 0.4],
        speed: [0.02, 0.1],
        life: [0.6, 1],
        spread: 0.2,
        blend: 'over',
      },
      head,
    ),
  poison: (c) =>
    streaming(
      c,
      {
        rate: 14,
        size: [0.06, 0.14],
        speed: [0.05, 0.15],
        life: [0.5, 0.9],
        rise: 0.8,
        around: { radius: 0.8, height: 0.7 },
      },
      middle,
    ),
  burn: (c) =>
    streaming(
      c,
      {
        rate: 35,
        size: [0.12, 0.28],
        speed: [0.1, 0.3],
        life: [0.25, 0.45],
        rise: 2,
        around: { radius: 0.7, height: 0.6 },
      },
      middle,
    ),
  bleed: (c) =>
    streaming(
      c,
      {
        rate: 10,
        size: [0.05, 0.09],
        speed: [0, 0.05],
        life: [0.35, 0.5],
        rise: -5,
        around: { radius: 0.8, height: 0.4 },
        blend: 'over',
      },
      middle,
    ),
  armor_break: cracked,
  weaken: drooping,
  taunt: warned,
  lose_sight: (c) =>
    streaming(
      c,
      {
        rate: 12,
        size: [0.3, 0.55],
        speed: [0.05, 0.15],
        life: [0.8, 1.3],
        rise: 0.3,
        around: { radius: 0.9, height: 0.9 },
        blend: 'over',
      },
      middle,
    ),
};

/** Every status's mark, by id, in its colour. */
export function statusLooks(): [EffectId, Drawn][] {
  return statuses.map((s) => [statusEffect(s.id), (marks[s.id] ?? halo)(statusColor(s.id))]);
}
