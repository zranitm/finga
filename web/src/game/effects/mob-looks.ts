// Each mob attack's look, by its name: a ring through its windup, then its kind's travel and impact, and a spit's ball in flight.
import { mobs } from '@/gamedata/mobs';
import { overlaps } from '@/sim/step';

import type { Beats } from '../entities/beats';

import { animate, beatsOf } from './draw';
import type { G } from './draw';
import { mobEffect } from './ids';
import type { EffectId } from './ids';
import type { Body, Drawn, PlayOptions } from './types';

const FX = 0xfff4d6;
const SPIT = 0xc8e6a0; // an untinted mob's spit
const DEFAULT_SIZE = { w: 32, h: 32 };

type Kind = (typeof mobs)[number]['attacks'][number]['kind'];

/** The impact of an attack of kind as it lands, fading by p from 1 to 0, around the mob's body b. */
function impact(g: G, kind: Kind, b: Body, o: PlayOptions, p: number): void {
  const fade = 1 - p;
  const reach = o.reach ?? 0;
  switch (kind) {
    case 'front': {
      const r = (b.h + reach) / 2;
      const cx = b.x + o.facing * (b.w / 2);
      const from = o.facing > 0 ? -Math.PI / 2 : Math.PI / 2; // clockwise, y down: through its front
      g.lineStyle(3, FX, fade)
        .beginPath()
        .arc(cx, b.y - b.h / 2, r, from, from + Math.PI)
        .strokePath();
      return;
    }
    case 'leap':
      g.lineStyle(2, FX, fade).strokeEllipse(b.x, b.y, (b.w + 2 * reach) * (0.6 + 0.4 * p), 10);
      return;
    case 'around': {
      const s = 0.5 + 0.5 * p;
      g.lineStyle(3, FX, fade).strokeEllipse(
        b.x,
        b.y - b.h / 2,
        (b.w + 2 * reach) * s,
        (b.h + 2 * reach) * s,
      );
      return;
    }
    default:
      return;
  }
}

/** A rush's hit at t, from where the mob stood toward o.to over its travel; it stops at the first player it meets, as on the server. */
function rushLead(o: PlayOptions, b: Beats): (t: number, body: Body) => number {
  let x = o.x;
  let met = false;
  return (t, body) => {
    if (o.to === undefined) {
      return body.x;
    }
    if (met) {
      return x;
    }
    const want = o.x + (o.to - o.x) * Math.min(1, (t - b.hit) / Math.max(1, b.strikeEnd - b.hit));
    const half = body.w / 2;
    const swept = {
      x: Math.min(x, want) - half,
      y: body.y - body.h,
      w: Math.abs(want - x) + body.w,
      h: body.h,
    };
    let stop = want;
    for (const p of o.players?.() ?? []) {
      const box = { x: p.x - p.w / 2, y: p.y - p.h, w: p.w, h: p.h };
      if (overlaps(swept, box)) {
        met = true;
        const side = o.facing > 0 ? box.x - half : box.x + box.w + half; // its front at their near side, never back
        stop = o.facing > 0 ? Math.min(stop, Math.max(x, side)) : Math.max(stop, Math.min(x, side));
      }
    }
    x = stop;
    return x;
  };
}

/** A mob attack of kind, around the mob as it moves: from its windup to its impact's end, and a spit's balls while they fly. */
function mobLook(kind: Kind): Drawn {
  return (scene, o, now) => {
    const b = beatsOf(o, now);
    const lead = kind === 'rush' ? rushLead(o, b) : undefined;
    return animate(scene, b.end, (g, t) => {
      const body = o.follow?.() ?? { x: o.x, y: o.y, ...(o.size ?? DEFAULT_SIZE) };
      if (t < b.hit) {
        const p = (t - b.start) / Math.max(1, b.hit - b.start);
        const pulse = 0.2 + 0.4 * Math.sin(p * Math.PI * 4) ** 2;
        g.lineStyle(2, FX, pulse).strokeEllipse(
          body.x,
          body.y - body.h / 2,
          body.w + 8,
          body.h + 8,
        );
      } else if (t < b.strikeEnd) {
        if (lead) {
          const x = lead(t, body); // its hit, which goes on past a body a portal's edge stopped
          for (let i = 1; i <= 3; i++) {
            g.fillStyle(FX, 0.5 / i).fillCircle(
              x - o.facing * (body.w / 2 + i * 8),
              body.y - 3,
              4 - i,
            );
          }
        }
      } else if (t < b.readyAt) {
        const landed = kind === 'leap' && o.to !== undefined ? { ...body, x: o.to } : body; // where its hit came down
        impact(g, kind, landed, o, (t - b.strikeEnd) / Math.max(1, b.readyAt - b.strikeEnd));
      }
      for (const s of o.shots?.() ?? []) {
        g.fillStyle(o.color ?? SPIT, 1).fillCircle(s.x, s.y - s.h / 2, s.w / 2);
      }
    });
  };
}

/** Every mob attack's look, by name, drawn as its kind. */
export function mobLooks(): [EffectId, Drawn][] {
  const kinds = new Map<string, Kind>();
  for (const m of mobs) {
    for (const a of m.attacks as readonly { readonly name: string; readonly kind: Kind }[]) {
      kinds.set(a.name, a.kind);
    }
  }
  return [...kinds].map(([name, kind]) => [mobEffect(name), mobLook(kind)]);
}
