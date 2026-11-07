import { create } from '@bufbuild/protobuf';
import { act, render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import { isTyping } from '@/input/typing';
import { ChatChannel, ChatMessageSchema } from '@/protocol/messages';
import { chat } from '@/state/chat';
import { ui } from '@/state/ui';
import { resetStores } from '@/testing/reset-stores';

import { ChatBox, MAX_CHAT_CHARS, parseLine } from './ChatBox';

const line = (channel: ChatChannel, senderName: string, text: string) =>
  create(ChatMessageSchema, { channel, sender: `c-${senderName}`, senderName, text });

function setup() {
  const send = vi.fn(() => true);
  render(<ChatBox socket={{ send } as never} />);
  return { send, input: () => screen.getByLabelText('Chat message') };
}

describe('ChatBox', () => {
  beforeEach(() => {
    resetStores();
  });

  it.each([
    { raw: 'hello', want: { channel: ChatChannel.MAP, text: 'hello' } },
    { raw: '/p hello there', want: { channel: ChatChannel.PARTY, text: 'hello there' } },
    { raw: '/p', want: { channel: ChatChannel.PARTY, text: '' } },
    { raw: '/party no', want: { channel: ChatChannel.MAP, text: '/party no' } },
    { raw: '  spaced  ', want: { channel: ChatChannel.MAP, text: 'spaced' } },
  ])('parseLine($raw)', ({ raw, want }) => {
    expect(parseLine(raw)).toEqual(want);
  });

  it('Enter sends ChatSend with the typed text on the map channel, clears and leaves', async () => {
    const { send, input } = setup();
    await userEvent.type(input(), 'hello all{Enter}');
    expect(send).toHaveBeenCalledWith('chatSend', { channel: ChatChannel.MAP, text: 'hello all' });
    expect(input()).toHaveValue('');
    expect(input()).not.toHaveFocus();
    expect(isTyping()).toBe(false);
  });

  it('/p sends a party line', async () => {
    const { send, input } = setup();
    await userEvent.type(input(), '/p on my way{Enter}');
    expect(send).toHaveBeenCalledWith('chatSend', {
      channel: ChatChannel.PARTY,
      text: 'on my way',
    });
  });

  it('121 characters is blocked with the counter red; an empty line sends nothing', async () => {
    const { send, input } = setup();
    await userEvent.type(input(), 'x'.repeat(MAX_CHAT_CHARS + 1));
    expect(screen.getByLabelText('Characters')).toHaveTextContent('121/120');
    expect(screen.getByLabelText('Characters')).toHaveClass('chat-over');
    await userEvent.keyboard('{Enter}');
    expect(send).not.toHaveBeenCalled();
    await userEvent.clear(input());
    await userEvent.keyboard('{Enter}');
    expect(send).not.toHaveBeenCalled();
  });

  it('the Enter key (through the ui store) focuses the input, which then has the keyboard; Escape leaves', async () => {
    const { input } = setup();
    act(() => ui.getState().openChat());
    expect(input()).toHaveFocus();
    expect(isTyping()).toBe(true);
    await userEvent.keyboard('{Escape}');
    expect(input()).not.toHaveFocus();
    expect(isTyping()).toBe(false);
    expect(ui.getState().chatOpen).toBe(false);
  });

  it('an incoming ChatMessage renders with the right tag, and the last eight show', () => {
    setup();
    act(() => {
      chat.getState().applyChatMessage(line(ChatChannel.MAP, 'Robin', 'hi'), 1);
      chat.getState().applyChatMessage(line(ChatChannel.PARTY, 'Ayla', 'go'), 2);
    });
    const items = screen.getAllByRole('listitem');
    expect(items[0]).toHaveTextContent('[Map] Robin: hi');
    expect(items[1]).toHaveTextContent('[Party] Ayla: go');
    expect(items[1]).toHaveClass('chat-party');
    act(() => {
      for (let i = 0; i < 10; i++) {
        chat.getState().applyChatMessage(line(ChatChannel.MAP, 'N', `l${i}`), 10 + i);
      }
    });
    expect(screen.getAllByRole('listitem')).toHaveLength(8);
  });
});
