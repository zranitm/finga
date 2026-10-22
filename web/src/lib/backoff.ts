// Exponential backoff with full jitter, capped: the delay before attempt n
// is a uniform random value in [0, min(cap, base * 2^n)]. Used by the
// reconnect loop in net.
export interface BackoffOptions {
  /** The first attempt's upper bound in milliseconds. */
  readonly baseMs: number;
  /** The upper bound never grows past this. */
  readonly capMs: number;
  /** A source of numbers in [0, 1); Math.random by default. */
  readonly random?: () => number;
}

/** The delay before attempt `attempt` (0 for the first retry). */
export function backoff(attempt: number, opts: BackoffOptions): number {
  if (attempt < 0 || !Number.isInteger(attempt)) {
    throw new RangeError(`backoff attempt must be a non-negative integer, got ${attempt}`);
  }
  const random = opts.random ?? Math.random;
  const bound = Math.min(opts.capMs, opts.baseMs * 2 ** attempt);
  return Math.floor(random() * bound);
}
