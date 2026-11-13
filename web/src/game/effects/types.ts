import type Phaser from 'phaser';

import type { Beats } from '../entities/beats';

/** One play: at the caster's feet facing its way, tier a skill's stage from 0, timed to the action's beats. */
export interface PlayOptions {
  readonly x: number;
  readonly y: number;
  readonly facing: 1 | -1;
  readonly tier?: number;
  readonly beats?: Beats;
}

/** Ends a play early, as the caster's next action does; a play that already ended ignores it. */
export type Stop = () => void;

/** An effect's drawn version: one play's shapes and tweens, from now on the scene's clock. */
export type Drawn = (scene: Phaser.Scene, o: PlayOptions, now: number) => Stop;
