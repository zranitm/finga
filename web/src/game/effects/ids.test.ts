import { describe, expect, it } from 'vitest';

import { hitEffects } from './ids';

describe('hitEffects', () => {
  it('plays a puff for a miss or a dodge, a clang for a block, else the source’s spark, a crit’s on top', () => {
    expect(hitEffects('physical', { miss: true })).toEqual(['hit.miss']);
    expect(hitEffects('magic', { dodge: true })).toEqual(['hit.miss']);
    expect(hitEffects('physical', { block: true })).toEqual(['hit.block']);
    expect(hitEffects('magic', {})).toEqual(['hit.magic']);
    expect(hitEffects('physical', { crit: true })).toEqual(['hit.physical', 'hit.crit']);
  });
});
