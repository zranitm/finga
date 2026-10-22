import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import { createRestClient } from '@/net/rest';
import { PRESET_KEY, settings } from '@/state/settings';
import { fakeFetch } from '@/testing/fake-fetch';
import { resetStores } from '@/testing/reset-stores';

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
    render(
      <SettingsPanel rest={createRestClient({ origin: '', fetch: ff.fetch })} onClose={vi.fn()} />,
    );
    await waitFor(() => expect(ff.of('GET', '/api/settings')).toHaveLength(1));
    await userEvent.click(screen.getByLabelText(/WASD/));
    expect(settings.getState().preset).toBe('wasd');
    expect(localStorage.getItem(PRESET_KEY)).toBe('wasd');
    await waitFor(() => expect(ff.of('PUT', '/api/settings')).toHaveLength(1));
    expect(ff.of('PUT', '/api/settings')[0]?.body).toEqual({ control_preset: 'wasd' });
    expect(screen.getByLabelText(/WASD/)).toBeChecked();
  });

  it('the saved preset from the account wins over the cache when it arrives', async () => {
    const ff = fakeFetch(() => ({ status: 200, body: { control_preset: 'wasd' } }));
    render(
      <SettingsPanel rest={createRestClient({ origin: '', fetch: ff.fetch })} onClose={vi.fn()} />,
    );
    expect(screen.getByLabelText(/Classic/)).toBeChecked();
    await waitFor(() => expect(settings.getState().preset).toBe('wasd'));
    expect(screen.getByLabelText(/WASD/)).toBeChecked();
  });

  it('a failed save shows the message and keeps the local choice, and Done closes', async () => {
    const onClose = vi.fn();
    const ff = fakeFetch(({ method }) =>
      method === 'PUT'
        ? { status: 500, body: { code: 'internal', message: 'database unavailable' } }
        : { status: 200, body: { control_preset: 'classic' } },
    );
    render(
      <SettingsPanel rest={createRestClient({ origin: '', fetch: ff.fetch })} onClose={onClose} />,
    );
    await userEvent.click(screen.getByLabelText(/WASD/));
    expect(await screen.findByRole('alert')).toHaveTextContent('database unavailable');
    expect(settings.getState().preset).toBe('wasd');
    await userEvent.click(screen.getByRole('button', { name: 'Done' }));
    expect(onClose).toHaveBeenCalledTimes(1);
  });
});
