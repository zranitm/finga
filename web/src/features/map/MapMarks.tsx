// A map's ground and marks as SVG in metres, x east and y south from the map's north edge, shared by the minimap and the map window (map-views.md sections 5 and 6).
import type { ReactNode } from 'react';

import { useQuestPlayer } from '@/features/shared/useQuestPlayer';
import { maps } from '@/gamedata/maps';
import { npcs } from '@/gamedata/npcs';
import { mobById } from '@/progression/mobs';
import { MARKER_TEXT, markerFor } from '@/progression/quests';
import type { Marker } from '@/progression/quests';
import { mapView } from '@/state/map-view';

const SPAWN_DEFAULT = 0x9a9a9a; // an untinted mob's circle

const hex = (c: number) => `#${c.toString(16).padStart(6, '0')}`;

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

/** The map's ground and marks at `unit` metres a pixel; children, such as labels, go over the fixed marks and under the moving ones. */
export function MapMarks({
  unit,
  children,
}: {
  readonly unit: number;
  readonly children?: ReactNode;
}) {
  const mapId = mapView.use((s) => s.mapId);
  const [sx, sz] = mapView.use((s) => s.size);
  const image = mapView.use((s) => s.image);
  const me = mapView.use((s) => s.me);
  const party = mapView.use((s) => s.party);
  const lock = mapView.use((s) => s.lock);
  const boss = mapView.use((s) => s.boss);
  const markers = useNpcMarkers(mapId);
  const map = maps.find((m) => m.id === mapId);
  if (!map) {
    return null;
  }
  const y = (z: number) => sz - z;
  const dot = 3 * unit; // a dot's radius: 3 pixels
  return (
    <>
      {image && (
        <image href={image} x={0} y={0} width={sx} height={sz} preserveAspectRatio="none" />
      )}
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
      {map.portals.map((p) => (
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
      ))}
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
      {children}
      {party.map((p) => (
        <circle
          key={p.id}
          className="map-party"
          data-id={p.id}
          cx={p.x}
          cy={y(p.z)}
          r={dot}
          fill="#5fd35f"
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
          points="-1,0.6 -1,-0.6 -0.5,0 0,-0.9 0.5,0 1,-0.6 1,0.6"
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
    </>
  );
}
