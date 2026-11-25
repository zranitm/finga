// A battle map's marks in the world (pvp.md section 13): each point a ring on the ground and a glow over it in its holder's colour from the player's side, the name tags' green and red, pulsing between the two while contested; and a see-through red wall round the other side's base while the player walks their side's ground.
import { Constants } from '@babylonjs/core/Engines/constants';
import type { InputBlock } from '@babylonjs/core/Materials/Node/Blocks/Input/inputBlock';
import type { NodeMaterial } from '@babylonjs/core/Materials/Node/nodeMaterial';
import { StandardMaterial } from '@babylonjs/core/Materials/standardMaterial';
import { Color3 } from '@babylonjs/core/Maths/math.color';
import { CreateCylinder } from '@babylonjs/core/Meshes/Builders/cylinderBuilder';
import { Mesh } from '@babylonjs/core/Meshes/mesh';
import { VertexData } from '@babylonjs/core/Meshes/mesh.vertexData';
import type { Scene } from '@babylonjs/core/scene';

import { height } from '@/sim/terrain';
import type { MatchPoint } from '@/state/pvp';

import { groundUnder } from './map';
import type { CapturePoint, Circle, WorldMap } from './map';
import { AREA_ORDER } from './see-through';
import type { Palette } from './toon';

type Whose = 'ally' | 'enemy' | 'none' | 'contested';

const COLOR: Record<Exclude<Whose, 'contested'>, Color3> = {
  ally: Color3.FromHexString('#7fe08a'),
  enemy: Color3.FromHexString('#ff6b62'),
  none: Color3.FromHexString('#d8d8d8'),
};
const PULSE_MS = 1000; // a contested point's colour from the player's side's to the other's and back
const RING_M = 0.25; // the ring's width, inside the point's edge
const RING_ALPHA = 0.9;
const FILL_ALPHA = 0.2;
const FILL_GLOW = 0.25; // light added past the fill's alpha, as a boss area's glow
const LIFT_M = 0.04; // just over the ground, as the telegraphs
const STEP_M = 0.5; // the fill's rings apart, a terrain cell
const AROUND = 64; // segments round a point or a base at least

/** Segments round a circle of radius metres: AROUND, or more so its edge runs no more than STEP_M straight between vertices. */
const around = (radius: number) => Math.max(AROUND, Math.ceil((2 * Math.PI * radius) / STEP_M));
const WALL = { alpha: 0.3, over: 3, sink: 0.5 }; // see-through; metres over the base's highest ground and under its lowest

export class BattleMarks {
  private readonly rings: Mesh[];
  private readonly fills: Mesh[];
  private readonly walls: Mesh[];
  private readonly ringLooks: Record<Whose, NodeMaterial>;
  private readonly fillLooks: Record<Whose, NodeMaterial>;
  private readonly wallLook: StandardMaterial;
  private shown = '';

  constructor(
    scene: Scene,
    private readonly map: WorldMap,
    private readonly points: readonly CapturePoint[],
    bases: readonly Circle[],
    palette: Palette,
  ) {
    const looks = (make: (c: Color3) => NodeMaterial) => ({
      ally: make(COLOR.ally),
      enemy: make(COLOR.enemy),
      none: make(COLOR.none),
      contested: make(COLOR.ally), // its colour set each frame it shows
    });
    this.ringLooks = looks((c) => palette.decal(c, RING_ALPHA, Constants.ALPHA_COMBINE));
    this.fillLooks = looks((c) =>
      palette.decal(c.scale(FILL_ALPHA + FILL_GLOW), FILL_ALPHA, Constants.ALPHA_PREMULTIPLIED),
    );
    this.ringLooks.contested.name = 'point contested ring';
    this.fillLooks.contested.name = 'point contested glow';
    this.rings = points.map((p) =>
      this.laid(scene, `point ring ${p.id}`, p, p.radius - RING_M, p.radius),
    );
    this.fills = points.map((p) => this.laid(scene, `point glow ${p.id}`, p, 0, p.radius - RING_M));
    const w = new StandardMaterial('base wall', scene);
    w.disableLighting = true;
    w.emissiveColor = COLOR.enemy;
    w.alpha = WALL.alpha;
    w.alphaMode = Constants.ALPHA_COMBINE;
    w.backFaceCulling = false;
    this.wallLook = w;
    this.walls = bases.map((b, i) => this.wall(scene, `base wall ${i + 1}`, b));
    this.show([], 0, 0, 0);
  }

