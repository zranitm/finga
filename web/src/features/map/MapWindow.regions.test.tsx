// The World tab with a region on the chart, which no shipped region is yet (large-regions.md section 8): the test region charted by a fixture, as the chart reads the maps as its module loads.
import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { afterAll, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest';

import type * as MapViewModule from '@/state/map-view';
import { NO_GROUND } from '@/state/map-view';
import type * as TeleportModule from '@/state/teleport';
import type * as WaypointsModule from '@/state/waypoints';
import type * as ResetModule from '@/testing/reset-stores';
import { user } from '@/testing/user';

import type * as WindowModule from './MapWindow';

// A worker shares its modules across files, and one before this may have loaded the window with the real chart: this file loads the window and the stores it reads afresh, under the mock, and leaves the next file to load its own.
let MapWindow: typeof WindowModule.MapWindow;
let mapView: typeof MapViewModule.mapView;
let teleport: typeof TeleportModule.teleport;
let waypoints: typeof WaypointsModule.waypoints;
let resetStores: typeof ResetModule.resetStores;
beforeAll(async () => {
  vi.resetModules();
  ({ MapWindow } = await import('./MapWindow'));
  ({ mapView } = await import('@/state/map-view'));
  ({ teleport } = await import('@/state/teleport'));
  ({ waypoints } = await import('@/state/waypoints'));
  ({ resetStores } = await import('@/testing/reset-stores'));
});
afterAll(() => {
  vi.resetModules();
});

vi.mock('@/gamedata/maps', async (importOriginal) => {
  const real = await importOriginal<{ maps: readonly { readonly id: string }[] }>();
  return {
    maps: real.maps.map((m) => (m.id === 'test_region' ? { ...m, place: [90, 46] } : m)),
  };
});

const TOWN = { id: 'town', name: 'Test Town', x: 124, z: 85, y: 0, main: true };
const WEST = { id: 'west', name: 'West Field', x: 60, z: 85, y: 0, main: false };
const NORTH = { id: 'north', name: 'North Field', x: 118, z: 150, y: 0, main: false };

/** Each map's layout as the app gives it, the test region's with its waypoints. */
const layoutOf = vi.fn((id: string) =>
  Promise.resolve({
    size: id === 'test_region' ? ([256, 256] as const) : ([48, 32] as const),
    ground: id === 'test_region' ? ({ kind: 'tiles', folder: 'bake/tiles' } as const) : NO_GROUND,
    waypoints: id === 'test_region' ? [TOWN, WEST, NORTH] : [],
  }),
);

describe('MapWindow, with a region on the chart', () => {
  beforeEach(() => {
    resetStores();
    act(() => {
      mapView.getState().showMap('town', [48, 32], NO_GROUND, []);
      waypoints.getState().find('west');
    });
  });

  it('a picked region is a region, reached by its waypoints alone: its layout marks them, lit once found, and a picked one offers Teleport here as This map does', async () => {
    const request = vi.fn<(t: string, d: unknown, seq?: number) => Promise<unknown>>(() =>
      Promise.resolve({ ok: true, code: 0 }),
    );
    const onClose = vi.fn();
    render(
      <MapWindow
        socket={{ request, sequence: 42 } as never}
        layoutOf={layoutOf}
        onClose={onClose}
      />,
    );
    await user.click(screen.getByRole('tab', { name: 'World' }));
    await user.click(screen.getByRole('button', { name: 'Test Region' }));
    const svg = await screen.findByRole('img', { name: 'Test Region' });
    expect(layoutOf).toHaveBeenCalledWith('test_region');
    expect(screen.getByText('Region')).toBeInTheDocument();
    expect(svg.querySelectorAll('image.map-tile')).toHaveLength(4); // its tiles, from its layout
    expect(screen.getByRole('group', { name: 'Zoom' })).toBeInTheDocument(); // which pan and zoom, as This region's do
    expect(screen.queryByRole('button', { name: 'Teleport here' })).not.toBeInTheDocument(); // none by the region's id
    expect(
      [...svg.querySelectorAll('.map-waypoint-found')].map((g) => g.getAttribute('data-waypoint')),
    ).toEqual(['west']);
    await user.click(within(svg).getByRole('button', { name: 'North Field' }));
    expect(screen.getByRole('region', { name: 'Waypoint' })).toHaveTextContent('Not found yet');
    expect(screen.queryByRole('button', { name: 'Teleport here' })).not.toBeInTheDocument();
    await user.click(within(svg).getByRole('button', { name: 'West Field' }));
    await user.click(screen.getByRole('button', { name: 'Teleport here' }));
    expect(request).toHaveBeenCalledWith('teleport', { waypoint: 'west' }, 42);
    await waitFor(() => {
      expect(onClose).toHaveBeenCalledOnce();
    });
    expect(teleport.getState().to).toEqual({ waypoint: 'west', name: 'West Field' });
  });

  it('the chart’s node and card call a region a region, not by levels it hasn’t', async () => {
    render(<MapWindow socket={{} as never} layoutOf={layoutOf} onClose={vi.fn()} />);
    await user.click(screen.getByRole('tab', { name: 'World' }));
    const chart = screen.getByRole('group', { name: 'World' });
    const node = chart.querySelector('.map-node[data-map="test_region"]')!;
    expect(node.querySelector('.map-node-levels')).toHaveTextContent(/^Region$/);
    fireEvent.pointerEnter(node);
    const card = within(screen.getByRole('tooltip'));
    expect([card.getByText('Test Region'), card.getByText('Region')]).toHaveLength(2);
    expect(card.queryByText(/^Lv /)).not.toBeInTheDocument(); // its camps' mobs keep theirs
  });
});
