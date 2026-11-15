import { env } from '@/config/env';
import { GameHandle } from '@/game/main';
import type { GameView } from '@/game/main';
import { World3DHandle } from '@/game/world3d/handle';
import { createRestClient } from '@/net/rest';
import type { RestClient } from '@/net/rest';
import { GameSocket } from '@/net/socket';
import { binaryCodec, jsonCodec } from '@/protocol/codec';

export interface Services {
  readonly rest: RestClient;
  readonly socket: GameSocket;
  readonly game: GameView;
}

/** Falls back to the page's origin when apiOrigin is empty. */
export function socketUrl(apiOrigin: string, pageOrigin: string): string {
  const origin = apiOrigin === '' ? pageOrigin : apiOrigin;
  return origin.replace(/^http/, 'ws') + '/ws';
}

/** The 3D spike draws the world when the page's query asks for it with world=3d. */
export function wants3d(search: string): boolean {
  return new URLSearchParams(search).get('world') === '3d';
}

export function createServices(): Services {
  const socket = new GameSocket({
    url: socketUrl(env.apiOrigin, location.origin),
    codec: env.wsJson ? jsonCodec : binaryCodec,
    delayMs: env.netDelayMs,
  });
  const game = wants3d(location.search) ? new World3DHandle() : new GameHandle({ socket });
  return { rest: createRestClient(), socket, game };
}
