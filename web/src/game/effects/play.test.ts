import type Phaser from 'phaser';
import { describe, expect, it, vi } from 'vitest';

import { recordingScene } from '@/testing/recording-scene';
import type { Call } from '@/testing/recording-scene';

import { beatsOf } from '../entities/beats';
import { ART_SCALE } from '../keys';
import { REGISTRY } from '../registry';

import { damageType, effectIds, hitEffects } from './ids';
import { parseManifest } from './manifest';
import type { Manifest } from './manifest';
import { loadEffects, play, readyEffects, sheetKey, soundKey } from './play';

/** A recording scene at 1000 ms with the manifest, files and animations given. */
const sceneWith = (manifest?: Manifest, loaded: readonly string[] = [], animated = loaded) =>
  recordingScene({ manifest, loaded, animated });

const attackBeats = beatsOf(
  { kind: 'attack', ticks: 20, hit: 0, cancel: 0.85, cancelable: true },
  1000,
  50,
); // cancel point at 1850
const at = { x: 300, y: 608, facing: -1 as const, beats: attackBeats };
const manifest = (raw: unknown) => parseManifest(raw, effectIds);
const sheet = { sheet: 'effects/x.png', frameWidth: 64, frameHeight: 48, frames: 6, fps: 20 };

describe('play', () => {
  it('plays the drawn version when no art is listed, redrawn each frame until stop takes it away', () => {
    const { scene, named, frame, running } = sceneWith();
    const stop = play(scene, 'attack.warrior', at);
    expect(named('add.graphics')).toHaveLength(1);
    frame(1100);
    expect(named('graphics.clear').length).toBeGreaterThanOrEqual(2);
    expect(named('graphics.lineBetween').length).toBeGreaterThan(0);
    stop();
    stop();
    expect(named('graphics.destroy')).toHaveLength(1);
    expect(running()).toBe(0);
  });

  it('a map change ends whatever still plays', () => {
    const { scene, named, running, shutdown } = sceneWith();
    play(scene, 'skill.heal', at);
    play(scene, 'hit.crit', at);
    expect(running()).toBe(2);
    shutdown();
    expect([running(), named('graphics.destroy').length]).toEqual([0, 2]);
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
    expect(named('add.graphics')).toHaveLength(2); // strike's drawn arc
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

describe('hits', () => {
  it('a hit sparks in its damage type, a crit bursts on top, and a miss only puffs', () => {
    expect(hitEffects('physical', {})).toEqual(['hit.physical']);
    expect(hitEffects('magic', { crit: true })).toEqual(['hit.magic', 'hit.crit']);
    expect(hitEffects('magic', { miss: true, crit: true })).toEqual(['hit.miss']);
    expect([damageType('magic'), damageType('physical'), damageType(null)]).toEqual([
      'magic',
      'physical',
      'physical',
    ]);
  });

  it.each(['hit.physical', 'hit.magic', 'hit.crit', 'hit.miss'] as const)(
    '%s plays at the target, sized to it within bounds, and is gone in a moment',
    (id) => {
      const reach = (size?: { w: number; h: number }) => {
        const { scene, calls, frame, running } = sceneWith();
        play(scene, id, { x: 300, y: 580, facing: 1, ...(size && { size }) });
        let far = 0;
        for (let t = 1000; t <= 1300; t += 10) {
          frame(t);
          for (const [name, ...a] of calls.splice(0)) {
            const n = a.map(Number);
            if (name === 'graphics.lineBetween') {
              const [x0 = 0, y0 = 0, x1 = 0, y1 = 0] = n;
              far = Math.max(far, Math.hypot(x0 - 300, y0 - 580), Math.hypot(x1 - 300, y1 - 580));
            } else if (name.endsWith('Circle')) {
              const [x = 0, y = 0, r = 0] = n;
              far = Math.max(far, Math.hypot(x - 300, y - 580) + r);
            }
          }
          if (t === 1100) {
            expect(running()).toBe(1); // still bright a tenth of a second on
          }
        }
        expect(running()).toBe(0); // gone in a quarter of a second
        return far;
      };
      if (id === 'hit.miss') {
        expect(reach({ w: 300, h: 300 })).toBe(reach()); // a miss is the same small puff on anything
        return;
      }
      expect(reach({ w: 32, h: 28 })).toBeLessThan(reach({ w: 84, h: 60 })); // a rat's spark outsizes a slime's
      expect(reach({ w: 400, h: 400 })).toBe(reach({ w: 900, h: 900 })); // up to a cap
      expect(reach({ w: 4, h: 4 })).toBeGreaterThan(8); // a small target's still shows
    },
  );
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
