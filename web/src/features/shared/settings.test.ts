import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { RestError } from '@/net/rest';
import type { Settings } from '@/net/rest';
import { settings } from '@/state/settings';
import { resetStores } from '@/testing/reset-stores';

import { loadSettings, saveSettings, watchPlaySettings } from './settings';

function watched(answer: () => Promise<Settings> = () => Promise.resolve({})) {
  const putSettings = vi.fn<(s: Settings) => Promise<Settings>>(() => answer());
  const notice = vi.fn();
  const stop = watchPlaySettings({ putSettings }, notice);
  return { putSettings, notice, stop };
}

describe('saving the zoom and walk mode', () => {
  beforeEach(() => {
    localStorage.clear();
    resetStores();
    vi.useFakeTimers();
  });
  afterEach(() => vi.useRealTimers());

  it('saves the settings once, 800 ms after the last of a turn of the wheel, with the zoom it ended on', () => {
    const { putSettings, stop } = watched();
    for (const zoom of [0.125, 0.25, 0.375]) {
      settings.getState().setZoom(zoom);
      vi.advanceTimersByTime(100);
    }
    vi.advanceTimersByTime(699);
    expect(putSettings).not.toHaveBeenCalled();
    vi.advanceTimersByTime(1);
    expect(putSettings.mock.calls.map(([s]) => s.zoom)).toEqual([0.375]);
    stop();
  });

  it('switches of walk mode save the settings once, 800 ms after the last, with the mode it ended on', () => {
    const { putSettings, stop } = watched();
    settings.getState().setWalk(true);
    vi.advanceTimersByTime(300);
    settings.getState().setWalk(false);
    vi.advanceTimersByTime(300);
    settings.getState().setWalk(true);
    vi.advanceTimersByTime(799);
    expect(putSettings).not.toHaveBeenCalled();
    vi.advanceTimersByTime(1);
    expect(putSettings.mock.calls.map(([s]) => s.walk)).toEqual([true]);
    stop();
  });

  it('another setting, which its own screen saves, is none to save', () => {
    const { putSettings, stop } = watched();
    settings.getState().setShowMinimap(false);
    settings.getState().setZoom(0);
    settings.getState().setWalk(false);
    vi.advanceTimersByTime(1000);
    expect(putSettings).not.toHaveBeenCalled();
    stop();
  });

  it('leaving the world saves a zoom still waiting, and nothing after', () => {
    const { putSettings, stop } = watched();
    settings.getState().setZoom(1);
    stop();
    expect(putSettings.mock.calls.map(([s]) => s.zoom)).toEqual([1]);
    settings.getState().setZoom(0.5);
    vi.advanceTimersByTime(1000);
    expect(putSettings).toHaveBeenCalledTimes(1);
  });

  it('says why a save failed', async () => {
    const { notice, stop } = watched(() =>
      Promise.reject(new RestError(400, 110009, 'The camera’s zoom couldn’t be saved.')),
    );
    settings.getState().setZoom(0.5);
    await vi.advanceTimersByTimeAsync(800);
    expect(notice.mock.calls).toEqual([['The camera’s zoom couldn’t be saved.']]);
    stop();
  });
});

describe('reading the settings', () => {
  beforeEach(() => {
    localStorage.clear();
    resetStores();
    vi.useFakeTimers();
  });
  afterEach(() => vi.useRealTimers());

  /** The account's settings as a server keeps them, each call in order. */
  function account(held: Settings = {}) {
    let saved = held;
    const calls: string[] = [];
    return {
      calls,
      rest: {
        putSettings: vi.fn((s: Settings) => {
          calls.push(`put walk=${String(s.walk)} zoom=${String(s.zoom)}`);
          saved = s;
          return Promise.resolve(s);
        }),
        getSettings: vi.fn(() => {
          calls.push('get');
          return Promise.resolve(saved);
        }),
      },
    };
  }

  it('a read saves a switch still waiting first, so it reads it, and the switch stays', async () => {
    const { rest, calls } = account({ walk: false, zoom: 0 });
    const stop = watchPlaySettings(rest, vi.fn());
    settings.getState().setWalk(true);
    settings.getState().setZoom(0.125);
    vi.advanceTimersByTime(100); // Settings opened well inside the save's 800 ms
    await loadSettings(rest);
    expect(calls).toEqual(['put walk=true zoom=0.125', 'get']);
    expect([settings.getState().walk, settings.getState().zoom]).toEqual([true, 0.125]);
    vi.advanceTimersByTime(1000);
    expect(calls).toHaveLength(2); // nothing waits any more
    stop();
  });

  /** A server whose PUT and GET each answer when the test says. */
  function slow() {
    let land!: () => void;
    let answer!: (s: Settings) => void;
    const rest = {
      putSettings: vi.fn((put: Settings) => new Promise<Settings>((r) => (land = () => r(put)))),
      getSettings: vi.fn(() => new Promise<Settings>((r) => (answer = r))),
    };
    return { rest, land: () => land(), answer: (s: Settings) => answer(s) };
  }

  it('a read waits for a save under way, and a setting changed while it waits keeps its change', async () => {
    const { rest, land, answer } = slow();
    settings.getState().setChatPinned(true);
    const saving = saveSettings(rest);
    const reading = loadSettings(rest);
    await Promise.resolve();
    expect(rest.getSettings).not.toHaveBeenCalled(); // the pin's save is still out
    settings.getState().setShowMinimap(false); // while the read waits for it
    land();
    await saving;
    await vi.waitFor(() => expect(rest.getSettings).toHaveBeenCalledOnce());
    answer({ chat_pinned: true, zoom: 0.25 });
    await reading;
    expect([
      settings.getState().chatPinned,
      settings.getState().showMinimap,
      settings.getState().zoom,
    ]).toEqual([true, false, 0.25]);
  });

  it('a walk switch while the login read is out keeps walk, the read sets the rest, and the next save carries the saved keys', async () => {
    const { rest, answer } = slow();
    const stop = watchPlaySettings(rest, vi.fn());
    const reading = loadSettings(rest);
    await vi.waitFor(() => expect(rest.getSettings).toHaveBeenCalledOnce());
    settings.getState().setWalk(true); // Shift while the GET is out
    answer({ walk: false, zoom: 0.5, hide_minimap: true, keys: { attack: 'KeyB' } });
    await reading;
    const { walk, zoom, showMinimap, keys } = settings.getState();
    expect({ walk, zoom, showMinimap, keys }).toEqual({
      walk: true,
      zoom: 0.5,
      showMinimap: false,
      keys: { attack: 'KeyB' },
    });
    vi.advanceTimersByTime(1000);
    expect(rest.putSettings.mock.calls.map(([put]) => put)).toEqual([
      expect.objectContaining({
        walk: true,
        zoom: 0.5,
        hide_minimap: true,
        keys: { attack: 'KeyB' },
      }),
    ]);
    stop();
  });
});
