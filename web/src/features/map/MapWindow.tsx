// The map window (map-views.md sections 6 and 12): This map, the whole map with its marks named and, in a region, its channels and waypoints (large-regions.md sections 4 and 8), and World, every map on the chart linked by its portals, any of which opens its layout.
import { useEffect, useRef, useState } from 'react';
import type { CSSProperties } from 'react';

import { problemOf } from '@/features/shared/problem';
import { useNow } from '@/features/shared/useNow';
import { timeLeft } from '@/features/shared/words';
import { WorldWindow } from '@/features/shared/WorldWindow';
import { mapLinks } from '@/gamedata/map-links';
import { maps } from '@/gamedata/maps';
import { npcs } from '@/gamedata/npcs';
import { clashes, stackTags, TAG_GAP } from '@/lib/tags';
import type { TagBox } from '@/lib/tags';
import type { GameSocket } from '@/net/socket';
import { mobById } from '@/progression/mobs';
import { mobName } from '@/progression/names';
import type { ChannelList } from '@/protocol/messages';
import { character } from '@/state/character';
import { fieldBoss } from '@/state/field-boss';
import type { BossDown } from '@/state/field-boss';
import { mapView } from '@/state/map-view';
import type { MapGround, MapWaypoint, Spot } from '@/state/map-view';
import { party } from '@/state/party';
import { pvp } from '@/state/pvp';
import { teleport } from '@/state/teleport';
import type { Destination } from '@/state/teleport';
import { toasts } from '@/state/toasts';
import { waypoints } from '@/state/waypoints';

import { crownsOf, useAreaBosses } from './crowns';
import type { AreaBosses, Crown } from './crowns';
import { MapMarks, useNpcMarkers } from './MapMarks';
import { usePanZoom, ZOOM_STEP } from './pan-zoom';
import { MARK_REACH_PX, WaypointMarks } from './WaypointMarks';

type MapData = (typeof maps)[number];
type Tab = 'here' | 'world';

const CHANNELS_EVERY_MS = 5000; // a region's channel list, asked for again while This map shows it
const VIEW_W = 480; // This map's largest size, in pixels
const VIEW_H = 320;
const LABEL_PX = 12;
const CHAR_PX = 6.5; // a label's width a character, near enough for keeping labels apart
const DOT_PX = 3;
const CHART_PX = 5; // pixels a unit of the world chart
const CHART_PAD = 10; // chart units round the outermost maps
const GROUP_GAP_M = 6; // spawn circles of one mob this close at their edges share one label
const LABEL_REACH_M = 8; // every spawn circle has a label of its own mob on it, or this close to its edge
const BOSS_ROOM_PX = 6; // more room than tags keep round a beaten boss's label, so its countdown reads apart

const nameOf = (id: string) => maps.find((m) => m.id === id)?.name ?? id;

type Charted = (typeof maps)[number] & { readonly place: readonly [number, number] };

/** The maps on the world chart: every one but a match's, which only a match reaches. */
const charted = maps.filter((m): m is Charted => m.place !== null);
const levels = (m: MapData) => `Lv ${m.min_level}–${m.max_level}`;
/** What a map is, under its name: a town, a region, whose areas have levels of their own, or a hunting map's levels. */
const kindOf = (m: MapData) =>
  m.kind === 'town' ? 'Town' : m.kind === 'region' ? 'Region' : levels(m);

/** A label on This map, in pixels: its text centred on x, sitting on bottom; a beaten boss's counts down to its time back. */
export interface MapLabel extends TagBox {
  readonly kind: 'spawn' | 'area' | 'town' | 'portal' | 'npc' | 'boss' | 'me';
  readonly text: string;
  readonly down?: BossDown;
}

type Spawn = MapData['spawns'][number];

/** Where a region's area is named: the middle of its corners' polygon, by area. */
function areaMiddle(corners: readonly (readonly [number, number])[]): { x: number; z: number } {
  let twice = 0;
  let cx = 0;
  let cz = 0;
  corners.forEach(([x0, z0], i) => {
    const [x1, z1] = corners[(i + 1) % corners.length]!;
    const cross = x0 * z1 - x1 * z0;
    twice += cross;
    cx += (x0 + x1) * cross;
    cz += (z0 + z1) * cross;
  });
  return { x: cx / (3 * twice), z: cz / (3 * twice) };
}

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

