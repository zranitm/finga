// A fetch double for the REST client: scripted replies by method and path,
// and a record of every call. Imported by tests only.
import type { FetchLike, HttpResponse } from '@/net/rest';

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
  /** The calls for one method and path. */
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
  mp: 28,
  map_id: 'town',
};
