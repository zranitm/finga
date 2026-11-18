import { TargetCamera } from '@babylonjs/core/Cameras/targetCamera';
import { NullEngine } from '@babylonjs/core/Engines/nullEngine';
import { Vector3 } from '@babylonjs/core/Maths/math.vector';
import { Scene } from '@babylonjs/core/scene';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { movement } from '@/gamedata/movement';
import { builtShaders } from '@/testing/shaders';

import { ShieldBubbles } from './shield-bubbles';

let engine: NullEngine;
let scene: Scene;

beforeEach(() => {
  engine = new NullEngine();
  scene = new Scene(engine);
});

afterEach(() => {
  scene.dispose();
  engine.dispose();
});

const bubbles = () => scene.meshes.filter((m) => m.name === 'shield bubble' && m.isEnabled());

describe('ShieldBubbles', () => {
  it('draws a bubble round each player with shield left, the body inside it, and takes away the ones that ran out', () => {
    const b = new ShieldBubbles(scene);
    expect(bubbles()).toEqual([]); // its first made ahead, hidden
    b.show(
      new Map([
        ['me', { x: 2, y: 1, z: 3 }],
        ['p:a', { x: 8, y: 0, z: 3 }],
      ]),
      0,
    );
    expect(bubbles().map((m) => [m.position.x, m.position.z])).toEqual([
      [2, 3],
      [8, 3],
    ]);
    for (const [m, feet] of [
      [bubbles()[0]!, 1],
      [bubbles()[1]!, 0],
    ] as const) {
      m.computeWorldMatrix(true);
      const { minimumWorld: lo, maximumWorld: hi } = m.getBoundingInfo().boundingBox;
      expect([
        hi.x - lo.x > 2 * movement.radius,
        lo.y <= feet,
        hi.y >= feet + movement.height,
      ]).toEqual([true, true, true]); // the body inside, feet to head
    }
    b.show(new Map([['p:a', { x: 9, y: 0, z: 3 }]]), 100);
    expect(bubbles().map((m) => m.position.x)).toEqual([9]);
    b.dispose();
    expect(scene.meshes.filter((m) => m.name === 'shield bubble')).toEqual([]);
    expect(scene.materials.filter((m) => m.name === 'shield bubble')).toEqual([]);
  });

  it('builds its bubble’s shader ahead, stretched as a bubble shows, so the first shield builds none', async () => {
    new TargetCamera('camera', new Vector3(0, 2, -6), scene).setTarget(Vector3.Zero());
    const b = new ShieldBubbles(scene);
    await vi.waitFor(() => expect(builtShaders(engine)).not.toEqual([]));
    const built = new Set(builtShaders(engine));
    b.show(new Map([['me', { x: 0, y: 0, z: 0 }]]), 0);
    scene.render();
    expect(builtShaders(engine).filter((k) => !built.has(k))).toEqual([]);
  });
});