  /** Each point in its holder's colour from the player's side, grey for no one's, and a contested one between the two sides' at now; and the wall round the other base while the player walks kept, their side's ground, else none. */
  show(held: readonly MatchPoint[], side: number, kept: number, now: number): void {
    const whose = (h: MatchPoint | undefined): Whose => {
      if (h?.contested) {
        return 'contested';
      }
      const holder = h?.holder ?? 0;
      return holder === 0 ? 'none' : holder === side ? 'ally' : 'enemy';
    };
    const looks = this.points.map((p) => whose(held.find((h) => h.id === p.id)));
    if (looks.includes('contested')) {
      this.pulse(now);
    }
    const key = `${looks.join(' ')} ${kept}`;
    if (key === this.shown) {
      return;
    }
    this.shown = key;
    looks.forEach((w, i) => {
      this.rings[i]!.material = this.ringLooks[w];
      this.fills[i]!.material = this.fillLooks[w];
    });
    this.walls.forEach((m, i) => {
      m.setEnabled(i === 2 - kept); // side 1 is kept from base 2; none with 0
    });
  }

  /** The contested look's colour at now: the player's side's on each whole PULSE_MS, the other's halfway. */
  private pulse(now: number): void {
    const toward = (1 - Math.cos((2 * Math.PI * now) / PULSE_MS)) / 2;
    const c = Color3.Lerp(COLOR.ally, COLOR.enemy, toward);
    (this.ringLooks.contested.getBlockByName('color') as InputBlock).value = c;
    (this.fillLooks.contested.getBlockByName('color') as InputBlock).value = c.scale(
      FILL_ALPHA + FILL_GLOW,
    );
  }

  dispose(): void {
    for (const m of [...this.rings, ...this.fills, ...this.walls]) {
      m.dispose();
    }
    for (const m of [...Object.values(this.ringLooks), ...Object.values(this.fillLooks)]) {
      m.dispose();
    }
    this.wallLook.dispose();
  }

  /** A band on the ground round c from inner to outer metres out, a disc from 0, following the ground in rings STEP_M apart. */
  private laid(scene: Scene, name: string, c: Circle, inner: number, outer: number): Mesh {
    const radii = [inner];
    for (let r = inner + STEP_M; r < outer; r += STEP_M) {
      radii.push(r);
    }
    radii.push(outer);
    const centre = groundUnder(this.map, c.x, c.z);
    const segments = around(outer);
    const positions: number[] = [];
    for (const r of radii) {
      for (let k = 0; k <= segments; k++) {
        const a = (k / segments) * 2 * Math.PI;
        const [x, z] = [Math.sin(a) * r, Math.cos(a) * r];
        positions.push(x, groundUnder(this.map, c.x + x, c.z + z) - centre + LIFT_M, z);
      }
    }
    const indices: number[] = [];
    for (let j = 0; j + 1 < radii.length; j++) {
      for (let k = 0; k < segments; k++) {
        const a = j * (segments + 1) + k;
        const b = a + segments + 1;
        indices.push(a, b, a + 1, a + 1, b, b + 1);
      }
    }
    const data = new VertexData();
    data.positions = positions;
    data.indices = indices;
    const m = new Mesh(name, scene);
    data.applyToMesh(m);
    m.position.set(c.x, centre, c.z);
    m.isPickable = false;
    m.alphaIndex = AREA_ORDER;
    return m;
  }

  /** An open wall round base b, from under its lowest ground to WALL.over above its highest. */
  private wall(scene: Scene, name: string, b: Circle): Mesh {
    const t = this.map.geometry.terrain;
    const segments = around(b.radius);
    const ground = Array.from({ length: segments }, (_, k) => {
      const a = (k / segments) * 2 * Math.PI;
      return height(t, b.x + Math.sin(a) * b.radius, b.z + Math.cos(a) * b.radius);
    });
    const bottom = Math.min(...ground) - WALL.sink;
    const top = Math.max(...ground) + WALL.over;
    const m = CreateCylinder(
      name,
      { diameter: 2 * b.radius, height: top - bottom, tessellation: segments, cap: Mesh.NO_CAP },
      scene,
    );
    m.position.set(b.x, (top + bottom) / 2, b.z);
    m.material = this.wallLook;
    m.isPickable = false;
    return m;
  }
}
