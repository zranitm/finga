import { beforeEach, describe, expect, it } from 'vitest';

import { resetStores } from '@/testing/reset-stores';

import { chat, MAX_LINES } from './chat';

describe('chat store', () => {
  beforeEach(() => {
    resetStores();
  });

  it('tags map and party lines and keeps them in order with their time', () => {
    chat
      .getState()
      .applyChatMessage({ channel: 'map', sender: 'c1', sender_name: 'Ayla', text: 'hi' }, 1000);
    chat
      .getState()
      .applyChatMessage({ channel: 'party', sender: 'c2', sender_name: 'Robin', text: 'yo' }, 2000);
    expect(chat.getState().lines).toEqual([
      { channel: 'map', sender: 'c1', sender_name: 'Ayla', text: 'hi', at: 1000 },
      { channel: 'party', sender: 'c2', sender_name: 'Robin', text: 'yo', at: 2000 },
    ]);
  });

  it('keeps the last 100 lines', () => {
    for (let k = 0; k < MAX_LINES + 25; k++) {
      chat
        .getState()
        .applyChatMessage(
          { channel: 'map', sender: 'c1', sender_name: 'Ayla', text: `line ${k}` },
          k,
        );
    }
    const lines = chat.getState().lines;
    expect(lines).toHaveLength(MAX_LINES);
    expect(lines[0]?.text).toBe('line 25');
    expect(lines.at(-1)?.text).toBe(`line ${MAX_LINES + 24}`);
  });

  it('stamps the time when none is given, and clears', () => {
    const before = Date.now();
    chat
      .getState()
      .applyChatMessage({ channel: 'map', sender: 'c1', sender_name: 'Ayla', text: 'now' });
    expect(chat.getState().lines[0]?.at).toBeGreaterThanOrEqual(before);
    chat.getState().clear();
    expect(chat.getState().lines).toEqual([]);
  });
});
