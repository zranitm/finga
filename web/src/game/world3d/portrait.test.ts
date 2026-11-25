import { readFileSync } from 'node:fs';
import path from 'node:path';

import { NullEngine } from '@babylonjs/core/Engines/nullEngine';
import type { TransformNode } from '@babylonjs/core/Meshes/transformNode';
import { Scene } from '@babylonjs/core/scene';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { Mock, MockInstance } from 'vitest';

import { pieces } from '@/progression/pieces';
import { OWN_LOOK } from '@/state/wardrobe';
import type { Picks, Portrait } from '@/state/wardrobe';

import type { Figure } from './body';
import { Figures } from './figures';
import { Portraits } from './portrait';

const MODELS = path.resolve(process.cwd(), 'public/models');
const KNIGHT = 'kaykit/Knight.glb'; // the warrior's figure

let engine: NullEngine;
let source: Mock<(f: string) => Uint8Array>;
let portraits: Portraits;
let scene: Scene;
let drawImage: Mock;
let renders: MockInstance<Scene['render']>;
const game = { width: 512, height: 256 } as HTMLCanvasElement; // the game's canvas, NullEngine's size

/** A window's canvas laid out at w × h, its 2D context drawing into drawImage. */
function windowCanvas(w = 200, h = 160): HTMLCanvasElement {
  const canvas = document.createElement('canvas');
  Object.defineProperty(canvas, 'clientWidth', { value: w });
  Object.defineProperty(canvas, 'clientHeight', { value: h });
  vi.spyOn(canvas, 'getContext').mockReturnValue({
    drawImage,
    clearRect: vi.fn(),
  } as unknown as CanvasRenderingContext2D);
  return canvas;
}

beforeEach(() => {
  engine = new NullEngine();
  vi.spyOn(engine, 'getRenderingCanvas').mockReturnValue(game);
  source = vi.fn((f: string) => new Uint8Array(readFileSync(path.join(MODELS, f))));
  portraits = new Portraits(engine, source);
  scene = engine.scenes[0]!;
  drawImage = vi.fn();
  renders = vi.spyOn(Scene.prototype, 'render');
});

afterEach(() => {
  portraits.dispose();
  vi.restoreAllMocks();
  engine.dispose();
});

/** Shows look on p and waits till its figure is in. */
async function shown(p: Portrait, look: Picks = OWN_LOOK): Promise<void> {
  const make = vi.spyOn(Figures.prototype, 'make');
  p.show('warrior', look);
  await (make.mock.results.at(-1)!.value as Promise<Figure>);
  await Promise.resolve(); // the show's own then
  make.mockRestore();
}

const HAT: Picks = { ...OWN_LOOK, headwear: 'pointed_hat' };
/** The pointed hat worn, as HAT shows it. */
const hat = () => scene.meshes.find((m) => m.name === 'Mage_Hat worn');

/** The figures' roots in the portraits' scene, by their model file. */
const roots = () => scene.transformNodes.filter((n) => n.name.endsWith('.glb'));

/** What a figure leaves in the scene and on the engine. */
const counts = () => ({
  meshes: scene.meshes.length,
  nodes: scene.transformNodes.length,
  skeletons: scene.skeletons.length,
  clips: scene.animationGroups.length,
  materials: scene.materials.length,
  textures: engine.getLoadedTexturesCache().length,
});

