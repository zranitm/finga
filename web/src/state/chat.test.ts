import { create } from '@bufbuild/protobuf';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { ChatChannel, ChatMessageSchema } from '@/protocol/messages';
import { resetStores } from '@/testing/reset-stores';

import { chat, MAX_LINES } from './chat';

const line = (channel: ChatChannel, senderName: string, text: string) =>
  create(ChatMessageSchema, { channel, sender: `c-${senderName}`, senderName, text });

describe('chat store', () => {
  beforeEach(() => {
    vi.useFakeTimers();
    resetStores();
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  it('tags map and party lines and notices, and keeps them in order with the time they came', () => {
    vi.setSystemTime(1000);
    chat.getState().applyChatMessage(line(ChatChannel.MAP, 'Ayla', 'hi'));
    vi.setSystemTime(1500);
    chat.getState().notice('King Slime has appeared in the Forest!');
    vi.setSystemTime(2000);
    chat.getState().applyChatMessage(line(ChatChannel.PARTY, 'Robin', 'yo'));
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
      chat.getState().applyChatMessage(line(ChatChannel.MAP, 'Ayla', `line ${k}`));
    }
    const lines = chat.getState().lines;
    expect(lines).toHaveLength(MAX_LINES);
    expect(lines[0]?.text).toBe('line 25');
    expect(lines.at(-1)?.text).toBe(`line ${MAX_LINES + 24}`);
  });

  it('clears', () => {
    chat.getState().applyChatMessage(line(ChatChannel.MAP, 'Ayla', 'now'));
    chat.getState().clear();
    expect(chat.getState().lines).toEqual([]);
  });
});
