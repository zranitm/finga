// A mob with no model yet (3d-world.md section 11): primitives of its family's shape filling its circle and height, never wider, in its tint, toon-drawn and inked, moved by hand.
import { Color3 } from '@babylonjs/core/Maths/math.color';
import { Vector3 } from '@babylonjs/core/Maths/math.vector';
import { CreateBox } from '@babylonjs/core/Meshes/Builders/boxBuilder';
import { CreateCapsule } from '@babylonjs/core/Meshes/Builders/capsuleBuilder';
import { CreateSphere } from '@babylonjs/core/Meshes/Builders/sphereBuilder';
import type { Mesh } from '@babylonjs/core/Meshes/mesh';
import { TransformNode } from '@babylonjs/core/Meshes/transformNode';
import type { Scene } from '@babylonjs/core/scene';

import { heldTime } from '../systems/hit-stop';
import type { Hold } from '../systems/hit-stop';

import type { Beats } from './beats';
import type { Figure, Gait, Lighting, Pose } from './body';
import { RISE_MS } from './body';
import type { StandInLook } from './looks';
import { linearOf } from './toon';

const EYE = 0xf4f4f4;
const PUPIL = 0x1a1a22;
const FLINCH_MS = 300;
const LUNGE = 0.25; // of its radius, forward at the strike
const BOB = 0.04; // of its height, up and down as it idles; twice as it walks
const FLAP_HZ = 4;
const FORWARD = new Vector3(0, 0, 1); // a lying capsule's axis
const FIT = 0.98; // of its radius, the farthest any part reaches from its centre, as a model's

/** A stand-in's motion this frame, from its act and pose. */
interface Motion {
  readonly lunge: number; // 0 to 1
  readonly tilt: number; // 0 standing to 1 lying
  readonly shake: number; // metres side to side
}

export class StandIn implements Figure {
  readonly root: TransformNode;
  private readonly body: TransformNode; // bobs, lunges and tilts within the root
  private readonly wings: Mesh[] = [];
  private beats: Beats | null = null;
  private posed: { readonly pose: Pose; readonly at: number } = { pose: 'up', at: -Infinity };
  private walked = 0; // ms of walking, which paces the bob
  private held: Hold | null = null;

  constructor(
    scene: Scene,
    private readonly look: StandInLook,
    lighting: Lighting,
  ) {
    this.root = new TransformNode(`stand-in ${look.shape}`, scene);
    this.body = new TransformNode('body', scene);
    this.body.parent = this.root;
    const toon = (hex: number) => lighting.palette.toon({ texture: null, color: linearOf(hex) });
    const skin = toon(look.color);
    const r = look.radius;
    const h = look.height;
    const part = (m: Mesh, color = skin) => {
      m.parent = this.body;
      m.material = color;
      lighting.palette.outline(m);
      lighting.shadows.addShadowCaster(m, false);
      return m;
    };
    let eyesAt = { y: h * 0.6, z: r * 0.8, size: r * 0.3 };
    switch (look.shape) {
      case 'flyer': {
        const b = part(
          CreateSphere(
            'body',
            { diameterX: r * 0.8, diameterY: h * 0.5, diameterZ: r * 0.8 },
            scene,
          ),
        );
        b.position.y = h * 0.6;
        const span = r * 0.7; // each wing, from inside the body out to the fit
        for (const side of [-1, 1]) {
          const w = part(
            CreateBox('wing', { width: span, height: h * 0.06, depth: r * 0.6 }, scene),
          );
          w.position.set(side * (r * FIT - span / 2), h * 0.65, 0);
          w.setPivotPoint(new Vector3((-side * span) / 2, 0, 0));
          this.wings.push(w);
        }
        eyesAt = { y: h * 0.65, z: r * 0.36, size: r * 0.2 };
        break;
      }
      case 'shell': {
        const foot = part(
          CreateCapsule(
            'foot',
            { radius: r * 0.45, height: 2 * r * FIT, orientation: FORWARD },
            scene,
          ),
        );
        foot.position.set(0, r * 0.45, 0);
        const d = Math.min(2 * r, h) * 0.85;
        const shell = part(CreateSphere('shell', { diameter: d }, scene), toon(darker(look.color)));
        shell.position.set(0, h * 0.5, d / 2 - r * FIT); // its back on the fit
        eyesAt = { y: r * 0.95, z: r * 0.7, size: r * 0.22 };
        break;
      }
      case 'beast': {
        const size = h * 0.1;
        const headAt = r * FIT - h * 0.22 - size * 0.6; // the pupils' front on the fit
        const length = headAt + r * FIT; // the body from the fit behind to under the head
        const b = part(
          CreateCapsule(
            'body',
            { radius: Math.min(h * 0.32, length / 2), height: length, orientation: FORWARD },
            scene,
          ),
        );
        b.position.set(0, h * 0.4, headAt - length / 2);
        const head = part(CreateSphere('head', { diameter: h * 0.5 }, scene));
        head.position.set(0, h * 0.62, headAt);
        eyesAt = { y: h * 0.7, z: headAt + h * 0.22, size };
        break;
      }
      case 'brute': {
        const d = Math.min(r * 1.1, h * 0.35); // the head, sitting on the body within its height
        const tall = h - d * 0.7;
        const round = Math.min(r * 0.85, tall / 2);
        const b = part(CreateCapsule('body', { radius: round, height: tall }, scene));
        b.position.y = tall / 2;
        b.scaling.x = b.scaling.z = (r * 0.85) / round; // as broad as its circle, however short
        const head = part(CreateSphere('head', { diameter: d }, scene));
        head.position.y = h - d / 2;
        eyesAt = { y: h - d / 2, z: d * 0.45, size: d * 0.2 };
        break;
      }
    }
    for (const side of [-1, 1]) {
      const eye = part(CreateSphere('eye', { diameter: eyesAt.size }, scene), toon(EYE));
      eye.position.set(side * eyesAt.size * 0.75, eyesAt.y, eyesAt.z);
      const pupil = part(
        CreateSphere('pupil', { diameter: eyesAt.size * 0.5 }, scene),
        toon(PUPIL),
      );
      pupil.position.set(side * eyesAt.size * 0.75, eyesAt.y, eyesAt.z + eyesAt.size * 0.35);
    }
  }

