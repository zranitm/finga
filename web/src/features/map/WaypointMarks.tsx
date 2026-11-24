// A region's waypoints on a plan (large-regions.md section 8), in its metres as MapMarks draws: each stone a diamond, lit once found and grey till then; picking one names it below the plan.
import type { MapWaypoint } from '@/state/map-view';
import { waypoints as foundWaypoints } from '@/state/waypoints';

const LIT = '#00dce6'; // the stone's crystal, as the bake's debug map marks it
const GREY = '#8a8a8a';
const DIAMOND = '0,-1.4 1,0 0,1.4 -1,0';
const MARK_PX = 5; // a mark's half width in pixels; its half height is 1.4 times it
const RING = 2.2; // a picked mark's ring's radius, in half widths
/** How far This map's labels keep from a mark's spot in pixels: its pick ring, the ring's stroke, and a clear pixel, as drawn text runs a little past its box. */
export const MARK_REACH_PX = RING * MARK_PX + 2;

/** waypoints on a plan of height metres north to south, at unit metres a pixel; picked's ringed, and a click picks one. */
export function WaypointMarks({
  waypoints,
  unit,
  height,
  picked,
  onPick,
}: {
  readonly waypoints: readonly MapWaypoint[];
  readonly unit: number;
  readonly height: number;
  readonly picked: string | null;
  readonly onPick: (id: string) => void;
}) {
  const found = foundWaypoints.use((s) => s.found);
  const size = MARK_PX * unit;
  return (
    <>
      {waypoints.map((w) => {
        const lit = found.has(w.id);
        const y = height - w.z;
        return (
          <g
            key={w.id}
            className={`map-waypoint${lit ? ' map-waypoint-found' : ''}`}
            data-waypoint={w.id}
            role="button"
            tabIndex={0}
            aria-label={w.name}
            aria-pressed={w.id === picked}
            onClick={() => onPick(w.id)}
            onKeyDown={(e) => {
              if (e.key === 'Enter' || e.key === ' ') {
                e.preventDefault();
                onPick(w.id);
              }
            }}
          >
            <title>{w.name}</title>
            {w.id === picked && (
              <circle cx={w.x} cy={y} r={RING * size} fill="none" stroke={LIT} strokeWidth={unit} />
            )}
            <polygon
              points={DIAMOND}
              transform={`translate(${w.x} ${y}) scale(${size})`}
              fill={lit ? LIT : GREY}
              stroke="#1a1a22"
              strokeWidth={0.25}
            />
          </g>
        );
      })}
    </>
  );
}
