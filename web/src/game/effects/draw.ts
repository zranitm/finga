// What the drawn looks are made of (3d-world.md section 10): meshes in the effects' shared unlit materials, each faded by its visibility and stepped each frame on the world's clock.
import { VertexBuffer } from '@babylonjs/core/Buffers/buffer';
import { Vector3 } from '@babylonjs/core/Maths/math.vector';
import type { AbstractMesh } from '@babylonjs/core/Meshes/abstractMesh';
import { CreateBox } from '@babylonjs/core/Meshes/Builders/boxBuilder';
import { CreateCylinder } from '@babylonjs/core/Meshes/Builders/cylinderBuilder';
import { CreatePolyhedron } from '@babylonjs/core/Meshes/Builders/polyhedronBuilder';
import { CreateSphere } from '@babylonjs/core/Meshes/Builders/sphereBuilder';
import { CreateTorus } from '@babylonjs/core/Meshes/Builders/torusBuilder';
import { CreateTube } from '@babylonjs/core/Meshes/Builders/tubeBuilder';
import { Mesh } from '@babylonjs/core/Meshes/mesh';
import { VertexData } from '@babylonjs/core/Meshes/mesh.vertexData';
import { TrailMesh } from '@babylonjs/core/Meshes/trailMesh';
import { TransformNode } from '@babylonjs/core/Meshes/transformNode';

import type { Beats } from '../world3d/beats';
import { groundUnder } from '../world3d/map';

import { EDGE } from './colors';
import type { Blend, Fx } from './fx';
import type { Point, PlayOptions, Stop } from './types';

/** Where a look leaves the body, as a shot does: its chest. */
export const CHEST_M = 1.1;
const UNTIMED_STRIKE_MS = 150; // the beats of a play given none: a skill's strike and ready point
const UNTIMED_READY_MS = 450;
const EDGE_M = 0.035; // how far a band's dark edge shows past it
const GROUND_LIFT_M = 0.04; // a decal just over the ground it lies on

export const clamp01 = (v: number): number => Math.min(1, Math.max(0, v));

/** The play's beats, or for one given none, a skill's from now. */
export function beatsOf(o: PlayOptions, now: number): Beats {
  return (
    o.beats ?? {
      start: now,
      hit: now,
      strikeEnd: now + UNTIMED_STRIKE_MS,
      readyAt: now + UNTIMED_READY_MS,
      end: now + UNTIMED_READY_MS,
    }
  );
}

/** How far through the strike now is: 0 at the hit, 1 at its end. */
export function through(b: Beats, now: number): number {
  return clamp01((now - b.hit) / Math.max(1, b.strikeEnd - b.hit));
}

/** How strongly a look shows: fully through the strike, then fading to nothing by `by`, the ready point unless sooner. */
export function strength(b: Beats, now: number, by = b.readyAt): number {
  if (now < b.hit) {
    return 0;
  }
  if (now < b.strikeEnd) {
    return 1;
  }
  return clamp01(1 - (now - b.strikeEnd) / Math.max(1, by - b.strikeEnd));
}

/** A fixed pseudo-random share in [0, 1) for n: the same jag each frame of one flicker. */
export function jitter(n: number): number {
  const s = Math.sin(n * 12.9898) * 43758.5453;
  return s - Math.floor(s);
}

/** The way yaw faces across the ground. */
export function forward(yaw: number): { readonly x: number; readonly z: number } {
  return { x: Math.sin(yaw), z: Math.cos(yaw) };
}

/** A point `d` metres from p along yaw, `side` metres to its right, `up` metres higher. */
export function along(p: Point, yaw: number, d: number, side = 0, up = 0): Point {
  const f = forward(yaw);
  return { x: p.x + f.x * d + f.z * side, y: p.y + up, z: p.z + f.z * d - f.x * side };
}

/** A look's meshes, drawn in fx's material for color and taken away with it; laid on the ground, at alpha. */
export function paint<M extends AbstractMesh>(
  fx: Fx,
  mesh: M,
  color: number,
  blend: Blend = 'add',
  alpha = 1,
): M {
  mesh.material = fx.material(color, blend, alpha);
  mesh.isPickable = false;
  mesh.alwaysSelectAsActiveMesh = true; // reshaped each frame, and always by the player
  return mesh;
}

/** Runs a look until `until`: step(t) each frame, then its parts taken away. */
export function animate(
  fx: Fx,
  now: number,
  until: number,
  parts: readonly { dispose(): void }[],
  step: (t: number) => void,
): Stop {
  return fx.everyFrame(now, until, step, () => {
    for (const p of parts) {
      p.dispose();
    }
  });
}

/** A flat band along an arc, its dark edge under it: drawn from angle a0 to a1 off its root's forward (+x positive), its outer side at r, as wide as width(t) along it, t from 0 at a0 to 1 at a1. Its root turns and rolls it: a swing across the front, or an overhead one. */
export class Band {
  readonly root: TransformNode;
  private readonly main: Mesh;
  private readonly edge: Mesh;

