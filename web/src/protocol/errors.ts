// Every refusal the server can send, by its six-digit code, and what the client says for it; the server's message is for developers.
import { errorCodes } from '@/gamedata/error-codes';
import { limits } from '@/gamedata/limits';

/** The catalog's codes by name, generated from server/testdata/errors/codes.json: ERR.EQUIP_TAB_FULL. */
export const ERR = errorCodes;

export type ErrorCode = (typeof ERR)[keyof typeof ERR];

const texts: Record<ErrorCode, string> = {
  [ERR.MALFORMED_BODY]: 'The request was malformed. Reload the page.',
  [ERR.MISSING_FIELD]: 'Fill in every field.',
  [ERR.BODY_TOO_LARGE]: 'That request is too large.',
  [ERR.BODY_TIMEOUT]: 'The request took too long. Try again.',
  [ERR.NO_SUCH_ENDPOINT]: 'The server doesn’t know that request. Reload the page.',
  [ERR.METHOD_NOT_ALLOWED]: 'The server doesn’t know that request. Reload the page.',
  [ERR.RATE_LIMITED]: 'Too fast. Wait a moment.',
  [ERR.NOT_IMPLEMENTED]: 'That isn’t available yet.',
  [ERR.BAD_ID]: 'The request was malformed. Reload the page.',
  [ERR.INTERNAL]: 'Something went wrong on the server. Try again.',
  [ERR.UNAVAILABLE]: 'The server is busy. Try again.',

  [ERR.UNAUTHENTICATED]: 'Wrong username or password, or your session has ended.',
  [ERR.ACCOUNT_NOT_FOUND]: 'That account doesn’t exist.',
  [ERR.USERNAME_TAKEN]: 'That username is taken.',
  [ERR.LOGIN_RATE_LIMITED]: 'Too many login attempts. Try again in a minute.',
  [ERR.BAD_USERNAME]: 'A username is 3 to 16 letters, digits or underscores.',
  [ERR.BAD_PASSWORD]: 'A password is 8 to 128 characters.',
  [ERR.BAD_CONTROL_PRESET]: 'Choose Classic or WASD.',
  [ERR.SESSION_RACE]: 'Another login is in progress. Try again.',

  [ERR.CHARACTER_NOT_FOUND]: 'That character doesn’t exist.',
  [ERR.NAME_TAKEN]: 'That name is taken.',
  [ERR.BAD_CHARACTER_NAME]: 'A name is 3 to 12 letters and digits, starting with a letter.',
  [ERR.CLASS_UNAVAILABLE]: 'That class isn’t available yet.',
  [ERR.CHARACTER_SLOTS_FULL]: 'This account already has the most characters allowed.',
  [ERR.UNKNOWN_CLASS]: 'That class doesn’t exist.',
  [ERR.BAD_SKILL_BAR]: 'That can’t go on the skill bar.',

  [ERR.ITEM_NOT_FOUND]: 'That item isn’t there any more.',
  [ERR.SLOT_TAKEN]: 'That slot is taken.',
  [ERR.SLOT_HOLDS_OTHER_ITEM]: 'That slot holds a different item.',
  [ERR.STACK_FULL]: 'That stack is full.',
  [ERR.EQUIP_TAB_FULL]: 'Your Equip tab is full.',
  [ERR.USE_TAB_FULL]: 'Your Use tab is full.',
  [ERR.ETC_TAB_FULL]: 'Your Etc tab is full.',
  [ERR.IDEMPOTENCY_KEY_REUSED]: 'That request was made already. Try again.',
  [ERR.BAD_QUANTITY]: 'That quantity isn’t possible.',
  [ERR.BAD_SLOT]: 'That slot doesn’t exist.',
  [ERR.WRONG_TAB]: 'An item stays in its own tab.',
  [ERR.UNKNOWN_ITEM]: 'That item doesn’t exist.',
  [ERR.ITEM_WORN]: 'Unequip it first.',
  [ERR.LEVEL_TOO_LOW]: 'Your level is too low for that item.',
  [ERR.STR_TOO_LOW]: 'Your STR is too low for that item.',
  [ERR.DEX_TOO_LOW]: 'Your DEX is too low for that item.',
  [ERR.INT_TOO_LOW]: 'Your INT is too low for that item.',
  [ERR.AGI_TOO_LOW]: 'Your AGI is too low for that item.',
  [ERR.VIT_TOO_LOW]: 'Your VIT is too low for that item.',
  [ERR.WRONG_CLASS]: 'Your class can’t use that item.',
  [ERR.WRONG_EQUIP_SLOT]: 'That item doesn’t go there.',
  [ERR.NOT_EQUIPMENT]: 'Only equipment can be worn.',
  [ERR.NOTHING_WORN]: 'Nothing is worn there.',
  [ERR.NOT_ENOUGH_ITEMS]: 'You don’t have everything the quest asks for.',
  [ERR.NOT_USABLE]: 'That item can’t be used.',
  [ERR.BAD_ITEM_BATCH]: 'Pick one to 24 items, each once.',

  [ERR.CHARACTER_DEAD]: 'You can’t do that while dead.',
  [ERR.NOT_ENOUGH_POINTS]: 'You don’t have that many points.',
  [ERR.POINTS_CHANGED]: 'Your points changed. Try again.',
  [ERR.NO_FREE_RESET]: 'Your free reset is used.',
  [ERR.BAD_POINTS]: 'Spend at least one point.',
  [ERR.NPC_OUT_OF_REACH]: 'Move closer.',
  [ERR.QUEST_UNAVAILABLE]: 'That quest isn’t offered here.',
  [ERR.QUEST_CONDITIONS_UNMET]: 'You don’t meet that quest’s conditions yet.',
  [ERR.QUEST_ALREADY_ACTIVE]: 'You have that quest already.',
  [ERR.QUEST_ALREADY_FINISHED]: 'You’ve finished that quest.',
  [ERR.TOO_MANY_QUESTS]: `You have ${limits.max_active_quests} quests already.`,
  [ERR.QUEST_NOT_READY]: 'That quest isn’t finished yet.',
  [ERR.HP_FULL]: 'Your HP is already full.',
  [ERR.MP_FULL]: 'Your MP is already full.',
  [ERR.NOT_ENOUGH_SKILL_POINTS]: 'You don’t have that many skill points.',
  [ERR.SKILL_POINTS_CHANGED]: 'Your skill points changed. Try again.',
  [ERR.SKILL_NOT_LEARNABLE]: 'Your class can’t learn that skill.',
  [ERR.BAD_SKILL_POINTS]: 'Spend at least one skill point.',
  [ERR.SKILL_NOT_LEARNED]: 'Put a point in that skill first.',
  [ERR.SKILL_PASSIVE]: 'A passive skill is always on.',
  [ERR.NOT_IN_TOWN]: 'You can’t use that in town.',
  [ERR.SKILL_COOLING_DOWN]: 'That skill isn’t ready yet.',
  [ERR.NOT_ENOUGH_MP]: 'Not enough MP.',
  [ERR.SKILL_WAITING]: 'Another skill is already waiting for its turn.',
  [ERR.PROMOTION_CONDITIONS_UNMET]: 'You don’t meet every condition yet.',
  [ERR.NOT_YOUR_NEXT_CLASS]: 'This trainer can’t promote you to that class.',

  [ERR.NOT_PARTY_LEADER]: 'Only the party leader can do that.',
  [ERR.PARTY_FULL]: 'The party is full.',
  [ERR.ALREADY_IN_PARTY]: 'You’re in a party already.',
  [ERR.TARGET_IN_PARTY]: 'That character is in a party already.',
  [ERR.NO_PARTY]: 'You’re not in a party.',
  [ERR.NOT_PARTY_MEMBER]: 'That character isn’t in your party.',
  [ERR.TARGET_NOT_ON_MAP]: 'That character isn’t on your map.',
  [ERR.ALREADY_INVITED]: 'You’ve invited them already.',
  [ERR.INVITE_EXPIRED]: 'The invite has expired.',
  [ERR.INVITE_SELF]: 'You can’t invite yourself.',
  [ERR.CHAT_EMPTY]: 'Nothing to say.',
  [ERR.CHAT_TOO_LONG]: `A chat line is at most ${limits.chat_line_max} characters.`,
  [ERR.BAD_CHAT_CHANNEL]: 'That chat channel doesn’t exist.',
  [ERR.CHAT_NO_PARTY]: 'You’re not in a party.',
  [ERR.CHAT_RATE_LIMITED]: 'Too fast. Wait a moment.',
  [ERR.PARTY_CHAT_UNAVAILABLE]: 'Party chat isn’t available right now.',
};

/** A code this client knows; a newer server may send one it doesn't. */
export function isErrorCode(code: number): code is ErrorCode {
  return Object.hasOwn(texts, code);
}

/** The client's words for a request with no refusal to read; the technical detail goes to the console. */
export const noAnswerText = {
  // Either may still have landed, so it says only that.
  timeout: 'No answer yet. Try again.',
  disconnected: 'No answer yet. Try again.',
  network: 'Can’t reach the server. Check your connection and try again.',
  malformed: 'The server isn’t answering right now. Try again in a moment.',
} as const;

/** A refusal's code as the client keeps it: 0, which no failure has, is the server's mistake. */
export function refusalCode(code: number): number {
  return code === 0 ? ERR.INTERNAL : code;
}

/** What the client says for a code; one it doesn't know shows its number, so the number reaches support. */
export function errorText(code: number): string {
  return isErrorCode(code) ? texts[code] : `Something went wrong (error ${code}).`;
}
