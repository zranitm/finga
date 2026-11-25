import { Constants } from '@babylonjs/core/Engines/constants';
import { NullEngine } from '@babylonjs/core/Engines/nullEngine';
import type { InputBlock } from '@babylonjs/core/Materials/Node/Blocks/Input/inputBlock';
import type { NodeMaterial } from '@babylonjs/core/Materials/Node/nodeMaterial';
import { Color3 } from '@babylonjs/core/Maths/math.color';
import { Vector3 } from '@babylonjs/core/Maths/math.vector';
import { Mesh } from '@babylonjs/core/Meshes/mesh';
import { Scene } from '@babylonjs/core/scene';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { CHEST_M } from './follow';
import { SeaRays } from './sea-rays';
import { Palette } from './toon';

let engine: NullEngine;
let scene: Scene;
let buffer: Float32Array; // the rays' instances, as handed the mesh: Babylon keeps its world matrices from the first read

beforeEach(() => {
  engine = new NullEngine();
  scene = new Scene(engine);
  const set = Object.getOwnPropertyDescriptor(Mesh.prototype, 'thinInstanceSetBuffer')!
    .value as Mesh['thinInstanceSetBuffer']; // the unspied one, to call through
  vi.spyOn(Mesh.prototype, 'thinInstanceSetBuffer').mockImplementation(function (
    this: Mesh,
    ...args
  ) {
    buffer = args[1]!;
    set.apply(this, args);
  });
});

afterEach(() => {
  vi.restoreAllMocks();
  scene.dispose();
  engine.dispose();
});

/** A ray as drawn: its middle, its way up its length and that length, its way and width across, and the way its face looks. */
interface Card {
  readonly mid: Vector3;
  readonly along: Vector3;
  readonly length: number;
  readonly across: Vector3;
  readonly width: number;
  readonly facing: Vector3;
}

function cards(mesh: Mesh): Card[] {
  return Array.from({ length: mesh.thinInstanceCount }, (_, i) => {
    const m = buffer.subarray(16 * i, 16 * i + 16);
    const across = new Vector3(m[0], m[1], m[2]);
    const along = new Vector3(m[4], m[5], m[6]);
    return {
      mid: new Vector3(m[12], m[13], m[14]),
      along: along.normalizeToNew(),
      length: along.length(),
      across: across.length() > 0 ? across.normalizeToNew() : across,
      width: across.length(),
      facing: new Vector3(m[8], m[9], m[10]).normalize(),
    };
  });
}

/** How far p is from the card: 0 on it. */
function offCard(c: Card, p: Vector3): number {
  const d = p.subtract(c.mid);
  const u = Math.max(-c.width / 2, Math.min(c.width / 2, Vector3.Dot(d, c.across)));
  const v = Math.max(-c.length / 2, Math.min(c.length / 2, Vector3.Dot(d, c.along)));
  const on = c.mid.add(c.across.scale(u)).add(c.along.scale(v));
  return Vector3.Distance(p, on);
}

/** The camera 16 m behind a chest at (x, y, z) on a yaw, as the follow camera stands, at its height for a pitch. */
function eyeOn(chest: Vector3, yaw: number, pitch: number, back = 16): Vector3 {
  return new Vector3(
    chest.x - back * Math.cos(pitch) * Math.sin(yaw),
    chest.y + back * Math.sin(pitch),
    chest.z - back * Math.cos(pitch) * Math.cos(yaw),
  );
}

