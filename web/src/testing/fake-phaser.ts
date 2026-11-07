// Use: vi.mock('phaser', async () => (await import('@/testing/fake-phaser')).fakePhaser());

type Listener = (...args: unknown[]) => void;

/** Phaser's event emitter, as far as scenes use it. */
export class FakeEvents {
  private entries: { event: string; fn: Listener; once: boolean }[] = [];

  on(event: string, fn: Listener): this {
    this.entries.push({ event, fn, once: false });
    return this;
  }

  once(event: string, fn: Listener): this {
    this.entries.push({ event, fn, once: true });
    return this;
  }

  off(event: string, fn: Listener): this {
    this.entries = this.entries.filter((e) => e.event !== event || e.fn !== fn);
    return this;
  }

  emit(event: string, ...args: unknown[]): void {
    for (const e of this.entries.filter((e) => e.event === event)) {
      if (e.once) {
        this.off(event, e.fn);
      }
      e.fn(...args);
    }
  }

  removeAllListeners(): void {
    this.entries = [];
  }
}

const FADE_OUT_COMPLETE = 'camerafadeoutcomplete';

export class FakeCamera extends FakeEvents {
  readonly width = 800;
  readonly height = 600;
  fading = false;

  fadeIn(): this {
    return this;
  }

  fadeOut(): this {
    this.fading = true;
    return this;
  }

  setBounds(): this {
    return this;
  }

  startFollow(): this {
    return this;
  }

  /** The fade-out ends, as Phaser's camera reports on a later frame. */
  endFade(): void {
    this.emit(FADE_OUT_COMPLETE, this);
  }
}

/** A game object that takes any call and chains, for drawing a test does not look at. */
function drawn(): unknown {
  const self: unknown = new Proxy(() => undefined, {
    get: (_target, key) => {
      if (key === Symbol.toPrimitive) {
        return () => 0;
      }
      return key === 'then' ? undefined : self;
    },
    apply: () => self,
  });
  return self;
}

export class FakeScene {
  readonly add = drawn();
  readonly tweens = drawn();
  readonly cameras = { main: new FakeCamera() };
  readonly scale = new FakeEvents();
  readonly events = new FakeEvents();
  readonly time = { now: 0 };
  readonly registry = new Map<string, unknown>();
  readonly cache = { json: new Map<string, unknown>() };
  readonly game = { events: new FakeEvents() };
  /** A restart asked for, which Phaser runs on the next step. */
  restarting: { readonly data: unknown } | null = null;
  /** A stop asked for, which Phaser runs on the next step. */
  stopping = false;
  readonly scene = {
    restart: (data?: unknown) => {
      this.restarting = { data };
    },
    stop: () => {
      this.stopping = true;
    },
  };

  constructor(readonly config: unknown) {}
}

interface SceneHooks {
  init?(data: unknown): void;
  create?(data: unknown): void;
  update?(time: number, delta: number): void;
}

/** Runs a scene as Phaser's scene manager does: a restart waits for the next step, and a stop destroys the cameras before the scene's own shutdown handlers run. */
export class SceneRunner {
  private readonly scene: FakeScene & SceneHooks;
  private running = false;

  constructor(scene: object) {
    this.scene = scene as FakeScene & SceneHooks;
  }

  get camera(): FakeCamera {
    return this.scene.cameras.main;
  }

  get isRunning(): boolean {
    return this.running;
  }

  start(data: unknown): void {
    this.running = true;
    this.scene.cameras.main = new FakeCamera();
    this.scene.init?.(data);
    this.scene.create?.(data);
  }

  step(time: number, delta: number): void {
    if (this.scene.stopping) {
      this.scene.stopping = false;
      this.stop();
    }
    const restart = this.scene.restarting;
    this.scene.restarting = null;
    if (restart) {
      this.stop();
      this.start(restart.data);
    }
    this.scene.time.now = time;
    if (this.running) {
      this.scene.update?.(time, delta);
    }
  }

  stop(): void {
    this.running = false;
    this.scene.cameras.main.removeAllListeners();
    this.scene.events.emit('shutdown');
  }
}

export class FakeGame {
  static instances: FakeGame[] = [];
  static reset(): void {
    FakeGame.instances = [];
  }
  static live(): FakeGame[] {
    return FakeGame.instances.filter((g) => !g.destroyed);
  }

  destroyed = false;
  readonly started: [string, unknown][] = [];
  readonly stopped: string[] = [];
  private readonly active = new Set<string>();
  private readonly data = new Map<string, unknown>();
  readonly registry = {
    set: (key: string, value: unknown) => {
      this.data.set(key, value);
    },
    get: (key: string) => this.data.get(key),
  };
  readonly scene = {
    start: (key: string, data?: unknown) => {
      this.started.push([key, data]);
      this.active.add(key);
    },
    stop: (key: string) => {
      this.stopped.push(key);
      this.active.delete(key);
    },
    isActive: (key: string) => this.active.has(key),
  };

  constructor(readonly config: { parent?: unknown }) {
    FakeGame.instances.push(this);
  }

  destroy(): void {
    this.destroyed = true;
  }
}

export function fakePhaser() {
  const Phaser = {
    AUTO: 0,
    Game: FakeGame,
    Scene: FakeScene,
    Scale: { RESIZE: 3, Events: { RESIZE: 'resize' } },
    Scenes: { Events: { SHUTDOWN: 'shutdown' } },
    Cameras: { Scene2D: { Events: { FADE_OUT_COMPLETE } } },
  };
  return { default: Phaser };
}
