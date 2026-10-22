// The asset manifest: every texture key and the file it loads from, under
// public/assets. A Node test checks that each key in TEX is here and that
// every file exists, so a missing or renamed file fails before it 404s.
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
  { key: TEX.warriorStand, file: p('alienBlue_stand') },
  { key: TEX.warriorWalk1, file: p('alienBlue_walk1') },
  { key: TEX.warriorWalk2, file: p('alienBlue_walk2') },
  { key: TEX.warriorJump, file: p('alienBlue_jump') },
  { key: TEX.warriorClimb1, file: p('alienBlue_climb1') },
  { key: TEX.warriorClimb2, file: p('alienBlue_climb2') },
  { key: TEX.warriorHit, file: p('alienBlue_hit') },
  { key: TEX.bowmanStand, file: p('alienGreen_stand') },
  { key: TEX.bowmanWalk1, file: p('alienGreen_walk1') },
  { key: TEX.bowmanWalk2, file: p('alienGreen_walk2') },
  { key: TEX.bowmanJump, file: p('alienGreen_jump') },
  { key: TEX.bowmanClimb1, file: p('alienGreen_climb1') },
  { key: TEX.bowmanClimb2, file: p('alienGreen_climb2') },
  { key: TEX.bowmanHit, file: p('alienGreen_hit') },
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
