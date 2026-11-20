import { readFileSync } from 'node:fs';
import path from 'node:path';

import { NullEngine } from '@babylonjs/core/Engines/nullEngine';
import { DirectionalLight } from '@babylonjs/core/Lights/directionalLight';
import '@babylonjs/core/Lights/Shadows/shadowGeneratorSceneComponent';
import { Vector3 } from '@babylonjs/core/Maths/math.vector';
import { Mesh } from '@babylonjs/core/Meshes/mesh';
import { Scene } from '@babylonjs/core/scene';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { movement } from '@/gamedata/movement';
import { servePublic } from '@/testing/public-on-disk';
import { TINY, tinyFile } from '@/testing/region-fixture';

import { sunShadows } from './body';
import type { Lighting } from './body';
import { buildChunk } from './chunk-build';
import type { BuiltChunk, ChunkRequest, ChunkSource } from './chunk-build';
import { Models } from './models';
import type { Place } from './place';
import { loadRegion, readChunk } from './region-bake';
import { RegionGround } from './region-ground';
import { buildRegion, chunkDistance, chunksNear, detailAt } from './region-place';
import { Palette } from './toon';

const MODELS = path.resolve(process.cwd(), 'public/models');
const WHOLE = 16 * 16 * 6; // the overview's indices with no holes: 128 m every 8 m

/** Once every promise settled so far has run what waits on it: no clock, just the queue. */
const drained = () => new Promise<void>((r) => setImmediate(r));

/** Chunks the test hands over itself: each waits till answer builds it, or fails it where fails says. */
class Held implements ChunkSource {
  readonly built: BuiltChunk[] = [];
  private waiting: {
    req: ChunkRequest;
    resolve: (b: BuiltChunk) => void;
    reject: (e: Error) => void;
  }[] = [];

  constructor(
    private readonly asked: string[],
    private readonly fails: (url: string) => boolean,
    private readonly alter: (b: BuiltChunk) => BuiltChunk = (b) => b,
  ) {}

  build(req: ChunkRequest): Promise<BuiltChunk> {
    this.asked.push(req.url.slice(req.url.indexOf('chunks/')));
    return new Promise((resolve, reject) => this.waiting.push({ req, resolve, reject }));
  }

  close(): void {
    this.waiting = [];
  }

  /** Builds every chunk asked so far and hands each over, then lets the region take them. */
  async answer(): Promise<void> {
    for (const { req, resolve, reject } of this.waiting.splice(0)) {
      if (this.fails(req.url)) {
        reject(new Error('gone'));
      } else {
        const b = this.alter(await buildChunk(req));
        this.built.push(b);
        resolve(b);
      }
    }
    await drained();
  }
}

/** Models that keep each load, so a test can wait for the kit models a region asked for. */
class Loading extends Models {
  readonly loads: Promise<unknown>[] = [];

  override load(file: string) {
    const c = super.load(file);
    this.loads.push(c);
    return c;
  }
}

describe('which chunks a target wants', () => {
  it('measures to the nearest point of a chunk, 0 inside it', () => {
    expect(chunkDistance(1, 1, 64, 100, 100)).toBe(0);
    expect(chunkDistance(1, 1, 64, 30, 100)).toBe(34);
    expect(chunkDistance(1, 1, 64, 30, 30)).toBeCloseTo(Math.hypot(34, 34));
  });

  it('draws every sample within 64 m, every 2nd within 128 m and every 4th beyond', () => {
    expect([0, 63.9, 64, 127.9, 128, 300].map(detailAt)).toEqual([0, 0, 1, 1, 2, 2]);
  });

  it('lists the chunks within reach, held to the region, nearest first', () => {
    const near = chunksNear(16, 16, 64, 100, 30, 100);
    expect(near[0]).toEqual({ cx: 1, cz: 0, d: 0 });
    expect(near.map((c) => c.d)).toEqual([...near.map((c) => c.d)].sort((a, b) => a - b));
    expect(near.every((c) => c.d <= 100 && c.cx >= 0 && c.cz >= 0)).toBe(true);
    expect(near.some((c) => c.cx === 3 && c.cz === 0)).toBe(true); // 92 m east
    expect(near.some((c) => c.cx === 3 && c.cz === 2)).toBe(false); // 128 m off
  });
});

