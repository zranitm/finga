import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { combat } from './combat';

const s = () => combat.getState();

describe('combat', () => {
  beforeEach(() => {
    vi.useFakeTimers();
    s().clear();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it('a fight keeps the player in combat for the combat time after its last event', () => {
    s().fought();
    vi.advanceTimersByTime(3_000);
    s().fought();
    vi.advanceTimersByTime(4_999);
    expect(s().inCombat).toBe(true);
    vi.advanceTimersByTime(1);
    expect(s().inCombat).toBe(false);
  });

  it('a clear ends it at once, and its old timer ends nothing after', () => {
    s().fought();
    s().clear();
    expect(s().inCombat).toBe(false);
    vi.advanceTimersByTime(1_000);
    s().fought();
    vi.advanceTimersByTime(4_000);
    expect(s().inCombat).toBe(true);
  });
});
