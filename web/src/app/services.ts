// The one REST client, the one game socket and the one game handle the
// app uses, built from the environment; tests build their own with fakes
// and hand them to App.
import { env } from '@/config/env';
import { GameHandle } from '@/game/main';
import { createRestClient } from '@/net/rest';
import type { RestClient } from '@/net/rest';
import { GameSocket } from '@/net/socket';

export interface Services {
  readonly rest: RestClient;
  readonly socket: GameSocket;
  readonly game: GameHandle;
}

/** ws(s)://host/ws for the API origin, or the page's own origin when it is empty. */
export function socketUrl(apiOrigin: string, pageOrigin: string): string {
  const origin = apiOrigin === '' ? pageOrigin : apiOrigin;
  return origin.replace(/^http/, 'ws') + '/ws';
}

export function createServices(): Services {
  const socket = new GameSocket({
    url: socketUrl(env.apiOrigin, location.origin),
    delayMs: env.netDelayMs,
  });
  return { rest: createRestClient(), socket, game: new GameHandle({ socket }) };
}