describe('a sea’s light rays (underground-and-underwater.md section 7)', () => {
  it('draw six to eight faint cards as one mesh, added to what’s behind, writing no depth, faded into nothing by the fog', () => {
    const rays = new SeaRays(scene, new Palette(scene, 'sea'));
    const chest = new Vector3(40, CHEST_M, 40);
    rays.frame(0, eyeOn(chest, 0, 0.6), chest);
    const mesh = scene.getMeshByName('sea rays') as Mesh;
    expect(mesh.thinInstanceCount).toBeGreaterThanOrEqual(6);
    expect(mesh.thinInstanceCount).toBeLessThanOrEqual(8);
    const m = mesh.material as NodeMaterial;
    expect(m.alphaMode).toBe(Constants.ALPHA_ADD);
    expect(m.disableDepthWrite).toBe(true);
    expect((m.getBlockByName('rayPeak') as InputBlock).value).toBeLessThanOrEqual(0.15); // faint
    expect((m.getBlockByName('fogColor') as InputBlock).value).toEqual(Color3.Black()); // added light the fog hides
  });

  it('slant each ray down from above, its face turned to the camera about its length', () => {
    const rays = new SeaRays(scene, new Palette(scene, 'sea'));
    const chest = new Vector3(10, 3 + CHEST_M, -20);
    for (const yaw of [0, 1.3, 2.9, 4.4]) {
      const eye = eyeOn(chest, yaw, 0.7);
      rays.frame(yaw * 7, eye, chest);
      for (const c of cards(scene.getMeshByName('sea rays') as Mesh)) {
        expect(c.along.y).toBeGreaterThan(0.9); // from above
        expect(Math.hypot(c.along.x, c.along.z)).toBeGreaterThan(0.05); // slanting
        expect(c.length).toBeGreaterThan(15); // long
        expect(Vector3.Dot(c.facing, c.along)).toBeCloseTo(0, 6);
        const toEye = eye.subtract(c.mid);
        expect(Vector3.Dot(c.facing, toEye)).toBeGreaterThan(0);
        if (c.width > 0) {
          expect(Vector3.Dot(c.across, toEye.normalize())).toBeCloseTo(0, 5); // as square to the camera as its length lets it
        }
      }
    }
  });

  it('never cross the camera’s line to the player’s chest, wherever they stand and look, as the rays drift', () => {
    const rays = new SeaRays(scene, new Palette(scene, 'sea'));
    const mesh = scene.getMeshByName('sea rays') as Mesh;
    let near = 0;
    for (let k = 0; k < 400; k++) {
      const chest = new Vector3(((k * 37) % 300) - 150, (k % 7) + CHEST_M, ((k * 53) % 300) - 150);
      const eye = eyeOn(chest, k * 0.61, 0.15 + (k % 5) * 0.25, 6 + (k % 4) * 10);
      rays.frame(k * 1.37, eye, chest);
      for (const c of cards(mesh).filter((c) => c.width > 0)) {
        // one thinned to nothing draws nothing
        const gap = Math.min(
          ...Array.from({ length: 201 }, (_, i) => offCard(c, Vector3.Lerp(eye, chest, i / 200))),
        );
        expect(gap).toBeGreaterThan(0.05); // the line's sampled every few centimetres
        if (gap < 3) {
          near++;
        }
      }
    }
    expect(near).toBeGreaterThan(0); // some came close, so the check bore on them
  });

  it('drift slowly, and keep round the player as they go', () => {
    const rays = new SeaRays(scene, new Palette(scene, 'sea'));
    const mesh = scene.getMeshByName('sea rays') as Mesh;
    const chest = new Vector3(0, CHEST_M, 0);
    rays.frame(100, eyeOn(chest, 0, 0.6), chest);
    const before = cards(mesh).map((c) => c.mid);
    rays.frame(101, eyeOn(chest, 0, 0.6), chest);
    const moved = cards(mesh).map((c, i) => Vector3.Distance(c.mid, before[i]!));
    expect(Math.max(...moved)).toBeGreaterThan(0.05);
    expect(Math.max(...moved)).toBeLessThan(1); // slowly, under a metre a second
    for (let x = 0; x <= 400; x += 50) {
      const at = new Vector3(x, CHEST_M, x / 2);
      rays.frame(100 + x, eyeOn(at, 0, 0.6), at);
      const shown = cards(mesh).filter((c) => c.width > 0);
      expect(shown.length).toBeGreaterThanOrEqual(3);
      for (const c of shown) {
        expect(Math.hypot(c.mid.x - at.x, c.mid.z - at.z)).toBeLessThan(40); // inside the fog's reach
      }
    }
  });

  it('thin a ray to nothing before it wraps to the far side, so none pops', () => {
    const rays = new SeaRays(scene, new Palette(scene, 'sea'));
    const mesh = scene.getMeshByName('sea rays') as Mesh;
    const chest = new Vector3(5, CHEST_M, 5);
    const eye = new Vector3(5, 40, 4); // high over the chest, out of every ray's way
    let last: Card[] | null = null;
    let wraps = 0;
    for (let t = 0; t < 600; t += 0.1) {
      rays.frame(t, eye, chest);
      const now = cards(mesh);
      now.forEach((c, i) => {
        const was = last?.[i];
        if (!was) {
          return;
        }
        if (Vector3.Distance(c.mid, was.mid) > 10) {
          wraps++;
          expect(c.width).toBeLessThan(0.05);
          expect(was.width).toBeLessThan(0.05);
        } else {
          expect(Math.abs(c.width - was.width)).toBeLessThan(0.2); // no ray jumps in width either
        }
      });
      last = now;
    }
    expect(wraps).toBeGreaterThan(0);
  });
});
