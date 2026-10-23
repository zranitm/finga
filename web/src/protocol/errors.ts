// The error codes the server sends, in REST error bodies and in failed
// Responses, and the Kicked reasons. Every failure has its own code
// (name_taken, party_full); the server's table is the reference and writes
// server/testdata/errors/codes.json, which errors.test.ts compares with this list.

/** Every code in server/testdata/errors/codes.json, sorted. */
export const errorCodes = [
  'account_not_found',
  'already_in_party',
  'already_invited',
  'bad_character_name',
  'bad_chat_channel',
  'bad_control_preset',
  'bad_id',
  'bad_password',
  'bad_quantity',
  'bad_slot',
  'bad_username',
  'body_too_large',
  'character_not_found',
  'character_slots_full',
  'chat_empty',
  'chat_no_party',
  'chat_rate_limited',
  'chat_too_long',
  'chat_unavailable',
  'class_unavailable',
  'idempotency_key_reused',
  'internal',
  'inventory_full',
  'inventory_unavailable',
  'invite_expired',
  'invite_self',
  'item_not_found',
  'login_rate_limited',
  'malformed_body',
  'method_not_allowed',
  'missing_field',
  'name_taken',
  'no_party',
  'no_such_endpoint',
  'not_implemented',
  'not_party_leader',
  'not_party_member',
  'parties_unavailable',
  'party_chat_unavailable',
  'party_full',
  'pickup_busy',
  'pickups_unavailable',
  'rate_limited',
  'session_race',
  'slot_holds_other_item',
  'slot_taken',
  'target_not_on_map',
  'unauthenticated',
  'unknown_class',
  'unknown_item',
  'username_taken',
  'wrong_tab',
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
