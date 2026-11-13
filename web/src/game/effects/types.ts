import type Phaser from 'phaser';

import type { Beats } from '../entities/beats';

export interface Size {
  readonly w: number;
  readonly h: number;
}

/** A player or mob as drawn now: its feet at (x, y), its box w by h. */
export interface Body extends Size {
  readonly x: number;
  readonly y: number;
}

/** One play: at the caster's feet facing its way, or a hit at its target's middle sized to it; tier a skill's stage from 0. */
export interface PlayOptions {
  readonly x: number;
  readonly y: number;
  readonly facing: 1 | -1;
  readonly size?: Size;
  readonly tier?: number;
  readonly beats?: Beats;
  /** Who the action reached so far, where they are now: the foes it hit, the players it healed, a buff's caster. */
  readonly targets?: () => readonly Body[];
}

/** Ends a play early, as the caster's next action does; a play that already ended ignores it. */
export type Stop = () => void;

/** An effect's drawn version: one play's shapes and tweens, from now on the scene's clock. */
export type Drawn = (scene: Phaser.Scene, o: PlayOptions, now: number) => Stop;
