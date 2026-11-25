// The map views with a place below the test region, which no shipped region has yet (underground-and-underwater.md section 7): The Deep and Next Region added to the maps by a fixture, as the views read the maps as their modules load.
import { act, render, screen, waitFor, within } from '@testing-library/react';
import { afterAll, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest';

import type * as MapViewModule from '@/state/map-view';
import { NO_GROUND } from '@/state/map-view';
import type * as TeleportModule from '@/state/teleport';
import type * as WaypointsModule from '@/state/waypoints';
import type * as ResetModule from '@/testing/reset-stores';
import { user } from '@/testing/user';

import type * as WindowModule from './MapWindow';
import type * as MinimapModule from './Minimap';

vi.mock('@/gamedata/maps', async (importOriginal) => {
  const { belowTheTestRegion } = await import('@/testing/places-below');
  return belowTheTestRegion(await importOriginal());
});

// A worker shares its modules across files, and one before this may have loaded the views with the real maps: this file loads them and the stores they read afresh, under the mock, and leaves the next file to load its own.
let MapWindow: typeof WindowModule.MapWindow;
let Minimap: typeof MinimapModule.Minimap;
let mapView: typeof MapViewModule.mapView;
let teleport: typeof TeleportModule.teleport;
let waypoints: typeof WaypointsModule.waypoints;
let resetStores: typeof ResetModule.resetStores;
beforeAll(async () => {
  vi.resetModules();
  ({ MapWindow } = await import('./MapWindow'));
  ({ Minimap } = await import('./Minimap'));
  ({ mapView } = await import('@/state/map-view'));
  ({ teleport } = await import('@/state/teleport'));
  ({ waypoints } = await import('@/state/waypoints'));
  ({ resetStores } = await import('@/testing/reset-stores'));
});
afterAll(() => {
  vi.resetModules();
});

const MOUTH = { id: 'deep_mouth', name: 'Deep Mouth', x: 24, z: 64, y: 0, main: false };
const HALL = { id: 'deep_hall', name: 'Deep Hall', x: 90, z: 70, y: 0, main: false };

/** Each map's layout as the app gives it: The Deep's with its two waypoints. */
const layoutOf = vi.fn((id: string) =>
  Promise.resolve({
    size: [128, 128] as const,
    ground: NO_GROUND,
    waypoints: id === 'deep' ? [MOUTH, HALL] : [],
  }),
);

const texts = (root: Element, selector: string) =>
  [...root.querySelectorAll(selector)].map((t) => t.textContent);

/** The map window open on This region, the player on map. */
function openOn(
  map: string,
  request = vi.fn<(t: string, d: unknown, seq?: number) => Promise<unknown>>(
    () => new Promise(() => undefined),
  ), // the bosses and channels queries, unanswered
  onClose = vi.fn(),
) {
  act(() => {
    mapView.getState().showMap(map, [256, 256], NO_GROUND, []);
  });
  render(
    <MapWindow socket={{ request, sequence: 42 } as never} layoutOf={layoutOf} onClose={onClose} />,
  );
}

describe('the map views round a place below (underground-and-underwater.md section 7)', () => {
  beforeEach(() => {
    resetStores();
    layoutOf.mockClear();
    act(() => {
      waypoints.getState().find('deep_mouth');
    });
  });

  it('This region marks a gate down as a cave mouth, named for the place below with its levels; a gate beside it stays a ring to where it goes', () => {
    openOn('test_region');
    const svg = screen.getByRole('img', { name: 'Test Region' });
    expect(svg.querySelector('path.map-cave[data-to="deep"]')).not.toBeNull();
    expect(svg.querySelector('circle.map-portal[data-to="deep"]')).toBeNull();
    expect(svg.querySelector('circle.map-portal[data-to="next_region"]')).not.toBeNull();
    expect(texts(svg, '.map-label-portal').sort()).toEqual(['The Deep Lv 3–7', 'To Next Region']);
  });

  it('lists the place below’s waypoints under This region, lit once found: a found one picked teleports there, one not found says so', async () => {
    const request = vi.fn<(t: string, d: unknown, seq?: number) => Promise<unknown>>(() =>
      Promise.resolve({ ok: true, code: 0 }),
    );
    const onClose = vi.fn();
    openOn('test_region', request, onClose);
    const below = await screen.findByRole('region', { name: 'Below' });
    expect(layoutOf).toHaveBeenCalledWith('deep');
    expect(below).toHaveTextContent(/^The Deep Lv 3–7/);
    const mouth = within(below).getByRole('button', { name: 'Deep Mouth' });
    const hall = within(below).getByRole('button', { name: 'Deep Hall' });
    expect([mouth.classList.contains('found'), hall.classList.contains('found')]).toEqual([
      true,
      false,
    ]);
    await user.click(hall);
    expect(hall).toHaveAttribute('aria-pressed', 'true');
    expect(screen.getByRole('region', { name: 'Waypoint' })).toHaveTextContent('Not found yet');
    expect(screen.queryByRole('button', { name: 'Teleport here' })).not.toBeInTheDocument();
    await user.click(mouth);
    await user.click(screen.getByRole('button', { name: 'Teleport here' }));
    expect(request).toHaveBeenCalledWith('teleport', { waypoint: 'deep_mouth' }, 42);
    await waitFor(() => {
      expect(onClose).toHaveBeenCalledOnce();
    });
    expect(teleport.getState().to).toEqual({ waypoint: 'deep_mouth', name: 'Deep Mouth' });
  });

  it('inside the place below, This region is the place’s own, named for it, with nothing below it, and its way back a ring up to the region above', () => {
    openOn('deep');
    const svg = screen.getByRole('img', { name: 'The Deep' });
    expect(screen.queryByRole('region', { name: 'Below' })).not.toBeInTheDocument();
    expect(layoutOf).not.toHaveBeenCalled();
    expect(svg.querySelector('circle.map-portal[data-to="test_region"]')).not.toBeNull();
    expect(texts(svg, '.map-label-portal')).toEqual(['To Test Region']);
  });

  it('the minimap draws the cave mouth, and a party member through the gate as a ring at it', () => {
    render(<Minimap />);
    act(() => {
      mapView.getState().showMap('test_region', [256, 256], NO_GROUND, []);
      mapView.getState().mark({
        me: { x: 60, z: 46, yaw: 0 },
        party: [
          { id: 'c-robin', name: 'Robin', x: 60, z: 40, gate: true },
          { id: 'c-jade', name: 'Jade', x: 64, z: 50 },
        ],
        lock: null,
        boss: null,
      });
    });
    expect(document.querySelector('.map-cave[data-to="deep"]')).not.toBeNull();
    const dots = [...document.querySelectorAll('.map-party')].map((c) => [
      c.getAttribute('data-id'),
      c.classList.contains('map-party-gate'),
      c.getAttribute('fill'),
    ]);
    expect(dots).toEqual([
      ['c-robin', true, 'none'],
      ['c-jade', false, '#5fd35f'],
    ]);
  });
});
