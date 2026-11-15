import { describe, expect, it } from 'vitest';

import { upperBody } from './rig';

const rig = [
  { name: 'root', parent: null },
  { name: 'hips', parent: 'root' },
  { name: 'spine', parent: 'hips' },
  { name: 'chest', parent: 'spine' },
  { name: 'upperarm.l', parent: 'chest' },
  { name: 'head', parent: 'chest' },
  { name: 'upperleg.l', parent: 'hips' },
  { name: 'kneeIK.l', parent: 'root' },
  { name: 'elbowIK.l', parent: 'root' },
  { name: 'handIK.r', parent: 'root' },
];

describe('upperBody', () => {
  it('takes the spine and all above it, with the arms’ controls', () => {
    expect([...upperBody(rig)].sort()).toEqual([
      'chest',
      'elbowIK.l',
      'handIK.r',
      'head',
      'spine',
      'upperarm.l',
    ]);
  });

  it('leaves the hips, legs and their controls to the lower body', () => {
    const upper = upperBody(rig);
    for (const lower of ['root', 'hips', 'upperleg.l', 'kneeIK.l']) {
      expect(upper.has(lower)).toBe(false);
    }
  });
});
