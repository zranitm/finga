import { describe, expect, it } from 'vitest';

import { movement } from '@/gamedata/movement';
import type { Move } from '@/progression/skills';
import type { Input } from '@/sim/input';
import { DIAGONAL } from '@/sim/step';
import type { State } from '@/sim/step';

import type { Mark } from './aim';
import { moveStart, wayFor } from './skill-way';

const facingEast: State = {
  x: 10,
  y: 0,
  z: 10,
  vx: 0,
  vy: 0,
  vz: 0,
  on_ground: true,
  hx: 1,
  hz: 0,
};
const noKeys: Input = { left: false, right: false, up: false, down: false, jump: false };
const lockAt = (x: number, z: number): Mark => ({
  id: 1,
  x,
  z,
  radius: 0.5,
  hp: 30,
  inView: true,
  inFrame: true,
});
const move = (toward: string, kind = 'dash', distance = 4): Move => ({
  kind,
  distance,
  time_ms: 200,
  toward,
  stop: true,
  through: false,
});
const ring = 0.5 + movement.radius;

describe('wayFor', () => {
  it('dashes at the lock to its near contact ring, held to the distance', () => {
    expect(wayFor(move('lock'), facingEast, noKeys, lockAt(10, 13))).toEqual({
      x: 0,
      z: 1,
      length: 3 - ring,
    });
    expect(wayFor(move('lock'), facingEast, noKeys, lockAt(10, 30))).toEqual({
      x: 0,
      z: 1,
      length: 4,
    });
    expect(wayFor(move('lock'), facingEast, noKeys, lockAt(10.5, 10)).length).toBe(0); // already touching
  });

  it('blinks at the lock to its far contact ring, behind it', () => {
    expect(wayFor(move('lock', 'blink', 8), facingEast, noKeys, lockAt(13, 14))).toEqual({
      x: 0.6,
      z: 0.8,
      length: 5 + ring,
    });
  });

  it('goes along the heading for a lock skill with no lock', () => {
    expect(wayFor(move('lock'), facingEast, noKeys, null)).toEqual({ x: 1, z: 0, length: 4 });
  });

  it('goes back away from the lock, or back from the heading with none', () => {
    expect(wayFor(move('back'), facingEast, noKeys, lockAt(10, 13))).toEqual({
      x: -0,
      z: -1,
      length: 4,
    });
    expect(wayFor(move('back'), facingEast, noKeys, null)).toEqual({ x: -1, z: -0, length: 4 });
  });

  it('goes along the held keys, as the sim heads them, or back from the heading while standing', () => {
    const upLeft = { ...noKeys, up: true, left: true };
    expect(wayFor(move('heading'), facingEast, upLeft, lockAt(10, 13))).toEqual({
      x: -DIAGONAL,
      z: DIAGONAL,
      length: 4,
    });
    expect(wayFor(move('heading'), facingEast, noKeys, null)).toEqual({ x: -1, z: -0, length: 4 });
    expect(wayFor(move('heading'), facingEast, null, null)).toEqual({ x: -1, z: -0, length: 4 });
  });
});

describe('moveStart', () => {
  const at: State = { x: 6, y: 0, z: 6, vx: 0, vy: 0, vz: 0, on_ground: true, hx: 1, hz: 0 };
  const way = { x: -1, z: 0, length: 4 };
  const flat = { size_x: 12, size_z: 12, terrain: null as never, boxes: [], cylinders: [] };

  it('starts a dash or leap meeting mobs as its data says: stopping at one, or passing through them from a clear start', () => {
    const leap = { ...move('back', 'leap'), stop: false, through: true };
    expect(moveStart(leap, way, 18)(at, flat, movement).move).toMatchObject({
      through: true,
      clear_x: 6,
      clear_z: 6,
      stop: false,
    });
    const roll = moveStart(move('back', 'leap'), way, 18)(at, flat, movement).move;
    expect(roll?.stop).toBe(true);
    expect(roll).not.toHaveProperty('through');
  });
});
