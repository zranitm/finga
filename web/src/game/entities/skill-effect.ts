// An action drawn as a simple shape until there's art: a skill's box, line or ring, or a melee attack's slash arc, at full strength through the strike and gone by the cancel point.
import type Phaser from 'phaser';

import type { Skill, Stage } from '@/progression/skills';

import { DEPTH } from '../depth';

import type { Beats } from './beats';

const EDGE = 0x1d1a16; // a dark edge under each stroke, so a pale one shows on pale ground too
const SLASH_DEG = 80; // the arc's sweep either side of straight ahead

export type Effect =
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

// Each apart from the cue's white flash and from the forest's grass.
const colors = { physical: 0xff6a3d, magic: 0xb070f0, heal: 0x4fd8ff, buff: 0xf0b23a } as const;
const slashColors = { physical: 0xfff6d8, magic: 0xd0a0ff } as const; // brighter: a basic attack shows no box

/** A skill's colour: its shape's, and the caster's flash at its strike. */
export function colorOf(skill: Skill, stage: Stage): number {
  if (skill.effect === 'heal' || skill.effect === 'buff') {
    return colors[skill.effect];
  }
  return stage.damage === 'magic' ? colors.magic : colors.physical;
}

/** Where the stage reaches from a caster whose feet centre is (x, y), as the server finds its targets: box is the top left corner. */
export function effectOf(
  skill: Skill,
  stage: Stage,
  x: number,
  y: number,
  facing: 1 | -1,
  height: number,
): Effect | null {
  const s = stage.shape;
  const color = colorOf(skill, stage);
  const middle = y - height / 2;
  switch (s?.kind) {
    case 'melee': {
      const w = s.width ?? 0;
      const h = s.height ?? 0;
      return { kind: 'box', x: facing > 0 ? x : x - w, y: y - h, w, h, color };
    }
    case 'ranged':
      return { kind: 'line', x1: x, x2: x + facing * (s.range ?? 0), y: middle, color };
    case 'area':
      return { kind: 'ring', x, y: middle, r: s.radius ?? 0, color };
    default:
      return null;
  }
}

/** A melee basic attack's slash through its box, as the server finds its targets, from a caster whose feet centre is (x, y). */
export function slashOf(
  x: number,
  y: number,
  facing: 1 | -1,
  box: { readonly width: number; readonly height: number },
  damage: 'physical' | 'magic',
): Effect {
  return {
    kind: 'slash',
    x,
    y: y - box.height / 2,
    rx: box.width,
    ry: box.height / 2,
    facing,
    color: slashColors[damage],
  };
}

/** Draws the effect at full strength through the strike, fading out by the cancel point; the returned remove takes it away sooner, when a new action cuts this one short. */
export function drawEffect(scene: Phaser.Scene, e: Effect, b: Beats, now: number): () => void {
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

/** A thick crescent: the arc, with a thinner one inside it, each over a dark edge. */
function slash(
  scene: Phaser.Scene,
  e: Extract<Effect, { kind: 'slash' }>,
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
