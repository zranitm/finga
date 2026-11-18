// The drawn hits (combat-effects.md section 4), 2D's in metres: rays from the target's middle sized to it, physical or magic, a crit's burst on top, and a miss's grey puff, each flat to the camera, in front of the target, over a dark edge; each kind's shapes made once a map.
import { Vector3 } from '@babylonjs/core/Maths/math.vector';
import { Mesh } from '@babylonjs/core/Meshes/mesh';
import { VertexData } from '@babylonjs/core/Meshes/mesh.vertexData';
import { TransformNode } from '@babylonjs/core/Meshes/transformNode';

import { EDGE } from './colors';
import { animate, paint } from './draw';
import type { Fx } from './fx';
import type { Drawn, PlayOptions, Stop } from './types';

const DEFAULT_SIZE = { radius: 0.5, height: 1 };
const PX = 1 / 32; // 2D's pixel, in metres
const EDGE_ALPHA = 0.45;
const REACH_STEP_M = 0.05; // reaches rounded to this, so a kind keeps a few shapes a map
const MAX_SPARKS = 32; // past this the oldest goes, so a crowd's hits can't pile up

/** A spark's reach in metres: about half the target's larger side, so a boss's is bigger and a bat's still shows. */
function reach(o: PlayOptions): number {
  const s = o.size ?? DEFAULT_SIZE;
  const r = Math.min(1.1, Math.max(0.3, 0.45 * Math.max(2 * s.radius, s.height)));
  return Math.round(r / REACH_STEP_M) * REACH_STEP_M;
}

/** Flat strokes and discs in a plane: x right and y up as the camera sees it. */
class Shape {
  private readonly positions: number[] = [];
  private readonly indices: number[] = [];

  /** A stroke w wide from (x0, y0) to (x1, y1). */
  stroke(x0: number, y0: number, x1: number, y1: number, w: number): this {
    const l = Math.hypot(x1 - x0, y1 - y0) || 1;
    const [nx, ny] = [((y0 - y1) / l) * (w / 2), ((x1 - x0) / l) * (w / 2)];
    const i = this.positions.length / 3;
    this.positions.push(
      x0 + nx,
      y0 + ny,
      0,
      x1 + nx,
      y1 + ny,
      0,
      x1 - nx,
      y1 - ny,
      0,
      x0 - nx,
      y0 - ny,
      0,
    );
    this.indices.push(i, i + 1, i + 2, i, i + 2, i + 3);
    return this;
  }

  disc(x: number, y: number, r: number, n = 12): this {
    const c = this.positions.length / 3;
    this.positions.push(x, y, 0);
    for (let k = 0; k < n; k++) {
      const a = (k / n) * 2 * Math.PI;
      this.positions.push(x + Math.cos(a) * r, y + Math.sin(a) * r, 0);
      this.indices.push(c, c + 1 + k, c + 1 + ((k + 1) % n));
    }
    return this;
  }

  /** A circle's outline, w wide. */
  ring(r: number, w: number, n = 24): this {
    for (let k = 0; k < n; k++) {
      const [a0, a1] = [(k / n) * 2 * Math.PI, ((k + 1) / n) * 2 * Math.PI];
      this.stroke(Math.cos(a0) * r, Math.sin(a0) * r, Math.cos(a1) * r, Math.sin(a1) * r, w);
    }
    return this;
  }

  mesh(fx: Fx): Mesh {
    const m = new Mesh('spark', fx.scene);
    const vd = new VertexData();
    vd.positions = this.positions;
    vd.indices = this.indices;
    vd.applyToMesh(m);
    return m;
  }
}

/** Rays out from the middle, long and short in turn, the long ones level and upright. */
function rays(n: number, r: number, w: number): Shape {
  const s = new Shape();
  for (let i = 0; i < n; i++) {
    const a = (i / n) * 2 * Math.PI;
    const len = i % 2 === 0 ? r : r * 0.55;
    const [dx, dy] = [Math.cos(a), Math.sin(a)];
    s.stroke(dx * r * 0.3, dy * r * 0.3, dx * len, dy * len, w);
  }
  return s;
}

/** A layer of a spark: its shape, built only the first time a map draws it, in its colour. */
interface Layer {
  readonly shape: () => Shape;
  readonly color: number;
  readonly alpha: number;
}

/** The sparks showing on each map, oldest first. */
const showing = new WeakMap<Fx, Set<Stop>>();

