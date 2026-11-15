import { beforeEach, describe, expect, it, vi } from 'vitest';

import { World3DHandle } from './handle';
import { run } from './run';

vi.mock('./run', () => ({ run: vi.fn() }));

const entry = { mapId: 'forest', characterId: 'c1', cls: 'beginner', name: 'Ann', tickRateHz: 20 };

describe('World3DHandle', () => {
  beforeEach(() => {
    vi.mocked(run).mockReset();
  });

  it('opens the stage once it is mounted and a world is entered, and closes it on leave', async () => {
    const close = vi.fn();
    vi.mocked(run).mockResolvedValue(close);
    const handle = new World3DHandle();
    const parent = document.createElement('div');
    handle.attach(parent);
    expect(run).not.toHaveBeenCalled();
    handle.enter(entry);
    await vi.waitFor(() => expect(run).toHaveBeenCalledWith(parent, entry));
    await Promise.resolve();
    handle.leave();
    expect(close).toHaveBeenCalledOnce();
  });

  it('closes a stage that finishes loading after the world was left', async () => {
    const close = vi.fn();
    let loaded: (close: () => void) => void = () => undefined;
    vi.mocked(run).mockReturnValue(new Promise((resolve) => (loaded = resolve)));
    const handle = new World3DHandle();
    handle.enter(entry);
    const detach = handle.attach(document.createElement('div'));
    await vi.waitFor(() => expect(run).toHaveBeenCalledOnce());
    detach();
    loaded(close);
    await vi.waitFor(() => expect(close).toHaveBeenCalledOnce());
  });
});
