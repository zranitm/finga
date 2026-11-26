import { existsSync, readFileSync } from 'node:fs';
import path from 'node:path';

import { VertexBuffer } from '@babylonjs/core/Buffers/buffer';
import { TargetCamera } from '@babylonjs/core/Cameras/targetCamera';
import { NullEngine } from '@babylonjs/core/Engines/nullEngine';
import { Vector3 } from '@babylonjs/core/Maths/math.vector';
import type { AbstractMesh } from '@babylonjs/core/Meshes/abstractMesh';
import type { Mesh } from '@babylonjs/core/Meshes/mesh';
import { Scene } from '@babylonjs/core/scene';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { servePublic } from '@/testing/public-on-disk';
import { builtShaders } from '@/testing/shaders';

import { beatsOf } from '../world3d/beats';
import { loadMap } from '../world3d/map';
import type { WorldMap } from '../world3d/map';
import { Palette } from '../world3d/toon';

import type { Art, Fx } from './fx';
import { effectIds } from './ids';
import { MANIFEST_FILE, parseManifest } from './manifest';

const played: string[] = [];
const sounds = { loaded: false };
vi.mock('./sounds', () => ({
  loadSounds: (
    listed: readonly (readonly [string, string])[],
    into: Map<string, { play(): void }>,
  ) => {
    for (const [id, file] of listed.filter(([, f]) => !f.includes('broken'))) {
      into.set(id, { play: () => played.push(file) });
    }
    sounds.loaded = true;
    return Promise.resolve();
  },
}));

const sheet = { sheet: 'effects/x.png', frameWidth: 128, frameHeight: 64, frames: 6, fps: 20 };
const at = { x: 4, y: 1, z: 20, yaw: -Math.PI / 2 };
const attackBeats = beatsOf(
  { kind: 'attack', ticks: 20, hit: 0, cancel: 0.3, ready: 0.85, cancelable: true },
  1000,
  50,
); // ready point at 1850

let engine: NullEngine;
let scene: Scene;
let map: WorldMap;

/** A fresh page's art, read from the manifest served as raw. */
async function artOf(raw: unknown): Promise<{ art: Art; Fx: typeof Fx }> {
  vi.resetModules();
  vi.stubGlobal('fetch', () => Promise.resolve(Response.json(raw)));
  const fx = await import('./fx');
  return { art: await fx.loadArt(), Fx: fx.Fx };
}

