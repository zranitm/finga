import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { saveQueue } from './save-queue';

describe('saveQueue', () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  it('saves once, the delay after the last of a run of changes, what stood when it was due', async () => {
    let value = 0;
    const sent: number[] = [];
    const q = saveQueue(() => {
      const v = value;
      return () => {
        sent.push(v);
        return Promise.resolve();
      };
    }, 100);
    value = 1;
    q.changed();
    await vi.advanceTimersByTimeAsync(60);
    value = 2;
    q.changed();
    await vi.advanceTimersByTimeAsync(99);
    expect(sent).toEqual([]);
    await vi.advanceTimersByTimeAsync(1);
    expect(sent).toEqual([2]);
  });

  it('sends one at a time, in order, and settles once all have', async () => {
    const finish: (() => void)[] = [];
    const sent: number[] = [];
    let n = 0;
    const q = saveQueue(() => {
      const mine = ++n;
      return () => {
        sent.push(mine);
        return new Promise<void>((resolve) => finish.push(resolve));
      };
    }, 10);
    q.changed();
    q.flush();
    q.changed();
    q.flush();
    await Promise.resolve();
    expect(sent).toEqual([1]); // the second waits for the first
    let settled = false;
    void q.settled().then(() => {
      settled = true;
    });
    finish[0]!();
    await vi.advanceTimersByTimeAsync(0);
    expect(sent).toEqual([1, 2]);
    expect(settled).toBe(false);
    finish[1]!();
    await vi.advanceTimersByTimeAsync(0);
    expect(settled).toBe(true);
  });

  it('flush with nothing waiting sends nothing', async () => {
    const send = vi.fn(() => Promise.resolve());
    const q = saveQueue(() => send, 10);
    q.flush();
    await vi.advanceTimersByTimeAsync(20);
    expect(send).not.toHaveBeenCalled();
    await expect(q.settled()).resolves.toBeUndefined();
  });
});
