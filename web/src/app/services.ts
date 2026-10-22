// The one REST client and the one game socket the app uses, built from the
// environment; tests build their own with fakes and hand them to App.
import { env } from '@/config/env';
import { createRestClient } from '@/net/rest';
import type { RestClient } from '@/net/rest';
import { GameSocket } from '@/net/socket';

export interface Services {
  readonly rest: RestClient;
  readonly socket: GameSocket;
}

/** ws(s)://host/ws for the API origin, or the page's own origin when it is empty. */
export function socketUrl(apiOrigin: string, pageOrigin: string): string {
  const origin = apiOrigin === '' ? pageOrigin : apiOrigin;
  return origin.replace(/^http/, 'ws') + '/ws';
}

export function createServices(): Services {
  return {
    rest: createRestClient(),
    socket: new GameSocket({ url: socketUrl(env.apiOrigin, location.origin) }),
  };
}
