// The drawn hits (combat-effects.md section 4), 2D's in metres: rays from the target's middle sized to it, physical or magic, a crit's burst on top, and a miss's grey puff, each flat to the camera, in front of the target, over a dark edge.
import { Vector3 } from '@babylonjs/core/Maths/math.vector';
import { Mesh } from '@babylonjs/core/Meshes/mesh';
import { VertexData } from '@babylonjs/core/Meshes/mesh.vertexData';
import { TransformNode } from '@babylonjs/core/Meshes/transformNode';

import { EDGE } from './colors';
import { animate } from './draw';
import type { Blend, Fx } from './fx';
import type { Drawn, PlayOptions } from './types';

const DEFAULT_SIZE = { radius: 0.5, height: 1 };
const PX = 1 / 32; // 2D's pixel, in metres
const EDGE_ALPHA = 0.45;

/** A spark's reach in metres: about half the target's larger side, so a boss's is bigger and a bat's still shows. */
function reach(o: PlayOptions): number {
  const s = o.size ?? DEFAULT_SIZE;
  return Math.min(1.1, Math.max(0.3, 0.45 * Math.max(2 * s.radius, s.height)));
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

  mesh(fx: Fx, root: TransformNode, color: number, blend: Blend, order: number): Mesh {
    const m = new Mesh('spark', fx.scene);
    const vd = new VertexData();
    vd.positions = this.positions;
    vd.indices = this.indices;
    vd.applyToMesh(m);
    m.parent = root;
    m.material = fx.material(color, blend);
    m.isPickable = false;
    m.alwaysSelectAsActiveMesh = true;
    m.alphaIndex = order; // the edge under the colour
    return m;
  }
}

/** Rays out from the middle, long and short in turn, a long one along the blow. */
function rays(n: number, r: number, w: number, mirror: number): Shape {
  const s = new Shape();
  for (let i = 0; i < n; i++) {
    const a = (i / n) * 2 * Math.PI;
    const len = i % 2 === 0 ? r : r * 0.55;
    const [dx, dy] = [mirror * Math.cos(a), Math.sin(a)];
    s.stroke(dx * r * 0.3, dy * r * 0.3, dx * len, dy * len, w);
  }
  return s;
}

interface Layer {
  readonly shape: Shape;
  readonly color: number;
  readonly blend: Blend;
  readonly alpha: number;
}

/** A burst over ms at the target's middle, brought out in front of it toward the camera: it springs from half to full size at once and holds bright, then thins out; rise lifts it as it goes. */
function burst(ms: number, layers: (o: PlayOptions, mirror: number) => Layer[], rise = 0): Drawn {
  return (fx, o, now) => {
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
    const mirror = Math.sin(o.yaw) < 0 ? -1 : 1;
    const drawn = layers(o, mirror).map((l, i) => ({
      mesh: l.shape.mesh(fx, root, l.color, l.blend, i),
      alpha: l.alpha,
    }));
    return animate(fx, now, now + ms, [root], (t) => {
      const k = Math.min(1, (t - now) / ms);
      root.scaling.setAll(0.5 + 0.6 * (1 - (1 - k) ** 3));
      root.position.y = at.y + rise * k;
      const fade = k < 0.4 ? 1 : 1 - (k - 0.4) / 0.6;
      for (const d of drawn) {
        d.mesh.visibility = d.alpha * fade;
      }
    });
  };
}

/** A shape with its dark edge under it, edge px wider. */
const edged = (shape: (w: number) => Shape, w: number, edge: number, color: number): Layer[] => [
  { shape: shape(w + edge), color: EDGE, blend: 'over', alpha: EDGE_ALPHA },
  { shape: shape(w), color, blend: 'over', alpha: 1 },
];

const physical = burst(160, (o, m) =>
  edged((w) => rays(8, reach(o), w, m), 3.5 * PX, 3 * PX, 0xfff0c8),
);

const magic = burst(200, (o) => {
  const r = reach(o);
  const dots = new Shape();
  for (let i = 0; i < 4; i++) {
    const a = (i / 4) * 2 * Math.PI + Math.PI / 4;
    dots.disc(Math.cos(a) * r, Math.sin(a) * r, 2.5 * PX);
  }
  return [
    ...edged((w) => new Shape().ring(r * 0.7, w), 3 * PX, 3 * PX, 0xc890ff),
    { shape: dots, color: 0xf0e0ff, blend: 'over', alpha: 1 },
  ];
});

const crit = burst(240, (o, m) => {
  const r = reach(o) * 1.4;
  return [
    ...edged((w) => rays(12, r, w, m), 4 * PX, 3 * PX, 0xffd166),
    { shape: new Shape().disc(0, 0, r * 0.22), color: 0xfffbe8, blend: 'over', alpha: 0.9 },
  ];
});

/** A grey puff that rises and thins: the blow went past. */
const miss = burst(
  220,
  () => {
    const puff = new Shape();
    for (const [dx, dy, r] of [
      [-6, -2, 5],
      [5, 0, 6],
      [0, 5, 4],
    ] as const) {
      puff.disc(dx * PX, dy * PX, r * PX);
    }
    return [{ shape: puff, color: 0xb8b8c8, blend: 'over', alpha: 0.7 }];
  },
  10 * PX,
);

export const hitsDrawn = {
  'hit.physical': physical,
  'hit.magic': magic,
  'hit.crit': crit,
  'hit.miss': miss,
} as const;
