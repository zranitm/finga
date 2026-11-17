// The map window (map-views.md section 6): This map, the whole map with its marks named, and World, every map on the chart linked by its portals.
import { useState } from 'react';
import type { CSSProperties } from 'react';

import { WorldWindow } from '@/features/shared/WorldWindow';
import { mapLinks } from '@/gamedata/map-links';
import { maps } from '@/gamedata/maps';
import { npcs } from '@/gamedata/npcs';
import { clashes, stackTags, TAG_GAP } from '@/lib/tags';
import type { TagBox } from '@/lib/tags';
import { mobById } from '@/progression/mobs';
import { character } from '@/state/character';
import { mapView } from '@/state/map-view';
import type { Spot } from '@/state/map-view';
import { party } from '@/state/party';

import { MapMarks, useNpcMarkers } from './MapMarks';

type MapData = (typeof maps)[number];
type Tab = 'here' | 'world';

const VIEW_W = 480; // This map's largest size, in pixels
const VIEW_H = 320;
const LABEL_PX = 12;
const CHAR_PX = 6.5; // a label's width a character, near enough for keeping labels apart
const DOT_PX = 3;
const CHART_PX = 5; // pixels a unit of the world chart
const CHART_PAD = 10; // chart units round the outermost maps
const GROUP_GAP_M = 6; // spawn circles of one mob this close at their edges share one label
const LABEL_REACH_M = 8; // every spawn circle has a label of its own mob on it, or this close to its edge

const nameOf = (id: string) => maps.find((m) => m.id === id)?.name ?? id;
const levels = (m: MapData) => `Lv ${m.min_level}–${m.max_level}`;

/** A label on This map, in pixels: its text centred on x, sitting on bottom. */
export interface MapLabel extends TagBox {
  readonly kind: 'spawn' | 'portal' | 'npc' | 'me';
  readonly text: string;
}

type Spawn = MapData['spawns'][number];

/** The map's spawn circles as labelled: those of one mob that meet or come within GROUP_GAP_M of another share one label, the chain cut where one label would leave a circle out of LABEL_REACH_M. Each group's circles come nearest its middle first. */
function spawnGroups(spawns: MapData['spawns']): Spawn[][] {
  const parent = spawns.map((_, i) => i);
  const root = (i: number): number => (parent[i] === i ? i : (parent[i] = root(parent[i]!)));
  spawns.forEach((a, i) => {
    spawns.forEach((b, j) => {
      if (
        j > i &&
        a.mob === b.mob &&
        Math.hypot(a.x - b.x, a.z - b.z) <= a.radius + b.radius + GROUP_GAP_M
      ) {
        parent[root(j)] = root(i);
      }
    });
  });
  const groups = new Map<number, Spawn[]>();
  spawns.forEach((s, i) => groups.set(root(i), [...(groups.get(root(i)) ?? []), s]));
  return [...groups.values()].flatMap(within);
}

/** How far a point is from a spawn circle's edge: 0 on it. */
const fromEdge = (p: { x: number; z: number }, s: Spawn) =>
  Math.max(0, Math.hypot(s.x - p.x, s.z - p.z) - s.radius);

const middleOf = (members: readonly Spawn[]) => ({
  x: members.reduce((sum, s) => sum + s.x, 0) / members.length,
  z: members.reduce((sum, s) => sum + s.z, 0) / members.length,
});

/** Where in circle s a label best reaches its group: its centre moved toward the group's middle, by at most three quarters of its radius. */
function inward(s: Spawn, middle: { x: number; z: number }): { x: number; z: number } {
  const d = Math.hypot(middle.x - s.x, middle.z - s.z);
  const k = d === 0 ? 0 : Math.min(d, 0.75 * s.radius) / d;
  return { x: s.x + (middle.x - s.x) * k, z: s.z + (middle.z - s.z) * k };
}

/** A chain cut where one label would leave a circle further than LABEL_REACH_M: the circle nearest the middle and those its label reaches, then the rest, cut again. */
function within(members: Spawn[]): Spawn[][] {
  const middle = middleOf(members);
  const off = (s: Spawn) => Math.hypot(s.x - middle.x, s.z - middle.z);
  const sorted = members.toSorted((a, b) => off(a) - off(b));
  const spot = inward(sorted[0]!, middle);
  const near = sorted.filter((s) => fromEdge(spot, s) <= LABEL_REACH_M);
  const far = sorted.filter((s) => fromEdge(spot, s) > LABEL_REACH_M);
  return [near, ...(far.length > 0 ? within(far) : [])];
}

