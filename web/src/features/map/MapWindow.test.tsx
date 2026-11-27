import { readFileSync } from 'node:fs';
import path from 'node:path';

import { create } from '@bufbuild/protobuf';
import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import { mapLinks } from '@/gamedata/map-links';
import { maps } from '@/gamedata/maps';
import { npcs } from '@/gamedata/npcs';
import { clashes } from '@/lib/tags';
import { mobById } from '@/progression/mobs';
import { ERR } from '@/protocol/errors';
import {
  FieldBossGoneSchema,
  FieldBossSchema,
  MatchPhase,
  MatchStateSchema,
  PartyStateSchema,
  PartyViewSchema,
} from '@/protocol/messages';
import { character } from '@/state/character';
import { fieldBoss } from '@/state/field-boss';
import { mapView, NO_GROUND } from '@/state/map-view';
import { party } from '@/state/party';
import { pvp } from '@/state/pvp';
import { teleport } from '@/state/teleport';
import { toasts } from '@/state/toasts';
import { waypoints } from '@/state/waypoints';
import { refusal } from '@/testing/refusal';
import { resetStores } from '@/testing/reset-stores';
import { user } from '@/testing/user';

import { mapLabels, MapWindow } from './MapWindow';

const map = (id: string) => maps.find((m) => m.id === id)!;

/** A map's layout as the app gives it: its size, and no image, as jsdom draws none. */
const layoutOf = vi.fn((id: string) =>
  Promise.resolve({
    size: id === 'crossroads' ? ([48, 32] as const) : ([72, 40] as const),
    ground: NO_GROUND,
    waypoints: [],
  }),
);

function onMap(id: string, x: number, z: number) {
  const m = map(id);
  const size = id === 'crossroads' ? ([48, 32] as const) : ([72, 40] as const);
  act(() => {
    mapView.getState().showMap(m.id, size, NO_GROUND, []);
    mapView.getState().mark({ me: { x, z, yaw: 0 }, party: [], lock: null, boss: null });
  });
}

/** A socket no test here waits on: a region's map window asks it for the bosses, and it never answers. */
const idle = { request: vi.fn(() => new Promise<never>(() => undefined)), sequence: 0 } as never;

/** A socket whose teleport requests answer with answer, as pressed during input 42. */
function asking(answer: () => Promise<unknown>) {
  const request = vi.fn<(t: string, d: unknown, seq?: number) => Promise<unknown>>(answer);
  return { socket: { request, sequence: 42 } as never, request };
}

const texts = (root: Element, selector: string) =>
  [...root.querySelectorAll(selector)].map((t) => t.textContent);