describe('portraits (outfits.md section 7)', () => {
  it('draw a figure each frame into the game canvas’s corner at its own canvas’s size, and copy that out', async () => {
    const canvas = windowCanvas();
    await shown(portraits.portrait(canvas));
    portraits.draw(0, 16);
    expect(renders).toHaveBeenCalledOnce();
    const viewport = scene.activeCamera!.viewport;
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
    expect(scene.activeCamera!.viewport).toBe(viewport); // the same one, set again
  });

  it('draw nothing till a figure is in, nor once it’s closed, which takes its figure and its canvas out of the frame', async () => {
    const canvas = windowCanvas();
    const p = portraits.portrait(canvas);
    portraits.draw(0, 16);
    expect(renders).not.toHaveBeenCalled();
    await shown(p);
    expect(roots().map((n) => n.name)).toEqual([KNIGHT]);
    p.close();
    const asked = vi.mocked(canvas.getContext).mock.calls.length;
    portraits.draw(16, 16);
    expect([renders.mock.calls.length, drawImage.mock.calls.length]).toEqual([0, 0]);
    expect(vi.mocked(canvas.getContext).mock.calls.length).toBe(asked);
    expect(roots()).toEqual([]);
  });

  it('let a figure that comes in after its portrait closed go', async () => {
    const make = vi.spyOn(Figures.prototype, 'make');
    const p = portraits.portrait(windowCanvas());
    p.show('warrior', OWN_LOOK);
    p.close();
    await (make.mock.results[0]!.value as Promise<Figure>);
    await Promise.resolve();
    expect(roots()).toEqual([]);
  });

  it('draw each of two open portraits with its own figure alone', async () => {
    const a = portraits.portrait(windowCanvas());
    const b = portraits.portrait(windowCanvas(120, 120));
    await shown(a);
    await shown(b, HAT);
    const [mine, theirs] = roots();
    expect(hat()!.isDescendantOf(theirs!)).toBe(true);
    const seen: boolean[][] = [];
    renders.mockImplementation(() => {
      seen.push([mine!.isEnabled(), theirs!.isEnabled()]);
    });
    portraits.draw(0, 16);
    expect(seen).toEqual([
      [true, false],
      [false, true],
    ]);
  });

  it('keep the latest show’s figure, turned as dragged, and let one an older show made go, though it comes in after', async () => {
    const held: { older?: Promise<Figure>; release?: () => void } = {};
    vi.spyOn(Figures.prototype, 'make').mockImplementationOnce(function (this: Figures, look) {
      held.older = new Promise<Figure>((done) => {
        held.release = () => done(this.make(look)); // the spy's once is spent: the real make
      });
      return held.older;
    });
    const p = portraits.portrait(windowCanvas());
    p.turn(0.5);
    p.show('warrior', HAT); // held back
    await shown(p);
    held.release!();
    await held.older;
    await Promise.resolve();
    expect(roots().map((n) => n.name)).toEqual([KNIGHT]);
    expect(hat()).toBeUndefined();
    const root: TransformNode = roots()[0]!;
    expect(root.rotation.y).toBe(0.5);
    p.turn(0.25);
    expect(root.rotation.y).toBe(0.75);
  });

  it('load each model once while the world is open: a window opened again loads none', async () => {
    const first = portraits.portrait(windowCanvas());
    await shown(first, { ...OWN_LOOK, headwear: 'pointed_hat' });
    first.close();
    source.mockClear();
    const again = portraits.portrait(windowCanvas());
    await shown(again, { ...OWN_LOOK, headwear: 'pointed_hat' });
    expect(source).not.toHaveBeenCalled();
    expect(roots().map((n) => n.name)).toEqual([KNIGHT]);
  });

  it('leave nothing of their figures as they close, however many were tried on: the models’ and palette’s own stay as they were', async () => {
    const looks: Picks[] = [
      OWN_LOOK,
      { ...OWN_LOOK, hideHeadwear: true },
      ...pieces
        .filter((p) => p.slot !== 'weapon' || p.kind === 'one_handed')
        .map((p) => ({ ...OWN_LOOK, [p.slot]: p.id })),
    ];
    const tryAll = async () => {
      const p = portraits.portrait(windowCanvas());
      for (const look of looks) {
        await shown(p, look);
        portraits.draw(0, 16);
      }
      p.close();
    };
    expect(counts()).toMatchObject({ meshes: 0, nodes: 0, skeletons: 0, clips: 0 });
    await tryAll();
    const cached = counts();
    expect(cached).toMatchObject({ meshes: 0, nodes: 0, skeletons: 0, clips: 0 }); // the files' own aren't in the scene
    await tryAll();
    expect(counts()).toEqual(cached);
    expect(looks.length * 2).toBeGreaterThanOrEqual(20);
  });
});
