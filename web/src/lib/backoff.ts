// Full jitter: uniform in [0, min(cap, base * 2^n)].
export interface BackoffOptions {
  readonly baseMs: number;
  readonly capMs: number;
}

/** attempt is 0 for the first retry. */
export function backoff(attempt: number, opts: BackoffOptions): number {
  if (attempt < 0 || !Number.isInteger(attempt)) {
    throw new RangeError(`backoff attempt must be a non-negative integer, got ${attempt}`);
  }
  const bound = Math.min(opts.capMs, opts.baseMs * 2 ** attempt);
  return Math.floor(Math.random() * bound);
}
