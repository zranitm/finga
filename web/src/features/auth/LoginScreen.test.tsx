import { render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { isTyping } from '@/input/typing';
import { clearToken, getToken } from '@/net/token';
import { ERR } from '@/protocol/errors';
import { KickReason } from '@/protocol/messages';
import { connection } from '@/state/connection';
import { fakeFetch, restClientOn } from '@/testing/fake-fetch';
import { resetStores } from '@/testing/reset-stores';
import { user } from '@/testing/user';

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
    render(<LoginScreen rest={restClientOn(ff.fetch)} onLoggedIn={onLoggedIn} />);
    await user.type(screen.getByLabelText('Username'), username);
    await user.type(screen.getByLabelText('Password'), password);
    await user.click(screen.getByRole('button', { name: 'Log in' }));
    expect(screen.getByRole('alert')).toHaveTextContent(rule);
    expect(ff.calls).toHaveLength(0);
    expect(onLoggedIn).not.toHaveBeenCalled();
  });

  it('a 401 shows the client’s words for it', async () => {
    const ff = fakeFetch(() => ({
      status: 401,
      body: { code: ERR.UNAUTHENTICATED, message: 'invalid credentials or session' },
    }));
    render(<LoginScreen rest={restClientOn(ff.fetch)} onLoggedIn={vi.fn()} />);
    await user.type(screen.getByLabelText('Username'), 'alice');
    await user.type(screen.getByLabelText('Password'), 'wrong horse');
    await user.click(screen.getByRole('button', { name: 'Log in' }));
    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Wrong username or password, or your session has ended.',
    );
  });

  it('the login throttle shows the client’s wait message', async () => {
    const ff = fakeFetch(() => ({
      status: 429,
      body: { code: ERR.LOGIN_RATE_LIMITED, message: 'too many login attempts' },
    }));
    render(<LoginScreen rest={restClientOn(ff.fetch)} onLoggedIn={vi.fn()} />);
    await user.type(screen.getByLabelText('Username'), 'alice');
    await user.type(screen.getByLabelText('Password'), 'correct horse');
    await user.click(screen.getByRole('button', { name: 'Log in' }));
    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Too many login attempts. Try again in a minute.',
    );
  });

  it.each([
    {
      name: 'no server reached',
      reply: () => new TypeError('Failed to fetch'),
      text: 'Can’t reach the server. Check your connection and try again.',
    },
    {
      name: 'a proxy page while the gateway restarts',
      reply: () => ({ status: 502 }),
      text: 'The server isn’t answering right now. Try again in a moment.',
    },
  ])(
    '$name shows the client’s words, not the browser’s or the proxy’s',
    async ({ reply, text }) => {
      const ff = fakeFetch(reply);
      render(<LoginScreen rest={restClientOn(ff.fetch)} onLoggedIn={vi.fn()} />);
      await user.type(screen.getByLabelText('Username'), 'alice');
      await user.type(screen.getByLabelText('Password'), 'correct horse');
      await user.click(screen.getByRole('button', { name: 'Log in' }));
      expect(await screen.findByRole('alert')).toHaveTextContent(text);
    },
  );

  it('a successful login keeps the token and moves on', async () => {
    const ff = fakeFetch(() => ({ status: 201, body: { token: 'tok-1' } }));
    const onLoggedIn = vi.fn();
    render(<LoginScreen rest={restClientOn(ff.fetch)} onLoggedIn={onLoggedIn} />);
    await user.type(screen.getByLabelText('Username'), 'alice');
    await user.type(screen.getByLabelText('Password'), 'correct horse');
    await user.click(screen.getByRole('button', { name: 'Log in' }));
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
    render(<LoginScreen rest={restClientOn(ff.fetch)} onLoggedIn={onLoggedIn} />);
    await user.click(screen.getByRole('button', { name: /sign up/i }));
    await user.type(screen.getByLabelText('Username'), 'alice');
    await user.type(screen.getByLabelText('Password'), 'correct horse');
    await user.click(screen.getByRole('button', { name: 'Create account' }));
    await waitFor(() => expect(onLoggedIn).toHaveBeenCalledWith('alice'));
    expect(ff.calls.map((c) => c.path)).toEqual(['/api/accounts', '/api/sessions']);
  });

  it('typing in the form keeps the keys from the game, and shows why the player was kicked', async () => {
    connection.getState().setConnection('kicked', { kick: KickReason.LOGGED_IN_ELSEWHERE });
    render(
      <LoginScreen
        rest={restClientOn(fakeFetch(() => ({ status: 500 })).fetch)}
        onLoggedIn={vi.fn()}
      />,
    );
    expect(screen.getByText(/logged in somewhere else/i)).toBeInTheDocument();
    const field = screen.getByLabelText('Username');
    await user.click(field);
    expect(isTyping(document.activeElement)).toBe(true);
    field.blur();
    expect(isTyping(document.activeElement)).toBe(false);
  });

  it('a kick reason this client does not know still says the server closed the connection', () => {
    connection.getState().setConnection('kicked', { kick: 99 as KickReason });
    render(
      <LoginScreen
        rest={restClientOn(fakeFetch(() => ({ status: 500 })).fetch)}
        onLoggedIn={vi.fn()}
      />,
    );
    expect(screen.getByText('The server closed the connection.')).toBeInTheDocument();
  });
});