/** This map's labels at scale pixels a metre. A spawn group's label sits on one of its own circles, nearest the group's middle first, stepped up or down inside the circle, wherever it's in LABEL_REACH_M of all the group's circles and covers no quest mark or label before it; failing that, it's lifted. Portals, NPCs and the player follow, each lifted clear of everything before it. */
export function mapLabels(
  map: MapData,
  me: Spot | null,
  scale: number,
  size: readonly [number, number],
  marked: ReadonlySet<string>,
): MapLabel[] {
  const y = (z: number) => (size[1] - z) * scale;
  const box = (kind: MapLabel['kind'], text: string, x: number, bottom: number) => {
    const w = text.length * CHAR_PX + 4;
    const h = LABEL_PX + 2;
    const inside = (v: number, lo: number, hi: number) => Math.min(Math.max(v, lo), hi);
    return {
      kind,
      text,
      x: inside(x * scale, w / 2, size[0] * scale - w / 2), // a label by the map's edge stays in view
      bottom: Math.max(bottom, h),
      w,
      h,
    };
  };
  const here = npcs.npcs.filter((n) => n.map === map.id);
  const placed: TagBox[] = here
    .filter((n) => marked.has(n.id))
    .map((n) => ({ x: n.x * scale, bottom: y(n.z) - 2 * DOT_PX, w: 10, h: 14 }));
  const step = LABEL_PX + 2 + TAG_GAP;
  const tried = spawnGroups(map.spawns).map((members) => {
    const mob = mobById(members[0]!.mob);
    const text = `${mob?.name ?? members[0]!.mob} Lv ${mob?.level ?? '?'}`;
    const middle = middleOf(members);
    const at = (p: { x: number; z: number }, k: number) =>
      box('spawn', text, p.x, y(p.z) + LABEL_PX / 2 + k * step);
    const reaches = (t: MapLabel) =>
      members.every(
        (s) =>
          fromEdge({ x: t.x / scale, z: size[1] - (t.bottom - t.h / 2) / scale }, s) <=
          LABEL_REACH_M,
      );
    // Each circle's spot toward the middle, then its centre, each stepped up and down while inside the circle.
    const spots = members.flatMap((s) =>
      [inward(s, middle), s].flatMap((p) => {
        const inside = (k: number) =>
          Math.hypot(p.x - s.x, p.z - (k * step) / scale - s.z) < s.radius;
        const ks = [0];
        for (let k = 1; inside(k) || inside(-k); k++) {
          ks.push(...[-k, k].filter(inside));
        }
        return ks.map((k) => at(p, k));
      }),
    );
    const fit = spots.filter(reaches);
    return fit.length > 0 ? fit : spots.slice(0, 1);
  });
  // The groups with the fewest spots go first, so a small lone circle isn't crowded out by a chain with room to spare.
  const spawns = tried
    .toSorted((a, b) => a.length - b.length)
    .map((tries) => {
      const free = tries.find((t) => !placed.some((p) => clashes(t, p)));
      const label = free ?? { ...tries[0]!, bottom: stackTags(placed, [tries[0]!])[0]! };
      placed.push(label);
      return label;
    });
  const rest: MapLabel[] = [
    ...map.portals.map((p) =>
      box('portal', `To ${nameOf(p.to)}`, p.x, y(p.z) - Math.max(p.radius * scale, 2 * DOT_PX) - 2),
    ),
    ...here.map((n) => box('npc', n.name, n.x, y(n.z) - DOT_PX - 2)),
    ...(me ? [box('me', 'You', me.x, y(me.z) - 3 * DOT_PX - 2)] : []),
  ];
  const bottoms = stackTags(placed, rest);
  return [...spawns, ...rest.map((l, i) => ({ ...l, bottom: bottoms[i]! }))];
}

function ThisMap() {
  const mapId = mapView.use((s) => s.mapId);
  const [sx, sz] = mapView.use((s) => s.size);
  const me = mapView.use((s) => s.me);
  const markers = useNpcMarkers(mapId);
  const map = maps.find((m) => m.id === mapId);
  if (!map) {
    return <p className="muted">No map yet.</p>;
  }
  const scale = Math.min(VIEW_W / sx, VIEW_H / sz);
  const labels = mapLabels(map, me, scale, [sx, sz], new Set(markers.keys()));
  return (
    <svg
      className="map-here"
      role="img"
      aria-label={map.name}
      width={sx * scale}
      height={sz * scale}
      viewBox={`0 0 ${sx} ${sz}`}
    >
      <MapMarks unit={1 / scale}>
        {labels.map((l, i) => (
          <text
            key={i}
            className={`map-label map-label-${l.kind}`}
            x={l.x / scale}
            y={l.bottom / scale}
            fontSize={LABEL_PX / scale}
            textAnchor="middle"
          >
            {l.text}
          </text>
        ))}
      </MapMarks>
    </svg>
  );
}