  act(_kind: 'attack' | 'skill', beats: Beats): void {
    this.beats = beats;
  }

  pose(p: Pose, now: number): void {
    this.posed = { pose: p, at: now };
  }

  hold(h: Hold): void {
    this.held = h;
  }

  update(now: number, dtMs: number, gait: Gait): void {
    const moving = gait.pace !== 'still';
    this.walked = moving ? this.walked + dtMs : 0;
    const m = this.motion(heldTime(this.held, now));
    const h = this.look.height;
    const bob = (moving ? 2 : 1) * BOB * h * Math.abs(Math.sin((moving ? this.walked : now) / 160));
    this.body.position.set(m.shake, m.tilt === 0 ? bob : 0, m.lunge * LUNGE * this.look.radius);
    this.body.rotation.z = (m.tilt * Math.PI) / 2;
    this.body.position.y -= m.tilt * h * 0.25;
    for (const [i, w] of this.wings.entries()) {
      w.rotation.z =
        (i === 0 ? 1 : -1) * 0.5 * Math.sin((now / 1000) * FLAP_HZ * 2 * Math.PI) * (1 - m.tilt);
    }
  }

  /** A stand-in leaves no afterimage: only players cancel. */
  afterimage(): null {
    return null;
  }

  dispose(): void {
    this.root.dispose(); // its materials stay: the palette shares them
  }

  private motion(now: number): Motion {
    const b = this.beats;
    let lunge = 0;
    if (b && now >= b.start && now < b.readyAt) {
      lunge =
        now < b.hit
          ? (now - b.start) / Math.max(1, b.hit - b.start)
          : 1 - (now - b.hit) / Math.max(1, b.readyAt - b.hit);
    }
    const { pose, at } = this.posed;
    const since = now - at;
    switch (pose) {
      case 'flinch':
        return { lunge: 0, tilt: 0, shake: since < FLINCH_MS ? 0.05 * Math.sin(since / 15) : 0 };
      case 'down':
      case 'dead':
        return { lunge: 0, tilt: Math.min(1, since / 200), shake: 0 };
      case 'rising':
        return { lunge, tilt: Math.max(0, 1 - since / RISE_MS), shake: 0 };
      case 'up':
        return { lunge, tilt: 0, shake: 0 };
    }
  }
}

/** hex a third darker, for a shell on its body. */
function darker(hex: number): number {
  const c = Color3.FromHexString(`#${hex.toString(16).padStart(6, '0')}`).scale(0.66);
  return parseInt(c.toHexString().slice(1), 16);
}