/** This map's labels at scale pixels a metre. A spawn group's label sits on one of its own circles, nearest the group's middle first, stepped up or down inside the circle, wherever it's in LABEL_REACH_M of all the group's circles and covers no quest mark, waypoint mark or label before it; failing that, it's lifted. A region's areas, named at their middles with a hunting area's levels, then portals and NPCs follow, each lifted clear of everything before it; then each beaten boss's, under its crown or over it, wherever it has BOSS_ROOM_PX round it, else lifted as the rest are; then the player's. */
export function mapLabels(
  map: MapData,
  me: Spot | null,
  scale: number,
  size: readonly [number, number],
  marked: ReadonlySet<string>,
  crowns: readonly Crown[],
  stones: readonly Spot[],
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
  const placed: TagBox[] = [
    ...here
      .filter((n) => marked.has(n.id))
      .map((n) => ({ x: n.x * scale, bottom: y(n.z) - 2 * DOT_PX, w: 10, h: 14 })),
    ...stones.map((w) => ({
      x: w.x * scale,
      bottom: y(w.z) + MARK_REACH_PX,
      w: 2 * MARK_REACH_PX,
      h: 2 * MARK_REACH_PX,
    })),
  ];
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
  const marks: MapLabel[] = [
    ...map.areas.map((a) => {
      const middle = areaMiddle(a.area);
      const bottom = y(middle.z) + LABEL_PX / 2; // centred on it
      return a.levels
        ? box('area', `${a.name} Lv ${a.levels[0]}–${a.levels[1]}`, middle.x, bottom)
        : box('town', a.name, middle.x, bottom);
    }),
    ...map.portals.map((p) =>
      box('portal', `To ${nameOf(p.to)}`, p.x, y(p.z) - Math.max(p.radius * scale, 2 * DOT_PX) - 2),
    ),
    ...here.map((n) => box('npc', n.name, n.x, y(n.z) - DOT_PX - 2)),
  ];
  const markBottoms = stackTags(placed, marks);
  const settled = marks.map((l, i) => ({ ...l, bottom: markBottoms[i]! }));
  const around = [...placed, ...settled];
  const roomy = (t: TagBox) =>
    !around.some((p) =>
      clashes({ ...t, bottom: t.bottom + BOSS_ROOM_PX, h: t.h + 2 * BOSS_ROOM_PX }, p),
    );
  const last: MapLabel[] = [];
  for (const c of crowns) {
    if (!c.down) {
      continue;
    }
    const text = backIn(c.down.type, 0);
    const [under, over] = [
      box('boss', text, c.x, y(c.z) + 2 * DOT_PX + LABEL_PX),
      box('boss', text, c.x, y(c.z) - 3 * DOT_PX - 2),
    ];
    last.push({ ...([under, over].find(roomy) ?? over), down: c.down });
  }
  if (me) {
    last.push(box('me', 'You', me.x, y(me.z) - 3 * DOT_PX - 2));
  }
  const bottoms = stackTags(around, last);
  return [...spawns, ...settled, ...last.map((l, i) => ({ ...l, bottom: bottoms[i]! }))];
}

/** A beaten boss's label: its name and the time until it's back. */
const backIn = (type: string, ms: number) => `${mobName(type)}: back in ${timeLeft(ms)}`;

/** The beaten boss's label text, counting down each second. */
function BackIn({ down }: { readonly down: BossDown }) {
  const now = useNow(1000);
  return backIn(down.type, down.backAt - now);
}

/** A map's size in metres east and north, its ground and a region's waypoints, which the map window is given for a map the scene hasn't drawn. */
export interface MapLayout {
  readonly size: readonly [number, number];
  readonly ground: MapGround;
  readonly waypoints: readonly MapWaypoint[];
}

/** A channel asked of the server by ask: its answer starts the bar for to and closes the window; a refusal says why as a notice and leaves the window open. */
function useChannel(onClose: () => void) {
  const [asking, setAsking] = useState(false);
  const start = (ask: () => Promise<unknown>, to: Destination) => {
    setAsking(true);
    ask().then(
      () => {
        teleport.getState().start(to);
        onClose();
      },
      (err: unknown) => {
        setAsking(false);
        toasts.getState().push(problemOf(err));
      },
    );
  };
  return { asking, start };
}