/** A node's card beside it, toward the chart's middle, so it stays inside the window. */
function cardAt(x: number, y: number, w: number, h: number): CSSProperties {
  return {
    ...(x < w / 2 ? { left: x + 16 } : { right: w - x + 16 }),
    ...(y < h / 2 ? { top: y - 8 } : { bottom: h - y - 8 }),
  };
}

function World() {
  const here = mapView.use((s) => s.mapId);
  const myId = character.use((s) => s.character?.id);
  const members = party.use((s) => s.party?.members) ?? [];
  const [shown, setShown] = useState<string | null>(null);
  const xs = maps.map((m) => m.place[0]);
  const ys = maps.map((m) => m.place[1]);
  const [x0, y0] = [Math.min(...xs) - CHART_PAD, Math.min(...ys) - CHART_PAD];
  const [w, h] = [Math.max(...xs) + CHART_PAD - x0, Math.max(...ys) + CHART_PAD - y0];
  const at = (id: string) => maps.find((m) => m.id === id)?.place ?? [0, 0];
  const card = maps.find((m) => m.id === shown);
  return (
    <div className="map-world">
      <svg
        role="img"
        aria-label="World"
        width={w * CHART_PX}
        height={h * CHART_PX}
        viewBox={`${x0} ${y0} ${w} ${h}`}
      >
        {mapLinks.map(([a, b]) => (
          <line
            key={`${a}-${b}`}
            className="map-link"
            data-link={`${a}-${b}`}
            x1={at(a)[0]}
            y1={at(a)[1]}
            x2={at(b)[0]}
            y2={at(b)[1]}
          />
        ))}
        {maps.map((m) => {
          const [x, y] = m.place;
          const town = m.kind === 'town';
          const names = members.filter(
            (p) => p.online && p.mapId === m.id && p.characterId !== myId,
          );
          return (
            <g
              key={m.id}
              className="map-node"
              data-map={m.id}
              tabIndex={0}
              aria-label={m.name}
              onPointerEnter={() => setShown(m.id)}
              onPointerLeave={() => setShown(null)}
              onFocus={() => setShown(m.id)}
              onBlur={() => setShown(null)}
            >
              {m.id === here && (
                <>
                  <circle className="map-here-ring" cx={x} cy={y} r={3.6} />
                  <text className="map-node-you" x={x} y={y - 4.6} textAnchor="middle">
                    You are here
                  </text>
                </>
              )}
              {town ? (
                <polygon
                  className="map-town"
                  points="0,-2.4 2.2,-0.6 1.5,-0.6 1.5,1.8 -1.5,1.8 -1.5,-0.6 -2.2,-0.6"
                  transform={`translate(${x} ${y})`}
                />
              ) : (
                <circle className="map-hunt" cx={x} cy={y} r={1.8} />
              )}
              <text className="map-node-name" x={x} y={y + 4.4} textAnchor="middle">
                {m.name}
              </text>
              {!town && (
                <text className="map-node-levels" x={x} y={y + 6.6} textAnchor="middle">
                  {levels(m)}
                </text>
              )}
              {names.map((p, i) => (
                <text
                  key={p.characterId}
                  className="map-node-party"
                  x={x}
                  y={y + (town ? 6.6 : 8.8) + 2.2 * i}
                  textAnchor="middle"
                >
                  {p.name}
                </text>
              ))}
            </g>
          );
        })}
      </svg>
      {card && (
        <div
          className="map-card"
          role="tooltip"
          style={cardAt(
            (card.place[0] - x0) * CHART_PX,
            (card.place[1] - y0) * CHART_PX,
            w * CHART_PX,
            h * CHART_PX,
          )}
        >
          <strong>{card.name}</strong>
          <div className="muted">{card.kind === 'town' ? 'Town' : levels(card)}</div>
          <ul>
            {[...new Set(card.spawns.map((s) => s.mob))]
              .flatMap((id) => mobById(id) ?? [])
              .sort((a, b) => a.level - b.level)
              .map((mob) => (
                <li key={mob.id}>
                  {mob.name} Lv {mob.level}
                </li>
              ))}
          </ul>
        </div>
      )}
    </div>
  );
}

export function MapWindow({ onClose }: { readonly onClose: () => void }) {
  const [tab, setTab] = useState<Tab>('here');
  return (
    <WorldWindow name="map" title="Map" onClose={onClose} initialX={360} initialY={80}>
      <div className="map-window">
        <div className="inventory-tabs" role="tablist">
          {(['here', 'world'] as const).map((t) => (
            <button
              key={t}
              type="button"
              role="tab"
              aria-selected={t === tab}
              className={`btn btn-quiet${t === tab ? ' tab-active' : ''}`}
              onClick={() => setTab(t)}
            >
              {t === 'here' ? 'This map' : 'World'}
            </button>
          ))}
        </div>
        {tab === 'here' ? <ThisMap /> : <World />}
      </div>
    </WorldWindow>
  );
}