describe('a region drawn from its bake', () => {
  let engine: NullEngine;
  let scene: Scene;
  let lighting: Lighting;
  let asked: string[];
  let source: Held;
  let models: Loading;
  let place: Place | undefined;
  let ground: RegionGround;
  let landed: number;
  const meshes = (prefix: string) =>
    scene.meshes.filter((m) => m.name.startsWith(prefix)) as Mesh[];
  const chunkNames = () =>
    meshes('chunk ')
      .map((m) => m.name)
      .sort();
  const props = (k: string) => scene.getTransformNodeByName(`chunk ${k} props`);
  const copies = (k: string) =>
    (props(k)?.getChildMeshes(true) ?? []).filter((m): m is Mesh => m instanceof Mesh);
  const overview = (name = 'overview') => scene.getMeshByName(name) as Mesh;

  async function built(
    fails: (url: string) => boolean = () => false,
    alter?: (b: BuiltChunk) => BuiltChunk,
  ): Promise<void> {
    source = new Held(asked, fails, alter);
    models = new Loading(scene, (f) => new Uint8Array(readFileSync(path.join(MODELS, f))));
    const { region, overview } = await loadRegion(TINY);
    ground = new RegionGround(region, movement.max_slope);
    landed = 0;
    place = buildRegion(
      scene,
      { ref: TINY, region, overview, ground },
      lighting,
      models,
      source,
      () => landed++,
    );
  }

  /** The kit models every chunk made so far asked for, loaded and copied in. */
  async function kitsIn(): Promise<void> {
    await Promise.allSettled(models.loads);
    await drained();
  }

  /** Follows (x, z) till the chunks round it are built and made, and their props are in. */
  async function settle(x: number, z: number): Promise<void> {
    place!.follow(x, z);
    await source.answer();
    place!.follow(x, z);
    place!.follow(x, z); // two a frame
    await kitsIn();
  }

  beforeEach(() => {
    servePublic();
    engine = new NullEngine();
    engine.getCaps().instancedArrays = true;
    scene = new Scene(engine);
    const sun = new DirectionalLight('sun', new Vector3(0.35, -1, 0.6).normalize(), scene);
    lighting = { palette: new Palette(scene), shadows: sunShadows(sun, 256) };
    asked = [];
  });

  afterEach(() => {
    place?.dispose();
    place = undefined;
    scene.dispose();
    engine.dispose();
    vi.unstubAllGlobals();
  });

  it('starts with the whole overview, then makes the chunks round the target, two a frame, cutting the overview where they stand', async () => {
    await built();
    expect(overview().getTotalIndices()).toBe(WHOLE);
    place!.follow(30, 60);
    expect(asked).toEqual(['chunks/0_0.bin', 'chunks/0_1.bin', 'chunks/1_0.bin', 'chunks/1_1.bin']);
    expect(chunkNames()).toEqual([]);
    await source.answer();
    place!.follow(30, 60);
    expect(chunkNames().length).toBe(2);
    expect(overview('overview skirts').getTotalIndices()).toBe(2 * 8 * 6); // where each meets one not yet drawn
    place!.follow(30, 60);
    expect(chunkNames()).toEqual(['chunk 0_0', 'chunk 0_1', 'chunk 1_0', 'chunk 1_1']);
    expect(overview().getTotalIndices()).toBe(0);
    expect(overview('overview skirts').getTotalIndices()).toBe(0);
    expect(asked.length).toBe(4); // each asked once
    const c = meshes('chunk 1_1')[0]!;
    expect(c.position.asArray()).toEqual([64, 0, 64]);
    expect(c.getTotalIndices()).toBe((32 * 32 * 2 + 4 * 32 * 2) * 3); // every sample, with its skirts
  });

  it('draws each chunk at its detail and its props within 160 m, and frees it past 320 m', async () => {
    await built();
    await settle(30, 60);
    expect(chunkNames().length).toBe(4);
    for (const k of ['0_0', '1_1']) {
      const oaks = readChunk(tinyFile(`chunks/${k}.bin`)).props.models.length; // the tiny plan scatters oaks alone
      for (const part of copies(k)) {
        expect(part.thinInstanceCount).toBe(oaks);
        expect((part.getChildMeshes(true)[0] as Mesh).thinInstanceCount).toBe(oaks); // its ink
      }
    }
    expect(lighting.shadows.getShadowMap()!.renderList).toHaveLength(0); // no casting till the cascades come
    for (const part of copies('1_1')) {
      const { minimumWorld: lo, maximumWorld: hi } = part.getBoundingInfo().boundingBox;
      for (const m of part.thinInstanceGetWorldMatrices()) {
        const at = m.getTranslation();
        expect([
          at.x >= lo.x - 1e-3,
          at.x <= hi.x + 1e-3,
          at.z >= lo.z - 1e-3,
          at.z <= hi.z + 1e-3,
        ]).toEqual([true, true, true, true]); // culled by where its copies stand
      }
    }
    const templates = scene
      .getTransformNodeByName('kit models')!
      .getChildMeshes(false)
      .map((m) => m.name);
    expect(templates.length).toBeGreaterThan(0);
    expect(new Set(templates).size).toBe(templates.length); // each kit model loaded once for every chunk
    const [a, b] = [copies('0_0')[0]!, copies('1_1')[0]!]; // the same part in two chunks
    expect(a.name.split(' ').slice(0, -1)).toEqual(b.name.split(' ').slice(0, -1));
    expect(a.geometry).not.toBe(b.geometry); // each its own: thin instances' matrices live on the geometry
    expect(a.getChildMeshes(true)[0]!.geometry).toBe(a.geometry); // its ink draws the same instances

    place!.follow(-100, 60); // the west chunks 100 m off, the east ones 164 m
    expect(meshes('chunk 0_0')[0]!.getTotalIndices()).toBe((16 * 16 * 2 + 4 * 16 * 2) * 3);
    expect(meshes('chunk 1_0')[0]!.getTotalIndices()).toBe((8 * 8 * 2 + 4 * 8 * 2) * 3);
    expect(props('0_0')!.isEnabled()).toBe(true);
    expect(props('1_0')!.isEnabled()).toBe(false);

    place!.follow(-330, 60); // the west chunks 330 m off: freed; the east ones too
    expect(chunkNames()).toEqual([]);
    expect(props('0_0')).toBeNull();
    expect(overview().getTotalIndices()).toBe(WHOLE);
    expect(overview('overview skirts').getTotalIndices()).toBe(0);
  });

  it("leaves the overview where a chunk won't come, asking for it once", async () => {
    await built((url) => url.endsWith('1_1.bin'));
    const said = vi.spyOn(console, 'info').mockImplementation(() => undefined);
    await settle(30, 60);
    for (let i = 0; i < 5; i++) {
      place!.follow(30, 60);
    }
    expect(chunkNames()).toEqual(['chunk 0_0', 'chunk 0_1', 'chunk 1_0']);
    expect(asked.filter((a) => a.endsWith('1_1.bin')).length).toBe(1);
    expect(overview().getTotalIndices()).toBe(8 * 8 * 6);
    const skirts = overview('overview skirts');
    expect(skirts.getTotalIndices()).toBe(2 * 8 * 6); // 1_0's north and 0_1's east, which meet it
    const at = skirts.getVerticesData('position')!;
    const skirtOf = (cx: number, cz: number) =>
      source.built.find((b) => b.cx === cx && b.cz === cz)!.ground.skirt;
    for (let v = 0; v < at.length / 3; v += 2) {
      const [x, y, z, foot] = [at[3 * v]!, at[3 * v + 1]!, at[3 * v + 2]!, at[3 * v + 4]!];
      const north = v < 18; // 1_0's side comes first, its nine tops and feet
      expect(north ? z : x).toBe(64);
      expect(y - foot).toBeCloseTo(north ? skirtOf(1, 0) : skirtOf(0, 1), 5);
    }
    expect(said).toHaveBeenCalledWith(expect.stringContaining('chunk 1_1 of tiny'));
    said.mockRestore();
  });

  it('takes a chunk whose ground doesn’t fit the region as one that didn’t come', async () => {
    await built(undefined, (b) =>
      b.cx === 1 && b.cz === 1 ? { ...b, sim: { ...b.sim, n: 17 } } : b,
    );
    const said = vi.spyOn(console, 'info').mockImplementation(() => undefined);
    await settle(30, 60);
    place!.follow(30, 60);
    expect(chunkNames()).toEqual(['chunk 0_0', 'chunk 0_1', 'chunk 1_0']);
    expect([landed, ground.covers(32, 32, 0), ground.covers(96, 96, 0)]).toEqual([3, true, false]);
    expect(said).toHaveBeenCalledWith(
      "finga: chunk 1_1 of tiny didn't load: chunk 1_1 has 17 samples a side; the region's have 33",
    );
  });

  it('makes no chunk that was built while the target went out of its reach', async () => {
    await built();
    place!.follow(30, 60);
    await source.answer(); // all four built
    place!.follow(-400, 60);
    expect(chunkNames()).toEqual([]);
    place!.follow(-400, 60);
    expect(chunkNames()).toEqual([]);
  });

  it('leaves no props for a chunk freed before its models came', async () => {
    await built();
    place!.follow(30, 60);
    await source.answer();
    place!.follow(30, 60); // two made, their oaks still loading
    expect(chunkNames().length).toBe(2);
    place!.follow(-400, 60); // and freed
    await kitsIn();
    expect(
      scene.getTransformNodeByName('kit models')!.getChildMeshes(false).length,
    ).toBeGreaterThan(0);
    expect(scene.meshes.filter((m) => / \d+_\d+$/.test(m.name))).toEqual([]);
  });

  it('copies no kit model into a region gone while it loaded', async () => {
    await built();
    await source.answer();
    place!.follow(30, 60);
    await source.answer();
    place!.follow(30, 60); // two made, their oaks loading
    expect(models.loads.length).toBeGreaterThan(0);
    place!.dispose();
    place = undefined;
    await kitsIn();
    expect(scene.meshes).toEqual([]);
  });

  it('asks for four chunks at a time, nearest first, in a region with more in reach', async () => {
    const disk = globalThis.fetch;
    vi.stubGlobal('fetch', async (url: string) => {
      const r = await disk(url);
      return url.endsWith('region.json')
        ? Response.json({ ...(await r.json()), chunks: [4, 4] })
        : r;
    }); // the tiny bake, said to be 4 by 4 chunks
    await built(); // none answered
    place!.follow(100, 100);
    place!.follow(100, 100);
    expect(asked).toEqual(['chunks/1_1.bin', 'chunks/2_1.bin', 'chunks/1_2.bin', 'chunks/1_0.bin']); // 0, 28, 28 and 36 m off
  });

  it('drops a chunk that comes after it went out of reach, and one that comes after the region went', async () => {
    await built();
    place!.follow(30, 60);
    place!.follow(-400, 60);
    await source.answer();
    place!.follow(30, 60); // asked again, as they're back in reach
    expect(chunkNames()).toEqual([]);
    place!.dispose();
    place = undefined;
    await source.answer();
    expect(scene.fogMode).toBe(Scene.FOGMODE_NONE);
    expect(scene.meshes).toEqual([]);
  });

  it('hands each chunk’s ground to the sim as it comes, drawn or dropped, and says so; none once the region went', async () => {
    await built();
    place!.follow(30, 60);
    expect(ground.covers(30, 60, 0)).toBe(false);
    place!.follow(-400, 60); // out of reach before they come
    await source.answer();
    expect([landed, ground.covers(64, 64, 64)]).toEqual([4, true]);
    expect(chunkNames()).toEqual([]); // not drawn
    const c = readChunk(tinyFile('chunks/1_0.bin'));
    expect(ground.geometry.terrain.heights[32 * 65 + 64]).toBe(c.heights[(32 + 1) * 35 + 32 + 1]);

    await built();
    place!.follow(30, 60);
    place!.dispose();
    place = undefined;
    await source.answer();
    expect([landed, ground.covers(0, 0, 0)]).toEqual([0, false]);
  });
});
