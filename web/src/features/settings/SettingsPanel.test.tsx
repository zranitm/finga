import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { watchPlaySettings } from '@/features/shared/settings';
import { SETTINGS_SAVE_DELAY_MS } from '@/features/shared/useSettingsSaves';
import { ERR } from '@/protocol/errors';
import { settings } from '@/state/settings';
import { fakeFetch, restClientOn } from '@/testing/fake-fetch';
import { resetStores } from '@/testing/reset-stores';
import { user } from '@/testing/user';

import { SettingsPanel } from './SettingsPanel';

/** The switches' saves wait out their moment; a test's timers turn fake once the account's settings are in. */
const settle = (ms = SETTINGS_SAVE_DELAY_MS) => act(() => vi.advanceTimersByTimeAsync(ms));

describe('SettingsPanel', () => {
  beforeEach(() => {
    localStorage.clear();
    resetStores();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it('opened inside a switch’s save, it saves the switch first and keeps it, in the store and the account', async () => {
    let held: Record<string, unknown> = { walk: false, zoom: 0 };
    const ff = fakeFetch(({ method, body }) => {
      if (method === 'PUT') {
        held = body as Record<string, unknown>;
      }
      return { status: 200, body: held };
    });
    const rest = restClientOn(ff.fetch);
    const stop = watchPlaySettings(rest, vi.fn());
    settings.getState().setWalk(true); // the switch, then Settings 100 ms later
    render(<SettingsPanel rest={rest} onClose={vi.fn()} />);
    await waitFor(() => expect(ff.of('GET', '/api/settings')).toHaveLength(1));
    expect(ff.of('PUT', '/api/settings').map((c) => (c.body as { walk: boolean }).walk)).toEqual([
      true,
    ]);
    expect(settings.getState().walk).toBe(true);
    stop();
  });

  it('Key bindings opens the key-binding window in place, and Back returns', async () => {
    const ff = fakeFetch(() => ({ status: 200, body: {} }));
    render(<SettingsPanel rest={restClientOn(ff.fetch)} onClose={vi.fn()} />);
    expect(screen.queryByRole('radio')).toBeNull(); // no presets
    await user.click(screen.getByRole('button', { name: 'Key bindings' }));
    expect(screen.getByRole('button', { name: 'Skill 1: Q' })).toBeInTheDocument();
    expect(screen.queryByLabelText('Show the minimap')).toBeNull();
    await user.click(screen.getByRole('button', { name: 'Back' }));
    expect(screen.getByLabelText('Show the minimap')).toBeInTheDocument();
  });

  it('the auto-assign switch shows the account’s setting, and a change saves it with the rest', async () => {
    const ff = fakeFetch(({ method }) =>
      method === 'PUT'
        ? { status: 200, body: { auto_assign_points: false } }
        : { status: 200, body: { auto_assign_points: true } },
    );
    render(<SettingsPanel rest={restClientOn(ff.fetch)} onClose={vi.fn()} />);
    const auto = screen.getByLabelText(/Auto-assign at every level-up/);
    expect(auto).not.toBeChecked();
    await waitFor(() => expect(auto).toBeChecked());
    expect(settings.getState().autoAssignPoints).toBe(true);
    vi.useFakeTimers();
    fireEvent.click(auto);
    expect(settings.getState().autoAssignPoints).toBe(false);
    await settle();
    expect(ff.of('PUT', '/api/settings')).toHaveLength(1);
    expect(ff.of('PUT', '/api/settings')[0]?.body).toEqual({
      auto_assign_points: false,
      chat_pinned: false,
      hide_minimap: false,
      zoom: 0,
      walk: false,
      keys: {},
    });
  });

  it('an account that never set auto-assign, the chat pin, the minimap, the zoom or walk mode has both off, the minimap on, the camera close and sprint mode', async () => {
    settings.getState().setAutoAssignPoints(true);
    settings.getState().setChatPinned(true);
    settings.getState().setShowMinimap(false);
    settings.getState().setZoom(0.5);
    settings.getState().setWalk(true);
    const ff = fakeFetch(() => ({ status: 200, body: {} }));
    render(<SettingsPanel rest={restClientOn(ff.fetch)} onClose={vi.fn()} />);
    await waitFor(() => expect(settings.getState().autoAssignPoints).toBe(false));
    expect([
      settings.getState().chatPinned,
      settings.getState().showMinimap,
      settings.getState().zoom,
      settings.getState().walk,
    ]).toEqual([false, true, 0, false]);
  });

  it('the account’s zoom and walk mode arrive with its settings, and a change here saves them as they were', async () => {
    const ff = fakeFetch(({ method }) =>
      method === 'PUT'
        ? { status: 200, body: {} }
        : { status: 200, body: { zoom: 0.625, walk: true } },
    );
    render(<SettingsPanel rest={restClientOn(ff.fetch)} onClose={vi.fn()} />);
    await waitFor(() => expect(settings.getState().zoom).toBe(0.625));
    expect(settings.getState().walk).toBe(true);
    vi.useFakeTimers();
    fireEvent.click(screen.getByLabelText('Show the minimap'));
    await settle();
    expect(ff.of('PUT', '/api/settings')).toHaveLength(1);
    expect(ff.of('PUT', '/api/settings')[0]?.body).toMatchObject({
      hide_minimap: true,
      zoom: 0.625,
      walk: true,
    });
  });

  it('the minimap switch shows the account’s setting, and turning it off saves it hidden', async () => {
    const ff = fakeFetch(({ method }) =>
      method === 'PUT' ? { status: 200, body: { hide_minimap: true } } : { status: 200, body: {} },
    );
    render(<SettingsPanel rest={restClientOn(ff.fetch)} onClose={vi.fn()} />);
    await waitFor(() => expect(ff.of('GET', '/api/settings')).toHaveLength(1));
    const minimap = screen.getByLabelText('Show the minimap');
    expect(minimap).toBeChecked();
    vi.useFakeTimers();
    fireEvent.click(minimap);
    expect(settings.getState().showMinimap).toBe(false);
    await settle();
    expect(ff.of('PUT', '/api/settings')).toHaveLength(1);
    expect(ff.of('PUT', '/api/settings')[0]?.body).toEqual({
      auto_assign_points: false,
      chat_pinned: false,
      hide_minimap: true,
      zoom: 0,
      walk: false,
      keys: {},
    });
  });

  it('the account’s chat pin arrives with its settings, and a change here keeps it', async () => {
    const ff = fakeFetch(({ method }) =>
      method === 'PUT'
        ? { status: 200, body: { chat_pinned: true } }
        : { status: 200, body: { chat_pinned: true } },
    );
    render(<SettingsPanel rest={restClientOn(ff.fetch)} onClose={vi.fn()} />);
    await waitFor(() => expect(settings.getState().chatPinned).toBe(true));
    vi.useFakeTimers();
    fireEvent.click(screen.getByLabelText('Show the minimap'));
    await settle();
    expect(ff.of('PUT', '/api/settings')).toHaveLength(1);
    expect(ff.of('PUT', '/api/settings')[0]?.body).toMatchObject({ chat_pinned: true });
  });

  it('a failed save shows the message and keeps the local choice, and Done closes', async () => {
    const onClose = vi.fn();
    const ff = fakeFetch(({ method }) =>
      method === 'PUT'
        ? { status: 500, body: { code: ERR.INTERNAL, message: 'database unavailable' } }
        : { status: 200, body: {} },
    );
    render(<SettingsPanel rest={restClientOn(ff.fetch)} onClose={onClose} />);
    await waitFor(() => expect(ff.of('GET', '/api/settings')).toHaveLength(1));
    vi.useFakeTimers();
    fireEvent.click(screen.getByLabelText('Show the minimap'));
    await settle();
    expect(screen.getByRole('alert')).toHaveTextContent(
      'Something went wrong on the server. Try again.',
    );
    expect(settings.getState().showMinimap).toBe(false);
    fireEvent.click(screen.getByRole('button', { name: 'Done' }));
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  // From the REST limit (#361): each flip was its own PUT, so a run of them spent an account's burst.
  it('ten fast flips save once, a moment after the last, with the switches as they ended', async () => {
    const ff = fakeFetch(() => ({ status: 200, body: {} }));
    render(<SettingsPanel rest={restClientOn(ff.fetch)} onClose={vi.fn()} />);
    await waitFor(() => expect(ff.of('GET', '/api/settings')).toHaveLength(1));
    vi.useFakeTimers();
    for (let k = 0; k < 5; k++) {
      fireEvent.click(screen.getByLabelText(/Auto-assign at every level-up/));
      fireEvent.click(screen.getByLabelText('Show the minimap'));
    }
    await settle(SETTINGS_SAVE_DELAY_MS - 1);
    expect(ff.of('PUT', '/api/settings')).toHaveLength(0);
    await settle(1);
    expect(ff.of('PUT', '/api/settings').map((c) => c.body)).toEqual([
      {
        auto_assign_points: true,
        chat_pinned: false,
        hide_minimap: true,
        zoom: 0,
        walk: false,
        keys: {},
      },
    ]);
  });

  it('a flip just before the window closes is saved as it closes', async () => {
    const ff = fakeFetch(() => ({ status: 200, body: {} }));
    const { unmount } = render(<SettingsPanel rest={restClientOn(ff.fetch)} onClose={vi.fn()} />);
    await waitFor(() => expect(ff.of('GET', '/api/settings')).toHaveLength(1));
    vi.useFakeTimers();
    fireEvent.click(screen.getByLabelText('Show the minimap'));
    expect(ff.of('PUT', '/api/settings')).toHaveLength(0);
    unmount();
    await act(() => Promise.resolve());
    expect(ff.of('PUT', '/api/settings').map((c) => c.body)).toMatchObject([
      { hide_minimap: true },
    ]);
  });
});
