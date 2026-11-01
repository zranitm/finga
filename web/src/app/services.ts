import { env } from '@/config/env';
import { GameHandle } from '@/game/main';
import { createRestClient } from '@/net/rest';
import type { RestClient } from '@/net/rest';
import { GameSocket } from '@/net/socket';
import { binaryCodec, jsonCodec } from '@/protocol/codec';

export interface Services {
  readonly rest: RestClient;
  readonly socket: GameSocket;
  readonly game: GameHandle;
}

/** Falls back to the page's origin when apiOrigin is empty. */
export function socketUrl(apiOrigin: string, pageOrigin: string): string {
  const origin = apiOrigin === '' ? pageOrigin : apiOrigin;
  return origin.replace(/^http/, 'ws') + '/ws';
}

export function createServices(): Services {
  const socket = new GameSocket({
    url: socketUrl(env.apiOrigin, location.origin),
    codec: env.wsJson ? jsonCodec : binaryCodec,
    delayMs: env.netDelayMs,
  });
  return { rest: createRestClient(), socket, game: new GameHandle({ socket }) };
}
