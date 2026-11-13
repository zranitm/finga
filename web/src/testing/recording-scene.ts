// A scene for effects tests that records what each play adds, draws, tweens, animates and sounds, and steps frames on its clock.
import type Phaser from 'phaser';

import { REGISTRY } from '@/game/registry';

export type Call = readonly [string, ...unknown[]];

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

export interface Recorded {
  readonly manifest?: unknown;
  /** Texture, sound and animation keys that loaded. */
  readonly loaded?: readonly string[];
  /** Animation keys made, the loaded ones unless given. */
  readonly animated?: readonly string[];
}

/** A scene at 1000 ms. frame(at) moves its clock and runs one frame's update; named(call) is each call of that name's args. */
export function recordingScene({ manifest, loaded = [], animated = loaded }: Recorded = {}) {
  const calls: Call[] = [];
  const updates = new Set<() => void>();
  const shutdowns = new Set<() => void>();
  const has = (key: string) => loaded.includes(key);
  const time = { now: 1000 };
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
    events: {
      on: (event: string, f: () => void) => event === 'update' && updates.add(f),
      once: (event: string, f: () => void) => event === 'shutdown' && shutdowns.add(f),
      off: (event: string, f: () => void) => (event === 'update' ? updates : shutdowns).delete(f),
    },
    time,
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
  const frame = (at: number) => {
    time.now = at;
    for (const f of [...updates]) {
      f();
    }
  };
  return {
    scene: scene as unknown as Phaser.Scene,
    registry: scene.registry,
    calls,
    named,
    frame,
    /** How many plays still redraw each frame. */
    running: () => updates.size,
    /** The scene shuts down, as on a map change. */
    shutdown: () => [...shutdowns].forEach((f) => f()),
  };
}