/** map's layout to fit the window: the scene's map, or one from its layout; the player's label and the live marks only on their own. A region's waypoints are marked, picked's ringed, and a region pans and zooms. */
function Plan({
  map,
  layout,
  others,
  picked,
  onPick,
}: {
  readonly map: MapData;
  readonly layout?: MapLayout;
  readonly others?: AreaBosses | null;
  readonly picked: string | null;
  readonly onPick: (id: string) => void;
}) {
  const current = mapView.use((s) => s.mapId);
  const size = mapView.use((s) => s.size);
  const shown = mapView.use((s) => s.waypoints);
  const marks = layout?.waypoints ?? (map.id === current ? shown : []);
  const me = mapView.use((s) => s.me);
  const down = fieldBoss.use((s) => s.down);
  const area = mapView.use((s) => s.area);
  const crowns = map.id === current ? crownsOf(map, area, down, others ?? null) : [];
  const markers = useNpcMarkers(map.id);
  const ground = mapView.use((s) => s.ground);
  const [sx, sz] = layout?.size ?? size;
  const fit = Math.min(VIEW_W / sx, VIEW_H / sz);
  const svg = useRef<SVGSVGElement>(null);
  const { view, zoom, most, zoomBy, onPointerDown, onClickCapture } = usePanZoom(
    svg,
    [sx, sz],
    fit,
    (layout?.ground ?? ground).kind === 'tiles',
  );
  const scale = fit * zoom; // pixels a metre as drawn
  const labels = mapLabels(
    map,
    map.id === current ? me : null,
    scale,
    [sx, sz],
    new Set(markers.keys()),
    crowns,
    marks,
  );
  const plan = (
    <svg
      ref={svg}
      className={most > 1 ? 'map-here map-here-pans' : 'map-here'}
      role="img"
      aria-label={map.name}
      width={sx * fit}
      height={sz * fit}
      viewBox={`${view.x} ${view.y} ${view.w} ${view.h}`}
      onPointerDown={onPointerDown}
      onClickCapture={onClickCapture}
    >
      <MapMarks
        unit={1 / scale}
        crowns={crowns}
        view={view}
        {...(layout && { other: { id: map.id, ...layout } })}
      >
        {labels.map((l, i) => (
          <text
            key={i}
            className={`map-label map-label-${l.kind}`}
            x={l.x / scale}
            y={l.bottom / scale}
            fontSize={LABEL_PX / scale}
            textAnchor="middle"
          >
            {l.down ? <BackIn down={l.down} /> : l.text}
          </text>
        ))}
        <WaypointMarks
          waypoints={marks}
          unit={1 / scale}
          height={sz}
          picked={picked}
          onPick={onPick}
        />
      </MapMarks>
    </svg>
  );
  if (most <= 1) {
    return plan;
  }
  return (
    <div className="map-plan">
      {plan}
      <div className="map-zoom" role="group" aria-label="Zoom">
        <button
          type="button"
          aria-label="Zoom in"
          disabled={zoom >= most}
          onClick={() => zoomBy(ZOOM_STEP)}
        >
          +
        </button>
        <button
          type="button"
          aria-label="Zoom out"
          disabled={zoom <= 1}
          onClick={() => zoomBy(1 / ZOOM_STEP)}
        >
          −
        </button>
      </div>
    </div>
  );
}

function ThisMap({ socket, onClose }: Pick<WorldProps, 'socket' | 'onClose'>) {
  const mapId = mapView.use((s) => s.mapId);
  const channel = mapView.use((s) => s.channel);
  const marks = mapView.use((s) => s.waypoints);
  const [picked, setPicked] = useState<string | null>(null);
  const map = maps.find((m) => m.id === mapId);
  const others = useAreaBosses(socket, map?.kind === 'region');
  if (!map) {
    return <p className="muted">No map yet.</p>;
  }
  const pick = marks.find((w) => w.id === picked); // ids are unique across regions, so a pick goes with its map
  return (
    <>
      {channel > 0 && <Channels socket={socket} onClose={onClose} />}
      {/* keyed so a new map's plan starts whole */}
      <Plan key={map.id} map={map} others={others} picked={pick?.id ?? null} onPick={setPicked} />
      {pick && <WaypointPick waypoint={pick} socket={socket} onClose={onClose} />}
    </>
  );
}

