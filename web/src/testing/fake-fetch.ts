import { vi } from 'vitest';

import { createRestClient } from '@/net/rest';
import type { RestClient } from '@/net/rest';

/** The part of a Response the REST client reads. */
export interface HttpResponse {
  readonly status: number;
  readonly ok: boolean;
  json(): Promise<unknown>;
}

export type FetchLike = (url: string, init: RequestInit) => Promise<HttpResponse>;

/** A REST client whose calls go to this fetch, stubbed in as the global one until the test ends. */
export function restClientOn(fetch: FetchLike): RestClient {
  vi.stubGlobal('fetch', fetch);
  return createRestClient();
}

export interface FetchCall {
  readonly method: string;
  readonly path: string;
  readonly body: unknown;
  readonly auth: string | undefined;
}

export type Reply = { status: number; body?: unknown } | Error;
export type Script = (call: FetchCall) => Reply;

export interface FakeFetch {
  readonly fetch: FetchLike;
  readonly calls: FetchCall[];
  of(method: string, path: string): FetchCall[];
}

export function fakeFetch(script: Script): FakeFetch {
  const calls: FetchCall[] = [];
  const fetch: FetchLike = (url, init) => {
    const headers = (init.headers ?? {}) as Record<string, string>;
    const call: FetchCall = {
      method: init.method ?? 'GET',
      path: url.replace(/^https?:\/\/[^/]+/, ''),
      body: typeof init.body === 'string' ? JSON.parse(init.body) : undefined,
      auth: headers.Authorization,
    };
    calls.push(call);
    const reply = script(call);
    if (reply instanceof Error) {
      return Promise.reject(reply);
    }
    const res: HttpResponse = {
      status: reply.status,
      ok: reply.status >= 200 && reply.status < 300,
      json: () =>
        reply.body === undefined
          ? Promise.reject(new Error('no body'))
          : Promise.resolve(reply.body),
    };
    return Promise.resolve(res);
  };
  return {
    fetch,
    calls,
    of: (method, path) => calls.filter((c) => c.method === method && c.path === path),
  };
}

export const sampleCharacter = {
  id: 'c-ayla',
  name: 'Ayla',
  class: 'warrior',
  level: 1,
  exp: 0,
  stats: { str: 12, dex: 5, int: 4, agi: 5, vit: 10 },
  hp: 204,
  map_id: 'town',
  skill_bar: ['', '', '', '', '', ''],
};
