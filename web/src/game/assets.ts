import { TEX } from './keys';
import type { TextureKey } from './keys';

export interface ImageAsset {
  readonly key: TextureKey;
  readonly file: string; // relative to public/assets
}

const R = 'remastered';
const g = (name: string) => `${R}/ground/${name}.png`;
const t = (name: string) => `${R}/tiles/${name}.png`;
const e = (name: string) => `${R}/enemies/${name}.png`;
const p = (name: string) => `${R}/players/${name}.png`;
const b = (name: string) => `${R}/backgrounds/${name}.png`;

export const images: readonly ImageAsset[] = [
  { key: TEX.dirtMid, file: g('dirtMid') },
  { key: TEX.dirtCenter, file: g('dirtCenter') },
  { key: TEX.dirtHalf, file: g('dirtHalf_mid') },
  { key: TEX.grassMid, file: g('grassMid') },
  { key: TEX.grassCenter, file: g('grassCenter') },
  { key: TEX.grassHalf, file: g('grassHalf_mid') },
  { key: TEX.stoneMid, file: g('stoneMid') },
  { key: TEX.stoneCenter, file: g('stoneCenter') },
  { key: TEX.stoneHalf, file: g('stoneHalf_mid') },
  { key: TEX.ladderMid, file: t('ladderMid') },
  { key: TEX.ladderTop, file: t('ladderTop') },
  { key: TEX.doorClosedMid, file: t('doorClosed_mid') },
  { key: TEX.doorClosedTop, file: t('doorClosed_top') },
  { key: TEX.doorOpenMid, file: t('doorOpen_mid') },
  { key: TEX.doorOpenTop, file: t('doorOpen_top') },
  { key: TEX.bgGrass, file: b('colored_grass') },
  { key: TEX.bgLand, file: b('colored_land') },
  { key: TEX.bgShroom, file: b('colored_shroom') },
  { key: TEX.bgCave, file: b('blue_land') },
  { key: TEX.alienBlueStand, file: p('alienBlue_stand') },
  { key: TEX.alienBlueWalk1, file: p('alienBlue_walk1') },
  { key: TEX.alienBlueWalk2, file: p('alienBlue_walk2') },
  { key: TEX.alienBlueJump, file: p('alienBlue_jump') },
  { key: TEX.alienBlueClimb1, file: p('alienBlue_climb1') },
  { key: TEX.alienBlueClimb2, file: p('alienBlue_climb2') },
  { key: TEX.alienBlueHit, file: p('alienBlue_hit') },
  { key: TEX.alienGreenStand, file: p('alienGreen_stand') },
  { key: TEX.alienGreenWalk1, file: p('alienGreen_walk1') },
  { key: TEX.alienGreenWalk2, file: p('alienGreen_walk2') },
  { key: TEX.alienGreenJump, file: p('alienGreen_jump') },
  { key: TEX.alienGreenClimb1, file: p('alienGreen_climb1') },
  { key: TEX.alienGreenClimb2, file: p('alienGreen_climb2') },
  { key: TEX.alienGreenHit, file: p('alienGreen_hit') },
  { key: TEX.slimeStand, file: e('slimeGreen') },
  { key: TEX.slimeMove, file: e('slimeGreen_move') },
  { key: TEX.slimeHit, file: e('slimeGreen_hit') },
  { key: TEX.slimeDead, file: e('slimeGreen_dead') },
  { key: TEX.snailStand, file: e('snail') },
  { key: TEX.snailMove, file: e('snail_move') },
  { key: TEX.snailDead, file: e('snail_shell') },
  { key: TEX.mouseStand, file: e('mouse') },
  { key: TEX.mouseMove, file: e('mouse_move') },
  { key: TEX.mouseDead, file: e('mouse_dead') },
  { key: TEX.flyStand, file: e('fly') },
  { key: TEX.flyMove, file: e('fly_move') },
  { key: TEX.flyDead, file: e('fly_dead') },
  { key: TEX.flask, file: 'generic-items/flask.png' },
];

/** Maps are served from public/maps, copied from data/maps before dev and build. */
export const mapFile = (id: string): string => `maps/${id}.tmj`;
