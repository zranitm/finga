import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { RestError } from '@/net/rest';
import type { Settings } from '@/net/rest';
import { settings } from '@/state/settings';
import { resetStores } from '@/testing/reset-stores';

import { watchPlaySettings } from './settings';

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
