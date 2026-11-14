// A scene for effects tests that records what each play adds, draws, tweens, animates and sounds, and steps frames on its clock.
import type Phaser from 'phaser';

import { REGISTRY } from '@/game/registry';

export type Call = readonly [string, ...unknown[]];

const WEBGL_ONLY = new Set<string | symbol>(['filters', 'filterCamera']); // what Phaser's canvas renderer leaves unset

/** A game object that records each call as `kind.method`, and chains; what it holds records the same way, as `sprite.filters.external.addGlow`. */
function recorded(calls: Call[], kind: string, webgl: boolean, owner?: unknown): unknown {
  const self: unknown = new Proxy(() => undefined, {
    get: (_t, key) =>
      key === 'then' || (!webgl && WEBGL_ONLY.has(key))
        ? undefined
        : recorded(calls, `${kind}.${String(key)}`, webgl, self),
    apply: (_t, _this, args: unknown[]) => {
      calls.push([kind, ...args]);
      return owner;
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
  /** How many frames each loaded sheet holds, 16 unless given. */
  readonly held?: Readonly<Record<string, number>>;
  /** False draws as Phaser's canvas renderer does: no filters. */
  readonly webgl?: boolean;
}

/** A scene at 1000 ms. frame(at) moves its clock and runs one frame's update and render; named(call) is each call of that name's args. */
export function recordingScene({
  manifest,
  loaded = [],
  animated = loaded,
  held = {},
  webgl = true,
}: Recorded = {}) {
  const calls: Call[] = [];
  const updates = new Set<() => void>();
  const shutdowns = new Set<() => void>();
  const renders = new Set<() => void>(); // each once, after a frame's updates, as Phaser renders
  const listeners: Record<string, Set<() => void>> = {
    update: updates,
    shutdown: shutdowns,
    render: renders,
  };
  const made = new Set<string>(); // animations created while it runs
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
            return recorded(calls, String(kind), webgl);
          },
      },
    ),
    tweens: recorded(calls, 'tweens', webgl),
    events: {
      on: (event: string, f: () => void) => event === 'update' && updates.add(f),
      once: (event: string, f: () => void) => event !== 'update' && listeners[event]?.add(f),
      off: (event: string, f: () => void) => listeners[event]?.delete(f),
    },
    time,
    registry: new Map<string, unknown>(manifest ? [[REGISTRY.effects, manifest]] : []),
    textures: { exists: has, get: (key: string) => ({ frameTotal: (held[key] ?? 16) + 1 }) },
    anims: {
      exists: (key: string) => animated.includes(key) || made.has(key),
      create: (c: { key: string }) => {
        calls.push(['anims.create', c]);
        made.add(c.key);
      },
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
    for (const f of [...renders]) {
      renders.delete(f);
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
