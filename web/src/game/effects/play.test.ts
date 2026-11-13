import type Phaser from 'phaser';
import { describe, expect, it, vi } from 'vitest';

import { skillById } from '@/progression/skills';

import { beatsOf } from '../entities/beats';
import { ART_SCALE } from '../keys';
import { REGISTRY } from '../registry';

import { effectIds } from './ids';
import { parseManifest } from './manifest';
import type { Manifest } from './manifest';
import { loadEffects, play, readyEffects, sheetKey, soundKey } from './play';

type Call = readonly [string, ...unknown[]];

/** A game object that records each call as `kind.method`, and chains. */
function recorded(calls: Call[], kind: string): unknown {
  const self: unknown = new Proxy(() => undefined, {
    get: (_t, key) =>
      key === 'then'
        ? undefined
        : (...args: unknown[]) => {
            calls.push([`${kind}.${String(key)}`, ...args]);
            return self;
          },
  });
  return self;
}

/** A scene at 1000 ms with the manifest, files and animations given, recording what it adds, tweens, animates and plays. */
function sceneWith(manifest?: Manifest, loaded: readonly string[] = [], animated = loaded) {
  const calls: Call[] = [];
  const has = (key: string) => loaded.includes(key);
  const scene = {
    add: new Proxy(
      {},
      {
        get:
          (_t, kind) =>
          (...args: unknown[]) => {
            calls.push([`add.${String(kind)}`, ...args]);
            return recorded(calls, String(kind));
          },
      },
    ),
    tweens: recorded(calls, 'tweens'),
    time: { now: 1000 },
    registry: new Map<string, unknown>(manifest ? [[REGISTRY.effects, manifest]] : []),
    textures: { exists: has },
    anims: {
      exists: (key: string) => animated.includes(key),
      create: (c: unknown) => calls.push(['anims.create', c]),
      generateFrameNumbers: (key: string, c: unknown) => ({ key, ...(c as object) }),
    },
    cache: { audio: { exists: has } },
    sound: { play: (key: string) => calls.push(['sound.play', key]) },
  };
  const named = (name: string) => calls.filter((c) => c[0] === name).map((c) => c.slice(1));
  return { scene: scene as unknown as Phaser.Scene, registry: scene.registry, calls, named };
}

const attackBeats = beatsOf(
  { kind: 'attack', ticks: 20, hit: 0, cancel: 0.85, cancelable: true },
  1000,
  50,
); // cancel point at 1850
const at = { x: 300, y: 608, facing: -1 as const, beats: attackBeats };
const manifest = (raw: unknown) => parseManifest(raw, effectIds);
const sheet = { sheet: 'effects/x.png', frameWidth: 64, frameHeight: 48, frames: 6, fps: 20 };

