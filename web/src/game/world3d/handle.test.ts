import { beforeEach, describe, expect, it, vi } from 'vitest';

import type { GameSocket } from '@/net/socket';
import { OWN_LOOK } from '@/state/wardrobe';

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
    vi.mocked(run).mockReturnValue({ close, portrait: vi.fn() });
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

  it('draws a portrait on the open world, and one asked for with none draws nothing', async () => {
    const drawn = { show: vi.fn(), turn: vi.fn(), close: vi.fn() };
    const portrait = vi.fn(() => drawn);
    vi.mocked(run).mockReturnValue({ close: vi.fn(), portrait });
    const handle = new World3DHandle(socket);
    const canvas = document.createElement('canvas');
    const before = handle.portrait(canvas);
    before.show('warrior', OWN_LOOK);
    before.close();
    handle.attach(document.createElement('div'));
    handle.enter(entry);
    await vi.waitFor(() => expect(run).toHaveBeenCalledOnce());
    expect(handle.portrait(canvas)).toBe(drawn);
    expect(portrait).toHaveBeenCalledWith(canvas);
    handle.leave();
    expect(handle.portrait(canvas)).not.toBe(drawn);
    expect(portrait).toHaveBeenCalledOnce();
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
