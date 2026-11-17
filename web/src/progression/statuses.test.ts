import { describe, expect, it } from 'vitest';

import { blocked } from './statuses';

describe('blocked', () => {
  it('stops acting under a hard status, skills alone under silence, and nothing once a status ends or for one that blocks neither', () => {
    expect(blocked([{ id: 'stun', ends: 120 }], 100)).toEqual({ act: true, skills: false });
    expect(blocked([{ id: 'pin', ends: 120 }], 100)).toEqual({ act: false, skills: false });
    expect(blocked([{ id: 'silence', ends: 120 }], 100)).toEqual({ act: false, skills: true });
    expect(
      blocked(
        [
          { id: 'slow', ends: 120 },
          { id: 'freeze', ends: 110 },
        ],
        100,
      ),
    ).toEqual({ act: true, skills: false });
    expect(blocked([{ id: 'stun', ends: 120 }], 120)).toEqual({ act: false, skills: false });
    expect(blocked([{ id: 'unknown', ends: 120 }], 100)).toEqual({ act: false, skills: false });
  });
});
