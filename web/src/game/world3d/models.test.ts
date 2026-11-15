import { readFileSync } from 'node:fs';
import path from 'node:path';

import { NullEngine } from '@babylonjs/core/Engines/nullEngine';
import { Scene } from '@babylonjs/core/scene';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';

import { Models } from './models';

const MODELS = path.resolve(process.cwd(), 'public/models');

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

describe('Models', () => {
  it('loads each file once however often it is asked for', async () => {
    const asked: string[] = [];
    const models = new Models(scene, (file) => {
      asked.push(file);
      return new Uint8Array(readFileSync(path.join(MODELS, file)));
    });
    const [a, b] = await Promise.all([
      models.load('quaternius/Mushnub.glb'),
      models.load('quaternius/Mushnub.glb'),
    ]);
    expect(a).toBe(b);
    expect(asked).toEqual(['quaternius/Mushnub.glb']);
  });

  it('forgets a load that failed, so the next asks again', async () => {
    let bytes: Uint8Array = new Uint8Array([1, 2, 3]); // not a glTF
    const asked: string[] = [];
    const models = new Models(scene, (file) => {
      asked.push(file);
      return bytes;
    });
    await expect(models.load('quaternius/Mushnub.glb')).rejects.toThrow();
    bytes = new Uint8Array(readFileSync(path.join(MODELS, 'quaternius/Mushnub.glb')));
    await expect(models.load('quaternius/Mushnub.glb')).resolves.toBeDefined();
    expect(asked).toEqual(['quaternius/Mushnub.glb', 'quaternius/Mushnub.glb']);
  });
});
