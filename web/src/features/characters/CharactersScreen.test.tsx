import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { createRestClient } from '@/net/rest';
import { clearToken, getToken, setToken } from '@/net/token';
import { fakeFetch, sampleCharacter } from '@/testing/fake-fetch';
import type { Script } from '@/testing/fake-fetch';
import { resetStores } from '@/testing/reset-stores';

import { CharactersScreen, NAME_RULE } from './CharactersScreen';

function setup(script: Script, props: Partial<Parameters<typeof CharactersScreen>[0]> = {}) {
  const ff = fakeFetch(script);
  const onPlay = vi.fn();
  const onLoggedOut = vi.fn();
  const onSettings = vi.fn();
  render(
    <CharactersScreen
      rest={createRestClient({ origin: '', fetch: ff.fetch })}
      username="alice"
      onPlay={onPlay}
      onLoggedOut={onLoggedOut}
      onSettings={onSettings}
      {...props}
    />,
  );
  return { ff, onPlay, onLoggedOut, onSettings };
}

const list = (characters: unknown[]) => ({ status: 200, body: { characters } });

describe('CharactersScreen', () => {
  beforeEach(() => {
    resetStores();
    setToken('tok-1');
  });
  afterEach(() => {
    clearToken();
  });

  it('lists the characters and plays one', async () => {
    const { onPlay } = setup(() => list([sampleCharacter]));
    expect(await screen.findByText('Ayla, level 1 warrior')).toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: 'Play' }));
    expect(onPlay).toHaveBeenCalledWith(sampleCharacter);
  });

  it.each([
    { name: 'a short name', value: 'Al' },
    { name: 'a name starting with a digit', value: '1Ayla' },
    { name: 'a name with a space', value: 'Ay la' },
    { name: 'a 13-letter name', value: 'Abcdefghijklm' },
  ])('$name is blocked locally', async ({ value }) => {
    const { ff } = setup(() => list([]));
    await screen.findByRole('form', { name: 'Create a character' });
    await userEvent.type(screen.getByLabelText('Name'), value);
    await userEvent.click(screen.getByRole('button', { name: 'Create' }));
    expect(screen.getByRole('alert')).toHaveTextContent(NAME_RULE);
    expect(ff.of('POST', '/api/characters')).toHaveLength(0);
  });

  it('creates a character with the chosen class and shows a 409 as the server says it', async () => {
    const { ff } = setup(({ method, path, body }) => {
      if (method === 'POST' && path === '/api/characters') {
        const b = body as { name: string; class: string };
        return b.name === 'Ayla'
          ? { status: 409, body: { code: 'name_taken', message: 'that name is taken' } }
          : {
              status: 201,
              body: { ...sampleCharacter, id: 'c-robin', name: b.name, class: b.class },
            };
      }
      return list([]);
    });
    await screen.findByRole('form', { name: 'Create a character' });
    await userEvent.type(screen.getByLabelText('Name'), 'Ayla');
    await userEvent.click(screen.getByRole('button', { name: 'Create' }));
    expect(await screen.findByRole('alert')).toHaveTextContent('that name is taken');
    await userEvent.clear(screen.getByLabelText('Name'));
    await userEvent.type(screen.getByLabelText('Name'), 'Robin');
    await userEvent.click(screen.getByLabelText('Bowman'));
    await userEvent.click(screen.getByRole('button', { name: 'Create' }));
    expect(await screen.findByText('Robin, level 1 bowman')).toBeInTheDocument();
    expect(ff.of('POST', '/api/characters').at(-1)?.body).toEqual({
      name: 'Robin',
      class: 'bowman',
    });
  });

  it('the cap message appears when the third character exists, and the fourth attempt is refused', async () => {
    const three = [1, 2, 3].map((n) => ({ ...sampleCharacter, id: `c${n}`, name: `Hero${n}` }));
    setup(() => list(three));
    expect(await screen.findByText(/most characters allowed \(3\)/)).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Create' })).toBeDisabled();
    expect(screen.getByLabelText('Name')).toBeDisabled();
  });

  it('a 409 from the server cap is shown when the list was shorter than the truth', async () => {
    setup(({ method }) =>
      method === 'POST'
        ? {
            status: 409,
            body: {
              code: 'character_slots_full',
              message: 'this account already has the most characters allowed',
            },
          }
        : list([sampleCharacter]),
    );
    await screen.findByText('Ayla, level 1 warrior');
    await userEvent.type(screen.getByLabelText('Name'), 'Robin');
    await userEvent.click(screen.getByRole('button', { name: 'Create' }));
    expect(await screen.findByRole('alert')).toHaveTextContent(/most characters allowed/);
  });

  it('log out calls DELETE /api/sessions/current, clears the token, and leaves', async () => {
    const { ff, onLoggedOut } = setup(({ method }) =>
      method === 'DELETE' ? { status: 204 } : list([]),
    );
    await screen.findByRole('form', { name: 'Create a character' });
    await userEvent.click(screen.getByRole('button', { name: 'Log out' }));
    await waitFor(() => expect(onLoggedOut).toHaveBeenCalledTimes(1));
    expect(ff.of('DELETE', '/api/sessions/current')).toHaveLength(1);
    expect(getToken()).toBeNull();
  });

  it('opens settings, and shows a failure to load', async () => {
    const { onSettings } = setup(({ method }) =>
      method === 'GET'
        ? { status: 500, body: { code: 'internal', message: 'database unavailable' } }
        : list([]),
    );
    expect(await screen.findByRole('alert')).toHaveTextContent('database unavailable');
    await userEvent.click(screen.getByRole('button', { name: 'Settings' }));
    expect(onSettings).toHaveBeenCalledTimes(1);
  });
});
