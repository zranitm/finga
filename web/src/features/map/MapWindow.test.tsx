import { readFileSync } from 'node:fs';
import path from 'node:path';

import { create } from '@bufbuild/protobuf';
import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
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
import { mapView } from '@/state/map-view';
import { party } from '@/state/party';
import { pvp } from '@/state/pvp';
import { teleport } from '@/state/teleport';
import { toasts } from '@/state/toasts';
import { refusal } from '@/testing/refusal';
import { resetStores } from '@/testing/reset-stores';
import { user } from '@/testing/user';

import { mapLabels, MapWindow } from './MapWindow';

const map = (id: string) => maps.find((m) => m.id === id)!;

/** A map's layout as the app gives it: its size, and no image, as jsdom draws none. */
const layoutOf = vi.fn((id: string) =>
  Promise.resolve({ size: id === 'town' ? ([48, 32] as const) : ([72, 40] as const), image: '' }),
);

function onMap(id: string, x: number, z: number) {
  const m = map(id);
  const size = id === 'town' ? ([48, 32] as const) : ([72, 40] as const);
  act(() => {
    mapView.getState().showMap(m.id, size, '');
    mapView.getState().mark({ me: { x, z, yaw: 0 }, party: [], lock: null, boss: null });
  });
}

/** A socket no test here asks anything of. */
const idle = { request: vi.fn(), sequence: 0 } as never;

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
    onMap('forest', 30, 10);
    render(<MapWindow socket={idle} layoutOf={layoutOf} onClose={vi.fn()} />);
    const svg = screen.getByRole('img', { name: 'Forest' });
    const slime = mobById('green_slime')!;
    const mushroom = mobById('orange_mushroom')!;
    expect(texts(svg, '.map-label-spawn')).toEqual(
      expect.arrayContaining([
        `${slime.name} Lv ${slime.level}`,
        `${mushroom.name} Lv ${mushroom.level}`,
      ]),
    );
    expect(texts(svg, '.map-label-portal').sort()).toEqual(['To Cave', 'To Town']);
    expect(texts(svg, '.map-label-npc')).toEqual(['Bowman Trainer']);
    expect(texts(svg, '.map-label-me')).toEqual(['You']);
  });

  it('This map’s labels keep clear of each other and of the quest marks, on every map', () => {
    const overlap = (a: { x: number; bottom: number; w: number; h: number }, b: typeof a) =>
      Math.abs(a.x - b.x) * 2 < a.w + b.w && a.bottom > b.bottom - b.h && b.bottom > a.bottom - a.h;
    for (const m of maps) {
      const marked = new Set(npcs.npcs.filter((n) => n.map === m.id).map((n) => n.id));
      const labels = mapLabels(m, { x: 20, z: 20 }, 6, [72, 40], marked, null);
      for (const [i, a] of labels.entries()) {
        for (const b of labels.slice(i + 1)) {
          expect([m.id, a.text, b.text, overlap(a, b)]).toEqual([m.id, a.text, b.text, false]);
        }
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
    return mapLabels(m, null, scale, [sx, sz], new Set(), null)
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
    const labels = mapLabels(m, null, scale, [sx, sz], new Set(), { type: spot.mob, backAt: 0 });
    return { labels, boss: labels.find((l) => l.kind === 'boss')!, crown: (sz - spot.z) * scale };
  };

  it('puts a beaten boss’s label under its crown when that has more room than tags keep: the forest’s, by the mesa slimes’ label', () => {
    const room = 6; // the boss label's, past the tags' 2 px
    const { labels, boss, crown } = bossLabels('forest');
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
      .filter((m) => m.spawns.length > 0)
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
      .filter((m) => m.spawns.length > 0)
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
    const guide = npcs.npcs.find((n) => n.id === 'town_guide')!;
    const labels = mapLabels(map('town'), { x: guide.x, z: guide.z }, 6, [48, 32], new Set(), null);
    const you = labels.find((l) => l.kind === 'me')!;
    const name = labels.find((l) => l.text === guide.name)!;
    expect(you.bottom).toBeLessThanOrEqual(name.bottom - name.h - 2);
  });

  it('World draws every map but a match’s at its place, a line for each pair of maps a portal joins, and a house for a town with no levels', async () => {
    onMap('forest', 30, 10);
    render(<MapWindow socket={idle} layoutOf={layoutOf} onClose={vi.fn()} />);
    await user.click(screen.getByRole('tab', { name: 'World' }));
    const chart = screen.getByRole('group', { name: 'World' });
    expect(chart.querySelectorAll('.map-node')).toHaveLength(
      maps.filter((m) => m.kind !== 'pvp').length,
    );
    expect(chart.querySelector('.map-node[data-map="ring"]')).toBeNull(); // only a match reaches it
    expect(
      [...chart.querySelectorAll('.map-link')].map((l) => l.getAttribute('data-link')).sort(),
    ).toEqual(mapLinks.map(([a, b]) => `${a}-${b}`).sort());
    const forest = chart.querySelector('.map-node[data-map="forest"]')!;
    const [fx, fy] = map('forest').place!;
    expect([
      forest.querySelector('.map-hunt')!.getAttribute('cx'),
      forest.querySelector('.map-hunt')!.getAttribute('cy'),
    ]).toEqual([String(fx), String(fy)]);
    expect(texts(forest, '.map-node-levels')).toEqual(['Lv 1–4']);
    const town = chart.querySelector('.map-node[data-map="town"]')!;
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
    act(() => pvp.getState().leftMatch());
    expect(screen.getByRole('tab', { name: 'World' })).toHaveAttribute('aria-selected', 'true'); // the tab chosen before
  });

  it('World rings the player’s map, “You are here”, and names each online party member under their map', async () => {
    onMap('forest', 30, 10);
    party.getState().applyPartyState(
      create(PartyStateSchema, {
        party: create(PartyViewSchema, {
          leader: 'c-robin',
          members: [
            { characterId: 'c-robin', name: 'Robin', mapId: 'cave', online: true },
            { characterId: 'c-jade', name: 'Jade', mapId: 'cave', online: false },
            { characterId: 'c-kai', name: 'Kai', mapId: 'town', online: true },
          ],
        }),
      }),
    );
    render(<MapWindow socket={idle} layoutOf={layoutOf} onClose={vi.fn()} />);
    await user.click(screen.getByRole('tab', { name: 'World' }));
    const chart = screen.getByRole('group', { name: 'World' });
    const node = (id: string) => chart.querySelector(`.map-node[data-map="${id}"]`)!;
    expect([
      node('forest').querySelector('.map-here-ring'),
      texts(node('forest'), '.map-node-you'),
    ]).toEqual([expect.anything(), ['You are here']]);
    expect(chart.querySelectorAll('.map-here-ring')).toHaveLength(1);
    expect([
      texts(node('cave'), '.map-node-party'),
      texts(node('town'), '.map-node-party'),
    ]).toEqual([['Robin'], ['Kai']]);
  });

  it('pointing at a map on World shows its name, levels and mobs with their levels', async () => {
    onMap('town', 20, 10);
    render(<MapWindow socket={idle} layoutOf={layoutOf} onClose={vi.fn()} />);
    await user.click(screen.getByRole('tab', { name: 'World' }));
    const chart = screen.getByRole('group', { name: 'World' });
    fireEvent.pointerEnter(chart.querySelector('.map-node[data-map="forest"]')!);
    const card = within(screen.getByRole('tooltip'));
    const slime = mobById('green_slime')!;
    expect(card.getByText('Forest')).toBeInTheDocument();
    expect(card.getByText('Lv 1–4')).toBeInTheDocument();
    expect(card.getByText(`${slime.name} Lv ${slime.level}`)).toBeInTheDocument();
    fireEvent.pointerLeave(chart.querySelector('.map-node[data-map="forest"]')!);
    expect(screen.queryByRole('tooltip')).not.toBeInTheDocument();
  });

  it('clicking a map on World shows its layout: its spawns, portals and NPCs named, and Back to the chart', async () => {
    onMap('town', 20, 10);
    render(<MapWindow socket={idle} layoutOf={layoutOf} onClose={vi.fn()} />);
    await user.click(screen.getByRole('tab', { name: 'World' }));
    await user.click(screen.getByRole('button', { name: 'Forest' }));
    const svg = await screen.findByRole('img', { name: 'Forest' });
    expect(layoutOf).toHaveBeenCalledWith('forest');
    expect(screen.getByText('Lv 1–4')).toBeInTheDocument();
    const slime = mobById('green_slime')!;
    expect(texts(svg, '.map-label-spawn')).toContain(`${slime.name} Lv ${slime.level}`);
    expect(texts(svg, '.map-label-portal').sort()).toEqual(['To Cave', 'To Town']);
    expect(texts(svg, '.map-label-npc')).toEqual(['Bowman Trainer']);
    expect(
      [...svg.querySelectorAll('.map-spawn')].map((c) => [
        c.getAttribute('data-mob'),
        c.getAttribute('cx'),
      ]),
    ).toEqual(map('forest').spawns.map((sp) => [sp.mob, String(sp.x)]));
    expect(svg.getAttribute('viewBox')).toBe('0 0 72 40');
    await user.click(screen.getByRole('button', { name: 'Back' }));
    expect(screen.getByRole('group', { name: 'World' })).toBeInTheDocument();
  });

  it('a picked map shows the player and the live marks only when it’s the player’s own', async () => {
    onMap('forest', 30, 10);
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
    await user.click(screen.getByRole('button', { name: 'Forest' }));
    const own = screen.getByRole('img', { name: 'Forest' });
    expect(own.querySelectorAll(live)).toHaveLength(5);
    expect(layoutOf).not.toHaveBeenCalled();
    await user.click(screen.getByRole('button', { name: 'Back' }));
    await user.click(screen.getByRole('button', { name: 'Cave' }));
    const other = await screen.findByRole('img', { name: 'Cave' });
    expect(other.querySelectorAll(live)).toHaveLength(0);
  });

  it('a beaten boss shows grey at its spot on its own map, the time until it’s back counting down, till it’s back', () => {
    vi.useFakeTimers();
    try {
      onMap('forest', 30, 10);
      act(() => {
        fieldBoss
          .getState()
          .applyGone(
            create(FieldBossGoneSchema, { mobId: 9, type: 'king_slime', backInMs: 192000n }),
          );
      });
      render(<MapWindow socket={idle} layoutOf={layoutOf} onClose={vi.fn()} />);
      const here = screen.getByRole('img', { name: 'Forest' });
      const [spotX, spotZ] = [map('forest').boss!.x, map('forest').boss!.z];
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

  it('a picked map names the party members online on it under its header', async () => {
    onMap('forest', 30, 10);
    party.getState().applyPartyState(
      create(PartyStateSchema, {
        party: create(PartyViewSchema, {
          leader: 'c-robin',
          members: [
            { characterId: 'c-robin', name: 'Robin', mapId: 'cave', online: true },
            { characterId: 'c-jade', name: 'Jade', mapId: 'cave', online: false },
            { characterId: 'c-kai', name: 'Kai', mapId: 'town', online: true },
          ],
        }),
      }),
    );
    render(<MapWindow socket={idle} layoutOf={layoutOf} onClose={vi.fn()} />);
    await user.click(screen.getByRole('tab', { name: 'World' }));
    fireEvent.keyDown(screen.getByRole('button', { name: 'Cave' }), { key: 'Enter' });
    const names = await screen.findByRole('list', { name: 'Party in Cave' });
    expect(
      within(names)
        .getAllByRole('listitem')
        .map((li) => li.textContent),
    ).toEqual(['Robin']);
  });

  it('a picked map whose layout won’t load says so', async () => {
    onMap('town', 20, 10);
    render(
      <MapWindow
        socket={idle}
        layoutOf={() => Promise.reject(new Error('map cave: 500'))}
        onClose={vi.fn()}
      />,
    );
    await user.click(screen.getByRole('tab', { name: 'World' }));
    await user.click(screen.getByRole('button', { name: 'Cave' }));
    expect(await screen.findByText('The map didn’t load.')).toBeInTheDocument();
  });

  it('a picked map offers Teleport here, but not on the player’s own map, nor while they’re dead', async () => {
    onMap('town', 20, 10);
    render(<MapWindow socket={idle} layoutOf={layoutOf} onClose={vi.fn()} />);
    await user.click(screen.getByRole('tab', { name: 'World' }));
    await user.click(screen.getByRole('button', { name: 'Forest' }));
    expect(screen.getByRole('button', { name: 'Teleport here' })).toBeEnabled();
    await user.click(screen.getByRole('button', { name: 'Back' }));
    await user.click(screen.getByRole('button', { name: 'Town' }));
    expect(screen.queryByRole('button', { name: 'Teleport here' })).not.toBeInTheDocument();
    await user.click(screen.getByRole('button', { name: 'Back' }));
    act(() => {
      character.getState().setDead(true);
    });
    await user.click(screen.getByRole('button', { name: 'Forest' }));
    expect(screen.queryByRole('button', { name: 'Teleport here' })).not.toBeInTheDocument();
  });

  it('Teleport here asks the server with the latest input’s seq; its answer closes the window and starts the channel', async () => {
    onMap('town', 20, 10);
    const { socket, request } = asking(() => Promise.resolve({ ok: true, code: 0 }));
    const onClose = vi.fn();
    render(<MapWindow socket={socket} layoutOf={layoutOf} onClose={onClose} />);
    await user.click(screen.getByRole('tab', { name: 'World' }));
    await user.click(screen.getByRole('button', { name: 'Forest' }));
    await user.click(screen.getByRole('button', { name: 'Teleport here' }));
    expect(request).toHaveBeenCalledWith('teleport', { mapId: 'forest' }, 42);
    await waitFor(() => {
      expect(onClose).toHaveBeenCalledOnce();
    });
    expect(teleport.getState().to).toBe('forest');
  });

  it('a refused teleport says why as a notice and leaves the window open', async () => {
    onMap('town', 20, 10);
    const { socket } = asking(() => Promise.reject(refusal(ERR.TELEPORT_BUSY)));
    const onClose = vi.fn();
    render(<MapWindow socket={socket} layoutOf={layoutOf} onClose={onClose} />);
    await user.click(screen.getByRole('tab', { name: 'World' }));
    await user.click(screen.getByRole('button', { name: 'Forest' }));
    await user.click(screen.getByRole('button', { name: 'Teleport here' }));
    await waitFor(() => {
      expect(toasts.getState().toasts.map((t) => t.text)).toEqual(['You’re already teleporting.']);
    });
    expect(screen.getByRole('button', { name: 'Teleport here' })).toBeEnabled();
    expect([onClose.mock.calls.length, teleport.getState().to]).toEqual([0, null]);
  });
});
