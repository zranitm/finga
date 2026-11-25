// A map's ground and marks as SVG in metres, x east and y south from the map's north edge, shared by the minimap and the map window (map-views.md sections 5 and 6); on a battle's map its bases and points too (pvp.md section 13).
import type { ReactNode } from 'react';

import { useQuestPlayer } from '@/features/shared/useQuestPlayer';
import { maps } from '@/gamedata/maps';
import { npcs } from '@/gamedata/npcs';
import { mobById } from '@/progression/mobs';
import { placesBelow } from '@/progression/places';
import { MARKER_TEXT, markerFor } from '@/progression/quests';
import type { Marker } from '@/progression/quests';
import { character } from '@/state/character';
import { fieldBoss } from '@/state/field-boss';
import { mapView } from '@/state/map-view';
import type { MapGround } from '@/state/map-view';
import { pvp, sideOf } from '@/state/pvp';

import { crownsOf } from './crowns';
import type { Crown } from './crowns';

const SPAWN_DEFAULT = 0x9a9a9a; // an untinted mob's circle
const CROWN = '-1,0.6 -1,-0.6 -0.5,0 0,-0.9 0.5,0 1,-0.6 1,0.6'; // a boss's mark, a unit round its middle
const CAVE_MOUTH = 'M-1.2,0.9 L-1.2,0 A1.2,1.2 0 0 1 1.2,0 L1.2,0.9 Z'; // a gate down to a cave, an arch a unit round its middle

const TILE_M = 128; // a region's map tile's side
const LEVELS = [1, 4] as const; // its tiles' metres a pixel, finest first
const BLUR = 2; // the screen pixels a tile's pixel may span before a finer level is drawn

const hex = (c: number) => `#${c.toString(16).padStart(6, '0')}`;

/** A box of the map in its SVG's metres: its west edge, its north edge as y down from the map's north, and its sides. */
export interface MapBox {
  readonly x: number;
  readonly y: number;
  readonly w: number;
  readonly h: number;
}

/** The ground at `unit` metres a pixel on a map size across: a map's one image, or a region's tiles meeting view at the coarsest level whose pixel spans at most BLUR screen pixels. */
function Ground({
  ground,
  size: [sx, sz],
  unit,
  view,
}: {
  readonly ground: MapGround;
  readonly size: readonly [number, number];
  readonly unit: number;
  readonly view: MapBox;
}) {
  if (ground.kind === 'image') {
    return (
      ground.url && (
        <image href={ground.url} x={0} y={0} width={sx} height={sz} preserveAspectRatio="none" />
      )
    );
  }
  const level = LEVELS.findLast((l) => l / unit <= BLUR) ?? LEVELS[0];
  const tiles = [];
  for (let tz = 0; tz < sz / TILE_M; tz++) {
    const y = sz - (tz + 1) * TILE_M;
    for (let tx = 0; tx < sx / TILE_M; tx++) {
      const x = tx * TILE_M;
      if (
        x < view.x + view.w &&
        x + TILE_M > view.x &&
        y < view.y + view.h &&
        y + TILE_M > view.y
      ) {
        tiles.push(
          <image
            key={`${tx}_${tz}`}
            className="map-tile"
            href={`${ground.folder}/${level}/${tx}_${tz}.png`}
            x={x}
            y={y}
            width={TILE_M + unit} // a screen pixel over its east and south neighbours, so no seam shows
            height={TILE_M + unit}
            preserveAspectRatio="none"
          />,
        );
      }
    }
  }
  return tiles;
}

type MapData = (typeof maps)[number];

/** A battle's sides from the player's, as the match bar colours them: theirs, the other, and no one's. */
const SIDE_COLOR = { ally: '#5fd35f', enemy: '#e04848', none: '#b8b8b8' } as const;

/** The quest mark over each NPC on the map that shows one, kept current with the player's quests. */
export function useNpcMarkers(mapId: string | null): ReadonlyMap<string, Marker> {
  const player = useQuestPlayer();
  return new Map(
    npcs.npcs.flatMap((n) => {
      const marker = n.map === mapId ? markerFor(n, player) : null;
      return marker ? [[n.id, marker] as const] : [];
    }),
  );
}

