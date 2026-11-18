import type { Beats } from '../world3d/beats';
import type { Figure } from '../world3d/body';

import type { Fx } from './fx';

export interface Point {
  readonly x: number;
  readonly y: number;
  readonly z: number;
}

/** A player or mob as drawn now: its feet at (x, y, z), its cylinder radius by height. */
export interface Body extends Point {
  readonly radius: number;
  readonly height: number;
}

/** A mob's spit in flight: its ball's centre and its size across. */
export interface Ball extends Point {
  readonly size: number;
}

/** One play: at the caster's feet turned to yaw, or a hit at its target's middle sized to it; tier a skill's stage from 0. */
export interface PlayOptions extends Point {
  /** Its way: an action's aim, a hit's from its source to its target. */
  readonly yaw: number;
  /** A hit's target, which sizes its spark. */
  readonly size?: { readonly radius: number; readonly height: number };
  readonly tier?: number;
  readonly beats?: Beats;
  /** Who the action reached so far, where they are now: the foes it hit, the players it healed. */
  readonly targets?: () => readonly Body[];
  /** Whoever the look moves with, as drawn now: a buff's caster, a mob in its attack. */
  readonly follow?: () => Body | undefined;
  /** The mob a skill was aimed at, as drawn now, which an area at the lock centres on within its reach. */
  readonly lock?: () => Body | undefined;
  /** The figure an afterimage copies, as it's posed now. */
  readonly copy?: Figure;
  /** How far a mob's attack reaches: past its body, a bite's reach and a hop's or a slam's spread; from its middle, how far a spit's ball flies. */
  readonly reach?: number;
  /** Where a mob's leap lands or its rush ends, as its hit goes. */
  readonly to?: Point;
  /** How wide a mob attack's lane is: a rush's body, a spit's ball. */
  readonly width?: number;
  /** A mob's spits in flight. */
  readonly shots?: () => readonly Ball[];
  /** When the world said a mob's attack acted, on the look's clock, which its result shows from; undefined till it does. Without it, the result shows at the strike's end. */
  readonly landed?: () => number | undefined;
  /** The colour a look takes from its actor: a mob's spit is its tint. */
  readonly color?: number;
  /** The colour of the status a mob's attack inflicts, which edges its area. */
  readonly edge?: number;
  /** A boss's attack's area on the ground: red where a block doesn't stop it, yellow where it does. Unset draws none, as a normal mob's or an elite's attack is read from its body. */
  readonly area?: 'red' | 'yellow';
}

/** Ends a play early, as the caster's next action does; a play that already ended ignores it. */
export type Stop = () => void;

/** An effect's drawn version: one play's meshes and particles, from now on the world's clock. */
export type Drawn = (fx: Fx, o: PlayOptions, now: number) => Stop;