describe('play', () => {
  it("draws a melee class's slash through its own box when no art is listed, and stop takes it away", () => {
    const { scene, named } = sceneWith();
    const stop = play(scene, 'attack.warrior', at);
    const points = named('graphics.lineTo') as [number, number][];
    expect(Math.min(...points.map(([x]) => x))).toBeCloseTo(300 - 64); // the warrior's 64 × 56 box, facing left
    expect(Math.max(...points.map(([, y]) => y))).toBeCloseTo(
      608 - 28 + 28 * Math.sin((80 * Math.PI) / 180),
    );
    const fade = named('tweens.add')[0]?.[0] as { alpha: number; duration: number };
    expect(fade.alpha).toBe(0);
    expect(fade.duration).toBeCloseTo((850 * 2) / 3); // from the strike's end to the cancel point
    stop();
    expect(named('graphics.destroy')).toHaveLength(1);
  });

  it("flies a ranged class's arrow or orb from the middle over its range", () => {
    const bow = sceneWith();
    play(bow.scene, 'attack.bowman', at);
    expect(bow.named('add.rectangle')).toEqual([[300, 580, 16, 3, 0xf0ead6]]);
    expect(bow.named('tweens.add')[0]?.[0]).toMatchObject({ x: 300 - 420, duration: 300 });
    const staff = sceneWith();
    play(staff.scene, 'attack.magician', { ...at, facing: 1 });
    expect(staff.named('add.circle')).toEqual([[300, 580, 5, 0xb070f0]]);
    expect(staff.named('tweens.add')[0]?.[0]).toMatchObject({ x: 300 + 360 });
  });

  it("draws a skill at its tier's stage, the first one by default", () => {
    const spark = skillById('spark')!;
    const reach = (tier?: number) => {
      const { scene, named } = sceneWith();
      play(scene, 'skill.spark', { ...at, ...(tier !== undefined && { tier }) });
      const line = named('add.line')[0];
      return [line?.[2], line?.[4]];
    };
    expect(reach()).toEqual([300, 300 - spark.stages[0]!.shape!.range!]);
    expect(reach(2)).toEqual([300, 300 - spark.stages[2]!.shape!.range!]);
  });

  it('plays a listed sheet that loaded as one animation to the cancel point, anchored, scaled and flipped', () => {
    const key = sheetKey('attack.thief');
    const { scene, named } = sceneWith(
      manifest({ 'attack.thief': { ...sheet, anchor: [0.2, 1] } }),
      [key],
    );
    const stop = play(scene, 'attack.thief', at);
    expect(named('add.graphics')).toEqual([]);
    expect(named('add.sprite')).toEqual([[300, 608, key]]);
    expect(named('sprite.setOrigin')).toEqual([[0.2, 1]]);
    expect(named('sprite.setScale')).toEqual([[ART_SCALE]]);
    expect(named('sprite.setFlipX')).toEqual([[true]]);
    expect(named('sprite.play')).toEqual([[{ key, duration: 850 }]]);
    expect(named('sprite.once')[0]?.[0]).toBe('animationcomplete');
    stop();
    expect(named('sprite.destroy')).toHaveLength(1);
  });

  it('falls back to the drawn version when a listed sheet did not load, saying so once', () => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => undefined);
    const { scene, named } = sceneWith(manifest({ 'skill.strike': sheet }));
    play(scene, 'skill.strike', at);
    play(scene, 'skill.strike', at);
    expect(named('add.sprite')).toEqual([]);
    expect(named('add.rectangle')).toHaveLength(2); // strike's box
    expect(warn).toHaveBeenCalledExactlyOnceWith(
      'finga: effect skill.strike: effects/x.png did not load, so its drawn version plays',
    );
  });

  it('plays a sound only when it is listed and loaded', () => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => undefined);
    const listed = manifest({
      'skill.heal': { sound: 'effects/heal.ogg' },
      'skill.haste': { sound: 'effects/haste.ogg' },
    });
    const { scene, named } = sceneWith(listed, [soundKey('skill.heal')]);
    play(scene, 'skill.heal', at);
    play(scene, 'skill.haste', at);
    play(scene, 'skill.focus', at);
    expect(named('sound.play')).toEqual([[soundKey('skill.heal')]]);
    expect(warn).toHaveBeenCalledExactlyOnceWith(
      'finga: effect skill.haste: effects/haste.ogg did not load, so it plays silent',
    );
  });

  it('plays nothing for an id with no drawn version, saying so once', () => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => undefined);
    const { scene, calls } = sceneWith();
    play(scene, 'mob.unknown', at)();
    expect(calls).toEqual([]);
    expect(warn).toHaveBeenCalledOnce();
  });
});

describe('loading effects', () => {
  it('queues each listed sheet and sound under assets/ once the manifest is in, and logs what it refused', () => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => undefined);
    const calls: Call[] = [];
    let complete: ((key: string, type: string, raw: unknown) => void) | undefined;
    const load = {
      json: (...a: unknown[]) => calls.push(['json', ...a]),
      once: (event: string, f: typeof complete) => {
        calls.push(['once', event]);
        complete = f;
      },
      spritesheet: (...a: unknown[]) => calls.push(['spritesheet', ...a]),
      audio: (...a: unknown[]) => calls.push(['audio', ...a]),
    };
    const done = vi.fn();
    loadEffects(load as unknown as Phaser.Loader.LoaderPlugin, done);
    expect(calls).toEqual([
      ['json', 'effects-manifest', 'assets/effects/manifest.json'],
      ['once', 'filecomplete-json-effects-manifest'],
    ]);
    complete?.('effects-manifest', 'json', {
      'skill.heal': { ...sheet, sound: 'effects/heal.ogg' },
      'skill.nope': sheet,
    });
    expect(calls.slice(2)).toEqual([
      [
        'spritesheet',
        sheetKey('skill.heal'),
        'assets/effects/x.png',
        { frameWidth: 64, frameHeight: 48 },
      ],
      ['audio', soundKey('skill.heal'), 'assets/effects/heal.ogg'],
    ]);
    expect(warn).toHaveBeenCalledExactlyOnceWith(
      'finga: effects manifest: "skill.nope" is not an effect id',
    );
    expect([...(done.mock.calls[0]?.[0] as Manifest).entries.keys()]).toEqual(['skill.heal']);
  });

  it('animates each sheet that loaded and hands the manifest to play; with none read, every effect draws', () => {
    const listed = manifest({ 'skill.heal': sheet, 'skill.haste': sheet });
    const loaded = sceneWith(undefined, [sheetKey('skill.heal')], []);
    readyEffects(loaded.scene, listed);
    expect(loaded.named('anims.create')).toEqual([
      [
        {
          key: sheetKey('skill.heal'),
          frames: { key: sheetKey('skill.heal'), start: 0, end: 5 },
          frameRate: 20,
        },
      ],
    ]);
    expect(loaded.registry.get(REGISTRY.effects)).toBe(listed);
    const none = sceneWith();
    readyEffects(none.scene, null);
    expect(none.registry.get(REGISTRY.effects)).toEqual({ entries: new Map(), problems: [] });
  });
});
