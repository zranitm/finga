import { create } from '@bufbuild/protobuf';
import { act, render, screen } from '@testing-library/react';
import { beforeEach, describe, expect, it } from 'vitest';

import { maps } from '@/gamedata/maps';
import { MatchPhase, MatchStateSchema, WelcomeSchema } from '@/protocol/messages';
import { character } from '@/state/character';
import { mapView, NO_GROUND } from '@/state/map-view';
import { pvp } from '@/state/pvp';
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
    mapView.getState().showMap('forest', SIZE, NO_GROUND, []);
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

  it('is named for its map, and in a region for the channel too', () => {
    render(<Minimap />);
    at(30, 10);
    expect(document.querySelector('.minimap-name')).toHaveTextContent(/^Forest$/);
    act(() => {
      mapView.getState().showMap('test_region', [256, 256], NO_GROUND, []);
      mapView.getState().inChannel(2);
    });
    at(124, 80);
    expect(document.querySelector('.minimap-name')).toHaveTextContent(/^Test Region · Ch\. 2$/);
  });

  it('draws a region’s tiles at 1 m a pixel, only those its view meets, each a screen pixel over its east and south neighbours', () => {
    render(<Minimap />);
    act(() => {
      mapView
        .getState()
        .showMap('test_region', [256, 256], { kind: 'tiles', folder: 'bake/tiles' }, []);
    });
    const tiles = () =>
      [...document.querySelectorAll('image.map-tile')].map((t) =>
        ['href', 'x', 'y', 'width'].map((a) => t.getAttribute(a)),
      );
    const over = String(128 + 40 / 180);
    at(124, 80); // the view x 104–144, y 156–196: across the line between the west tiles and the east
    expect(tiles()).toEqual([
      ['bake/tiles/1/0_0.png', '0', '128', over],
      ['bake/tiles/1/1_0.png', '128', '128', over],
    ]);
    at(124, 130); // y 106–146: four
    expect(tiles().map(([href]) => href)).toEqual([
      'bake/tiles/1/0_0.png',
      'bake/tiles/1/1_0.png',
      'bake/tiles/1/0_1.png',
      'bake/tiles/1/1_1.png',
    ]);
    at(200, 200); // x 180–220, y 36–76: one
    expect(tiles().map(([href]) => href)).toEqual(['bake/tiles/1/1_1.png']);
    at(148, 64); // x 128–168: its west edge on the line, so the west tile's left out
    expect(tiles().map(([href]) => href)).toEqual(['bake/tiles/1/1_0.png']);
    expect(document.querySelectorAll('image:not(.map-tile)')).toHaveLength(0);
  });

  it('marks no waypoint, as the map window alone does (large-regions.md section 8)', () => {
    render(<Minimap />);
    act(() => {
      mapView
        .getState()
        .showMap('test_region', [256, 256], NO_GROUND, [
          { id: 'town', name: 'Test Town', x: 124, z: 85, y: 0, main: true },
        ]);
    });
    at(124, 80);
    expect(screen.getByRole('img', { name: 'Minimap' })).toBeInTheDocument();
    expect(document.querySelector('.map-waypoint')).toBeNull();
  });

  it('hides when the setting says so', () => {
    render(<Minimap />);
    at(30, 10);
    act(() => settings.getState().setShowMinimap(false));
    expect(screen.queryByRole('img', { name: 'Minimap' })).not.toBeInTheDocument();
  });

  it('on a battle’s map marks the bases and points, the player’s side’s in the ally green, the other’s in the enemy red, no one’s grey, a contested edge dashed; all grey with no match', () => {
    act(() => {
      character
        .getState()
        .applyWelcome(create(WelcomeSchema, { character: { id: 'c-ayla', name: 'Ayla' } }));
      mapView.getState().showMap('battlefield', [140, 90], '', []);
    });
    render(<Minimap />);
    at(110, 45);
    const svg = screen.getByRole('img', { name: 'Minimap' });
    const looks = () => [
      ...[...svg.querySelectorAll('.map-base')].map((c) => [
        c.getAttribute('cx'),
        c.getAttribute('r'),
        c.getAttribute('fill'),
      ]),
      ...[...svg.querySelectorAll('.map-point')].map((c) => [
        c.getAttribute('data-point'),
        c.getAttribute('fill'),
        c.hasAttribute('stroke-dasharray'),
      ]),
    ];
    expect(looks()).toEqual([
      ['13', '9', '#b8b8b8'],
      ['127', '9', '#b8b8b8'],
      ['west', '#b8b8b8', false],
      ['middle', '#b8b8b8', false],
      ['east', '#b8b8b8', false],
    ]);
    const east = svg.querySelector('.map-point[data-point="east"]')!;
    expect(['cx', 'cy', 'r'].map((a) => east.getAttribute(a))).toEqual(['102', '45', '5']); // y 90 − 45
    act(() =>
      pvp.getState().applyMatchState(
        create(MatchStateSchema, {
          match: 'mt-1',
          mode: 'battle',
          phase: MatchPhase.FIGHT,
          points: [
            { id: 'west', holder: 1 },
            { id: 'middle', holder: 0, contested: true },
            { id: 'east', holder: 2 },
          ],
          sides: [{ fighters: [{ id: 'c-ash' }] }, { fighters: [{ id: 'c-ayla' }] }],
        }),
        0,
      ),
    );
    expect(looks()).toEqual([
      ['13', '9', '#e04848'],
      ['127', '9', '#5fd35f'], // Ayla is on side 2
      ['west', '#e04848', false],
      ['middle', '#b8b8b8', true],
      ['east', '#5fd35f', false],
    ]);
  });
});