/** The waypoint picked on a plan: its name, whether it's found, and Teleport here where the server takes one, to a found waypoint or a main town's, the player alive (large-regions.md section 8). */
function WaypointPick({
  waypoint,
  socket,
  onClose,
}: { readonly waypoint: MapWaypoint } & Pick<WorldProps, 'socket' | 'onClose'>) {
  const found = waypoints.use((s) => s.found.has(waypoint.id));
  const dead = character.use((s) => s.dead);
  const { asking, start } = useChannel(onClose);
  const go = () =>
    start(() => socket.request('teleport', { waypoint: waypoint.id }, socket.sequence), {
      waypoint: waypoint.id,
      name: waypoint.name,
    });
  return (
    <section className="map-waypoint-pick" aria-label="Waypoint">
      <strong>{waypoint.name}</strong>
      <span className="muted">
        {found ? 'Found' : waypoint.main ? 'Main town, always open' : 'Not found yet'}
      </span>
      {(found || waypoint.main) && !dead && (
        <button type="button" className="btn" disabled={asking} onClick={go}>
          Teleport here
        </button>
      )}
    </section>
  );
}

/** How full a channel is, in words: quiet under half the cap, busy below it, full at it. */
function load(players: number, cap: number): 'quiet' | 'busy' | 'full' {
  if (players >= cap) {
    return 'full';
  }
  return players < cap / 2 ? 'quiet' : 'busy';
}

/** A region's channels: the player's own, and each open one with how full it is and, but for the player's own and a full one, a switch to it, which channels as a teleport does. The list is asked for as it shows and every CHANNELS_EVERY_MS. */
function Channels({ socket, onClose }: Pick<WorldProps, 'socket' | 'onClose'>) {
  const mine = mapView.use((s) => s.channel);
  const dead = character.use((s) => s.dead);
  const [list, setList] = useState<ChannelList | null>(null);
  const { asking, start } = useChannel(onClose);
  useEffect(() => {
    let on = true;
    const ask = () => {
      socket.request('channels', {}).then(
        (r) => on && r.channels && setList(r.channels),
        () => undefined, // the next ask tries again
      );
    };
    ask();
    const every = setInterval(ask, CHANNELS_EVERY_MS);
    return () => {
      on = false;
      clearInterval(every);
    };
  }, [socket]);
  const go = (n: number) =>
    start(() => socket.request('switchChannel', { channel: n }, socket.sequence), { channel: n });
  return (
    <section className="map-channels" aria-label="Channels">
      <strong>Channel {mine}</strong>
      {list && (
        <ul>
          {list.channels.map((c) => {
            const word = load(c.players, list.cap);
            return (
              <li key={c.channel} data-channel={c.channel}>
                <span>Channel {c.channel}</span>
                <span className={`map-channel-load map-channel-${word}`}>{word}</span>
                {c.channel !== mine && word !== 'full' && !dead && (
                  <button
                    type="button"
                    className="btn"
                    disabled={asking}
                    onClick={() => go(c.channel)}
                  >
                    Switch
                  </button>
                )}
              </li>
            );
          })}
        </ul>
      )}
    </section>
  );
}

/** What the World tab needs from the app: each map's layout, the socket a teleport goes by, and closing the window. */
interface WorldProps {
  readonly socket: GameSocket;
  readonly layoutOf: (id: string) => Promise<MapLayout>;
  readonly onClose: () => void;
}

/** A map picked on the chart: a header with Back, Teleport here off the player's own map, and its party members, over its layout, which the scene's map draws from what it has and another from layoutOf. */
function Picked({
  map,
  socket,
  layoutOf,
  onClose,
  onBack,
}: WorldProps & { readonly map: MapData; readonly onBack: () => void }) {
  const own = mapView.use((s) => s.mapId) === map.id;
  const dead = character.use((s) => s.dead);
  const shown = mapView.use((s) => s.waypoints);
  const { asking, start } = useChannel(onClose);
  const [picked, setPicked] = useState<string | null>(null);
  const go = () =>
    start(() => socket.request('teleport', { mapId: map.id }, socket.sequence), { map: map.id });
  const myId = character.use((s) => s.character?.id);
  const members = party.use((s) => s.party?.members) ?? [];
  const [layout, setLayout] = useState<MapLayout | 'failed' | null>(null);
  useEffect(() => {
    if (own) {
      return undefined;
    }
    let on = true;
    layoutOf(map.id).then(
      (l) => on && setLayout(l),
      () => on && setLayout('failed'),
    );
    return () => {
      on = false;
    };
  }, [map.id, own, layoutOf]);
  const here = own
    ? []
    : members.filter((p) => p.online && p.mapId === map.id && p.characterId !== myId);
  const marks = own ? shown : layout && layout !== 'failed' ? layout.waypoints : [];
  const pick = marks.find((w) => w.id === picked);
  return (
    <div className="map-picked">
      <div className="map-picked-head">
        <button type="button" className="btn btn-quiet" onClick={onBack}>
          Back
        </button>
        <strong>{map.name}</strong>
        <span className="muted">{kindOf(map)}</span>
        {!own &&
          !dead &&
          map.kind !== 'region' && ( // a region is reached by its waypoints
            <button type="button" className="btn" disabled={asking} onClick={go}>
              Teleport here
            </button>
          )}
      </div>
      {here.length > 0 && (
        <ul className="map-picked-party" aria-label={`Party in ${map.name}`}>
          {here.map((p) => (
            <li key={p.characterId}>{p.name}</li>
          ))}
        </ul>
      )}
      {own ? (
        <Plan map={map} picked={pick?.id ?? null} onPick={setPicked} />
      ) : layout === 'failed' ? (
        <p className="muted">The map didn’t load.</p>
      ) : layout ? (
        <Plan map={map} layout={layout} picked={pick?.id ?? null} onPick={setPicked} />
      ) : (
        <p className="muted">Loading…</p>
      )}
      {pick && <WaypointPick waypoint={pick} socket={socket} onClose={onClose} />}
    </div>
  );
}

