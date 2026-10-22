import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { createRestClient } from '@/net/rest';
import { clearToken, getToken } from '@/net/token';
import { connection } from '@/state/connection';
import { ui } from '@/state/ui';
import { fakeFetch } from '@/testing/fake-fetch';
import { resetStores } from '@/testing/reset-stores';

import { LoginScreen } from './LoginScreen';
import { PASSWORD_RULE, USERNAME_RULE } from './validation';

describe('LoginScreen', () => {
  beforeEach(() => {
    resetStores();
  });
  afterEach(() => {
    clearToken();
  });

  it.each([
    { name: 'a short username', username: 'al', password: 'correct horse', rule: USERNAME_RULE },
    {
      name: 'a username with a space',
      username: 'al ice',
      password: 'correct horse',
      rule: USERNAME_RULE,
    },
    { name: 'a short password', username: 'alice', password: 'short', rule: PASSWORD_RULE },
  ])('$name blocks submit and shows the rule', async ({ username, password, rule }) => {
    const ff = fakeFetch(() => ({ status: 201, body: { token: 'never' } }));
    const onLoggedIn = vi.fn();
    render(
      <LoginScreen
        rest={createRestClient({ origin: '', fetch: ff.fetch })}
        onLoggedIn={onLoggedIn}
      />,
    );
    await userEvent.type(screen.getByLabelText('Username'), username);
    await userEvent.type(screen.getByLabelText('Password'), password);
    await userEvent.click(screen.getByRole('button', { name: 'Log in' }));
    expect(screen.getByRole('alert')).toHaveTextContent(rule);
    expect(ff.calls).toHaveLength(0);
    expect(onLoggedIn).not.toHaveBeenCalled();
  });

  it('a 401 shows the server message', async () => {
    const ff = fakeFetch(() => ({
      status: 401,
      body: { code: 'unauthenticated', message: 'invalid credentials or session' },
    }));
    render(
      <LoginScreen rest={createRestClient({ origin: '', fetch: ff.fetch })} onLoggedIn={vi.fn()} />,
    );
    await userEvent.type(screen.getByLabelText('Username'), 'alice');
    await userEvent.type(screen.getByLabelText('Password'), 'wrong horse');
    await userEvent.click(screen.getByRole('button', { name: 'Log in' }));
    expect(await screen.findByRole('alert')).toHaveTextContent('invalid credentials or session');
  });

  it('the login throttle shows a wait message', async () => {
    const ff = fakeFetch(() => ({
      status: 429,
      body: { code: 'rate_limited', message: 'too many login attempts' },
    }));
    render(
      <LoginScreen rest={createRestClient({ origin: '', fetch: ff.fetch })} onLoggedIn={vi.fn()} />,
    );
    await userEvent.type(screen.getByLabelText('Username'), 'alice');
    await userEvent.type(screen.getByLabelText('Password'), 'correct horse');
    await userEvent.click(screen.getByRole('button', { name: 'Log in' }));
    expect(await screen.findByRole('alert')).toHaveTextContent(/wait a minute/i);
  });

  it('a successful login keeps the token and moves on', async () => {
    const ff = fakeFetch(() => ({ status: 201, body: { token: 'tok-1' } }));
    const onLoggedIn = vi.fn();
    render(
      <LoginScreen
        rest={createRestClient({ origin: '', fetch: ff.fetch })}
        onLoggedIn={onLoggedIn}
      />,
    );
    await userEvent.type(screen.getByLabelText('Username'), 'alice');
    await userEvent.type(screen.getByLabelText('Password'), 'correct horse');
    await userEvent.click(screen.getByRole('button', { name: 'Log in' }));
    await waitFor(() => expect(onLoggedIn).toHaveBeenCalledWith('alice'));
    expect(ff.of('POST', '/api/sessions')[0]?.body).toEqual({
      username: 'alice',
      password: 'correct horse',
    });
    expect(getToken()).toBe('tok-1');
  });

  it('sign up registers, then logs in', async () => {
    const ff = fakeFetch(({ path }) =>
      path === '/api/accounts'
        ? { status: 201, body: { id: 'a1', username: 'alice', created_at: 'now' } }
        : { status: 201, body: { token: 'tok-new' } },
    );
    const onLoggedIn = vi.fn();
    render(
      <LoginScreen
        rest={createRestClient({ origin: '', fetch: ff.fetch })}
        onLoggedIn={onLoggedIn}
      />,
    );
    await userEvent.click(screen.getByRole('button', { name: /sign up/i }));
    await userEvent.type(screen.getByLabelText('Username'), 'alice');
    await userEvent.type(screen.getByLabelText('Password'), 'correct horse');
    await userEvent.click(screen.getByRole('button', { name: 'Create account' }));
    await waitFor(() => expect(onLoggedIn).toHaveBeenCalledWith('alice'));
    expect(ff.calls.map((c) => c.path)).toEqual(['/api/accounts', '/api/sessions']);
  });

  it('typing in the form captures the keyboard, and shows why the player was kicked', async () => {
    connection.getState().setConnection('kicked', { kick: 'logged_in_elsewhere' });
    render(
      <LoginScreen
        rest={createRestClient({ origin: '', fetch: fakeFetch(() => ({ status: 500 })).fetch })}
        onLoggedIn={vi.fn()}
      />,
    );
    expect(screen.getByText(/logged in somewhere else/i)).toBeInTheDocument();
    const field = screen.getByLabelText('Username');
    await userEvent.click(field);
    expect(ui.getState().captureKeyboard).toBe(true);
    field.blur();
    expect(ui.getState().captureKeyboard).toBe(false);
  });
});
