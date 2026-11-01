// Use: vi.mock('phaser', async () => (await import('@/testing/fake-phaser')).fakePhaser());

export class FakeScene {
  constructor(readonly config: unknown) {}
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
  };
  return { default: Phaser };
}
