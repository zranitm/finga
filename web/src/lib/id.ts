// Request IDs and idempotency keys: UUID v4 built from
// crypto.getRandomValues, prefixed r- and k- as the golden files show.
// Not crypto.randomUUID: it exists only in secure contexts (HTTPS or
// localhost), and a LAN playtest over plain http would crash on it.
// Idempotency keys must be random, not counters: the server keeps them for
// 24 hours per account, so a counter restarting after a reload would
// collide with a key already used.
export type RandomBytes = (bytes: Uint8Array<ArrayBuffer>) => Uint8Array<ArrayBuffer>;

const hex = Array.from({ length: 256 }, (_, n) => n.toString(16).padStart(2, '0'));

/** A UUID v4 from 16 random bytes with the version and variant bits set. */
export function uuidV4(random: RandomBytes = (b) => crypto.getRandomValues(b)): string {
  const b = random(new Uint8Array(16));
  if (b.length !== 16) {
    throw new RangeError(`uuidV4 needs 16 random bytes, got ${b.length}`);
  }
  b[6] = ((b[6] ?? 0) & 0x0f) | 0x40; // version 4
  b[8] = ((b[8] ?? 0) & 0x3f) | 0x80; // variant 10xx
  const h = Array.from(b, (n) => hex[n]).join('');
  return `${h.slice(0, 8)}-${h.slice(8, 12)}-${h.slice(12, 16)}-${h.slice(16, 20)}-${h.slice(20)}`;
}

/** A request ID for a command that expects a Response: r-<uuid>. */
export function requestId(random?: RandomBytes): string {
  return `r-${uuidV4(random)}`;
}

/** An idempotency key for an item action, made once per click: k-<uuid>. */
export function idempotencyKey(random?: RandomBytes): string {
  return `k-${uuidV4(random)}`;
}
