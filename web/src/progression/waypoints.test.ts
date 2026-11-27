import { describe, expect, it } from 'vitest';

import { maps } from '@/gamedata/maps';

import { waypointName } from './waypoints';
import type { Waypoint } from './waypoints';

describe('waypointName', () => {
  it('names every region waypoint, whichever map is showing', () => {
    const all = maps.flatMap((m): readonly Waypoint[] => m.waypoints);
    expect(all.length).toBeGreaterThan(0);
    for (const w of all) {
      expect(waypointName(w.id)).toBe(w.name);
    }
  });

  it('keeps the id of a waypoint this client has no data for', () => {
    expect(waypointName('no_such_waypoint')).toBe('no_such_waypoint');
  });
});
