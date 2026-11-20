// The character's PvP profile, read as they enter the world, after each match's end or a promotion, and when the Arena window asks: its standings go to the Play cards, and its build to the Arena tab while the player hasn't changed theirs.
import type { RestClient } from '@/net/rest';
import type { GameSocket } from '@/net/socket';
import { arenaBuild } from '@/state/arena-build';
import { pvpProfile } from '@/state/pvp-profile';

/** How long a read answers the Arena window's asks: a window opened and shut in a burst reads once. */
export const PROFILE_FRESH_MS = 10_000;

/** Reads the profile now and on each of those until the returned stop; a read that fails keeps the last, and only the newest read lands. An ask while a read is under way, or one landed within PROFILE_FRESH_MS, reads nothing. */
export function watchPvpProfile(
  rest: Pick<RestClient, 'getPvpProfile'>,
  socket: Pick<GameSocket, 'on'>,
  characterId: string,
): () => void {
  let latest = 0;
  let stopped = false;
  let reading = false;
  let landed = -Infinity;
  const read = () => {
    const mine = ++latest;
    reading = true;
    rest.getPvpProfile(characterId).then(
      (p) => {
        if (mine === latest) {
          reading = false;
          landed = performance.now();
        }
        if (mine === latest && !stopped) {
          pvpProfile.getState().apply(p.standings);
          arenaBuild.getState().load(p.build);
        }
      },
      () => {
        if (mine === latest) {
          reading = false; // the cards keep what they had; the next end or ask reads again
        }
      },
    );
  };
  read();
  const offs = [
    socket.on('matchEnded', read),
    socket.on('promoted', read),
    pvpProfile.subscribe((now, before) => {
      if (
        now.asked !== before.asked &&
        !reading &&
        performance.now() - landed >= PROFILE_FRESH_MS
      ) {
        read();
      }
    }),
  ];
  return () => {
    stopped = true;
    offs.forEach((off) => off());
  };
}
