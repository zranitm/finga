// The character's PvP profile, read as they enter the world, after each match's end or a promotion, and whenever the Arena window asks: its standings go to the Play cards, and its build to the Arena tab while the player hasn't changed theirs.
import type { RestClient } from '@/net/rest';
import type { GameSocket } from '@/net/socket';
import { arenaBuild } from '@/state/arena-build';
import { pvpProfile } from '@/state/pvp-profile';

/** Reads the profile now and on each of those until the returned stop; a read that fails keeps the last, and only the newest read lands. */
export function watchPvpProfile(
  rest: Pick<RestClient, 'getPvpProfile'>,
  socket: Pick<GameSocket, 'on'>,
  characterId: string,
): () => void {
  let latest = 0;
  let stopped = false;
  const read = () => {
    const mine = ++latest;
    rest.getPvpProfile(characterId).then(
      (p) => {
        if (mine === latest && !stopped) {
          pvpProfile.getState().apply(p.standings);
          arenaBuild.getState().load(p.build);
        }
      },
      () => undefined, // the cards keep what they had; the next end or ask reads again
    );
  };
  read();
  const offs = [
    socket.on('matchEnded', read),
    socket.on('promoted', read),
    pvpProfile.subscribe((now, before) => {
      if (now.asked !== before.asked) {
        read();
      }
    }),
  ];
  return () => {
    stopped = true;
    offs.forEach((off) => off());
  };
}