describe('effects', () => {
  const sparks = () => scene.transformNodes.filter((n) => n.name === 'spark');

  beforeEach(async () => {
    engine = new NullEngine();
    scene = new Scene(engine);
    servePublic();
    map = await loadMap('shroom_grove');
    played.length = 0;
    sounds.loaded = false;
  });

  afterEach(() => {
    scene.dispose();
    engine.dispose();
  });

  it('what ships parses clean, and every file it lists is there', () => {
    const publicDir = path.resolve(process.cwd(), 'public');
    const raw: unknown = JSON.parse(readFileSync(path.join(publicDir, MANIFEST_FILE), 'utf8'));
    const m = parseManifest(raw, effectIds);
    expect(m.problems).toEqual([]);
    const files = [...m.entries.values()].flatMap((e) => [e.sheet?.file, e.sound]);
    expect(files.filter((f) => f && !existsSync(path.join(publicDir, 'assets', f)))).toEqual([]);
  });

  it('plays the drawn version when no art is listed, and stop takes it away', async () => {
    const { art, Fx } = await artOf({});
    const fx = new Fx(scene, map, art, () => 1000, new Palette(scene));
    const stop = fx.play('hit.crit', at);
    expect(sparks()).toHaveLength(1);
    stop();
    stop();
    expect(sparks()).toHaveLength(0);
  });

  it('keeps a spark’s shapes once a map, every spark of that size sharing them, and the map’s end takes them away', async () => {
    const { art, Fx } = await artOf({});
    const fx = new Fx(scene, map, art, () => 1000, new Palette(scene));
    const before = scene.meshes.length;
    const kept = () => scene.meshes.filter((m) => m.name.startsWith('kept ')) as Mesh[];
    fx.play('hit.physical', at);
    fx.play('hit.physical', at);
    expect(kept()).toHaveLength(2); // the rays and their edge
    const drawn = scene.meshes.filter((m) => m.name === 'spark') as Mesh[];
    expect(drawn).toHaveLength(4);
    expect(new Set(drawn.map((m) => m.geometry))).toEqual(new Set(kept().map((m) => m.geometry)));
    fx.play('hit.physical', { ...at, size: { radius: 2, height: 3 } });
    expect(kept()).toHaveLength(4); // a bigger target's
    fx.dispose();
    expect(scene.meshes).toHaveLength(before);
  });

  it('shows at most 32 sparks at once, the oldest going first', async () => {
    const { art, Fx } = await artOf({});
    const fx = new Fx(scene, map, art, () => 1000, new Palette(scene));
    fx.play('hit.physical', at);
    const [oldest] = sparks();
    for (let i = 1; i < 40; i++) {
      fx.play(i % 2 ? 'hit.crit' : 'hit.miss', at);
    }
    const newest = sparks().at(-1);
    expect(sparks()).toHaveLength(32);
    expect(oldest?.isDisposed()).toBe(true);
    expect(newest?.isDisposed()).toBe(false);
  });

  it('plays a listed sheet as one animation to the ready point: a billboard anchored at the point, mirrored facing west', async () => {
    const { art, Fx } = await artOf({ 'attack.thief': { ...sheet, anchor: [0.25, 1] } });
    const fx = new Fx(scene, map, art, () => 1000, new Palette(scene));
    const stop = fx.play('attack.thief', { ...at, beats: attackBeats });
    const sprites = scene.spriteManagers?.[0]?.sprites ?? [];
    expect(sprites).toHaveLength(1);
    const s = sprites[0]!;
    expect([s.width, s.height]).toEqual([1, 0.5]); // 128 px a metre
    expect([s.position.x, s.position.y, s.position.z]).toEqual([4 + 0.25, 1 + 0.25, 20]);
    expect(s.invertU).toBe(true);
    expect([s.fromIndex, s.toIndex, s.delay]).toEqual([0, 5, 850 / 6]);
    stop();
    expect(scene.spriteManagers?.[0]?.sprites).toHaveLength(0);
  });

  it('falls back to the drawn version when a sheet holds fewer frames than listed, saying so once', async () => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => undefined);
    const { art, Fx } = await artOf({
      'hit.miss': { ...sheet, frameWidth: 300, frameHeight: 300 },
    });
    const fx = new Fx(scene, map, art, () => 1000, new Palette(scene));
    fx.play('hit.miss', at);
    fx.play('hit.miss', at);
    expect(sparks()).toHaveLength(2);
    expect(warn).toHaveBeenCalledExactlyOnceWith(
      'finga: effect hit.miss: effects/x.png holds 1 frames, not 6, so its drawn version plays',
    );
  });

  it('plays a listed sound at the start of its effect, and one that failed plays silent, saying so once', async () => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => undefined);
    const { art, Fx } = await artOf({
      'hit.physical': { sound: 'sfx/hit.ogg' },
      'hit.magic': { sound: 'sfx/broken.ogg' },
    });
    await vi.waitFor(() => expect(sounds.loaded).toBe(true));
    const fx = new Fx(scene, map, art, () => 1000, new Palette(scene));
    fx.play('hit.physical', at);
    fx.play('hit.magic', at);
    fx.play('hit.magic', at);
    fx.play('hit.crit', at);
    expect(played).toEqual(['sfx/hit.ogg']);
    expect(sparks()).toHaveLength(4); // each drawn as well
    expect(warn).toHaveBeenCalledExactlyOnceWith(
      "finga: effect hit.magic: sfx/broken.ogg isn't loaded, so it plays silent",
    );
  });

  it('a manifest that does not load leaves every effect drawn, saying why once', async () => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => undefined);
    vi.resetModules();
    vi.stubGlobal('fetch', () => Promise.resolve(new Response(null, { status: 404 })));
    const { loadArt } = await import('./fx');
    const art = await loadArt();
    expect(art.manifest.entries.size).toBe(0);
    expect(warn).toHaveBeenCalledExactlyOnceWith(
      "finga: effects manifest didn't load (Error: 404), so every effect is drawn",
    );
  });

  it('steps each play every frame till its end, and the map’s end stops what still plays', async () => {
    const { art, Fx } = await artOf({});
    let now = 1000;
    const fx = new Fx(scene, map, art, () => now, new Palette(scene));
    const steps: number[] = [];
    const ended: string[] = [];
    fx.everyFrame(
      now,
      1100,
      (t) => steps.push(t),
      () => ended.push('short'),
    );
    fx.everyFrame(
      now,
      5000,
      () => undefined,
      () => ended.push('long'),
    );
    for (now = 1050; now <= 1150; now += 50) {
      fx.update(now);
    }
    expect([steps, ended]).toEqual([[1000, 1050], ['short']]);
    fx.dispose();
    expect(ended).toEqual(['short', 'long']);
  });

  it('a play that another stops mid-frame takes no step after', async () => {
    const { art, Fx } = await artOf({});
    let now = 1000;
    const fx = new Fx(scene, map, art, () => now, new Palette(scene));
    const steps: string[] = [];
    let second = () => undefined as void;
    fx.everyFrame(
      now,
      5000,
      (t) => {
        steps.push(`first ${t}`);
        if (t > 1000) {
          second();
        }
      },
      () => undefined,
    );
    second = fx.everyFrame(
      now,
      5000,
      (t) => steps.push(`second ${t}`),
      () => undefined,
    );
    now = 1050;
    fx.update(now);
    expect(steps).toEqual(['first 1000', 'second 1000', 'first 1050']);
  });

  it('warms every look, silent and off the map as frames pass, leaving nothing: no look’s first play after, at any stage, builds a shader', async () => {
    const { art, Fx } = await artOf(
      Object.fromEntries(effectIds.map((id) => [id, { sound: 'sfx/hit.ogg' }])),
    );
    await vi.waitFor(() => expect(sounds.loaded).toBe(true));
    let now = 1000;
    const fx = new Fx(scene, map, art, () => now, new Palette(scene));
    new TargetCamera('camera', new Vector3(at.x, at.y + 6, at.z - 8), scene).setTarget(
      new Vector3(at.x, at.y, at.z),
    );
    const frame = async () => {
      now += 50;
      fx.update(now);
      scene.render();
      await new Promise((r) => setTimeout(r, 0)); // a particle system's shader source loads
    };
    const before = new Set<unknown>([...scene.meshes, ...scene.particleSystems]);
    const made = () =>
      [...scene.meshes, ...scene.particleSystems].filter(
        (m) => !before.has(m) && !m.name.startsWith('kept '),
      );
    const near = (m: AbstractMesh) => {
      const v = m.getVerticesData(VertexBuffer.PositionKind);
      const p = Vector3.TransformCoordinates(
        v ? Vector3.FromArray(v) : Vector3.Zero(),
        m.computeWorldMatrix(true),
      );
      return Math.abs(p.x) < 1000 && Math.abs(p.z) < 1000;
    };
    fx.warm();
    expect(builtShaders(engine)).not.toEqual([]); // built as a play starts, before a frame draws it
    const shown: string[] = [];
    for (let i = 0; i < 200 && made().length > 0; i++) {
      shown.push(
        ...scene.meshes
          .filter((m) => made().includes(m) && m.isEnabled() && m.visibility > 0 && near(m))
          .map((m) => m.name),
      );
      await frame();
    }
    expect([made(), shown, played]).toEqual([[], [], []]);
    const built = new Set(builtShaders(engine));
    const body = { ...at, radius: 0.5, height: 1 };
    for (const id of effectIds) {
      for (const tier of [0, 1, 2]) {
        const beats = {
          start: now,
          hit: now + 450,
          strikeEnd: now + 600,
          readyAt: now + 850,
          end: now + 1000,
        };
        fx.play(id, {
          ...at,
          tier,
          beats,
          size: body,
          targets: () => [body],
          follow: () => body,
          lock: () => body,
          reach: 2,
          to: { ...at, x: at.x + 3 },
          width: 1,
          shots: () => [{ ...at, size: 0.3 }],
          landed: () => beats.hit,
          edge: 0xffffff,
        });
      }
    }
    for (let i = 0; i < 30; i++) {
      await frame();
    }
    expect(builtShaders(engine).filter((k) => !built.has(k))).toEqual([]);
  });
});
