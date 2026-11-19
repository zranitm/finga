import { TargetCamera } from '@babylonjs/core/Cameras/targetCamera';
import { NullEngine } from '@babylonjs/core/Engines/nullEngine';
import { Vector3 } from '@babylonjs/core/Maths/math.vector';
import { Scene } from '@babylonjs/core/scene';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';

import { Pacing, POSE_EVERY } from './pacing';

let engine: NullEngine;
let scene: Scene;
let camera: TargetCamera;

beforeEach(() => {
  engine = new NullEngine();
  scene = new Scene(engine);
  camera = new TargetCamera('camera', new Vector3(0, 10, -20), scene);
  camera.setTarget(Vector3.Zero());
});

afterEach(() => {
  scene.dispose();
  engine.dispose();
});

/** The camera's view and projection, as a drawn frame leaves them. */
function screen() {
  camera.getViewMatrix(true);
  camera.getProjectionMatrix(true);
  return camera.getTransformationMatrix();
}

/** Which of frames frames pose a figure at (x, z), on beat, with the camera's target at the origin; screen says whether a frame was drawn. */
function posedFrames(
  pacing: Pacing,
  x: number,
  z: number,
  beat: number,
  frames: number,
  drawn = false,
): number[] {
  const out: number[] = [];
  for (let f = 1; f <= frames; f++) {
    pacing.next(0, 0, drawn ? screen() : null);
    if (pacing.poses(x, 1, z, 1, beat)) {
      out.push(f);
    }
  }
  return out;
}

describe('Pacing', () => {
  it('poses a figure every frame near the camera’s target, every 2nd beyond 30 m and every 4th beyond 60 m', () => {
    expect(POSE_EVERY).toEqual([
      [60, 4],
      [30, 2],
    ]);
    expect(posedFrames(new Pacing(), 30, 0, 0, 8)).toEqual([1, 2, 3, 4, 5, 6, 7, 8]);
    expect(posedFrames(new Pacing(), 0, 30.1, 0, 8)).toEqual([2, 4, 6, 8]);
    expect(posedFrames(new Pacing(), 60, 0, 0, 8)).toEqual([2, 4, 6, 8]);
    expect(posedFrames(new Pacing(), 42.5, 42.5, 0, 8)).toEqual([4, 8]); // 60.1 m off
  });

  it('gives each figure its own beat, so far ones spread across the frames', () => {
    const pacing = new Pacing();
    expect([pacing.beat(), pacing.beat(), pacing.beat()]).toEqual([0, 1, 2]);
    const far = (beat: number) => posedFrames(new Pacing(), 70, 0, beat, 8);
    expect([far(0), far(1), far(2), far(3)]).toEqual([
      [4, 8],
      [3, 7],
      [2, 6],
      [1, 5],
    ]);
  });

  it('never poses a figure off the screen as last drawn, but one whose bounds reach onto it', () => {
    const pacing = new Pacing();
    expect(posedFrames(pacing, 0, -25, 0, 4, true)).toEqual([]); // 5 m behind the camera
    expect(posedFrames(pacing, 0, 5, 0, 4, true)).toEqual([1, 2, 3, 4]);
    pacing.next(0, 0, screen());
    const edge = [...Array(60).keys()].find((x) => !pacing.poses(x, 1, 5, 0, 0))!;
    expect(edge).toBeGreaterThan(5);
    expect(pacing.poses(edge, 1, 5, 1, 0)).toBe(true); // just off, its bounds a metre across reaching on
    expect(posedFrames(new Pacing(), 0, -25, 0, 4)).toEqual([1, 2, 3, 4]); // no frame drawn yet to test against
  });
});
