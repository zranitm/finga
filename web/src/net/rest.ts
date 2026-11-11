// The token is read on every call, so a rotation after Hello is picked up at once.
import { env } from '@/config/env';
import { errorText } from '@/protocol/errors';

import { clearToken, getToken, setToken } from './token';

export interface Account {
  readonly id: string;
  readonly username: string;
  readonly created_at: string;
}

export interface Stats {
  readonly str: number;
  readonly dex: number;
  readonly int: number;
  readonly agi: number;
  readonly vit: number;
}

export interface Character {
  readonly id: string;
  readonly name: string;
  readonly class: string;
  readonly level: number;
  readonly exp: number;
  readonly stats: Stats;
  readonly hp: number;
  readonly mp: number;
  readonly map_id: string;
}

export type ControlPreset = 'classic' | 'wasd';

export interface Settings {
  readonly control_preset: ControlPreset;
  /** False when the account never set it. */
  readonly auto_assign_points?: boolean;
  /** False when the account never set it. */
  readonly chat_pinned?: boolean;
}

export interface HttpResponse {
  readonly status: number;
  readonly ok: boolean;
  json(): Promise<unknown>;
}

export type FetchLike = (url: string, init: RequestInit) => Promise<HttpResponse>;

export class RestError extends Error {
  constructor(
    readonly status: number,
    readonly code: number | 'network' | 'malformed', // a server's refusal, words from errorText, or a call with no readable answer
    message: string,
  ) {
    super(message);
    this.name = 'RestError';
  }
}

export interface RestClient {
  register(username: string, password: string): Promise<Account>;
  login(username: string, password: string): Promise<void>;
  logout(): Promise<void>;
  /** Ends every session of the account; a failure other than 401 keeps the token, as the others may still be open. */
  logoutEverywhere(): Promise<void>;
  listCharacters(): Promise<Character[]>;
  createCharacter(name: string, cls: string): Promise<Character>;
  getSettings(): Promise<Settings>;
  putSettings(settings: Settings): Promise<Settings>;
}

export interface RestOptions {
  /** Empty for same origin. */
  readonly origin?: string;
  readonly fetch?: FetchLike;
}

export function createRestClient(opts: RestOptions = {}): RestClient {
  const origin = opts.origin ?? env.apiOrigin;
  const doFetch: FetchLike = opts.fetch ?? ((url, init) => fetch(url, init));

  async function call<T>(method: string, path: string, body?: unknown): Promise<T> {
    const headers: Record<string, string> = {};
    if (body !== undefined) {
      headers['Content-Type'] = 'application/json';
    }
    const token = getToken();
    if (token !== null) {
      headers.Authorization = `Bearer ${token}`;
    }
    let res: HttpResponse;
    try {
      res = await doFetch(origin + path, {
        method,
        headers,
        ...(body !== undefined && { body: JSON.stringify(body) }),
      });
    } catch (err) {
      throw new RestError(0, 'network', err instanceof Error ? err.message : 'network failure');
    }
    if (res.status === 204) {
      return undefined as T;
    }
    let parsed: unknown;
    try {
      parsed = await res.json();
    } catch {
      throw new RestError(res.status, 'malformed', `${method} ${path}: the body is not JSON`);
    }
    if (!res.ok) {
      throw toRestError(res.status, parsed);
    }
    return parsed as T;
  }

  return {
    register: (username, password) =>
      call<Account>('POST', '/api/accounts', { username, password }),
    async login(username, password) {
      const { token } = await call<{ token: string }>('POST', '/api/sessions', {
        username,
        password,
      });
      setToken(token);
    },
    async logout() {
      try {
        await call<void>('DELETE', '/api/sessions/current');
      } finally {
        clearToken();
      }
    },
    async logoutEverywhere() {
      try {
        await call<void>('DELETE', '/api/sessions');
      } catch (err) {
        if (err instanceof RestError && err.status === 401) {
          clearToken();
        }
        throw err;
      }
      clearToken();
    },
    listCharacters: async () =>
      (await call<{ characters: Character[] }>('GET', '/api/characters')).characters,
    createCharacter: (name, cls) =>
      call<Character>('POST', '/api/characters', { name, class: cls }),
    getSettings: () => call<Settings>('GET', '/api/settings'),
    putSettings: (settings) => call<Settings>('PUT', '/api/settings', settings),
  };
}

function toRestError(status: number, body: unknown): RestError {
  if (typeof body === 'object' && body !== null && 'code' in body) {
    const { code } = body;
    if (typeof code === 'number') {
      return new RestError(status, code, errorText(code));
    }
  }
  return new RestError(status, 'malformed', `HTTP ${status} without an error body`);
}
