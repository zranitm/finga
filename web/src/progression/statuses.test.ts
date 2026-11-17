import { describe, expect, it } from 'vitest';

import { statuses } from '@/gamedata/statuses';

import { blocked, statusColor } from './statuses';

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

describe('statusColor', () => {
  it('gives every status in the data its own colour, and one it doesn’t know a status skill’s', () => {
    const ids = statuses.map((s) => s.id);
    const colors = ids.map(statusColor);
    expect(colors.every((c) => c !== statusColor('unknown'))).toBe(true);
    expect(new Set(colors).size).toBe(ids.length);
    expect(statusColor('unknown')).toBe(0x6b8cff);
  });
});
