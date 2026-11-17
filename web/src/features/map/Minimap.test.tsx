import { act, render, screen } from '@testing-library/react';
import { beforeEach, describe, expect, it } from 'vitest';

import { maps } from '@/gamedata/maps';
import { mapView } from '@/state/map-view';
import { settings } from '@/state/settings';
import { resetStores } from '@/testing/reset-stores';

import { Minimap } from './Minimap';

const forest = maps.find((m) => m.id === 'forest')!;
const SIZE = [72, 40] as const; // the forest's

function at(x: number, z: number, yaw = 0) {
  act(() => mapView.getState().mark({ me: { x, z, yaw }, party: [], lock: null, boss: null }));
}

describe('Minimap', () => {
  beforeEach(() => {
    resetStores();
    mapView.getState().showMap('forest', SIZE, '');
  });

  it('shows 40 m round the player, north up, and holds the view inside the map at its edges', () => {
    render(<Minimap />);
    expect(screen.queryByRole('img', { name: 'Minimap' })).not.toBeInTheDocument(); // the player not drawn yet
    at(40, 22);
    const svg = () => screen.getByRole('img', { name: 'Minimap' });
    expect(svg().getAttribute('viewBox')).toBe('20 0 40 40'); // 40 m north to south: all of it
    at(5, 22);
    expect(svg().getAttribute('viewBox')).toBe('0 0 40 40');
    at(70, 22);
    expect(svg().getAttribute('viewBox')).toBe('32 0 40 40');
  });

  it('places its marks by world x and z, north up: spawns, portals, NPCs and the player’s arrow along the heading', () => {
    render(<Minimap />);
    at(30, 10, Math.PI / 2);
    const svg = screen.getByRole('img', { name: 'Minimap' });
    const spawn = forest.spawns[0];
    const circle = svg.querySelector(`.map-spawn[data-mob="${spawn.mob}"]`)!;
    expect([
      circle.getAttribute('cx'),
      circle.getAttribute('cy'),
      circle.getAttribute('r'),
    ]).toEqual([String(spawn.x), String(SIZE[1] - spawn.z), String(spawn.radius)]);
    const portal = forest.portals[0];
    const ring = svg.querySelector(`.map-portal[data-to="${portal.to}"]`)!;
    expect([ring.getAttribute('cx'), ring.getAttribute('cy')]).toEqual([
      String(portal.x),
      String(SIZE[1] - portal.z),
    ]);
    expect(svg.querySelector('.map-me')!.getAttribute('transform')).toMatch(
      /^translate\(30 30\) rotate\(90\)/,
    ); // east: turned a quarter clockwise from north
  });

  it('marks the party members the scene places, the lock and the field boss, each only while given', () => {
    render(<Minimap />);
    at(30, 10);
    const svg = screen.getByRole('img', { name: 'Minimap' });
    expect(svg.querySelectorAll('.map-party, .map-lock, .map-boss')).toHaveLength(0);
    act(() =>
      mapView.getState().mark({
        me: { x: 30, z: 10, yaw: 0 },
        party: [{ id: 'c-robin', name: 'Robin', x: 34, z: 12 }],
        lock: { x: 28, z: 14 },
        boss: { x: 50, z: 20 },
      }),
    );
    const party = svg.querySelector('.map-party[data-id="c-robin"]')!;
    const lock = svg.querySelector('.map-lock circle')!;
    expect([
      [party.getAttribute('cx'), party.getAttribute('cy')],
      [lock.getAttribute('cx'), lock.getAttribute('cy')],
      svg.querySelector('.map-boss')!.getAttribute('transform')?.startsWith('translate(50 20)'),
    ]).toEqual([['34', '28'], ['28', '26'], true]);
  });

  it('hides when the setting says so', () => {
    render(<Minimap />);
    at(30, 10);
    act(() => settings.getState().setShowMinimap(false));
    expect(screen.queryByRole('img', { name: 'Minimap' })).not.toBeInTheDocument();
  });
});
