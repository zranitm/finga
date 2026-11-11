import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { createRestClient } from '@/net/rest';
import type { FetchLike } from '@/net/rest';
import { clearToken, getToken, setToken } from '@/net/token';
import { ERR } from '@/protocol/errors';
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

/** A server whose log outs wait until answer is called. */
function deletesHeld(characters: unknown[]) {
  let answer: () => void = () => undefined;
  const answered = new Promise<void>((resolve) => {
    answer = resolve;
  });
  const ff = fakeFetch(({ method }) => (method === 'DELETE' ? { status: 204 } : list(characters)));
  const fetch: FetchLike = async (url, init) => {
    if (init.method === 'DELETE') {
      await answered;
    }
    return ff.fetch(url, init);
  };
  return { ff, rest: createRestClient({ origin: '', fetch }), answer: () => answer() };
}

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
    expect(await screen.findByText('Ayla, level 1 Warrior')).toBeInTheDocument();
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

  it('creates a Beginner from a name alone, and shows a 409 in the client’s words', async () => {
    const { ff } = setup(({ method, path, body }) => {
      if (method === 'POST' && path === '/api/characters') {
        const b = body as { name: string };
        return b.name === 'Ayla'
          ? { status: 409, body: { code: ERR.NAME_TAKEN, message: 'that name is taken' } }
          : {
              status: 201,
              body: { ...sampleCharacter, id: 'c-robin', name: b.name, class: 'beginner' },
            };
      }
      return list([]);
    });
    await screen.findByRole('form', { name: 'Create a character' });
    await userEvent.type(screen.getByLabelText('Name'), 'Ayla');
    await userEvent.click(screen.getByRole('button', { name: 'Create' }));
    expect(await screen.findByRole('alert')).toHaveTextContent('That name is taken.');
    await userEvent.clear(screen.getByLabelText('Name'));
    await userEvent.type(screen.getByLabelText('Name'), 'Robin');
    expect(screen.queryByRole('radio')).not.toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: 'Create' }));
    expect(await screen.findByText('Robin, level 1 Beginner')).toBeInTheDocument();
    expect(ff.of('POST', '/api/characters').at(-1)?.body).toEqual({ name: 'Robin' });
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
              code: ERR.CHARACTER_SLOTS_FULL,
              message: 'this account already has the most characters allowed',
            },
          }
        : list([sampleCharacter]),
    );
    await screen.findByText('Ayla, level 1 Warrior');
    await userEvent.type(screen.getByLabelText('Name'), 'Robin');
    await userEvent.click(screen.getByRole('button', { name: 'Create' }));
    expect(await screen.findByRole('alert')).toHaveTextContent(
      'This account already has the most characters allowed.',
    );
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

  it('log out everywhere calls DELETE /api/sessions, clears the token, and leaves', async () => {
    const { ff, onLoggedOut } = setup(({ method }) =>
      method === 'DELETE' ? { status: 204 } : list([]),
    );
    await screen.findByRole('form', { name: 'Create a character' });
    await userEvent.click(screen.getByRole('button', { name: 'Log out everywhere' }));
    await waitFor(() => expect(onLoggedOut).toHaveBeenCalledTimes(1));
    expect(ff.of('DELETE', '/api/sessions')).toHaveLength(1);
    expect(getToken()).toBeNull();
  });

  it('a failed log out everywhere stays, says why, and keeps the session', async () => {
    const { onLoggedOut } = setup(({ method }) =>
      method === 'DELETE'
        ? { status: 503, body: { code: ERR.UNAVAILABLE, message: 'the account service is down' } }
        : list([]),
    );
    await screen.findByRole('form', { name: 'Create a character' });
    await userEvent.click(screen.getByRole('button', { name: 'Log out everywhere' }));
    expect(await screen.findByRole('alert')).toHaveTextContent('The server is busy. Try again.');
    expect(onLoggedOut).not.toHaveBeenCalled();
    expect(getToken()).toBe('tok-1');
  });

  it('log out everywhere sends one DELETE however often it is clicked while one is on its way', async () => {
    const { ff, rest, answer } = deletesHeld([]);
    const { onLoggedOut } = setup(() => list([]), { rest });
    await screen.findByRole('form', { name: 'Create a character' });
    const button = screen.getByRole('button', { name: 'Log out everywhere' });
    await userEvent.dblClick(button);
    expect(button).toBeDisabled();
    expect(screen.getByRole('button', { name: 'Log out' })).toBeDisabled();
    answer();
    await waitFor(() => expect(onLoggedOut).toHaveBeenCalledTimes(1));
    expect(ff.of('DELETE', '/api/sessions')).toHaveLength(1);
  });

  it.each([{ button: 'Log out' }, { button: 'Log out everywhere' }])(
    'Play waits while $button is on its way',
    async ({ button }) => {
      const { rest, answer } = deletesHeld([sampleCharacter]);
      const { onPlay, onLoggedOut } = setup(() => list([]), { rest });
      await screen.findByText('Ayla, level 1 Warrior');
      await userEvent.click(screen.getByRole('button', { name: button }));
      const play = screen.getByRole('button', { name: 'Play' });
      expect(play).toBeDisabled();
      await userEvent.click(play);
      expect(onPlay).not.toHaveBeenCalled();
      answer();
      await waitFor(() => expect(onLoggedOut).toHaveBeenCalledTimes(1));
    },
  );

  it('a failed log out everywhere can be tried again', async () => {
    const { ff } = setup(({ method }) =>
      method === 'DELETE'
        ? { status: 503, body: { code: ERR.UNAVAILABLE, message: 'down' } }
        : list([]),
    );
    await screen.findByRole('form', { name: 'Create a character' });
    const button = screen.getByRole('button', { name: 'Log out everywhere' });
    await userEvent.click(button);
    await screen.findByRole('alert');
    expect(button).toBeEnabled();
    await userEvent.click(button);
    await waitFor(() => expect(ff.of('DELETE', '/api/sessions')).toHaveLength(2));
  });

  it('opens settings, and shows a failure to load', async () => {
    const { onSettings } = setup(({ method }) =>
      method === 'GET'
        ? { status: 500, body: { code: ERR.INTERNAL, message: 'database unavailable' } }
        : list([]),
    );
    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Something went wrong on the server. Try again.',
    );
    await userEvent.click(screen.getByRole('button', { name: 'Settings' }));
    expect(onSettings).toHaveBeenCalledTimes(1);
  });

  it('a list that failed to load, as while the gateway restarts, loads again on Try again', async () => {
    let up = false;
    const { ff } = setup(() => (up ? list([sampleCharacter]) : { status: 502 }));
    expect(await screen.findByRole('alert')).toHaveTextContent(
      'The server isn’t answering right now. Try again in a moment.',
    );
    up = true;
    await userEvent.click(screen.getByRole('button', { name: 'Try again' }));
    expect(await screen.findByText('Ayla, level 1 Warrior')).toBeInTheDocument();
    expect(screen.queryByRole('alert')).not.toBeInTheDocument();
    expect(ff.of('GET', '/api/characters')).toHaveLength(2);
  });
});
