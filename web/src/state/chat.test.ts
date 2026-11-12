import { create } from '@bufbuild/protobuf';
import { beforeEach, describe, expect, it } from 'vitest';

import { ChatChannel, ChatMessageSchema } from '@/protocol/messages';
import { resetStores } from '@/testing/reset-stores';

import { chat, MAX_LINES } from './chat';

const line = (channel: ChatChannel, senderName: string, text: string) =>
  create(ChatMessageSchema, { channel, sender: `c-${senderName}`, senderName, text });

describe('chat store', () => {
  beforeEach(() => {
    resetStores();
  });

  it('tags map and party lines and notices, and keeps them in order with their time', () => {
    chat.getState().applyChatMessage(line(ChatChannel.MAP, 'Ayla', 'hi'), 1000);
    chat.getState().notice('King Slime has appeared in the Forest!', 1500);
    chat.getState().applyChatMessage(line(ChatChannel.PARTY, 'Robin', 'yo'), 2000);
    expect(chat.getState().lines).toEqual([
      {
        kind: 'said',
        channel: ChatChannel.MAP,
        sender: 'c-Ayla',
        senderName: 'Ayla',
        text: 'hi',
        at: 1000,
      },
      { kind: 'notice', text: 'King Slime has appeared in the Forest!', at: 1500 },
      {
        kind: 'said',
        channel: ChatChannel.PARTY,
        sender: 'c-Robin',
        senderName: 'Robin',
        text: 'yo',
        at: 2000,
      },
    ]);
  });

  it('keeps the last 100 lines', () => {
    for (let k = 0; k < MAX_LINES + 25; k++) {
      chat.getState().applyChatMessage(line(ChatChannel.MAP, 'Ayla', `line ${k}`), k);
    }
    const lines = chat.getState().lines;
    expect(lines).toHaveLength(MAX_LINES);
    expect(lines[0]?.text).toBe('line 25');
    expect(lines.at(-1)?.text).toBe(`line ${MAX_LINES + 24}`);
  });

  it('stamps the time when none is given, and clears', () => {
    const before = Date.now();
    chat.getState().applyChatMessage(line(ChatChannel.MAP, 'Ayla', 'now'));
    expect(chat.getState().lines[0]?.at).toBeGreaterThanOrEqual(before);
    chat.getState().clear();
    expect(chat.getState().lines).toEqual([]);
  });
});
