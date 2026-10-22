// The Response error codes exactly as page 4 lists them, and the Kicked
// reasons. The server is the reference: a code the server does not send
// has no business here.

/** Generic codes from pkg/errs, then the Stage 1 domain codes. */
export const errorCodes = [
  'invalid_argument',
  'unauthenticated',
  'not_found',
  'conflict',
  'rate_limited',
  'unavailable',
  'internal',
  'inventory_full',
  'idempotency_key_reused',
] as const;

export type ErrorCode = (typeof errorCodes)[number];

export const kickReasons = [
  'logged_in_elsewhere',
  'outdated_protocol',
  'server_restart',
  'abuse',
  'unauthenticated',
  'session_expired',
] as const;

export type KickReason = (typeof kickReasons)[number];
