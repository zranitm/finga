import { expect } from 'vitest';

export interface SentRequest {
  readonly t: string;
  readonly d: Record<string, unknown>;
}

/** The requests sent, each idempotency key checked as the client makes them, then left out. */
export function withoutKeys(calls: readonly SentRequest[]): SentRequest[] {
  return calls.map(({ t, d }) => {
    expect(String(d.idempotencyKey)).toMatch(/^k-/);
    return { t, d: Object.fromEntries(Object.entries(d).filter(([k]) => k !== 'idempotencyKey')) };
  });
}
