// The map window (map-views.md section 6): This map, the whole map with its marks named, and World, every map on the chart linked by its portals.
import { useState } from 'react';
import type { CSSProperties } from 'react';

import { WorldWindow } from '@/features/shared/WorldWindow';
import { mapLinks } from '@/gamedata/map-links';
import { maps } from '@/gamedata/maps';
import { npcs } from '@/gamedata/npcs';
import { stackTags } from '@/lib/tags';
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

const nameOf = (id: string) => maps.find((m) => m.id === id)?.name ?? id;
const levels = (m: MapData) => `Lv ${m.min_level}–${m.max_level}`;

/** A label on This map, in pixels: its text centred on x, sitting on bottom. */
export interface MapLabel extends TagBox {
  readonly kind: 'spawn' | 'portal' | 'npc' | 'me';
  readonly text: string;
}

/** This map's labels at scale pixels a metre, each lifted clear of the NPCs' quest marks and of the labels before it: spawns, portals, NPCs, then the player. */
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
  const labels: MapLabel[] = [
    ...map.spawns.map((s) => {
      const mob = mobById(s.mob);
      return box(
        'spawn',
        `${mob?.name ?? s.mob} Lv ${mob?.level ?? '?'}`,
        s.x,
        y(s.z) + LABEL_PX / 2,
      );
    }),
    ...map.portals.map((p) =>
      box('portal', `To ${nameOf(p.to)}`, p.x, y(p.z) - Math.max(p.radius * scale, 2 * DOT_PX) - 2),
    ),
    ...here.map((n) => box('npc', n.name, n.x, y(n.z) - DOT_PX - 2)),
    ...(me ? [box('me', 'You', me.x, y(me.z) - 3 * DOT_PX - 2)] : []),
  ];
  const marks = here
    .filter((n) => marked.has(n.id))
    .map((n) => ({ x: n.x * scale, bottom: y(n.z) - 2 * DOT_PX, w: 10, h: 14 }));
  const bottoms = stackTags(marks, labels);
  return labels.map((l, i) => ({ ...l, bottom: bottoms[i]! }));
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