/** A burst over ms at the target's middle, brought out in front of it toward the camera: it springs from half to full size at once and holds bright, then thins out; rise lifts it as it goes. */
function burst(kind: string, ms: number, layers: (r: number) => Layer[], rise = 0): Drawn {
  return (fx, o, now) => {
    const shown = showing.get(fx) ?? new Set<Stop>();
    showing.set(fx, shown);
    for (const oldest of shown) {
      if (shown.size < MAX_SPARKS) {
        break;
      }
      oldest();
    }
    const root = new TransformNode('spark', fx.scene);
    root.billboardMode = TransformNode.BILLBOARDMODE_ALL;
    const at = new Vector3(o.x, o.y, o.z);
    const camera = fx.scene.activeCamera?.globalPosition;
    if (camera) {
      at.addInPlace(
        camera
          .subtract(at)
          .normalize()
          .scaleInPlace(o.size?.radius ?? DEFAULT_SIZE.radius),
      );
    }
    root.position.copyFrom(at);
    const r = reach(o);
    const drawn = layers(r).map((l, i) => {
      const shape = () => l.shape().mesh(fx);
      const mesh = paint(fx, fx.shape(`${kind} ${r.toFixed(2)} ${i}`, shape), l.color, 'over');
      mesh.parent = root;
      mesh.alphaIndex = i; // the edge under the colour
      return { mesh, alpha: l.alpha };
    });
    let stop: Stop = () => undefined;
    const forget = { dispose: () => shown.delete(stop) };
    stop = animate(fx, now, now + ms, [root, forget], (t) => {
      const k = Math.min(1, (t - now) / ms);
      root.scaling.setAll(0.5 + 0.6 * (1 - (1 - k) ** 3));
      root.position.y = at.y + rise * k;
      const fade = k < 0.4 ? 1 : 1 - (k - 0.4) / 0.6;
      for (const d of drawn) {
        d.mesh.visibility = d.alpha * fade;
      }
    });
    shown.add(stop);
    return stop;
  };
}

/** A shape with its dark edge under it, edge px wider. */
const edged = (shape: (w: number) => Shape, w: number, edge: number, color: number): Layer[] => [
  { shape: () => shape(w + edge), color: EDGE, alpha: EDGE_ALPHA },
  { shape: () => shape(w), color, alpha: 1 },
];

const physical = burst('physical', 160, (r) =>
  edged((w) => rays(8, r, w), 3.5 * PX, 3 * PX, 0xfff0c8),
);

const magic = burst('magic', 200, (r) => [
  ...edged((w) => new Shape().ring(r * 0.7, w), 3 * PX, 3 * PX, 0xc890ff),
  {
    shape: () => {
      const dots = new Shape();
      for (let i = 0; i < 4; i++) {
        const a = (i / 4) * 2 * Math.PI + Math.PI / 4;
        dots.disc(Math.cos(a) * r, Math.sin(a) * r, 2.5 * PX);
      }
      return dots;
    },
    color: 0xf0e0ff,
    alpha: 1,
  },
]);

const crit = burst('crit', 240, (r) => [
  ...edged((w) => rays(12, r * 1.4, w), 4 * PX, 3 * PX, 0xffd166),
  { shape: () => new Shape().disc(0, 0, r * 1.4 * 0.22), color: 0xfffbe8, alpha: 0.9 },
]);

/** A short steel clang: a blow stopped on a guard. */
const block = burst('block', 180, (r) => [
  ...edged((w) => rays(10, r * 0.8, w), 3 * PX, 3 * PX, 0xdff0ff),
  { shape: () => new Shape().disc(0, 0, r * 0.18), color: 0xffffff, alpha: 0.95 },
]);

/** A grey puff that rises and thins: the blow went past. */
const miss = burst(
  'miss',
  220,
  () => [
    {
      shape: () => {
        const puff = new Shape();
        for (const [dx, dy, r] of [
          [-6, -2, 5],
          [5, 0, 6],
          [0, 5, 4],
        ] as const) {
          puff.disc(dx * PX, dy * PX, r * PX);
        }
        return puff;
      },
      color: 0xb8b8c8,
      alpha: 0.7,
    },
  ],
  10 * PX,
);

export const hitsDrawn = {
  'hit.physical': physical,
  'hit.magic': magic,
  'hit.crit': crit,
  'hit.miss': miss,
  'hit.block': block,
} as const;
