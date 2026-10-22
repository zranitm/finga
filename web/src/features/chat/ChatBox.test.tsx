import { act, render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import { chat } from '@/state/chat';
import { ui } from '@/state/ui';
import { resetStores } from '@/testing/reset-stores';

import { ChatBox, MAX_CHAT_CHARS, parseLine } from './ChatBox';

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
    { raw: 'hello', want: { channel: 'map', text: 'hello' } },
    { raw: '/p hello there', want: { channel: 'party', text: 'hello there' } },
    { raw: '/p', want: { channel: 'party', text: '' } },
    { raw: '/party no', want: { channel: 'map', text: '/party no' } },
    { raw: '  spaced  ', want: { channel: 'map', text: 'spaced' } },
  ])('parseLine($raw)', ({ raw, want }) => {
    expect(parseLine(raw)).toEqual(want);
  });

  it('Enter sends ChatSend with the typed text on the map channel, clears and leaves', async () => {
    const { send, input } = setup();
    await userEvent.type(input(), 'hello all{Enter}');
    expect(send).toHaveBeenCalledWith('ChatSend', { channel: 'map', text: 'hello all' });
    expect(input()).toHaveValue('');
    expect(input()).not.toHaveFocus();
    expect(ui.getState().captureKeyboard).toBe(false);
  });

  it('/p sends a party line', async () => {
    const { send, input } = setup();
    await userEvent.type(input(), '/p on my way{Enter}');
    expect(send).toHaveBeenCalledWith('ChatSend', { channel: 'party', text: 'on my way' });
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

  it('the Enter key (through the ui store) focuses the input and captures the keyboard; Escape leaves', async () => {
    const { input } = setup();
    act(() => ui.getState().openChat());
    expect(input()).toHaveFocus();
    expect(ui.getState().captureKeyboard).toBe(true);
    await userEvent.keyboard('{Escape}');
    expect(input()).not.toHaveFocus();
    expect(ui.getState().captureKeyboard).toBe(false);
    expect(ui.getState().chatOpen).toBe(false);
  });

  it('an incoming ChatMessage renders with the right tag, and the last eight show', () => {
    setup();
    act(() => {
      chat
        .getState()
        .applyChatMessage(
          { channel: 'map', sender: 'c-robin', sender_name: 'Robin', text: 'hi' },
          1,
        );
      chat
        .getState()
        .applyChatMessage(
          { channel: 'party', sender: 'c-ayla', sender_name: 'Ayla', text: 'go' },
          2,
        );
    });
    const items = screen.getAllByRole('listitem');
    expect(items[0]).toHaveTextContent('[Map] Robin: hi');
    expect(items[1]).toHaveTextContent('[Party] Ayla: go');
    expect(items[1]).toHaveClass('chat-party');
    act(() => {
      for (let i = 0; i < 10; i++) {
        chat
          .getState()
          .applyChatMessage(
            { channel: 'map', sender: 'c', sender_name: 'N', text: `l${i}` },
            10 + i,
          );
      }
    });
    expect(screen.getAllByRole('listitem')).toHaveLength(8);
  });
});