  constructor(
    fx: Fx,
    color: number,
    private readonly segments = 16,
  ) {
    this.root = new TransformNode('band', fx.scene);
    this.edge = paint(fx, this.strip(fx, 'band edge'), EDGE, 'over');
    this.main = paint(fx, this.strip(fx, 'band'), color, 'over');
    this.edge.alphaIndex = 0; // the edge under the band
    this.main.alphaIndex = 1;
  }

  draw(a0: number, a1: number, r: number, width: (t: number) => number, alpha: number): void {
    this.shape(this.main, a0, a1, r, width, 0);
    this.shape(this.edge, a0, a1, r, width, EDGE_M);
    this.main.visibility = alpha;
    this.edge.visibility = alpha * 0.5;
  }

  dispose(): void {
    this.root.dispose();
  }

  private strip(fx: Fx, name: string): Mesh {
    const n = this.segments;
    const m = new Mesh(name, fx.scene);
    const data = new VertexData();
    data.positions = new Float32Array((n + 1) * 6);
    const indices: number[] = [];
    for (let i = 0; i < n; i++) {
      const [a, b, c, d] = [2 * i, 2 * i + 1, 2 * i + 2, 2 * i + 3];
      indices.push(a, b, c, b, d, c);
    }
    data.indices = indices;
    data.applyToMesh(m, true);
    m.parent = this.root;
    return m;
  }

  private shape(
    m: Mesh,
    a0: number,
    a1: number,
    r: number,
    width: (t: number) => number,
    pad: number,
  ): void {
    const n = this.segments;
    const p = m.getVerticesData(VertexBuffer.PositionKind) as Float32Array; // each mesh's own, updated in place
    for (let i = 0; i <= n; i++) {
      const t = i / n;
      const a = a0 + (a1 - a0) * t;
      const outer = r + pad;
      const inner = Math.max(0, r - width(t) - pad);
      const [s, c] = [Math.sin(a), Math.cos(a)];
      p.set([outer * s, 0, outer * c, inner * s, 0, inner * c], i * 6);
    }
    m.updateVerticesData(VertexBuffer.PositionKind, p);
  }
}

/** A thick line through points: a tube whose path moves each frame, its count of points fixed. */
export class Line {
  private mesh: Mesh;
  private readonly path: Vector3[];

  constructor(
    fx: Fx,
    color: number,
    points: number,
    private readonly radius: number,
  ) {
    this.path = Array.from({ length: points }, (_, i) => new Vector3(0, 0, i * 0.01));
    this.mesh = paint(
      fx,
      CreateTube('line', { path: this.path, radius, tessellation: 6, updatable: true }, fx.scene),
      color,
    );
  }

  draw(points: readonly Point[], alpha: number): void {
    points.forEach((p, i) => this.path[i]?.set(p.x, p.y, p.z));
    this.mesh = CreateTube('line', { path: this.path, radius: this.radius, instance: this.mesh });
    this.mesh.visibility = alpha;
    this.mesh.setEnabled(alpha > 0);
  }

  dispose(): void {
    this.mesh.dispose();
  }
}

/** A jagged line from a to b: kinks turns of up to amp metres across it, sideways and up, the jags drawn from seed. */
export function zigzag(a: Point, b: Point, kinks: number, amp: number, seed: number): Point[] {
  const out: Point[] = [a];
  const dx = b.x - a.x;
  const dz = b.z - a.z;
  const len = Math.hypot(dx, dz) || 1;
  for (let i = 1; i <= kinks; i++) {
    const t = i / (kinks + 1);
    const side = (jitter(seed * 31 + i) * 2 - 1) * amp;
    const up = (jitter(seed * 17 + i) * 2 - 1) * amp * 0.6;
    out.push({
      x: a.x + dx * t + (dz / len) * side,
      y: a.y + (b.y - a.y) * t + up,
      z: a.z + dz * t - (dx / len) * side,
    });
  }
  out.push(b);
  return out;
}

/** A flat ring on the ground about its root, radius 1 before scaling. */
export function ring(fx: Fx, color: number, thickness = 0.06): Mesh {
  return paint(
    fx,
    fx.shape(`ring ${thickness}`, () =>
      CreateTorus('ring', { diameter: 2, thickness, tessellation: 40 }, fx.scene),
    ),
    color,
  );
}

export function ball(fx: Fx, color: number, diameter: number, blend: 'add' | 'over' = 'add'): Mesh {
  const shape = () => CreateSphere('ball', { diameter, segments: 8 }, fx.scene);
  return paint(fx, fx.shape(`ball ${diameter}`, shape), color, blend);
}

export function stone(fx: Fx, size: number): Mesh {
  const shape = () => CreatePolyhedron('stone', { type: 3, size }, fx.scene);
  return paint(fx, fx.shape(`stone ${size}`, shape), 0x8a7f70, 'over');
}

