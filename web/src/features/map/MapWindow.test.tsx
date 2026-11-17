import { readFileSync } from 'node:fs';
import path from 'node:path';

import { create } from '@bufbuild/protobuf';
import { act, fireEvent, render, screen, within } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import { mapLinks } from '@/gamedata/map-links';
import { maps } from '@/gamedata/maps';
import { npcs } from '@/gamedata/npcs';
import { mobById } from '@/progression/mobs';
import { PartyStateSchema, PartyViewSchema } from '@/protocol/messages';
import { mapView } from '@/state/map-view';
import { party } from '@/state/party';
import { resetStores } from '@/testing/reset-stores';
import { user } from '@/testing/user';

import { mapLabels, MapWindow } from './MapWindow';

const map = (id: string) => maps.find((m) => m.id === id)!;

function onMap(id: string, x: number, z: number) {
  const m = map(id);
  const size = id === 'town' ? ([48, 32] as const) : ([72, 40] as const);
  act(() => {
    mapView.getState().showMap(m.id, size, '');
    mapView.getState().mark({ me: { x, z, yaw: 0 }, party: [], lock: null, boss: null });
  });
}

const texts = (root: Element, selector: string) =>
  [...root.querySelectorAll(selector)].map((t) => t.textContent);

describe('MapWindow', () => {
  beforeEach(() => {
    resetStores();
  });

  it('This map names each spawn by its mob and level, each portal by where it leads, and the player', () => {
    onMap('forest', 30, 10);
    render(<MapWindow onClose={vi.fn()} />);
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
      const labels = mapLabels(m, { x: 20, z: 20 }, 6, [72, 40], marked);
      for (const [i, a] of labels.entries()) {
        for (const b of labels.slice(i + 1)) {
          expect([m.id, a.text, b.text, overlap(a, b)]).toEqual([m.id, a.text, b.text, false]);
        }
      }
    }
  });

  /** Each spawn label's mob and the middle of its text, in metres, at the window's scale (480 by 320 px at most). */
  const spawnLabels = (id: string) => {
    const m = map(id);
    const raw = readFileSync(path.resolve(process.cwd(), `public/maps/${id}.json`), 'utf8');
    const { size_x: sx, size_z: sz } = (
      JSON.parse(raw) as { geometry: { size_x: number; size_z: number } }
    ).geometry;
    const scale = Math.min(480 / sx, 320 / sz);
    return mapLabels(m, null, scale, [sx, sz], new Set())
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
    const labels = mapLabels(map('town'), { x: guide.x, z: guide.z }, 6, [48, 32], new Set());
    const you = labels.find((l) => l.kind === 'me')!;
    const name = labels.find((l) => l.text === guide.name)!;
    expect(you.bottom).toBeLessThanOrEqual(name.bottom - name.h - 2);
  });

  it('World draws every map at its place, a line for each pair of maps a portal joins, and a house for a town with no levels', async () => {
    onMap('forest', 30, 10);
    render(<MapWindow onClose={vi.fn()} />);
    await user.click(screen.getByRole('tab', { name: 'World' }));
    const chart = screen.getByRole('img', { name: 'World' });
    expect(chart.querySelectorAll('.map-node')).toHaveLength(maps.length);
    expect(
      [...chart.querySelectorAll('.map-link')].map((l) => l.getAttribute('data-link')).sort(),
    ).toEqual(mapLinks.map(([a, b]) => `${a}-${b}`).sort());
    const forest = chart.querySelector('.map-node[data-map="forest"]')!;
    const [fx, fy] = map('forest').place;
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
    render(<MapWindow onClose={vi.fn()} />);
    await user.click(screen.getByRole('tab', { name: 'World' }));
    const chart = screen.getByRole('img', { name: 'World' });
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
    render(<MapWindow onClose={vi.fn()} />);
    await user.click(screen.getByRole('tab', { name: 'World' }));
    const chart = screen.getByRole('img', { name: 'World' });
    fireEvent.pointerEnter(chart.querySelector('.map-node[data-map="forest"]')!);
    const card = within(screen.getByRole('tooltip'));
    const slime = mobById('green_slime')!;
    expect(card.getByText('Forest')).toBeInTheDocument();
    expect(card.getByText('Lv 1–4')).toBeInTheDocument();
    expect(card.getByText(`${slime.name} Lv ${slime.level}`)).toBeInTheDocument();
    fireEvent.pointerLeave(chart.querySelector('.map-node[data-map="forest"]')!);
    expect(screen.queryByRole('tooltip')).not.toBeInTheDocument();
  });
});
