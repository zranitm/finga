// A used skill drawn as a simple shape until there's art: the box it swings through, the line it flies along, or the ring around the caster, fading out.
import type Phaser from 'phaser';

import type { Skill, Stage } from '@/progression/skills';

export const EFFECT_MS = 300;
const EDGE = 0x1d1a16;

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
    };

const colors = { physical: 0xf0ead6, magic: 0xb070f0, heal: 0x7ac74f, buff: 0xf0b23a } as const;

function colorOf(skill: Skill, stage: Stage): number {
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

export function drawEffect(scene: Phaser.Scene, e: Effect): void {
  const shapes: Phaser.GameObjects.Shape[] = [];
  switch (e.kind) {
    case 'box':
      shapes.push(scene.add.rectangle(e.x + e.w / 2, e.y + e.h / 2, e.w, e.h, e.color, 0.35));
      break;
    case 'line':
      // A dark edge under the line, so a pale one shows on pale ground too.
      shapes.push(
        scene.add.line(0, 0, e.x1, e.y, e.x2, e.y, EDGE, 0.7).setOrigin(0, 0).setLineWidth(6),
        scene.add.line(0, 0, e.x1, e.y, e.x2, e.y, e.color).setOrigin(0, 0).setLineWidth(3),
      );
      break;
    case 'ring':
      shapes.push(scene.add.circle(e.x, e.y, e.r).setStrokeStyle(3, e.color));
      break;
  }
  for (const shape of shapes) {
    shape.setDepth(5);
  }
  scene.tweens.add({
    targets: shapes,
    alpha: 0,
    duration: EFFECT_MS,
    onComplete: () => shapes.forEach((shape) => shape.destroy()),
  });
}
