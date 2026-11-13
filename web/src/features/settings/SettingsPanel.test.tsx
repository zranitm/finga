import { render, screen, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import { ERR } from '@/protocol/errors';
import { PRESET_KEY, settings } from '@/state/settings';
import { fakeFetch, restClientOn } from '@/testing/fake-fetch';
import { resetStores } from '@/testing/reset-stores';
import { user } from '@/testing/user';

import { SettingsPanel } from './SettingsPanel';

describe('SettingsPanel', () => {
  beforeEach(() => {
    localStorage.clear();
    resetStores();
  });

  it('choosing WASD calls PUT /api/settings and updates the cached preset at once', async () => {
    const ff = fakeFetch(({ method }) =>
      method === 'PUT'
        ? { status: 200, body: { control_preset: 'wasd' } }
        : { status: 200, body: { control_preset: 'classic' } },
    );
    render(<SettingsPanel rest={restClientOn(ff.fetch)} onClose={vi.fn()} />);
    await waitFor(() => expect(ff.of('GET', '/api/settings')).toHaveLength(1));
    await user.click(screen.getByLabelText(/WASD/));
    expect(settings.getState().preset).toBe('wasd');
    expect(localStorage.getItem(PRESET_KEY)).toBe('wasd');
    await waitFor(() => expect(ff.of('PUT', '/api/settings')).toHaveLength(1));
    expect(ff.of('PUT', '/api/settings')[0]?.body).toEqual({
      control_preset: 'wasd',
      auto_assign_points: false,
      chat_pinned: false,
    });
    expect(screen.getByLabelText(/WASD/)).toBeChecked();
  });

  it('the saved preset from the account wins over the cache when it arrives', async () => {
    const ff = fakeFetch(() => ({ status: 200, body: { control_preset: 'wasd' } }));
    render(<SettingsPanel rest={restClientOn(ff.fetch)} onClose={vi.fn()} />);
    expect(screen.getByLabelText(/Classic/)).toBeChecked();
    await waitFor(() => expect(settings.getState().preset).toBe('wasd'));
    expect(screen.getByLabelText(/WASD/)).toBeChecked();
  });

  it('the auto-assign switch shows the account’s setting, and a change saves it with the preset', async () => {
    const ff = fakeFetch(({ method }) =>
      method === 'PUT'
        ? { status: 200, body: { control_preset: 'wasd', auto_assign_points: false } }
        : { status: 200, body: { control_preset: 'wasd', auto_assign_points: true } },
    );
    render(<SettingsPanel rest={restClientOn(ff.fetch)} onClose={vi.fn()} />);
    const auto = screen.getByLabelText(/Auto-assign at every level-up/);
    expect(auto).not.toBeChecked();
    await waitFor(() => expect(auto).toBeChecked());
    expect(settings.getState().autoAssignPoints).toBe(true);
    await user.click(auto);
    expect(settings.getState().autoAssignPoints).toBe(false);
    await waitFor(() => expect(ff.of('PUT', '/api/settings')).toHaveLength(1));
    expect(ff.of('PUT', '/api/settings')[0]?.body).toEqual({
      control_preset: 'wasd',
      auto_assign_points: false,
      chat_pinned: false,
    });
  });

  it('an account that never set auto-assign or the chat pin has both off', async () => {
    settings.getState().setAutoAssignPoints(true);
    settings.getState().setChatPinned(true);
    const ff = fakeFetch(() => ({ status: 200, body: { control_preset: 'classic' } }));
    render(<SettingsPanel rest={restClientOn(ff.fetch)} onClose={vi.fn()} />);
    await waitFor(() => expect(settings.getState().autoAssignPoints).toBe(false));
    expect(settings.getState().chatPinned).toBe(false);
  });

  it('the account’s chat pin arrives with its settings, and a change here keeps it', async () => {
    const ff = fakeFetch(({ method }) =>
      method === 'PUT'
        ? { status: 200, body: { control_preset: 'wasd', chat_pinned: true } }
        : { status: 200, body: { control_preset: 'classic', chat_pinned: true } },
    );
    render(<SettingsPanel rest={restClientOn(ff.fetch)} onClose={vi.fn()} />);
    await waitFor(() => expect(settings.getState().chatPinned).toBe(true));
    await user.click(screen.getByLabelText(/WASD/));
    await waitFor(() => expect(ff.of('PUT', '/api/settings')).toHaveLength(1));
    expect(ff.of('PUT', '/api/settings')[0]?.body).toMatchObject({ chat_pinned: true });
  });

  it('a failed save shows the message and keeps the local choice, and Done closes', async () => {
    const onClose = vi.fn();
    const ff = fakeFetch(({ method }) =>
      method === 'PUT'
        ? { status: 500, body: { code: ERR.INTERNAL, message: 'database unavailable' } }
        : { status: 200, body: { control_preset: 'classic' } },
    );
    render(<SettingsPanel rest={restClientOn(ff.fetch)} onClose={onClose} />);
    await user.click(screen.getByLabelText(/WASD/));
    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Something went wrong on the server. Try again.',
    );
    expect(settings.getState().preset).toBe('wasd');
    await user.click(screen.getByRole('button', { name: 'Done' }));
    expect(onClose).toHaveBeenCalledTimes(1);
  });
});