/** A battle map's bases and points: the player's base and points their side holds in their colour, the others' in the enemy's, the rest grey; a contested point's edge dashed. With no match, all grey. */
function Battle({
  map,
  unit,
  y,
}: {
  readonly map: MapData;
  readonly unit: number;
  readonly y: (z: number) => number;
}) {
  const match = pvp.use((s) => s.match);
  const me = character.use((s) => s.character?.id ?? '');
  const side = sideOf(match, me);
  const held = new Map(match?.points.map((p) => [p.id, p] as const));
  const ours = (s: number) => (side === 0 || s === 0 ? 'none' : s === side ? 'ally' : 'enemy');
  return (
    <g className="map-battle">
      {map.bases.map((b, i) => {
        const color = SIDE_COLOR[ours(i + 1)];
        return (
          <circle
            key={`b${i}`}
            className="map-base"
            cx={b.x}
            cy={y(b.z)}
            r={b.radius}
            fill={color}
            fillOpacity={0.15}
            stroke={color}
            strokeWidth={2 * unit}
          />
        );
      })}
      {map.points.map((p) => {
        const now = held.get(p.id);
        const color = SIDE_COLOR[ours(now?.holder ?? 0)];
        return (
          <circle
            key={p.id}
            className="map-point"
            data-point={p.id}
            cx={p.x}
            cy={y(p.z)}
            r={p.radius}
            fill={color}
            fillOpacity={0.3}
            stroke={color}
            strokeWidth={2 * unit}
            {...(now?.contested && { strokeDasharray: `${4 * unit} ${3 * unit}` })}
          />
        );
      })}
    </g>
  );
}

/** A map other than the one the scene drew: its id, size in metres east and north, and ground. */
export interface OtherMap {
  readonly id: string;
  readonly size: readonly [number, number];
  readonly ground: MapGround;
}

