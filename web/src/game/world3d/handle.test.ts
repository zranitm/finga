import { beforeEach, describe, expect, it, vi } from 'vitest';

import type { GameSocket } from '@/net/socket';

import { Arrival } from '../systems/arrival';

import { World3DHandle } from './handle';
import { run } from './run';

vi.mock('./run', () => ({ run: vi.fn() }));

const entry = { mapId: 'forest', characterId: 'c1', cls: 'beginner', name: 'Ann', tickRateHz: 20 };
const socket = { on: () => () => undefined } as unknown as GameSocket;

describe('World3DHandle', () => {
  beforeEach(() => {
    vi.mocked(run).mockReset();
  });

  it('opens the world once it is mounted and a world is entered, holding the map’s messages from the entry, and closes it on leave', async () => {
    const close = vi.fn();
    vi.mocked(run).mockReturnValue(close);
    const handle = new World3DHandle(socket);
    const parent = document.createElement('div');
    handle.attach(parent);
    expect(run).not.toHaveBeenCalled();
    handle.enter(entry);
    await vi.waitFor(() =>
      expect(run).toHaveBeenCalledWith(parent, entry, expect.any(Arrival), socket),
    );
    expect(vi.mocked(run).mock.calls[0]![2].mapId).toBe('forest');
    handle.leave();
    expect(close).toHaveBeenCalledOnce();
  });

  it('opens nothing for a world left before its code loaded', async () => {
    const handle = new World3DHandle(socket);
    handle.enter(entry);
    const detach = handle.attach(document.createElement('div'));
    detach();
    await import('./run');
    await Promise.resolve();
    expect(run).not.toHaveBeenCalled();
  });
});
