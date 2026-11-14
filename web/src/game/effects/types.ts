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
  /** Who the action reached so far, where they are now: the foes it hit, the players it healed. */
  readonly targets?: () => readonly Body[];
  /** The sprite an afterimage copies, as it's drawn now. */
  readonly copy?: Phaser.GameObjects.Sprite;
  /** Whoever the look moves with, as drawn now: a buff's caster, a mob in its attack. */
  readonly follow?: () => Body | undefined;
  /** How far a mob's attack reaches past its body: a bite's reach, a hop's or a slam's spread. */
  readonly reach?: number;
  /** Where a mob's leap lands or its rush ends, as its hit goes: past a portal's edge, where its body stops. */
  readonly to?: number;
  /** Every player as drawn now: a mob's rush stops at the first its hit meets. */
  readonly players?: () => readonly Body[];
  /** A mob's spits in flight, each a square body. */
  readonly shots?: () => readonly Body[];
  /** The colour a look takes from its actor: a mob's spit is its tint. */
  readonly color?: number;
}

/** Ends a play early, as the caster's next action does; a play that already ended ignores it. */
export type Stop = () => void;

/** An effect's drawn version: one play's shapes and tweens, from now on the scene's clock. */
export type Drawn = (scene: Phaser.Scene, o: PlayOptions, now: number) => Stop;