/** The map's ground and marks at `unit` metres a pixel, seen in `view`, the whole map if none: the scene's map, or `other`; children, such as labels, go over the fixed marks and under the moving ones, which show only on the player's own map, as do the bosses' crowns at their spots: `crowns`, else the player's own map's and area's. */
export function MapMarks({
  unit,
  view,
  other,
  crowns,
  children,
}: {
  readonly unit: number;
  readonly view?: MapBox;
  readonly other?: OtherMap;
  readonly crowns?: readonly Crown[];
  readonly children?: ReactNode;
}) {
  const current = mapView.use((s) => s.mapId);
  const size = mapView.use((s) => s.size);
  const ground = mapView.use((s) => s.ground);
  const marks = {
    me: mapView.use((s) => s.me),
    party: mapView.use((s) => s.party),
    lock: mapView.use((s) => s.lock),
    boss: mapView.use((s) => s.boss),
  };
  const down = fieldBoss.use((s) => s.down);
  const area = mapView.use((s) => s.area);
  const mapId = other?.id ?? current;
  const markers = useNpcMarkers(mapId);
  const map = maps.find((m) => m.id === mapId);
  if (!map) {
    return null;
  }
  const [sx, sz] = other?.size ?? size;
  const { me, party, lock, boss } =
    mapId === current ? marks : { me: null, party: [], lock: null, boss: null }; // the client tracks positions only here
  const y = (z: number) => sz - z;
  const dot = 3 * unit; // a dot's radius: 3 pixels
  const spots = crowns ?? crownsOf(map, area, down, null, boss !== null); // the window gives each plan's; the minimap draws the player's own map
  return (
    <>
      <Ground
        ground={other?.ground ?? ground}
        size={[sx, sz]}
        unit={unit}
        view={view ?? { x: 0, y: 0, w: sx, h: sz }}
      />
      {map.areas.map((a) => (
        <polygon
          key={a.id}
          className={`map-area map-area-${a.kind}`}
          data-area={a.id}
          points={a.area.map(([x, z]) => `${x},${y(z)}`).join(' ')}
          strokeWidth={unit}
        />
      ))}
      {map.spawns.map((s, i) => {
        const tint = hex(mobById(s.mob)?.look.tint ?? SPAWN_DEFAULT);
        return (
          <circle
            key={`s${i}`}
            className="map-spawn"
            data-mob={s.mob}
            cx={s.x}
            cy={y(s.z)}
            r={s.radius}
            fill={tint}
            fillOpacity={0.25}
            stroke={tint}
            strokeWidth={unit}
          />
        );
      })}
      {map.portals.map((p) => {
        if (placesBelow(map.id).some((m) => m.id === p.to)) {
          return (
            <path
              key={p.name}
              className="map-portal map-cave"
              data-to={p.to}
              d={CAVE_MOUTH}
              transform={`translate(${p.x} ${y(p.z)}) scale(${2 * dot})`}
              fill="#1a1a22"
              stroke="#7ac7ff"
              strokeWidth={0.25}
            />
          );
        }
        return (
          <circle
            key={p.name}
            className="map-portal"
            data-to={p.to}
            cx={p.x}
            cy={y(p.z)}
            r={Math.max(p.radius, 2 * dot)}
            fill="none"
            stroke="#7ac7ff"
            strokeWidth={2 * unit}
          />
        );
      })}
      {npcs.npcs
        .filter((n) => n.map === mapId)
        .map((n) => {
          const marker = markers.get(n.id);
          return (
            <g key={n.id} className="map-npc" data-npc={n.id}>
              <circle cx={n.x} cy={y(n.z)} r={dot} fill="#ffe08a" />
              {marker && (
                <text
                  className="map-marker"
                  x={n.x}
                  y={y(n.z) - 2 * dot}
                  fontSize={14 * unit}
                  textAnchor="middle"
                  fill="#ffd23f"
                >
                  {MARKER_TEXT[marker]}
                </text>
              )}
            </g>
          );
        })}
      {map.points.length > 0 && <Battle map={map} unit={unit} y={y} />}
      {spots.map((c) => (
        <polygon
          key={`${c.x} ${c.z}`}
          className={c.down ? 'map-boss map-boss-down' : 'map-boss-spot'}
          data-mob={c.mob}
          points={CROWN}
          transform={`translate(${c.x} ${y(c.z)}) scale(${2 * dot})`}
          fill={c.down ? '#8a8a8a' : '#e04040'}
        />
      ))}
      {children}
      <g className="map-live" style={{ pointerEvents: 'none' }}>
        {/* drawn over the marks, but a click goes through to a mark under it */}
        {party.map((p) => (
          <circle
            key={p.id}
            className={p.gate ? 'map-party map-party-gate' : 'map-party'}
            data-id={p.id}
            cx={p.x}
            cy={y(p.z)}
            r={dot}
            fill={p.gate ? 'none' : '#5fd35f'}
            stroke={p.gate ? '#5fd35f' : undefined}
            strokeWidth={p.gate ? unit : undefined}
          />
        ))}
        {lock && (
          <g className="map-lock">
            <circle cx={lock.x} cy={y(lock.z)} r={dot} fill="#ffdb73" />
            <circle
              cx={lock.x}
              cy={y(lock.z)}
              r={2 * dot}
              fill="none"
              stroke="#ffdb73"
              strokeWidth={unit}
            />
          </g>
        )}
        {boss && (
          <polygon
            className="map-boss"
            points={CROWN}
            transform={`translate(${boss.x} ${y(boss.z)}) scale(${2 * dot})`}
            fill="#e04040"
          />
        )}
        {me && (
          <polygon
            className="map-me"
            points="0,-1.4 1,1 0,0.4 -1,1"
            transform={`translate(${me.x} ${y(me.z)}) rotate(${(me.yaw * 180) / Math.PI}) scale(${2 * dot})`}
            fill="#ffffff"
            stroke="#1a1a22"
            strokeWidth={0.15}
          />
        )}
      </g>
    </>
  );
}
