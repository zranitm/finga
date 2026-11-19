// A pair of wings for tests, as a parts file holds them: two halves, each a triangle spread flat across the back from its root, which sits at its hinge behind the chest joint.
import { Document, NodeIO } from '@gltf-transform/core';

import type { PieceLook } from '@/game/world3d/outfit-looks';

export const HINGE: readonly [number, number, number] = [0.04, 0.15, -0.12]; // the left half's, in the chest's axes; the right mirrors it
export const SPAN = 0.6;

export const TEST_WINGS: PieceLook = {
  slot: 'wings',
  file: 'test/Wings.glb',
  meshes: ['Test_Wing_L', 'Test_Wing_R'],
};

/** The pair's parts file, a .glb. */
export async function wingsFile(): Promise<Uint8Array> {
  const doc = new Document();
  const buffer = doc.createBuffer();
  const scene = doc.createScene();
  const material = doc.createMaterial('wing').setBaseColorFactor([0.85, 0.9, 1, 1]);
  for (const [name, side] of [
    ['Test_Wing_L', 1],
    ['Test_Wing_R', -1],
  ] as const) {
    const position = doc
      .createAccessor()
      .setType('VEC3')
      .setBuffer(buffer)
      .setArray(new Float32Array([0, 0, 0, side * SPAN, 0.25, 0, side * 0.5 * SPAN, -0.3, 0]));
    const normal = doc
      .createAccessor()
      .setType('VEC3')
      .setBuffer(buffer)
      .setArray(new Float32Array([0, 0, -1, 0, 0, -1, 0, 0, -1]));
    const half = doc
      .createPrimitive()
      .setAttribute('POSITION', position)
      .setAttribute('NORMAL', normal)
      .setMaterial(material);
    const node = doc
      .createNode(name)
      .setMesh(doc.createMesh(name).addPrimitive(half))
      .setTranslation([side * HINGE[0], HINGE[1], HINGE[2]]);
    scene.addChild(node);
  }
  return new NodeIO().writeBinary(doc);
}