describe('MapWindow', () => {
  beforeEach(() => {
    resetStores();
  });

  it('This map names each spawn by its mob and level, each portal by where it leads, and the player', () => {
    onMap('shroom_grove', 30, 10);
    render(<MapWindow socket={idle} layoutOf={layoutOf} onClose={vi.fn()} />);
    const svg = screen.getByRole('img', { name: 'Shroom Grove' });
    const slime = mobById('spore_slime')!;
    const snail = mobById('mossback_snail')!;
    expect(texts(svg, '.map-label-spawn')).toEqual(
      expect.arrayContaining([
        `${slime.name} Lv ${slime.level}`,
        `${snail.name} Lv ${snail.level}`,
      ]),
    );
    expect(texts(svg, '.map-label-portal').sort()).toEqual(['To Spore Hollow', 'To Willowmere']);
    expect(texts(svg, '.map-label-me')).toEqual(['You']);
  });

  it('This map names an NPC by its name: the crossroads’ guide', () => {
    onMap('crossroads', 20, 10);
    render(<MapWindow socket={idle} layoutOf={layoutOf} onClose={vi.fn()} />);
    expect(texts(screen.getByRole('img', { name: 'Crossroads' }), '.map-label-npc')).toEqual([
      'Crossroads Guide',
    ]);
  });

  it('names a region’s areas at their middles, a hunting area with its levels and a town by its name, and outlines each, a town tinted', () => {
    const region = map('test_region');
    const scale = 320 / 256;
    const named = mapLabels(region, null, scale, [256, 256], new Set(), [], [])
      .filter((l) => l.kind === 'area' || l.kind === 'town')
      .map((l) => [l.kind, l.text, l.x / scale]);
    expect(named).toEqual([
      ['town', 'Test Town', 122],
      ['area', 'West Field Lv 1–4', 58],
      ['area', 'South Field Lv 10–14', 122],
      ['area', 'North Field Lv 15–18', 122],
      ['area', 'East Woods Lv 19–20', 192],
    ]);
    const ell = {
      ...region,
      spawns: [],
      portals: [],
      areas: [
        {
          id: 'ell',
          name: 'Ell',
          kind: 'hunting',
          levels: [1, 2],
          area: [
            [0, 0],
            [30, 0],
            [30, 10],
            [10, 10],
            [10, 30],
            [0, 30],
          ],
          boss: null,
        },
      ],
    } as unknown as Parameters<typeof mapLabels>[0]; // the generated maps' types are their own literals
    const [label] = mapLabels(ell, null, 10, [30, 30], new Set(), [], []);
    expect(label!.x / 10).toBeCloseTo(11, 6); // the L's middle by area, not its corners' 13.3
    expect(30 - (label!.bottom - label!.h / 2) / 10).toBeCloseTo(11, 0);
    act(() => {
      mapView.getState().showMap('test_region', [256, 256], NO_GROUND, []);
      mapView.getState().mark({ me: { x: 124, z: 80, yaw: 0 }, party: [], lock: null, boss: null });
    });
    render(<MapWindow socket={idle} layoutOf={layoutOf} onClose={vi.fn()} />);
    const svg = screen.getByRole('img', { name: 'Test Region' });
    expect(texts(svg, '.map-label-town')).toEqual(['Test Town']);
    expect(texts(svg, '.map-label-area')).toHaveLength(4);
    const outlines = [...svg.querySelectorAll('polygon.map-area')];
    expect(
      outlines.map((o) => [o.getAttribute('data-area'), o.classList.contains('map-area-town')]),
    ).toEqual([
      ['town', true],
      ['west', false],
      ['south', false],
      ['north', false],
      ['woods', false],
    ]);
    expect(outlines[1]!.getAttribute('points')).toBe('24,232 92,232 92,24 24,24'); // y down from the north
  });

  it('This map names a battle’s points', () => {
    act(() => {
      mapView.getState().showMap('battlefield', [140, 90], NO_GROUND, []);
      mapView.getState().mark({ me: { x: 20, z: 45, yaw: 0 }, party: [], lock: null, boss: null });
    });
    render(<MapWindow socket={idle} layoutOf={layoutOf} onClose={vi.fn()} />);
    const svg = screen.getByRole('img', { name: 'Battlefield' });
    expect(texts(svg, '.map-label-point')).toEqual(['West Ruin', 'Middle Ruin', 'East Ruin']);
    expect(svg.querySelectorAll('.map-point')).toHaveLength(3);
    expect(svg.querySelectorAll('.map-base')).toHaveLength(2);
  });

  it('This map’s labels keep clear of each other and of the quest marks, on every map', () => {
    const overlap = (a: { x: number; bottom: number; w: number; h: number }, b: typeof a) =>
      Math.abs(a.x - b.x) * 2 < a.w + b.w && a.bottom > b.bottom - b.h && b.bottom > a.bottom - a.h;
    for (const m of maps) {
      const marked = new Set(npcs.npcs.filter((n) => n.map === m.id).map((n) => n.id));
      const labels = mapLabels(m, { x: 20, z: 20 }, 6, [72, 40], marked, [], []);
      for (const [i, a] of labels.entries()) {
        for (const b of labels.slice(i + 1)) {
          expect([m.id, a.text, b.text, overlap(a, b)]).toEqual([m.id, a.text, b.text, false]);
        }
      }
    }
  });

  // From the waypoints playtest: two labels touched their marks by a pixel, and a picked mark's ring ran into one.
  it('This map’s labels keep clear of a region’s waypoint marks as drawn, picked: the test region’s, the player by the town’s stone', () => {
    const stones = [
      { x: 124, z: 85 },
      { x: 60, z: 85 },
      { x: 118, z: 150 },
      { x: 190, z: 100 },
    ];
    const scale = 320 / 256;
    const labels = mapLabels(
      map('test_region'),
      { x: 124, z: 80 },
      scale,
      [256, 256],
      new Set(),
      [],
      stones,
    );
    for (const s of stones) {
      const mark = { x: s.x * scale, bottom: (256 - s.z) * scale + 12.5, w: 25, h: 25 }; // its pick ring, 11 px round, its stroke, and a clear pixel
      for (const l of labels) {
        expect([s, l.text, clashes(l, mark)]).toEqual([s, l.text, false]);
      }
    }
  });

  /** A map's size in metres and its pixels a metre at the window's scale (480 by 320 px at most). */
  const fitted = (id: string) => {
    const raw = readFileSync(path.resolve(process.cwd(), `public/maps/${id}.json`), 'utf8');
    const { size_x: sx, size_z: sz } = (
      JSON.parse(raw) as { geometry: { size_x: number; size_z: number } }
    ).geometry;
    return { sx, sz, scale: Math.min(480 / sx, 320 / sz) };
  };
  /** Each spawn label's mob and the middle of its text, in metres, at the window's scale. */
  const spawnLabels = (id: string) => {
    const m = map(id);
    const { sx, sz, scale } = fitted(id);
    return mapLabels(m, null, scale, [sx, sz], new Set(), [], [])
      .filter((l) => l.kind === 'spawn')
      .map((l) => {
        const mob = m.spawns.find((s) => l.text.startsWith(`${mobById(s.mob)!.name} Lv`))!.mob;
        return { mob, x: l.x / scale, z: sz - (l.bottom - l.h / 2) / scale };
      });
  };
  const on = (id: string, mob: string, x: number, z: number) =>
    map(id).spawns.some((s) => s.mob === mob && Math.hypot(s.x - x, s.z - z) <= s.radius);

  it('gives close spawn circles of one mob one label, and keeps a group apart its own: the sandstone cliffs', () => {
    const labels = spawnLabels('sandstone_cliffs');
    // six vulture circles: west and south 4.5 m apart share one; the chain of north, east, the spur and the gate is cut where one label would leave a circle over 8 m away, east and the spur sharing
    expect(labels.filter((l) => l.mob === 'dust_vulture')).toHaveLength(4);
    // four slime circles: the mesa's and the slope's overlap; the west one and the east one stand apart
    expect(labels.filter((l) => l.mob === 'sun_slime')).toHaveLength(3);
  });

  /** A map's labels with its boss beaten, the boss's own, and where its crown's middle is, in pixels down. */
  const bossLabels = (id: string) => {
    const m = map(id);
    const spot = m.boss!;
    const { sx, sz, scale } = fitted(id);
    const labels = mapLabels(
      m,
      null,
      scale,
      [sx, sz],
      new Set(),
      [{ ...spot, down: { type: spot.mob, backAt: 0 } }],
      [],
    );
    return { labels, boss: labels.find((l) => l.kind === 'boss')!, crown: (sz - spot.z) * scale };
  };

  it('puts a beaten boss’s label under its crown when that has more room than tags keep: the frost trail’s', () => {
    const room = 6; // the boss label's, past the tags' 2 px
    const { labels, boss, crown } = bossLabels('frost_trail');
    const near = labels.filter(
      (l) =>
        l !== boss &&
        Math.abs(l.x - boss.x) * 2 < l.w + boss.w &&
        l.bottom + room > boss.bottom - boss.h &&
        l.bottom - l.h < boss.bottom + room,
    );
    expect([boss.bottom - boss.h >= crown, near.map((l) => l.text)]).toEqual([true, []]);
  });

  it('keeps a beaten boss’s label off its crown and clear of every label, on every map with a boss', () => {
    const DOT = 3; // the crown reaches 1.8 dots over its middle and 1.2 under
    const bad = maps
      .filter((m) => m.boss)
      .flatMap(({ id }) => {
        const { labels, boss, crown } = bossLabels(id);
        const off = boss.bottom - boss.h >= crown + 1.2 * DOT || boss.bottom <= crown - 1.8 * DOT;
        return [
          ...(off ? [] : [`${id}: on its crown`]),
          ...labels.filter((l) => l !== boss && clashes(l, boss)).map((l) => `${id}: ${l.text}`),
        ];
      });
    expect(bad).toEqual([]);
  });

  it('puts every spawn label on one of its own mob’s circles, on every map', () => {
    const off = maps
      .filter((m) => m.spawns.length > 0 && m.kind !== 'region') // a region's view pans and zooms its tiles (large-regions.md section 10)
      .flatMap((m) =>
        spawnLabels(m.id)
          .filter((l) => !on(m.id, l.mob, l.x, l.z))
          .map((l) => `${m.id} ${l.mob} (${l.x.toFixed(1)}, ${l.z.toFixed(1)})`),
      );
    expect(off).toEqual([]);
  });

  /** How far each circle of mob on map id is from the nearest label of its own mob: 0 under one, or from its edge. */
  const fromLabels = (id: string, mob: string) => {
    const own = spawnLabels(id).filter((l) => l.mob === mob);
    return map(id)
      .spawns.filter((s) => s.mob === mob)
      .map((s) => ({
        at: [s.x, s.z],
        far: Math.min(...own.map((l) => Math.max(0, Math.hypot(l.x - s.x, l.z - s.z) - s.radius))),
      }));
  };

  it('gives every spawn circle a label of its own mob on it or within 8 m of its edge, on every map', () => {
    const far = maps
      .filter((m) => m.spawns.length > 0 && m.kind !== 'region') // a region's view pans and zooms its tiles (large-regions.md section 10)
      .flatMap((m) =>
        [...new Set(m.spawns.map((s) => s.mob))].flatMap((mob) =>
          fromLabels(m.id, mob)
            .filter((c) => c.far > 8)
            .map((c) => `${m.id} ${mob} (${c.at.join(', ')}) ${c.far.toFixed(1)} m`),
        ),
      );
    expect(far).toEqual([]);
  });

  // A chain of five moth circles had one label, on a west circle, 22.5 m from the east one; the keep's west bats sat 12–17 m from theirs.
  it('cuts a chain so its far circles get their own label: spore hollow’s east moths, the keep’s west bats', () => {
    const at = (id: string, mob: string, x: number, z: number) =>
      fromLabels(id, mob).find((c) => c.at[0] === x && c.at[1] === z)!.far;
    expect(
      [
        at('spore_hollow', 'dust_moth', 56.5, 17),
        at('fallen_keep', 'shade_bat', 20, 12),
        at('fallen_keep', 'shade_bat', 20, 25),
      ].map((far) => far <= 8),
    ).toEqual([true, true, true]);
  });

  // A chain's middle can fall on another mob's circle: spore hollow's rats' on the east moths', the keep's west bats' on the gloom slimes'.
  it('keeps a chained group’s label off other mobs’ circles: spore hollow and the fallen keep', () => {
    for (const id of ['spore_hollow', 'fallen_keep']) {
      const onOthers = spawnLabels(id).filter((l) =>
        map(id).spawns.some((s) => s.mob !== l.mob && Math.hypot(s.x - l.x, s.z - l.z) <= s.radius),
      );
      expect([id, onOthers]).toEqual([id, []]);
    }
  });

  it('lifts the player’s label above an NPC’s name when the player stands at the NPC', () => {
    const guide = npcs.npcs.find((n) => n.id === 'crossroads_guide')!;
    const labels = mapLabels(
      map('crossroads'),
      { x: guide.x, z: guide.z },
      6,
      [48, 32],
      new Set(),
      [],
      [],
    );
    const you = labels.find((l) => l.kind === 'me')!;
    const name = labels.find((l) => l.text === guide.name)!;
    expect(you.bottom).toBeLessThanOrEqual(name.bottom - name.h - 2);
  });

  it('World draws every map with a place, a match’s excepted, a line for each pair of maps a portal joins, and a house for a town with no levels', async () => {
    onMap('shroom_grove', 30, 10);
    render(<MapWindow socket={idle} layoutOf={layoutOf} onClose={vi.fn()} />);
    await user.click(screen.getByRole('tab', { name: 'World' }));
    const chart = screen.getByRole('group', { name: 'World' });
    expect(chart.querySelectorAll('.map-node')).toHaveLength(
      maps.filter((m) => m.place !== null).length,
    );
    expect(chart.querySelector('.map-node[data-map="ring"]')).toBeNull(); // only a match reaches it
    expect(map('test_region').kind).toBe('region');
    expect(chart.querySelector('.map-node[data-map="test_region"]')).toBeNull(); // no place on the chart: only a teleport to its id reaches it
    expect(map('greyfang_cave').place).toBeNull(); // a place below is reached through its region
    expect(chart.querySelector('.map-node[data-map="greyfang_cave"]')).toBeNull();
    expect(
      [...chart.querySelectorAll('.map-link')].map((l) => l.getAttribute('data-link')).sort(),
    ).toEqual(mapLinks.map(([a, b]) => `${a}-${b}`).sort());
    const grove = chart.querySelector('.map-node[data-map="shroom_grove"]')!;
    const [fx, fy] = map('shroom_grove').place!;
    expect([
      grove.querySelector('.map-hunt')!.getAttribute('cx'),
      grove.querySelector('.map-hunt')!.getAttribute('cy'),
    ]).toEqual([String(fx), String(fy)]);
    expect(texts(grove, '.map-node-levels')).toEqual(['Lv 15–16']);
    const region = chart.querySelector('.map-node[data-map="willowmere"]')!; // a region is drawn as a hunting map, its kind under its name
    const [wx, wy] = map('willowmere').place!;
    expect([
      region.querySelector('.map-hunt')!.getAttribute('cx'),
      region.querySelector('.map-hunt')!.getAttribute('cy'),
      texts(region, '.map-node-levels'),
    ]).toEqual([String(wx), String(wy), ['Region']]);
    const town = chart.querySelector('.map-node[data-map="crossroads"]')!;
    expect([town.querySelector('.map-town'), town.querySelector('.map-node-levels')]).toEqual([
      expect.anything(),
      null,
    ]);
  });

  it('in a match shows This map alone: the world chart has no match’s map', async () => {
    render(<MapWindow socket={idle} layoutOf={layoutOf} onClose={vi.fn()} />);
    await user.click(screen.getByRole('tab', { name: 'World' }));
    act(() =>
      pvp
        .getState()
        .applyMatchState(
          create(MatchStateSchema, { match: 'mt-1', mode: 'duel', phase: MatchPhase.PREP }),
          0,
        ),
    );
    expect(
      screen.getAllByRole('tab').map((t) => [t.textContent, t.getAttribute('aria-selected')]),
    ).toEqual([['This map', 'true']]);
    act(() => {
      pvp.getState().leftMatch();
    });
    expect(screen.getByRole('tab', { name: 'World' })).toHaveAttribute('aria-selected', 'true'); // the tab chosen before
  });

  it('World rings the player’s map, “You are here”, and names each online party member under their map', async () => {
    onMap('shroom_grove', 30, 10);
    party.getState().applyPartyState(
      create(PartyStateSchema, {
        party: create(PartyViewSchema, {
          leader: 'c-robin',
          members: [
            { characterId: 'c-robin', name: 'Robin', mapId: 'spore_hollow', online: true },
            { characterId: 'c-jade', name: 'Jade', mapId: 'spore_hollow', online: false },
            { characterId: 'c-kai', name: 'Kai', mapId: 'crossroads', online: true },
          ],
        }),
      }),
    );
    render(<MapWindow socket={idle} layoutOf={layoutOf} onClose={vi.fn()} />);
    await user.click(screen.getByRole('tab', { name: 'World' }));
    const chart = screen.getByRole('group', { name: 'World' });
    const node = (id: string) => chart.querySelector(`.map-node[data-map="${id}"]`)!;
    expect([
      node('shroom_grove').querySelector('.map-here-ring'),
      texts(node('shroom_grove'), '.map-node-you'),
    ]).toEqual([expect.anything(), ['You are here']]);
    expect(chart.querySelectorAll('.map-here-ring')).toHaveLength(1);
    expect([
      texts(node('spore_hollow'), '.map-node-party'),
      texts(node('crossroads'), '.map-node-party'),
    ]).toEqual([['Robin'], ['Kai']]);
  });

  it('pointing at a map on World shows its name, levels and mobs with their levels', async () => {
    onMap('crossroads', 20, 10);
    render(<MapWindow socket={idle} layoutOf={layoutOf} onClose={vi.fn()} />);
    await user.click(screen.getByRole('tab', { name: 'World' }));
    const chart = screen.getByRole('group', { name: 'World' });
    fireEvent.pointerEnter(chart.querySelector('.map-node[data-map="shroom_grove"]')!);
    const card = within(screen.getByRole('tooltip'));
    const slime = mobById('spore_slime')!;
    expect(card.getByText('Shroom Grove')).toBeInTheDocument();
    expect(card.getByText('Lv 15–16')).toBeInTheDocument();
    expect(card.getByText(`${slime.name} Lv ${slime.level}`)).toBeInTheDocument();
    fireEvent.pointerLeave(chart.querySelector('.map-node[data-map="shroom_grove"]')!);
    expect(screen.queryByRole('tooltip')).not.toBeInTheDocument();
  });

  it('clicking a map on World shows its layout: its spawns, portals and NPCs named, and Back to the chart', async () => {
    onMap('crossroads', 20, 10);
    render(<MapWindow socket={idle} layoutOf={layoutOf} onClose={vi.fn()} />);
    await user.click(screen.getByRole('tab', { name: 'World' }));
    await user.click(screen.getByRole('button', { name: 'Shroom Grove' }));
    const svg = await screen.findByRole('img', { name: 'Shroom Grove' });
    expect(layoutOf).toHaveBeenCalledWith('shroom_grove');
    expect(screen.getByText('Lv 15–16')).toBeInTheDocument();
    const slime = mobById('spore_slime')!;
    expect(texts(svg, '.map-label-spawn')).toContain(`${slime.name} Lv ${slime.level}`);
    expect(texts(svg, '.map-label-portal').sort()).toEqual(['To Spore Hollow', 'To Willowmere']);
    expect(
      [...svg.querySelectorAll('.map-spawn')].map((c) => [
        c.getAttribute('data-mob'),
        c.getAttribute('cx'),
      ]),
    ).toEqual(map('shroom_grove').spawns.map((sp) => [sp.mob, String(sp.x)]));
    expect(svg.getAttribute('viewBox')).toBe('0 0 72 40');
    await user.click(screen.getByRole('button', { name: 'Back' }));
    expect(screen.getByRole('group', { name: 'World' })).toBeInTheDocument();
  });

  it('a picked map shows the player and the live marks only when it’s the player’s own', async () => {
    onMap('shroom_grove', 30, 10);
    act(() => {
      mapView.getState().mark({
        me: { x: 30, z: 10, yaw: 0 },
        party: [{ id: 'c-kai', name: 'Kai', x: 20, z: 12 }],
        lock: { x: 34, z: 10 },
        boss: { x: 50, z: 20 },
      });
    });
    layoutOf.mockClear();
    render(<MapWindow socket={idle} layoutOf={layoutOf} onClose={vi.fn()} />);
    await user.click(screen.getByRole('tab', { name: 'World' }));
    const live = '.map-me, .map-party, .map-lock, .map-boss, .map-label-me';
    await user.click(screen.getByRole('button', { name: 'Shroom Grove' }));
    const own = screen.getByRole('img', { name: 'Shroom Grove' });
    expect(own.querySelectorAll(live)).toHaveLength(5);
    expect(layoutOf).not.toHaveBeenCalled();
    await user.click(screen.getByRole('button', { name: 'Back' }));
    await user.click(screen.getByRole('button', { name: 'Spore Hollow' }));
    const other = await screen.findByRole('img', { name: 'Spore Hollow' });
    expect(other.querySelectorAll(live)).toHaveLength(0);
  });

  it('a beaten boss shows grey at its spot on its own map, the time until it’s back counting down, till it’s back', () => {
    vi.useFakeTimers();
    try {
      onMap('shroom_grove', 30, 10);
      act(() => {
        fieldBoss
          .getState()
          .applyGone(
            create(FieldBossGoneSchema, { mobId: 9, type: 'king_slime', backInMs: 192000n }),
          );
      });
      render(<MapWindow socket={idle} layoutOf={layoutOf} onClose={vi.fn()} />);
      const here = screen.getByRole('img', { name: 'Shroom Grove' });
      const [spotX, spotZ] = [map('shroom_grove').boss!.x, map('shroom_grove').boss!.z];
      expect(here.querySelector('.map-boss-down')?.getAttribute('transform')).toMatch(
        new RegExp(`^translate\\(${spotX} ${40 - spotZ}\\)`),
      );
      expect(texts(here, '.map-label-boss')).toEqual(['King Slime: back in 3:12']);
      act(() => {
        vi.advanceTimersByTime(61000);
      });
      expect(texts(here, '.map-label-boss')).toEqual(['King Slime: back in 2:11']);
      act(() => {
        fieldBoss
          .getState()
          .applyFieldBoss(create(FieldBossSchema, { mobId: 10, type: 'king_slime' }));
      });
      expect([here.querySelector('.map-boss-down'), texts(here, '.map-label-boss')]).toEqual([
        null,
        [],
      ]);
    } finally {
      vi.useRealTimers();
    }
  });

  it('a picked map shows no beaten boss of the player’s own map, at its own boss’s spot or anywhere', async () => {
    onMap('shroom_grove', 30, 10);
    act(() => {
      fieldBoss
        .getState()
        .applyGone(create(FieldBossGoneSchema, { mobId: 9, type: 'king_slime', backInMs: 60000n }));
    });
    render(<MapWindow socket={idle} layoutOf={layoutOf} onClose={vi.fn()} />);
    await user.click(screen.getByRole('tab', { name: 'World' }));
    await user.click(screen.getByRole('button', { name: 'Spore Hollow' }));
    const other = await screen.findByRole('img', { name: 'Spore Hollow' });
    expect([
      other.querySelectorAll('.map-boss-down').length,
      texts(other, '.map-label-boss'),
    ]).toEqual([0, []]);
  });

  it('a picked map names the party members online on it under its header', async () => {
    onMap('shroom_grove', 30, 10);
    party.getState().applyPartyState(
      create(PartyStateSchema, {
        party: create(PartyViewSchema, {
          leader: 'c-robin',
          members: [
            { characterId: 'c-robin', name: 'Robin', mapId: 'spore_hollow', online: true },
            { characterId: 'c-jade', name: 'Jade', mapId: 'spore_hollow', online: false },
            { characterId: 'c-kai', name: 'Kai', mapId: 'crossroads', online: true },
          ],
        }),
      }),
    );
    render(<MapWindow socket={idle} layoutOf={layoutOf} onClose={vi.fn()} />);
    await user.click(screen.getByRole('tab', { name: 'World' }));
    fireEvent.keyDown(screen.getByRole('button', { name: 'Spore Hollow' }), { key: 'Enter' });
    const names = await screen.findByRole('list', { name: 'Party in Spore Hollow' });
    expect(
      within(names)
        .getAllByRole('listitem')
        .map((li) => li.textContent),
    ).toEqual(['Robin']);
  });

  it('a region’s party names say each member’s channel, in the chart’s node and a picked map’s list; a land map’s say none', async () => {
    onMap('shroom_grove', 30, 10);
    party.getState().applyPartyState(
      create(PartyStateSchema, {
        party: create(PartyViewSchema, {
          leader: 'c-bo',
          members: [
            { characterId: 'c-bo', name: 'Bo', mapId: 'willowmere', channel: 2, online: true },
            { characterId: 'c-jade', name: 'Jade', mapId: 'willowmere', channel: 1, online: false },
            { characterId: 'c-kai', name: 'Kai', mapId: 'crossroads', online: true },
          ],
        }),
      }),
    );
    render(<MapWindow socket={idle} layoutOf={layoutOf} onClose={vi.fn()} />);
    await user.click(screen.getByRole('tab', { name: 'World' }));
    const chart = screen.getByRole('group', { name: 'World' });
    const node = (id: string) => chart.querySelector(`.map-node[data-map="${id}"]`)!;
    expect([
      texts(node('willowmere'), '.map-node-party'),
      texts(node('crossroads'), '.map-node-party'),
    ]).toEqual([['Bo · Ch. 2'], ['Kai']]);
    fireEvent.keyDown(screen.getByRole('button', { name: 'Willowmere' }), { key: 'Enter' });
    const names = await screen.findByRole('list', { name: 'Party in Willowmere' });
    expect(
      within(names)
        .getAllByRole('listitem')
        .map((li) => li.textContent),
    ).toEqual(['Bo · Ch. 2']);
  });

  it('a picked map whose layout won’t load says so', async () => {
    onMap('crossroads', 20, 10);
    render(
      <MapWindow
        socket={idle}
        layoutOf={() => Promise.reject(new Error('map spore_hollow: 500'))}
        onClose={vi.fn()}
      />,
    );
    await user.click(screen.getByRole('tab', { name: 'World' }));
    await user.click(screen.getByRole('button', { name: 'Spore Hollow' }));
    expect(await screen.findByText('The map didn’t load.')).toBeInTheDocument();
  });

  it('a picked map offers Teleport here, but not on the player’s own map, nor while they’re dead', async () => {
    onMap('crossroads', 20, 10);
    render(<MapWindow socket={idle} layoutOf={layoutOf} onClose={vi.fn()} />);
    await user.click(screen.getByRole('tab', { name: 'World' }));
    await user.click(screen.getByRole('button', { name: 'Shroom Grove' }));
    expect(screen.getByRole('button', { name: 'Teleport here' })).toBeEnabled();
    await user.click(screen.getByRole('button', { name: 'Back' }));
    await user.click(screen.getByRole('button', { name: 'Crossroads' }));
    expect(screen.queryByRole('button', { name: 'Teleport here' })).not.toBeInTheDocument();
    await user.click(screen.getByRole('button', { name: 'Back' }));
    act(() => {
      character.getState().setDead(true);
    });
    await user.click(screen.getByRole('button', { name: 'Shroom Grove' }));
    expect(screen.queryByRole('button', { name: 'Teleport here' })).not.toBeInTheDocument();
  });

  it('Teleport here asks the server with the latest input’s seq; its answer closes the window and starts the channel', async () => {
    onMap('crossroads', 20, 10);
    const { socket, request } = asking(() => Promise.resolve({ ok: true, code: 0 }));
    const onClose = vi.fn();
    render(<MapWindow socket={socket} layoutOf={layoutOf} onClose={onClose} />);
    await user.click(screen.getByRole('tab', { name: 'World' }));
    await user.click(screen.getByRole('button', { name: 'Shroom Grove' }));
    await user.click(screen.getByRole('button', { name: 'Teleport here' }));
    expect(request).toHaveBeenCalledWith('teleport', { mapId: 'shroom_grove' }, 42);
    await waitFor(() => {
      expect(onClose).toHaveBeenCalledOnce();
    });
    expect(teleport.getState().to).toEqual({ map: 'shroom_grove' });
  });

  describe('in a region’s channel', () => {
    const LIST = {
      cap: 150,
      channels: [
        { channel: 1, players: 150 },
        { channel: 2, players: 75 },
        { channel: 3, players: 74 },
      ],
    };
    /** A socket answering the channel list with LIST and a switch with switched, as pressed during input 42. */
    const region = (
      switched: () => Promise<unknown> = () => Promise.resolve({ ok: true, code: 0 }),
    ) => {
      const request = vi.fn<(t: string, d: unknown, seq?: number) => Promise<unknown>>((t) =>
        t === 'channels' ? Promise.resolve({ ok: true, code: 0, channels: LIST }) : switched(),
      );
      return { socket: { request, sequence: 42 } as never, request };
    };
    const inRegion = () => {
      onMap('test_region', 124, 80);
      act(() => mapView.getState().inChannel(2));
    };
    const rows = async () => {
      const list = await screen.findByRole('region', { name: 'Channels' });
      await within(list).findAllByRole('listitem');
      return within(list)
        .getAllByRole('listitem')
        .map((li) => [
          li.querySelector('span')!.textContent,
          li.querySelector('.map-channel-load')!.textContent,
          within(li).queryByRole('button', { name: 'Switch' }) !== null,
        ]);
    };

    it('This map names the player’s channel and lists each open one by how full it is: quiet under half the cap, busy below it, full at it; a switch on each but theirs and a full one', async () => {
      inRegion();
      const { socket } = region();
      render(<MapWindow socket={socket} layoutOf={layoutOf} onClose={vi.fn()} />);
      expect(await screen.findByText('Channel 2', { selector: 'strong' })).toBeInTheDocument();
      expect(await rows()).toEqual([
        ['Channel 1', 'full', false],
        ['Channel 2', 'busy', false],
        ['Channel 3', 'quiet', true],
      ]);
    });

    it('a switch asks the server with the latest input’s seq; its answer closes the window and starts the channel, the bar naming it', async () => {
      inRegion();
      const { socket, request } = region();
      const onClose = vi.fn();
      render(<MapWindow socket={socket} layoutOf={layoutOf} onClose={onClose} />);
      await rows();
      await user.click(screen.getByRole('button', { name: 'Switch' }));
      expect(request).toHaveBeenCalledWith('switchChannel', { channel: 3 }, 42);
      await waitFor(() => {
        expect(onClose).toHaveBeenCalledOnce();
      });
      expect(teleport.getState().to).toEqual({ channel: 3 });
    });

    it('a refused switch says why as a notice and leaves the window open', async () => {
      inRegion();
      const { socket } = region(() => Promise.reject(refusal(ERR.CHANNEL_FULL)));
      const onClose = vi.fn();
      render(<MapWindow socket={socket} layoutOf={layoutOf} onClose={onClose} />);
      await rows();
      await user.click(screen.getByRole('button', { name: 'Switch' }));
      await waitFor(() => {
        expect(toasts.getState().toasts.map((t) => t.text)).toEqual(['That channel is full.']);
      });
      expect(screen.getByRole('button', { name: 'Switch' })).toBeEnabled();
      expect([onClose.mock.calls.length, teleport.getState().to]).toEqual([0, null]);
    });

    it('asks for the list as it shows and every 5 s while it does, and no more once it goes', async () => {
      vi.useFakeTimers({ toFake: ['setInterval', 'clearInterval'] });
      try {
        inRegion();
        const { socket, request } = region();
        const { unmount } = render(
          <MapWindow socket={socket} layoutOf={layoutOf} onClose={vi.fn()} />,
        );
        const asked = () => request.mock.calls.filter(([t]) => t === 'channels').length;
        await rows();
        expect(asked()).toBe(1);
        act(() => {
          vi.advanceTimersByTime(4999);
        });
        expect(asked()).toBe(1);
        act(() => {
          vi.advanceTimersByTime(1);
        });
        act(() => {
          vi.advanceTimersByTime(5000);
        });
        expect(asked()).toBe(3);
        unmount();
        act(() => {
          vi.advanceTimersByTime(20000);
        });
        expect(asked()).toBe(3);
      } finally {
        vi.useRealTimers();
      }
    });

    it('offers no switch to the dead, and no channels off a region', async () => {
      inRegion();
      act(() => character.getState().setDead(true));
      const { socket, request } = region();
      const { unmount } = render(
        <MapWindow socket={socket} layoutOf={layoutOf} onClose={vi.fn()} />,
      );
      expect((await rows()).map(([, , s]) => s)).toEqual([false, false, false]);
      unmount();
      request.mockClear();
      onMap('shroom_grove', 30, 10);
      act(() => mapView.getState().inChannel(0));
      render(<MapWindow socket={socket} layoutOf={layoutOf} onClose={vi.fn()} />);
      expect(screen.queryByRole('region', { name: 'Channels' })).not.toBeInTheDocument();
      expect(request).not.toHaveBeenCalled();
    });
  });

  describe('in a region, its waypoints', () => {
    const TOWN = { id: 'town', name: 'Test Town', x: 124, z: 85, y: 0, main: true };
    const WEST = { id: 'west', name: 'West Field', x: 60, z: 85, y: 0, main: false };
    const NORTH = { id: 'north', name: 'North Field', x: 118, z: 150, y: 0, main: false };

    /** The player in the test region by its town's stone, the west field's found. */
    function inRegion() {
      act(() => {
        mapView.getState().showMap('test_region', [256, 256], NO_GROUND, [TOWN, WEST, NORTH]);
        mapView
          .getState()
          .mark({ me: { x: 124, z: 80, yaw: 0 }, party: [], lock: null, boss: null });
        waypoints.getState().find('west');
      });
    }

    /** The tiles' levels and count drawn for a square region side metres across, in the window's 480 × 320 px. */
    const drawn = (side: number) => {
      act(() => {
        mapView
          .getState()
          .showMap('test_region', [side, side], { kind: 'tiles', folder: 'bake/tiles' }, []); // the test region as one side across would be
        mapView
          .getState()
          .mark({ me: { x: 124, z: 80, yaw: 0 }, party: [], lock: null, boss: null });
      });
      const { unmount } = render(<MapWindow socket={idle} layoutOf={layoutOf} onClose={vi.fn()} />);
      const hrefs = [...document.querySelectorAll('image.map-tile')].map((t) =>
        t.getAttribute('href'),
      );
      unmount();
      return [new Set(hrefs.map((h) => h!.split('/')[2])), hrefs.length, hrefs.at(-1)];
    };

    it('This map draws a region’s every tile at 1 m a pixel, or at 4 m where a 4 m pixel spans at most 2 device pixels', () => {
      expect(drawn(256)).toEqual([new Set(['1']), 4, 'bake/tiles/1/1_1.png']); // 1.25 px a metre
      expect(drawn(512)).toEqual([new Set(['1']), 16, 'bake/tiles/1/3_3.png']); // 0.63: a 4 m pixel would span 2.5
      expect(drawn(768)).toEqual([new Set(['4']), 36, 'bake/tiles/4/5_5.png']); // 0.42: 1.67
      expect(drawn(1024)).toEqual([new Set(['4']), 64, 'bake/tiles/4/7_7.png']); // 0.31: 1.25
    });

    it('This map draws the finer tiles on a hi-DPI screen, judging a tile’s pixel by the device pixels it spans', () => {
      const dpr = Object.getOwnPropertyDescriptor(window, 'devicePixelRatio');
      Object.defineProperty(window, 'devicePixelRatio', { value: 2, configurable: true });
      try {
        expect(drawn(768)[0]).toEqual(new Set(['1'])); // 0.83 device px a metre: a 4 m pixel would span 3.33
        expect(drawn(1024)[0]).toEqual(new Set(['1'])); // 0.63: 2.5
        expect(drawn(2048)).toEqual([new Set(['4']), 256, 'bake/tiles/4/15_15.png']); // 0.31: 1.25
      } finally {
        Object.defineProperty(window, 'devicePixelRatio', dpr!);
      }
    });

    // From the waypoints playtest: at a stone's arrival, "You" sat over its mark and took the click, so the unstuck teleport needed the keyboard.
    it('a click on the player, a party member, the lock or a boss over a mark goes through to the mark', async () => {
      inRegion();
      const town = { x: TOWN.x, z: TOWN.z };
      act(() => {
        mapView.getState().mark({
          me: { x: 124, z: 87, yaw: 0 }, // the town stone's arrival
          party: [{ id: 'p-2', name: 'Bree', ...town }],
          lock: town,
          boss: town,
        });
      });
      render(<MapWindow socket={idle} layoutOf={layoutOf} onClose={vi.fn()} />);
      const svg = screen.getByRole('img', { name: 'Test Region' });
      const live = [...svg.querySelectorAll('.map-me, .map-party, .map-lock circle, .map-boss')];
      expect(live).toHaveLength(5);
      for (const marker of live) {
        await expect(user.click(marker)).rejects.toThrow(/pointer-events: none/);
      }
      await user.click(within(svg).getByRole('button', { name: 'Test Town' }));
      expect(screen.getByRole('region', { name: 'Waypoint' })).toHaveTextContent('Test Town');
    });

    it('This map marks each waypoint, lit once found and grey till then, and names a picked one: found, a main town’s, or not found yet, with Teleport here on the first two', async () => {
      inRegion();
      render(<MapWindow socket={idle} layoutOf={layoutOf} onClose={vi.fn()} />);
      const svg = screen.getByRole('img', { name: 'Test Region' });
      const ids = (selector: string) =>
        [...svg.querySelectorAll(selector)].map((g) => g.getAttribute('data-waypoint'));
      expect([ids('.map-waypoint'), ids('.map-waypoint-found')]).toEqual([
        ['town', 'west', 'north'],
        ['west'],
      ]);
      expect(
        [...svg.querySelectorAll('.map-waypoint polygon')].map((d) => d.getAttribute('fill')),
      ).toEqual(['#8a8a8a', '#00dce6', '#8a8a8a']); // grey till found, then the crystal's cyan
      for (const [name, says, open] of [
        ['West Field', 'Found', true],
        ['Test Town', 'Main town, always open', true],
        ['North Field', 'Not found yet', false],
      ] as const) {
        await user.click(within(svg).getByRole('button', { name }));
        const pick = screen.getByRole('region', { name: 'Waypoint' });
        expect(pick).toHaveTextContent(`${name}${says}`);
        expect(within(pick).queryByRole('button', { name: 'Teleport here' }) !== null).toBe(open);
      }
      act(() => {
        waypoints.getState().find('north');
      });
      expect(ids('.map-waypoint-found')).toEqual(['west', 'north']);
      expect(screen.getByRole('region', { name: 'Waypoint' })).toHaveTextContent(
        'North FieldFound',
      );
    });

    it('Teleport here on a waypoint asks the server for it with the latest input’s seq; its answer closes the window and starts the channel, named for the waypoint', async () => {
      inRegion();
      const { socket, request } = asking(() => Promise.resolve({ ok: true, code: 0 }));
      const onClose = vi.fn();
      render(<MapWindow socket={socket} layoutOf={layoutOf} onClose={onClose} />);
      await user.click(screen.getByRole('button', { name: 'West Field' }));
      await user.click(screen.getByRole('button', { name: 'Teleport here' }));
      expect(request).toHaveBeenCalledWith('teleport', { waypoint: 'west' }, 42);
      await waitFor(() => {
        expect(onClose).toHaveBeenCalledOnce();
      });
      expect(teleport.getState().to).toEqual({ waypoint: 'west', name: 'West Field' });
    });

    it('a waypoint offers no Teleport here while the player is dead, and a refused one says why and leaves the window open', async () => {
      inRegion();
      const { socket } = asking(() => Promise.reject(refusal(ERR.TELEPORT_NOT_FOUND)));
      const onClose = vi.fn();
      render(<MapWindow socket={socket} layoutOf={layoutOf} onClose={onClose} />);
      await user.click(screen.getByRole('button', { name: 'West Field' }));
      await user.click(screen.getByRole('button', { name: 'Teleport here' }));
      await waitFor(() => {
        expect(toasts.getState().toasts.map((t) => t.text)).toEqual([
          'You haven’t found that waypoint yet.',
        ]);
      });
      expect([onClose.mock.calls.length, teleport.getState().to]).toEqual([0, null]);
      act(() => {
        character.getState().setDead(true);
      });
      expect(screen.queryByRole('button', { name: 'Teleport here' })).not.toBeInTheDocument();
    });

    it('a pick goes with its map', async () => {
      inRegion();
      render(<MapWindow socket={idle} layoutOf={layoutOf} onClose={vi.fn()} />);
      await user.click(screen.getByRole('button', { name: 'West Field' }));
      onMap('shroom_grove', 30, 10);
      expect(screen.queryByRole('region', { name: 'Waypoint' })).not.toBeInTheDocument();
      expect(document.querySelector('.map-waypoint')).toBeNull();
    });
  });

  describe('This region crowns each area’s boss', () => {
    type Answer = { area: string; up: boolean; backInMs: bigint }[];
    /** A socket answering each bosses query with bosses; nothing else it's asked answers. */
    const answering = (bosses: Answer) =>
      vi.fn((t: string) =>
        t === 'bosses'
          ? Promise.resolve({ ok: true, code: 0, bosses: { bosses } })
          : new Promise<never>(() => undefined),
      );
    /** This region open, the player in area. */
    function inArea(area: string, request: ReturnType<typeof answering>) {
      act(() => {
        mapView.getState().showMap('test_region', [256, 256], NO_GROUND, []);
        mapView
          .getState()
          .mark({ me: { x: 124, z: 80, yaw: 0 }, party: [], lock: null, boss: null });
        mapView.getState().inArea(area);
      });
      return render(
        <MapWindow
          socket={{ request, sequence: 0 } as never}
          layoutOf={layoutOf}
          onClose={vi.fn()}
        />,
      );
    }
    /** Each crown at a spot: its mob, up or beaten, and where it's drawn. */
    const crowns = () =>
      [...document.querySelectorAll('.map-boss-spot, .map-boss-down')].map((c) => [
        c.getAttribute('data-mob'),
        c.classList.contains('map-boss-down') ? 'beaten' : 'up',
        /translate\(([^)]*)\)/.exec(c.getAttribute('transform')!)![1],
      ]);
    const beaten = (mob: string, ms: bigint) =>
      create(FieldBossGoneSchema, { mobId: 9, type: mob, backInMs: ms });

    it('crowns each area’s boss at its spot as the query last said: red while up, grey while beaten with its time back counting down; none before it answers', async () => {
      vi.useFakeTimers();
      try {
        inArea(
          'town',
          answering([
            { area: 'west', up: false, backInMs: 90000n },
            { area: 'south', up: true, backInMs: 0n },
            { area: 'north', up: true, backInMs: 0n },
            { area: 'woods', up: false, backInMs: 5000n },
          ]),
        );
        expect(crowns()).toEqual([]);
        await act(() => Promise.resolve());
        expect(crowns()).toEqual([
          ['king_slime', 'beaten', '46 146'],
          ['vampire_bat', 'up', '125 208'],
          ['spore_mother', 'up', '120 81'],
          ['moth_empress', 'beaten', '200 46'],
        ]);
        const svg = screen.getByRole('img', { name: 'Test Region' });
        expect(texts(svg, '.map-label-boss')).toEqual([
          'King Slime: back in 1:30',
          'Moth Empress: back in 0:05',
        ]);
        act(() => {
          vi.advanceTimersByTime(3000);
        });
        expect(texts(svg, '.map-label-boss')).toEqual([
          'King Slime: back in 1:27',
          'Moth Empress: back in 0:02',
        ]);
      } finally {
        vi.useRealTimers();
      }
    });

    it('takes the player’s own area’s boss from its frames, as a map’s, over what the query said: beaten, grey; up, only its live crown', async () => {
      act(() => {
        fieldBoss.getState().applyGone(beaten('king_slime', 60000n));
      });
      const answer: Answer = [
        { area: 'west', up: true, backInMs: 0n },
        { area: 'south', up: false, backInMs: 30000n },
      ];
      const { unmount } = inArea('west', answering(answer));
      await act(() => Promise.resolve());
      expect(crowns()).toEqual([
        ['king_slime', 'beaten', '46 146'],
        ['vampire_bat', 'beaten', '125 208'],
      ]); // the north and the woods unsaid: none
      unmount();
      act(() => {
        fieldBoss
          .getState()
          .applySnapshot(create(FieldBossSchema, { mobId: 10, type: 'king_slime' }), undefined);
      });
      const { unmount: away } = inArea(
        'west',
        answering([{ area: 'west', up: false, backInMs: 9000n }]),
      );
      await act(() => Promise.resolve());
      expect(crowns()).toEqual([['king_slime', 'up', '46 146']]); // up, its frames say, but out of their view: red at its spot
      act(() => {
        mapView
          .getState()
          .mark({ me: { x: 124, z: 80, yaw: 0 }, party: [], lock: null, boss: { x: 50, z: 140 } });
      });
      expect(crowns()).toEqual([]); // in view: its live crown, where it is, shows it
      away();
    });

    it('clears a channel’s crowns as the player switches, asks the new one at once, and drops the old one’s late answer', async () => {
      vi.useFakeTimers({ toFake: ['setInterval', 'clearInterval'] });
      try {
        const held: ((a: Answer) => void)[] = []; // each bosses query's answer, given when the test says
        const request = vi.fn((t: string) =>
          t === 'bosses'
            ? new Promise<{ ok: boolean; code: number; bosses: { bosses: Answer } }>((r) => {
                held.push((bosses) => r({ ok: true, code: 0, bosses: { bosses } }));
              })
            : new Promise<never>(() => undefined),
        );
        const answer = async (n: number, a: Answer) => {
          await act(async () => {
            held[n]!(a);
            await Promise.resolve();
          });
        };
        act(() => {
          mapView.getState().inChannel(1);
        });
        inArea('town', request);
        await answer(0, [{ area: 'south', up: false, backInMs: 30000n }]);
        expect(crowns()).toEqual([['vampire_bat', 'beaten', '125 208']]);
        vi.advanceTimersByTime(5000); // channel 1 asked again, its answer still out
        act(() => {
          mapView.getState().inChannel(2);
        });
        expect(held).toHaveLength(3); // channel 2 asked at once, not at the next 5 s
        expect(crowns()).toEqual([]); // channel 1's gone before channel 2 answers
        await answer(2, [{ area: 'south', up: true, backInMs: 0n }]);
        expect(crowns()).toEqual([['vampire_bat', 'up', '125 208']]);
        await answer(1, [{ area: 'south', up: false, backInMs: 20000n }]);
        expect(crowns()).toEqual([['vampire_bat', 'up', '125 208']]); // channel 1's late answer dropped
      } finally {
        vi.useRealTimers();
      }
    });

    it('asks every 5 s while This region shows, and never off a region', () => {
      vi.useFakeTimers({ toFake: ['setInterval', 'clearInterval'] });
      try {
        const request = answering([]);
        const asked = () => request.mock.calls.filter(([t]) => t === 'bosses').length;
        const { unmount } = inArea('town', request);
        expect(asked()).toBe(1);
        vi.advanceTimersByTime(4999);
        expect(asked()).toBe(1);
        vi.advanceTimersByTime(1);
        expect(asked()).toBe(2);
        unmount();
        vi.advanceTimersByTime(5000);
        expect(asked()).toBe(2);
        const off = answering([]);
        onMap('shroom_grove', 30, 10);
        render(
          <MapWindow
            socket={{ request: off, sequence: 0 } as never}
            layoutOf={layoutOf}
            onClose={vi.fn()}
          />,
        );
        vi.advanceTimersByTime(5000);
        expect(off).not.toHaveBeenCalled();
      } finally {
        vi.useRealTimers();
      }
    });
  });

  describe('This region pans and zooms', () => {
    /** The test region as a kilometre across would be, its tiles the ground: 0.31 px a metre whole. */
    function inKilometre() {
      act(() => {
        mapView
          .getState()
          .showMap('test_region', [1024, 1024], { kind: 'tiles', folder: 'bake/tiles' }, [
            { id: 'west', name: 'West Field', x: 60, z: 85, y: 0, main: false },
          ]);
        mapView
          .getState()
          .mark({ me: { x: 124, z: 80, yaw: 0 }, party: [], lock: null, boss: null });
      });
      render(<MapWindow socket={idle} layoutOf={layoutOf} onClose={vi.fn()} />);
      return screen.getByRole('img', { name: 'Test Region' });
    }
    const zoomIn = () => screen.getByRole('button', { name: 'Zoom in' });
    const tab = () => screen.getAllByRole('tab').map((t) => t.textContent);
    const zoomOut = () => screen.getByRole('button', { name: 'Zoom out' });
    const tiles = () =>
      [...document.querySelectorAll('image.map-tile')].map((t) => t.getAttribute('href'));

    it('zooms by the buttons about its middle, twice a press, till a metre spans 4 px; the tiles in view drawn finer as it does', async () => {
      const svg = inKilometre();
      expect(tab()).toEqual(['This region', 'World']);
      expect([svg.getAttribute('viewBox'), zoomOut()]).toEqual([
        '0 0 1024 1024',
        expect.anything(),
      ]);
      expect(zoomOut()).toBeDisabled();
      await user.click(zoomIn());
      expect(svg.getAttribute('viewBox')).toBe('256 256 512 512');
      expect([svg.getAttribute('width'), svg.getAttribute('height')]).toEqual(['320', '320']); // its size on screen kept
      expect(tiles()).toHaveLength(16); // 4 by 4 at 0.63 px a metre: 1 m a pixel
      expect(new Set(tiles().map((t) => t!.split('/')[2]))).toEqual(new Set(['1']));
      await user.click(zoomIn());
      await user.click(zoomIn());
      expect(svg.getAttribute('viewBox')).toBe('448 448 128 128'); // 8 times
      await user.click(zoomIn());
      expect(svg.getAttribute('viewBox')).toBe(`${512 - 40} ${512 - 40} 80 80`); // 12.8 times: 4 px a metre, the most
      expect(zoomIn()).toBeDisabled();
      for (let i = 0; i < 4; i++) {
        await user.click(zoomOut());
      }
      expect(svg.getAttribute('viewBox')).toBe('0 0 1024 1024');
      await user.click(zoomIn());
      onMap('shroom_grove', 30, 10);
      expect(screen.getByRole('img', { name: 'Shroom Grove' }).getAttribute('viewBox')).toBe(
        '0 0 72 40',
      ); // a new map whole
      expect(tab()).toEqual(['This map', 'World']);
    });

    it('zooms by the wheel about the pointer, and pans by a drag held inside the region', () => {
      const svg = inKilometre();
      fireEvent.wheel(svg, { deltaY: 400 });
      expect(svg.getAttribute('viewBox')).toBe('0 0 1024 1024'); // no further out than whole
      expect(fireEvent.wheel(svg, { deltaY: -400, clientX: 0, clientY: 0 })).toBe(false); // the page doesn't scroll; the test page puts the plan at 0, 0
      expect(svg.getAttribute('viewBox')).toBe('0 0 512 512'); // twice, the north-west corner kept
      fireEvent.wheel(svg, { deltaY: -400, clientX: 320, clientY: 320 }); // the region's middle: 512 m in at 0.63 px a metre
      expect(svg.getAttribute('viewBox')).toBe('256 256 256 256');
      fireEvent.pointerDown(svg, { button: 0, clientX: 100, clientY: 100 });
      fireEvent.pointerMove(window, { clientX: 20, clientY: 140 }); // 80 px west and 40 south at 1.25 px a metre
      fireEvent.pointerUp(window);
      expect(svg.getAttribute('viewBox')).toBe('320 224 256 256');
      fireEvent.pointerDown(svg, { button: 0, clientX: 0, clientY: 0 });
      fireEvent.pointerMove(window, { clientX: -2000, clientY: -2000 });
      fireEvent.pointerUp(window);
      expect(svg.getAttribute('viewBox')).toBe('768 768 256 256'); // held at the south-east corner
      fireEvent.pointerMove(window, { clientX: 2000, clientY: 2000 }); // the button's up: no more pan
      expect(svg.getAttribute('viewBox')).toBe('768 768 256 256');
      fireEvent.pointerDown(svg, { button: 2, clientX: 0, clientY: 0 });
      fireEvent.pointerMove(window, { clientX: 2000, clientY: 2000 });
      fireEvent.pointerUp(window);
      expect(svg.getAttribute('viewBox')).toBe('768 768 256 256'); // the right button's no drag
      fireEvent.pointerDown(svg, { button: 0, clientX: 0, clientY: 0 });
      fireEvent.pointerMove(window, { clientX: 2000, clientY: 2000 });
      fireEvent.pointerUp(window);
      expect(svg.getAttribute('viewBox')).toBe('0 0 256 256'); // held at the north-west corner
    });

    it('a drag that starts on a waypoint picks nothing, and a press that moves under 4 px still picks', () => {
      inKilometre();
      const west = screen.getByRole('button', { name: 'West Field' });
      fireEvent.pointerDown(west, { button: 0, clientX: 50, clientY: 50 });
      fireEvent.pointerMove(window, { clientX: 60, clientY: 50 });
      fireEvent.pointerUp(window);
      fireEvent.click(west);
      expect(screen.queryByRole('region', { name: 'Waypoint' })).not.toBeInTheDocument();
      fireEvent.pointerDown(west, { button: 0, clientX: 50, clientY: 50 });
      fireEvent.pointerMove(window, { clientX: 60, clientY: 50 });
      fireEvent.pointerUp(window); // let go off the plan: no click follows
      fireEvent.pointerDown(west, { button: 0, clientX: 50, clientY: 50 });
      fireEvent.pointerMove(window, { clientX: 53, clientY: 50 });
      fireEvent.pointerUp(window);
      fireEvent.click(west);
      expect(screen.getByRole('region', { name: 'Waypoint' })).toHaveTextContent('West Field');
    });

    it('zooms a step a notch for a wheel counting lines or pages, as Firefox and line-mode mice send', () => {
      const svg = inKilometre();
      fireEvent.wheel(svg, { deltaY: -3, deltaMode: 1, clientX: 0, clientY: 0 });
      expect(svg.getAttribute('viewBox')).toBe('0 0 512 512');
      fireEvent.wheel(svg, { deltaY: -1, deltaMode: 2, clientX: 0, clientY: 0 });
      expect(svg.getAttribute('viewBox')).toBe('0 0 256 256');
      fireEvent.wheel(svg, { deltaY: 3, deltaMode: 1, clientX: 0, clientY: 0 });
      expect(svg.getAttribute('viewBox')).toBe('0 0 512 512');
    });

    it('a click after a drag’s own picks, as a screen reader’s does with no press before it', async () => {
      inKilometre();
      const west = screen.getByRole('button', { name: 'West Field' });
      fireEvent.pointerDown(west, { button: 0, clientX: 50, clientY: 50 });
      fireEvent.pointerMove(window, { clientX: 60, clientY: 50 });
      fireEvent.pointerUp(window); // let go off the plan: no click follows
      await new Promise((r) => setTimeout(r));
      fireEvent.click(west);
      expect(screen.getByRole('region', { name: 'Waypoint' })).toHaveTextContent('West Field');
    });

    it('a cancelled press pans no more, and the click after it picks', () => {
      const svg = inKilometre();
      fireEvent.wheel(svg, { deltaY: -400, clientX: 0, clientY: 0 });
      fireEvent.pointerDown(svg, { button: 0, clientX: 100, clientY: 100 });
      fireEvent.pointerMove(window, { clientX: 20, clientY: 100 }); // 80 px west at 0.63 px a metre
      expect(svg.getAttribute('viewBox')).toBe('128 0 512 512');
      fireEvent.pointerCancel(window);
      fireEvent.pointerMove(window, { clientX: -2000, clientY: 100 });
      expect(svg.getAttribute('viewBox')).toBe('128 0 512 512');
      fireEvent.click(screen.getByRole('button', { name: 'West Field' }));
      expect(screen.getByRole('region', { name: 'Waypoint' })).toHaveTextContent('West Field');
    });

    it('lets go of a held press when the window closes', () => {
      const svg = inKilometre();
      const off = vi.spyOn(window, 'removeEventListener');
      fireEvent.pointerDown(svg, { button: 0, clientX: 100, clientY: 100 });
      cleanup();
      expect(off.mock.calls.map(([type]) => type)).toEqual(
        expect.arrayContaining(['pointermove', 'pointerup', 'pointercancel']),
      );
    });

    it('a map, however large, keeps its fit, with no buttons, and the wheel and a drag leave it be', () => {
      act(() => {
        mapView.getState().showMap('shroom_grove', [720, 400], NO_GROUND, []); // 0.67 px a metre, as a region zooms from
        mapView
          .getState()
          .mark({ me: { x: 30, z: 10, yaw: 0 }, party: [], lock: null, boss: null });
      });
      render(<MapWindow socket={idle} layoutOf={layoutOf} onClose={vi.fn()} />);
      const svg = screen.getByRole('img', { name: 'Shroom Grove' });
      expect(screen.queryByRole('group', { name: 'Zoom' })).not.toBeInTheDocument();
      expect(fireEvent.wheel(svg, { deltaY: -400 })).toBe(true); // the page's, as before
      fireEvent.pointerDown(svg, { button: 0, clientX: 100, clientY: 100 });
      fireEvent.pointerMove(window, { clientX: 20, clientY: 20 });
      fireEvent.pointerUp(window);
      expect(svg.getAttribute('viewBox')).toBe('0 0 720 400');
    });
  });

  it('a refused teleport says why as a notice and leaves the window open', async () => {
    onMap('crossroads', 20, 10);
    const { socket } = asking(() => Promise.reject(refusal(ERR.TELEPORT_BUSY)));
    const onClose = vi.fn();
    render(<MapWindow socket={socket} layoutOf={layoutOf} onClose={onClose} />);
    await user.click(screen.getByRole('tab', { name: 'World' }));
    await user.click(screen.getByRole('button', { name: 'Shroom Grove' }));
    await user.click(screen.getByRole('button', { name: 'Teleport here' }));
    await waitFor(() => {
      expect(toasts.getState().toasts.map((t) => t.text)).toEqual(['You’re already teleporting.']);
    });
    expect(screen.getByRole('button', { name: 'Teleport here' })).toBeEnabled();
    expect([onClose.mock.calls.length, teleport.getState().to]).toEqual([0, null]);
  });
});
