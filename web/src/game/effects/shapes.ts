// The drawn shapes until there's art: a skill's box, line or ring, a melee slash, and a ranged attack's arrow or orb.
import type Phaser from 'phaser';

import { movement } from '@/gamedata/movement';
import type { Shape, Skill, Stage } from '@/progression/skills';

import { DEPTH } from '../depth';
import type { Beats } from '../entities/beats';

import type { PlayOptions, Stop } from './types';

const EDGE = 0x1d1a16; // a dark edge under each stroke, so a pale one shows on pale ground too
const SLASH_DEG = 80; // the arc's sweep either side of straight ahead
const BODY_MID = movement.height / 2; // a shot flies, and a line or ring centres, at the caster's middle
const SHOT_PX_PER_MS = 1.4; // a ranged attack's arrow or spell crosses 420 px in 300 ms

export type ShapeDraw =
  | {
      readonly kind: 'box';
      readonly x: number;
      readonly y: number;
      readonly w: number;
      readonly h: number;
      readonly color: number;
    }
  | {
      readonly kind: 'line';
      readonly x1: number;
      readonly x2: number;
      readonly y: number;
      readonly color: number;
    }
  | {
      readonly kind: 'ring';
      readonly x: number;
      readonly y: number;
      readonly r: number;
      readonly color: number;
    }
  | {
      /** An arc in front of the feet at x, through the attack box: its far edge at rx, its top and bottom ry from y. */
      readonly kind: 'slash';
      readonly x: number;
      readonly y: number;
      readonly rx: number;
      readonly ry: number;
      readonly facing: 1 | -1;
      readonly color: number;
    };

// Each apart from the slash's pale cream and from the forest's grass.
const colors = { physical: 0xff6a3d, magic: 0xb070f0, heal: 0x4fd8ff, buff: 0xf0b23a } as const;
export const slashColors = { physical: 0xfff6d8, magic: 0xd0a0ff } as const; // brighter: a basic attack shows no box

/** A skill's colour: its shape's, and the caster's flash at its strike. */
export function colorOf(skill: Skill, stage: Stage): number {
  if (skill.effect === 'heal' || skill.effect === 'buff') {
    return colors[skill.effect];
  }
  return stage.damage === 'magic' ? colors.magic : colors.physical;
}

/** Where a stage reaches from the caster's feet, as the server finds its targets: a box, a line along the facing, a ring. */
export function shapeAt(s: Shape | null, o: PlayOptions, color: number): ShapeDraw | null {
  const middle = o.y - BODY_MID;
  switch (s?.kind) {
    case 'melee': {
      const w = s.width ?? 0;
      const h = s.height ?? 0;
      return { kind: 'box', x: o.facing > 0 ? o.x : o.x - w, y: o.y - h, w, h, color };
    }
    case 'ranged':
      return { kind: 'line', x1: o.x, x2: o.x + o.facing * (s.range ?? 0), y: middle, color };
    case 'area':
      return { kind: 'ring', x: o.x, y: middle, r: s.radius ?? 0, color };
    default:
      return null;
  }
}

/** A melee attack's slash through its box from the caster's feet, as the server finds its targets. */
export function slashAt(
  o: PlayOptions,
  box: { readonly width: number; readonly height: number },
  color: number,
): ShapeDraw {
  return {
    kind: 'slash',
    x: o.x,
    y: o.y - box.height / 2,
    rx: box.width,
    ry: box.height / 2,
    facing: o.facing,
    color,
  };
}

/** Draws the shape at full strength through the strike, faded by the cancel point; stop takes it away sooner. */
export function drawShape(scene: Phaser.Scene, e: ShapeDraw, b: Beats, now: number): Stop {
  const shapes: (Phaser.GameObjects.Shape | Phaser.GameObjects.Graphics)[] = [];
  switch (e.kind) {
    case 'box':
      shapes.push(
        scene.add
          .rectangle(e.x + e.w / 2, e.y + e.h / 2, e.w, e.h, e.color, 0.5)
          .setStrokeStyle(3, e.color),
      );
      break;
    case 'line':
      shapes.push(
        scene.add.line(0, 0, e.x1, e.y, e.x2, e.y, EDGE, 0.7).setOrigin(0, 0).setLineWidth(9),
        scene.add.line(0, 0, e.x1, e.y, e.x2, e.y, e.color).setOrigin(0, 0).setLineWidth(5),
      );
      break;
    case 'ring':
      shapes.push(
        scene.add.circle(e.x, e.y, e.r).setStrokeStyle(7, EDGE, 0.6),
        scene.add.circle(e.x, e.y, e.r, e.color, 0.15).setStrokeStyle(4, e.color),
      );
      break;
    case 'slash':
      shapes.push(slash(scene, e));
      break;
  }
  for (const shape of shapes) {
    shape.setDepth(DEPTH.effect);
  }
  const remove = () => {
    scene.tweens.killTweensOf(shapes);
    shapes.forEach((shape) => shape.destroy());
  };
  const fadeFrom = Math.max(now, b.strikeEnd);
  scene.tweens.add({
    targets: shapes,
    alpha: 0,
    delay: fadeFrom - now,
    duration: Math.max(1, b.cancelAt - fadeFrom),
    onComplete: remove,
  });
  return remove;
}

/** A ranged attack's arrow or orb, flying its range from the caster's middle along the facing. */
export function drawShot(scene: Phaser.Scene, o: PlayOptions, range: number, magic: boolean): Stop {
  const y = o.y - BODY_MID;
  const shape = magic
    ? scene.add.circle(o.x, y, 5, 0xb070f0)
    : scene.add.rectangle(o.x, y, 16, 3, 0xf0ead6);
  shape.setDepth(DEPTH.effect);
  const remove = () => {
    scene.tweens.killTweensOf(shape);
    shape.destroy();
  };
  scene.tweens.add({
    targets: shape,
    x: o.x + o.facing * range,
    duration: range / SHOT_PX_PER_MS,
    onComplete: remove,
  });
  return remove;
}

/** A thick crescent: the arc, with a thinner one inside it, each over a dark edge. */
function slash(
  scene: Phaser.Scene,
  e: Extract<ShapeDraw, { kind: 'slash' }>,
): Phaser.GameObjects.Graphics {
  const g = scene.add.graphics();
  const arc = (share: number, width: number, color: number, alpha: number) => {
    g.lineStyle(width, color, alpha).beginPath();
    for (let deg = -SLASH_DEG; deg <= SLASH_DEG; deg += 10) {
      const a = (deg * Math.PI) / 180;
      const px = e.x + e.facing * e.rx * share * Math.cos(a);
      const py = e.y + e.ry * share * Math.sin(a);
      if (deg === -SLASH_DEG) {
        g.moveTo(px, py);
      } else {
        g.lineTo(px, py);
      }
    }
    g.strokePath();
  };
  arc(1, 11, EDGE, 0.55);
  arc(1, 6, e.color, 1);
  arc(0.7, 6, EDGE, 0.45);
  arc(0.7, 3, e.color, 0.85);
  return g;
}
