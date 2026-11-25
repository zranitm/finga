import { readFileSync } from 'node:fs';
import path from 'node:path';

import { NullEngine } from '@babylonjs/core/Engines/nullEngine';
import type { TransformNode } from '@babylonjs/core/Meshes/transformNode';
import { Scene } from '@babylonjs/core/scene';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { Mock, MockInstance } from 'vitest';

import { OWN_LOOK } from '@/state/wardrobe';

import { Portraits } from './portrait';

const MODELS = path.resolve(process.cwd(), 'public/models');
const source = (f: string) => new Uint8Array(readFileSync(path.join(MODELS, f)));
const KNIGHT = 'kaykit/Knight.glb'; // the warrior's figure

let engine: NullEngine;
let portraits: Portraits;
let canvas: HTMLCanvasElement;
let drawImage: Mock;
let renders: MockInstance<Scene['render']>;
const game = { width: 512, height: 256 } as HTMLCanvasElement; // the game's canvas, NullEngine's size

beforeEach(() => {
  engine = new NullEngine();
  vi.spyOn(engine, 'getRenderingCanvas').mockReturnValue(game);
  portraits = new Portraits(engine, source);
  canvas = document.createElement('canvas');
  Object.defineProperty(canvas, 'clientWidth', { value: 200 });
  Object.defineProperty(canvas, 'clientHeight', { value: 160 });
  drawImage = vi.fn();
  vi.spyOn(canvas, 'getContext').mockReturnValue({
    drawImage,
    clearRect: vi.fn(),
  } as unknown as CanvasRenderingContext2D);
  renders = vi.spyOn(Scene.prototype, 'render');
});

afterEach(() => {
  portraits.dispose();
  vi.restoreAllMocks();
  engine.dispose();
});

/** The portrait scene's figure root, once its model is in. */
async function figureIn(): Promise<TransformNode> {
  let root: TransformNode | null = null;
  await vi.waitFor(() => {
    root = engine.scenes.flatMap((s) => s.transformNodes).find((n) => n.name === KNIGHT) ?? null;
    expect(root).not.toBeNull();
  });
  return root!;
}

describe('a portrait (outfits.md section 7)', () => {
  it('draws its figure each frame into the game canvas’s corner at its own canvas’s size, and copies that out', async () => {
    const p = portraits.portrait(canvas);
    p.show('warrior', OWN_LOOK);
    await figureIn();
    portraits.draw(0, 16);
    expect(renders).toHaveBeenCalledOnce();
    const viewport = engine.scenes[0]!.activeCamera!.viewport;
    expect([viewport.x, viewport.y, viewport.width, viewport.height]).toEqual([
      0,
      0,
      200 / 512,
      160 / 256,
    ]);
    expect([canvas.width, canvas.height]).toEqual([200, 160]);
    expect(drawImage).toHaveBeenCalledWith(game, 0, 256 - 160, 200, 160, 0, 0, 200, 160);
    portraits.draw(16, 16);
    expect(drawImage).toHaveBeenCalledTimes(2);
  });

  it('draws nothing till its figure is in, nor once it’s closed: its scene goes', async () => {
    const p = portraits.portrait(canvas);
    portraits.draw(0, 16);
    expect(renders).not.toHaveBeenCalled();
    p.show('warrior', OWN_LOOK);
    await figureIn();
    p.close();
    portraits.draw(16, 16);
    expect([renders.mock.calls.length, drawImage.mock.calls.length]).toEqual([0, 0]);
    expect(engine.scenes).toEqual([]);
  });

  it('keeps the latest show’s figure, turned as dragged, and lets an older one go', async () => {
    const p = portraits.portrait(canvas);
    p.turn(0.5);
    p.show('warrior', { ...OWN_LOOK, outfit: 'knight_plate' });
    p.show('warrior', OWN_LOOK);
    const root = await figureIn();
    await vi.waitFor(() => {
      expect(engine.scenes[0]!.transformNodes.filter((n) => n.name === KNIGHT)).toHaveLength(1);
    });
    expect(root.rotation.y).toBe(0.5);
    p.turn(0.25);
    expect(root.rotation.y).toBe(0.75);
  });
});
