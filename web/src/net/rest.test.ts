import { afterEach, describe, expect, it } from 'vitest';

import { createRestClient, RestError } from './rest';
import type { FetchLike, HttpResponse } from './rest';
import { clearToken, getToken, setToken } from './token';

interface Call {
  url: string;
  init: RequestInit;
}

function fakeFetch(reply: (call: Call) => { status: number; body?: unknown } | Error): {
  fetch: FetchLike;
  calls: Call[];
} {
  const calls: Call[] = [];
  const fetch: FetchLike = (url, init) => {
    const call = { url, init };
    calls.push(call);
    const r = reply(call);
    if (r instanceof Error) {
      return Promise.reject(r);
    }
    const res: HttpResponse = {
      status: r.status,
      ok: r.status >= 200 && r.status < 300,
      json: () =>
        r.body === undefined ? Promise.reject(new Error('no body')) : Promise.resolve(r.body),
    };
    return Promise.resolve(res);
  };
  return { fetch, calls };
}

const character = {
  id: 'c1',
  name: 'Ayla',
  class: 'warrior',
  level: 1,
  exp: 0,
  stats: { str: 12, dex: 5, int: 4, agi: 5, vit: 10 },
  hp: 204,
  mp: 28,
  map_id: 'town',
};

describe('rest client', () => {
  afterEach(() => {
    clearToken();
  });

  it.each([
    {
      name: 'register',
      run: (c: ReturnType<typeof createRestClient>) => c.register('alice', 'correct horse'),
      status: 201,
      body: { id: 'a1', username: 'alice', created_at: '2026-10-22T00:00:00Z' },
      method: 'POST',
      path: '/api/accounts',
      sent: { username: 'alice', password: 'correct horse' },
      want: { id: 'a1', username: 'alice', created_at: '2026-10-22T00:00:00Z' },
    },
    {
      name: 'list characters',
      run: (c: ReturnType<typeof createRestClient>) => c.listCharacters(),
      status: 200,
      body: { characters: [character] },
      method: 'GET',
      path: '/api/characters',
      sent: undefined,
      want: [character],
    },
    {
      name: 'create a character',
      run: (c: ReturnType<typeof createRestClient>) => c.createCharacter('Ayla', 'warrior'),
      status: 201,
      body: character,
      method: 'POST',
      path: '/api/characters',
      sent: { name: 'Ayla', class: 'warrior' },
      want: character,
    },
    {
      name: 'get settings',
      run: (c: ReturnType<typeof createRestClient>) => c.getSettings(),
      status: 200,
      body: { control_preset: 'classic' },
      method: 'GET',
      path: '/api/settings',
      sent: undefined,
      want: { control_preset: 'classic' },
    },
    {
      name: 'put settings',
      run: (c: ReturnType<typeof createRestClient>) => c.putSettings({ control_preset: 'wasd' }),
      status: 200,
      body: { control_preset: 'wasd' },
      method: 'PUT',
      path: '/api/settings',
      sent: { control_preset: 'wasd' },
      want: { control_preset: 'wasd' },
    },
  ])(
    '$name builds the right method, path and body',
    async ({ run, status, body, method, path, sent, want }) => {
      const { fetch, calls } = fakeFetch(() => ({ status, body }));
      const client = createRestClient({ origin: 'http://game.test', fetch });
      await expect(run(client)).resolves.toEqual(want);
      expect(calls).toHaveLength(1);
      expect(calls[0]?.url).toBe(`http://game.test${path}`);
      expect(calls[0]?.init.method).toBe(method);
      const headers = calls[0]?.init.headers as Record<string, string>;
      if (sent === undefined) {
        expect(calls[0]?.init.body).toBeUndefined();
        expect(headers['Content-Type']).toBeUndefined();
      } else {
        expect(JSON.parse(calls[0]?.init.body as string)).toEqual(sent);
        expect(headers['Content-Type']).toBe('application/json');
      }
    },
  );

  it('login keeps the token, every later call carries it, and logout forgets it', async () => {
    const { fetch, calls } = fakeFetch(({ url }) =>
      url.endsWith('/api/sessions')
        ? { status: 201, body: { token: 'tok-login' } }
        : { status: 204 },
    );
    const client = createRestClient({ origin: '', fetch });
    expect(
      (calls[0]?.init.headers as Record<string, string> | undefined)?.Authorization,
    ).toBeUndefined();
    await client.login('alice', 'correct horse');
    expect(getToken()).toBe('tok-login');
    expect((calls[0]?.init.headers as Record<string, string>).Authorization).toBeUndefined();
    await client.logout();
    expect(calls[1]?.url).toBe('/api/sessions/current');
    expect(calls[1]?.init.method).toBe('DELETE');
    expect((calls[1]?.init.headers as Record<string, string>).Authorization).toBe(
      'Bearer tok-login',
    );
    expect(getToken()).toBeNull();
  });

  it('the next call after a Welcome rotated the token sends the new one', async () => {
    const { fetch, calls } = fakeFetch(() => ({ status: 200, body: { characters: [] } }));
    const client = createRestClient({ origin: '', fetch });
    setToken('tok-login');
    await client.listCharacters();
    setToken('tok-rotated'); // what the socket does on Welcome
    await client.listCharacters();
    expect((calls[0]?.init.headers as Record<string, string>).Authorization).toBe(
      'Bearer tok-login',
    );
    expect((calls[1]?.init.headers as Record<string, string>).Authorization).toBe(
      'Bearer tok-rotated',
    );
  });

  it.each([
    {
      name: 'a 409 on create character keeps its own code, name_taken',
      status: 409,
      body: { code: 'name_taken', message: 'that name is taken' },
      code: 'name_taken',
      message: 'that name is taken',
    },
    {
      name: 'a 401 maps to unauthenticated',
      status: 401,
      body: { code: 'unauthenticated', message: 'invalid credentials or session' },
      code: 'unauthenticated',
      message: 'invalid credentials or session',
    },
    {
      name: 'a 429 keeps its own code, login_rate_limited',
      status: 429,
      body: { code: 'login_rate_limited', message: 'too many login attempts' },
      code: 'login_rate_limited',
      message: 'too many login attempts',
    },
    {
      name: 'an unknown code becomes internal',
      status: 500,
      body: { code: 'teapot', message: 'short and stout' },
      code: 'internal',
      message: 'short and stout',
    },
    {
      name: 'an error without a code is malformed',
      status: 502,
      body: {},
      code: 'malformed',
      message: /HTTP 502/,
    },
  ])('$name', async ({ status, body, code, message }) => {
    const { fetch } = fakeFetch(() => ({ status, body }));
    const client = createRestClient({ origin: '', fetch });
    const err = await client.createCharacter('Ayla', 'warrior').catch((e: unknown) => e);
    expect(err).toBeInstanceOf(RestError);
    const re = err as RestError;
    expect(re.status).toBe(status);
    expect(re.code).toBe(code);
    expect(re.message).toMatch(message);
  });

  it('a network failure is a RestError with the network code and no status', async () => {
    const { fetch } = fakeFetch(() => new Error('connection refused'));
    const client = createRestClient({ origin: '', fetch });
    const err = (await client.getSettings().catch((e: unknown) => e)) as RestError;
    expect(err.code).toBe('network');
    expect(err.status).toBe(0);
    expect(err.message).toBe('connection refused');
  });

  it('logout forgets the token even when the server call fails', async () => {
    const { fetch } = fakeFetch(() => ({
      status: 401,
      body: { code: 'unauthenticated', message: 'gone' },
    }));
    const client = createRestClient({ origin: '', fetch });
    setToken('tok-old');
    await expect(client.logout()).rejects.toBeInstanceOf(RestError);
    expect(getToken()).toBeNull();
  });
});
