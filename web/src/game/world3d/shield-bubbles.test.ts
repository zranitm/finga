import { NullEngine } from '@babylonjs/core/Engines/nullEngine';
import { Scene } from '@babylonjs/core/scene';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';

import { movement } from '@/gamedata/movement';

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
});