/** A node's card beside it, toward the chart's middle, so it stays inside the window. */
function cardAt(x: number, y: number, w: number, h: number): CSSProperties {
  return {
    ...(x < w / 2 ? { left: x + 16 } : { right: w - x + 16 }),
    ...(y < h / 2 ? { top: y - 8 } : { bottom: h - y - 8 }),
  };
}

function World(props: WorldProps) {
  const here = mapView.use((s) => s.mapId);
  const [picked, setPicked] = useState<string | null>(null);
  const myId = character.use((s) => s.character?.id);
  const members = party.use((s) => s.party?.members) ?? [];
  const [shown, setShown] = useState<string | null>(null);
  const xs = charted.map((m) => m.place[0]);
  const ys = charted.map((m) => m.place[1]);
  const [x0, y0] = [Math.min(...xs) - CHART_PAD, Math.min(...ys) - CHART_PAD];
  const [w, h] = [Math.max(...xs) + CHART_PAD - x0, Math.max(...ys) + CHART_PAD - y0];
  const at = (id: string) => charted.find((m) => m.id === id)?.place ?? [0, 0];
  const card = charted.find((m) => m.id === shown);
  const pick = charted.find((m) => m.id === picked);
  if (pick) {
    return <Picked map={pick} {...props} onBack={() => setPicked(null)} />;
  }
  return (
    <div className="map-world">
      <svg
        role="group"
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
        {charted.map((m) => {
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
              role="button"
              aria-label={m.name}
              onClick={() => setPicked(m.id)}
              onKeyDown={(e) => {
                if (e.key === 'Enter' || e.key === ' ') {
                  e.preventDefault();
                  setPicked(m.id);
                }
              }}
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
                  {kindOf(m)}
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
          <div className="muted">{kindOf(card)}</div>
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

/** layoutOf gives a map's layout for the World tab to draw when it's picked, and a teleport there goes by socket. */
export function MapWindow({ socket, layoutOf, onClose }: WorldProps) {
  const [chosen, setTab] = useState<Tab>('here');
  const inMatch = pvp.use((s) => s.match !== null);
  const mapId = mapView.use((s) => s.mapId);
  const inRegion = maps.find((m) => m.id === mapId)?.kind === 'region';
  const tab = inMatch ? 'here' : chosen; // a match's map isn't on the world chart (pvp.md section 13)
  return (
    <WorldWindow name="map" title="Map" onClose={onClose} initialX={360} initialY={80}>
      <div className="map-window">
        <div className="inventory-tabs" role="tablist">
          {(inMatch ? (['here'] as const) : (['here', 'world'] as const)).map((t) => (
            <button
              key={t}
              type="button"
              role="tab"
              aria-selected={t === tab}
              className={`btn btn-quiet${t === tab ? ' tab-active' : ''}`}
              onClick={() => setTab(t)}
            >
              {t === 'world' ? 'World' : inRegion ? 'This region' : 'This map'}
            </button>
          ))}
        </div>
        {tab === 'here' ? (
          <ThisMap socket={socket} onClose={onClose} />
        ) : (
          <World socket={socket} layoutOf={layoutOf} onClose={onClose} />
        )}
      </div>
    </WorldWindow>
  );
}
