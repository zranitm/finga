// The wait before a retry: its window min(cap, base × 2^n) doubles each attempt; full jitter waits anywhere in it, equal jitter in its upper half.
export interface BackoffOptions {
  readonly baseMs: number;
  readonly capMs: number;
  /** Full spreads many clients retrying at once, as after a restart; equal keeps one client's retries from coming all at once. */
  readonly jitter: 'full' | 'equal';
}

/** attempt is 0 for the first retry. */
export function backoff(attempt: number, opts: BackoffOptions): number {
  if (attempt < 0 || !Number.isInteger(attempt)) {
    throw new RangeError(`backoff attempt must be a non-negative integer, got ${attempt}`);
  }
  const bound = Math.min(opts.capMs, opts.baseMs * 2 ** attempt);
  const floor = opts.jitter === 'equal' ? bound / 2 : 0;
  return Math.floor(floor + Math.random() * (bound - floor));
}