/** An arrow along its root's forward, its head at the root, standing at `at`: a shaft, a head and a trail behind it as it flies. */
export function arrow(
  fx: Fx,
  a: {
    readonly shaft: number;
    readonly width: number;
    readonly color: number;
    readonly trail: number;
  },
  at: Point,
): { readonly root: TransformNode; readonly head: Mesh; dispose(): void } {
  const root = new TransformNode('arrow', fx.scene);
  root.position.set(at.x, at.y, at.z);
  root.computeWorldMatrix(true); // the trail starts here, not at the origin
  const shaft = paint(
    fx,
    fx.shape(`shaft ${a.width} ${a.shaft}`, () =>
      CreateBox('shaft', { width: a.width, height: a.width, depth: a.shaft }, fx.scene),
    ),
    a.color,
  );
  shaft.parent = root;
  shaft.position.z = -a.shaft / 2;
  const head = paint(
    fx,
    fx.shape(`head ${a.width}`, () =>
      CreateCylinder(
        'head',
        { diameterTop: 0, diameterBottom: a.width * 3, height: a.width * 5, tessellation: 4 },
        fx.scene,
      ),
    ),
    a.color,
  );
  head.parent = root;
  head.rotation.x = Math.PI / 2;
  const trail = new TrailMesh('trail', root, fx.scene, {
    diameter: a.width * 1.5,
    length: Math.max(2, Math.round(a.trail * 10)), // frames it keeps: about `trail` metres at a shot's pace
    autoStart: true,
  });
  paint(fx, trail, a.color);
  trail.visibility = 0.5;
  return {
    root,
    head,
    dispose: () => {
      trail.dispose();
      root.dispose();
    },
  };
}

/** A sheet laid on the ground at alpha: a grid of nu by nv cells whose corners `place` puts on the ground's plane, u across and v along from 0 to 1, each raised to the ground there; a telegraph, so its ghost shows where the place hides it. */
export class Patch {
  readonly mesh: Mesh;
  private readonly ghost: Mesh;

  constructor(
    private readonly fx: Fx,
    color: number,
    private readonly nu: number,
    private readonly nv: number,
    blend: 'ground' | 'tint' = 'ground',
    private readonly alpha = 1,
  ) {
    const m = new Mesh('patch', fx.scene);
    const data = new VertexData();
    data.positions = new Float32Array((nu + 1) * (nv + 1) * 3);
    const indices: number[] = [];
    for (let i = 0; i < nu; i++) {
      for (let j = 0; j < nv; j++) {
        const a = i * (nv + 1) + j;
        const b = a + nv + 1;
        indices.push(a, b, a + 1, b, b + 1, a + 1);
      }
    }
    data.indices = indices;
    data.applyToMesh(m, true);
    this.mesh = paint(fx, m, color, blend, alpha);
    this.ghost = fx.ghost(this.mesh, color, blend);
  }

  /** Shows it and its ghost, or hides them. */
  show(on: boolean): void {
    this.mesh.visibility = on ? 1 : 0;
    this.ghost.visibility = on ? this.alpha : 0;
  }

  /** Lays it on the ground under each place(u, v), feet at y choosing a solid's top within a step. */
  lay(
    place: (u: number, v: number) => { readonly x: number; readonly z: number },
    y: number,
  ): void {
    const p = this.mesh.getVerticesData(VertexBuffer.PositionKind) as Float32Array;
    for (let i = 0; i <= this.nu; i++) {
      for (let j = 0; j <= this.nv; j++) {
        const at = place(i / this.nu, j / this.nv);
        p.set(
          [at.x, groundUnder(this.fx.map, at.x, at.z, y) + GROUND_LIFT_M, at.z],
          (i * (this.nv + 1) + j) * 3,
        );
      }
    }
    this.mesh.updateVerticesData(VertexBuffer.PositionKind, p);
  }

  /** Lays it over the first k of `on`, laid already with as many columns: each corner where its column of `on` reaches v·k, so it lies on `on` with no ground read. */
  cover(on: Patch, k: number): void {
    const from = on.mesh.getVerticesData(VertexBuffer.PositionKind) as Float32Array;
    const p = this.mesh.getVerticesData(VertexBuffer.PositionKind) as Float32Array;
    for (let i = 0; i <= this.nu; i++) {
      for (let j = 0; j <= this.nv; j++) {
        const t = (j / this.nv) * clamp01(k) * on.nv;
        const j0 = Math.min(Math.floor(t), on.nv - 1);
        const f = t - j0;
        const a = (i * (on.nv + 1) + j0) * 3;
        const to = (i * (this.nv + 1) + j) * 3;
        for (let c = 0; c < 3; c++) {
          p[to + c] = from[a + c]! + (from[a + 3 + c]! - from[a + c]!) * f;
        }
      }
    }
    this.mesh.updateVerticesData(VertexBuffer.PositionKind, p);
  }

  dispose(): void {
    this.mesh.dispose();
  }
}
