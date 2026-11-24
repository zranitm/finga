// The World tab with a region on the chart, which no shipped region is yet (large-regions.md section 8): the test region charted by a fixture, as the chart reads the maps as its module loads.
import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import { mapView } from '@/state/map-view';
import { teleport } from '@/state/teleport';
import { waypoints } from '@/state/waypoints';
import { resetStores } from '@/testing/reset-stores';
import { user } from '@/testing/user';

import { MapWindow } from './MapWindow';

vi.mock('@/gamedata/maps', async (importOriginal) => {
  const real = await importOriginal<{ maps: readonly { readonly id: string }[] }>();
  return {
    maps: real.maps.map((m) => (m.id === 'test_region' ? { ...m, place: [90, 46] } : m)),
  };
});

const TOWN = { id: 'town', name: 'Test Town', x: 124, z: 85, main: true };
const WEST = { id: 'west', name: 'West Field', x: 60, z: 85, main: false };
const NORTH = { id: 'north', name: 'North Field', x: 118, z: 150, main: false };

/** Each map's layout as the app gives it, the test region's with its waypoints. */
const layoutOf = vi.fn((id: string) =>
  Promise.resolve({
    size: id === 'test_region' ? ([256, 256] as const) : ([48, 32] as const),
    image: '',
    waypoints: id === 'test_region' ? [TOWN, WEST, NORTH] : [],
  }),
);

describe('MapWindow, with a region on the chart', () => {
  beforeEach(() => {
    resetStores();
    act(() => {
      mapView.getState().showMap('town', [48, 32], '', []);
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
