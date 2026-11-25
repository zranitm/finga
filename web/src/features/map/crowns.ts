// The field bosses' crowns on a plan (map-views.md section 5, large-regions.md step 5c-3b): a hunting map's beaten boss grey at its spot, its live crown drawn where it is while up; a region's area bosses each at its spot, the player's own area's from its frames as a map's is, the others' from a bosses query.
import { useEffect, useState } from 'react';

import type { maps } from '@/gamedata/maps';
import type { GameSocket } from '@/net/socket';
import type { BossDown } from '@/state/field-boss';

const BOSSES_EVERY_MS = 5000; // a region's bosses, asked for again while This region shows them

type MapData = (typeof maps)[number];

/** A boss's crown at its spot: up, or beaten with its time back. */
export interface Crown {
  readonly mob: string;
  readonly x: number;
  readonly z: number;
  readonly down: BossDown | null;
}

/** How a region's area bosses stand, by area: when a beaten one's back on performance.now's clock, null for one up. */
export type AreaBosses = ReadonlyMap<string, number | null>;

/** The crowns at their spots on the player's own map: a hunting map's boss while beaten, as its live crown shows it up; in a region, the player's area's the same way, from down, and up at its spot while live draws no crown, as when it's out of the frames' view; every other area's as others last said, none before they've said. */
export function crownsOf(
  map: MapData,
  area: string | null,
  down: BossDown | null,
  others: AreaBosses | null,
  live: boolean,
): Crown[] {
  if (map.boss) {
    return down ? [{ ...map.boss, down }] : [];
  }
  return map.areas.flatMap((a): Crown[] => {
    const b = a.boss;
    if (!b) {
      return [];
    }
    if (a.id === area) {
      return down || !live ? [{ mob: b.mob, x: b.x, z: b.z, down }] : [];
    }
    const backAt = others?.get(a.id);
    if (backAt === undefined) {
      return [];
    }
    return [{ mob: b.mob, x: b.x, z: b.z, down: backAt === null ? null : { type: b.mob, backAt } }];
  });
}

/** How the region's area bosses stand on the player's map and channel, where, asked of the server now and every BOSSES_EVERY_MS while on; null while off or till the first answer there, so a channel's bosses never show on the next. */
export function useAreaBosses(socket: GameSocket, on: boolean, where: string): AreaBosses | null {
  const [said, setSaid] = useState<{ where: string; bosses: AreaBosses } | null>(null);
  useEffect(() => {
    if (!on) {
      return;
    }
    let current = true; // an answer that lands after a move is the last place's
    const ask = () => {
      socket.request('bosses', {}).then(
        (r) => {
          if (current && r.bosses) {
            const now = performance.now();
            const bosses = r.bosses.bosses.map(
              (b) => [b.area, b.up ? null : now + Number(b.backInMs)] as const,
            );
            setSaid({ where, bosses: new Map(bosses) });
          }
        },
        () => undefined, // the next ask tries again
      );
    };
    ask();
    const every = setInterval(ask, BOSSES_EVERY_MS);
    return () => {
      current = false;
      clearInterval(every);
    };
  }, [socket, on, where]);
  return on && said?.where === where ? said.bosses : null;
}
