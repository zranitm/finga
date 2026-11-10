import { create } from '@bufbuild/protobuf';
import { act, fireEvent, render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import { isTyping } from '@/input/typing';
import {
  CharacterViewSchema,
  ChatChannel,
  ChatMessageSchema,
  ExpGainedSchema,
  LevelUpSchema,
  LootKind,
  LootSchema,
  WelcomeSchema,
} from '@/protocol/messages';
import { character } from '@/state/character';
import { chat } from '@/state/chat';
import { combatLog } from '@/state/combat-log';
import { ui } from '@/state/ui';
import { resetStores } from '@/testing/reset-stores';

import { ChatBox, MAX_CHAT_CHARS, parseLine } from './ChatBox';

const line = (channel: ChatChannel, senderName: string, text: string) =>
  create(ChatMessageSchema, { channel, sender: `c-${senderName}`, senderName, text });

const loot = (kind: LootKind, template: string, quantity: number, ownerName = 'Ayla') =>
  create(LootSchema, {
    kind,
    mapId: 'meadow',
    dropId: 7,
    ownerId: `c-${ownerName.toLowerCase()}`,
    ownerName,
    template,
    quantity,
  });

const tab = (name: string) => screen.getByRole('tab', { name });
const shown = () => screen.getAllByRole('listitem').map((li) => li.textContent);

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
    { raw: 'hello', tab: 'general', want: { channel: ChatChannel.MAP, text: 'hello' } },
    {
      raw: '/p hello there',
      tab: 'general',
      want: { channel: ChatChannel.PARTY, text: 'hello there' },
    },
    { raw: '/p', tab: 'general', want: { channel: ChatChannel.PARTY, text: '' } },
    { raw: '/party no', tab: 'general', want: { channel: ChatChannel.MAP, text: '/party no' } },
    { raw: '  spaced  ', tab: 'general', want: { channel: ChatChannel.MAP, text: 'spaced' } },
    { raw: 'hello', tab: 'party', want: { channel: ChatChannel.PARTY, text: 'hello' } },
    { raw: '/p hello', tab: 'party', want: { channel: ChatChannel.PARTY, text: 'hello' } },
    { raw: 'hello', tab: 'combat', want: { channel: ChatChannel.MAP, text: 'hello' } },
    { raw: '/p hello', tab: 'combat', want: { channel: ChatChannel.PARTY, text: 'hello' } },
  ] as const)('parseLine($raw) from $tab', ({ raw, tab, want }) => {
    expect(parseLine(raw, tab)).toEqual(want);
  });

  it('Enter sends ChatSend with the typed text on the map channel, clears and leaves', async () => {
    const { send, input } = setup();
    await userEvent.type(input(), 'hello all{Enter}');
    expect(send).toHaveBeenCalledWith('chatSend', { channel: ChatChannel.MAP, text: 'hello all' });
    expect(input()).toHaveValue('');
    expect(input()).not.toHaveFocus();
    expect(isTyping()).toBe(false);
  });

  it.each([
    { name: 'an Enter that commits an IME composition', init: { key: 'Enter', isComposing: true } },
    { name: 'the same Enter as Safari reports it', init: { key: 'Enter', keyCode: 229 } },
    { name: 'an Escape that cancels a composition', init: { key: 'Escape', isComposing: true } },
  ])('$name stays in the field and sends nothing', async ({ init }) => {
    const { send, input } = setup();
    await userEvent.type(input(), 'konnichiwa');
    fireEvent.keyDown(input(), init);
    expect(send).not.toHaveBeenCalled();
    expect(input()).toHaveValue('konnichiwa');
    expect(input()).toHaveFocus();
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

  describe('tabs', () => {
    beforeEach(() => {
      act(() => {
        chat.getState().applyChatMessage(line(ChatChannel.MAP, 'Robin', 'hi'), 1);
        chat.getState().applyChatMessage(line(ChatChannel.PARTY, 'Ayla', 'go'), 2);
        combatLog.getState().applyExpGained(create(ExpGainedSchema, { amount: 12n, exp: 40n }));
      });
    });

    it('General shows map and party lines, Party only party lines, Combat the combat log', async () => {
      setup();
      expect(tab('General')).toHaveAttribute('aria-selected', 'true');
      expect(shown()).toEqual(['[Map] Robin: hi', '[Party] Ayla: go']);
      await userEvent.click(tab('Party'));
      expect(tab('Party')).toHaveAttribute('aria-selected', 'true');
      expect(tab('General')).toHaveAttribute('aria-selected', 'false');
      expect(shown()).toEqual(['[Party] Ayla: go']);
      await userEvent.click(tab('Combat'));
      expect(shown()).toEqual(['You gained 12 EXP.']);
    });

    it('new lines never switch the tab', async () => {
      setup();
      await userEvent.click(tab('Party'));
      act(() => {
        chat.getState().applyChatMessage(line(ChatChannel.MAP, 'Robin', 'again'), 3);
        combatLog.getState().applyLoot(loot(LootKind.COLLECTED, 'slime_gel', 1));
      });
      expect(tab('Party')).toHaveAttribute('aria-selected', 'true');
      expect(shown()).toEqual(['[Party] Ayla: go']);
    });

    it.each([
      { from: 'Party', typed: 'on my way', want: ChatChannel.PARTY, text: 'on my way' },
      { from: 'Combat', typed: 'nice drop', want: ChatChannel.MAP, text: 'nice drop' },
      { from: 'Combat', typed: '/p mine', want: ChatChannel.PARTY, text: 'mine' },
    ])('typing from $from sends "$typed" as $want', async ({ from, typed, want, text }) => {
      const { send, input } = setup();
      await userEvent.click(tab(from));
      await userEvent.type(input(), `${typed}{Enter}`);
      expect(send).toHaveBeenCalledWith('chatSend', { channel: want, text });
    });

    it('the Party tab says where a line goes', async () => {
      const { input } = setup();
      expect(input()).toHaveAttribute('placeholder', 'Enter to chat; /p for the party');
      await userEvent.click(tab('Party'));
      expect(input()).toHaveAttribute('placeholder', 'Enter to chat with the party');
    });

    it('switching tabs while typing keeps the draft and the focus', async () => {
      const { input } = setup();
      await userEvent.type(input(), 'half a');
      await userEvent.click(tab('Party'));
      expect(input()).toHaveFocus();
      expect(input()).toHaveValue('half a');
    });
  });

  describe('combat lines', () => {
    beforeEach(() => {
      act(() => {
        character.getState().applyWelcome(
          create(WelcomeSchema, {
            character: create(CharacterViewSchema, { id: 'c-ayla', name: 'Ayla', level: 4 }),
          }),
        );
      });
    });

    it.each([
      {
        name: 'EXP from a kill',
        add: () =>
          combatLog.getState().applyExpGained(create(ExpGainedSchema, { amount: 12n, exp: 40n })),
        text: 'You gained 12 EXP.',
      },
      {
        name: 'EXP lost on death',
        add: () =>
          combatLog.getState().applyExpGained(create(ExpGainedSchema, { amount: -30n, exp: 10n })),
        text: 'You lost 30 EXP.',
      },
      {
        name: 'a level-up',
        add: () =>
          combatLog
            .getState()
            .applyLevelUp(
              create(LevelUpSchema, { character: create(CharacterViewSchema, { level: 5 }) }),
            ),
        text: 'You reached level 5!',
      },
      {
        name: 'an item this player collected',
        add: () => combatLog.getState().applyLoot(loot(LootKind.COLLECTED, 'slime_gel', 2)),
        text: 'You got Slime Gel ★ ×2.',
      },
      {
        name: "a party member's item",
        add: () =>
          combatLog.getState().applyLoot(loot(LootKind.COLLECTED, 'iron_sword', 1, 'Robin')),
        text: 'Robin got Iron Sword ★★.',
      },
      {
        name: 'a drop waiting for room',
        add: () => combatLog.getState().applyLoot(loot(LootKind.WAITING, 'slime_gel', 2)),
        text: 'Inventory full: Slime Gel ★ ×2 waits on the ground.',
      },
      {
        name: 'a drop lost uncollected',
        add: () => combatLog.getState().applyLoot(loot(LootKind.LOST, 'slime_gel', 1)),
        text: 'Slime Gel ★ disappeared uncollected.',
      },
    ])('$name reads "$text"', async ({ add, text }) => {
      setup();
      await userEvent.click(tab('Combat'));
      act(add);
      expect(shown()).toEqual([text]);
    });

    it('an item name takes its star colour', async () => {
      setup();
      await userEvent.click(tab('Combat'));
      act(() => combatLog.getState().applyLoot(loot(LootKind.COLLECTED, 'gold_ring', 1)));
      expect(screen.getByText('Gold Ring ★★★★')).toHaveClass('stars-4');
    });

    it('the last eight show', async () => {
      setup();
      await userEvent.click(tab('Combat'));
      act(() => {
        for (let i = 1; i <= 10; i++) {
          combatLog.getState().applyExpGained(create(ExpGainedSchema, { amount: BigInt(i) }));
        }
      });
      expect(shown()).toHaveLength(8);
      expect(shown()[0]).toBe('You gained 3 EXP.');
    });
  });
});
