import { readFileSync } from 'node:fs';
import path from 'node:path';

import { create } from '@bufbuild/protobuf';
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import { isTyping } from '@/input/typing';
import type { Settings } from '@/net/rest';
import {
  CharacterViewSchema,
  ChatChannel,
  ChatMessageSchema,
  ExpGainedSchema,
  LevelUpSchema,
  LootKind,
  LootSchema,
  PromotedSchema,
  QuestChange,
  QuestChangedSchema,
  QuestStatus,
  WelcomeSchema,
} from '@/protocol/messages';
import { character } from '@/state/character';
import { chat, MAX_LINES } from '@/state/chat';
import { combatLog, MAX_COMBAT_LINES } from '@/state/combat-log';
import { settings } from '@/state/settings';
import { toasts } from '@/state/toasts';
import { ui } from '@/state/ui';
import { resetStores } from '@/testing/reset-stores';
import { user } from '@/testing/user';

import { ChatBox, MAX_CHAT_CHARS, parseLine } from './ChatBox';

const line = (channel: ChatChannel, senderName: string, text: string) =>
  create(ChatMessageSchema, { channel, sender: `c-${senderName}`, senderName, text });

const questChanged = (change: QuestChange, questId: string, progress: number[] = []) =>
  create(QuestChangedSchema, {
    change,
    quest: { questId, status: QuestStatus.ACTIVE, progress },
  });

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

const chatInput = () => screen.queryByLabelText('Chat message');
const isOpen = () => screen.queryByRole('tablist', { name: 'Chat tabs' }) !== null;

/** Renders the chat, open as Enter leaves it unless asked idle. */
function setup({ open = true } = {}) {
  const send = vi.fn(() => true);
  const putSettings = vi.fn((s: Settings) => Promise.resolve(s));
  render(<ChatBox socket={{ send } as never} rest={{ putSettings }} />);
  if (open) {
    act(() => ui.getState().openChat());
  }
  return { send, putSettings, input: () => screen.getByLabelText('Chat message') };
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

  it('Enter sends ChatSend with the typed text on the map channel, clears, and closes the chat', async () => {
    const { send, input } = setup();
    await user.type(input(), 'hello all{Enter}');
    expect(send).toHaveBeenCalledWith('chatSend', { channel: ChatChannel.MAP, text: 'hello all' });
    expect(isOpen()).toBe(false);
    expect(chatInput()).toBeNull();
    expect(isTyping(document.activeElement)).toBe(false);
    act(() => ui.getState().openChat());
    expect(input()).toHaveValue('');
  });

  it.each([
    { name: 'an Enter that commits an IME composition', init: { key: 'Enter', isComposing: true } },
    { name: 'the same Enter as Safari reports it', init: { key: 'Enter', keyCode: 229 } },
    { name: 'an Escape that cancels a composition', init: { key: 'Escape', isComposing: true } },
  ])('$name stays in the field and sends nothing', async ({ init }) => {
    const { send, input } = setup();
    await user.type(input(), 'konnichiwa');
    fireEvent.keyDown(input(), init);
    expect(send).not.toHaveBeenCalled();
    expect(input()).toHaveValue('konnichiwa');
    expect(input()).toHaveFocus();
  });

  it('/p sends a party line', async () => {
    const { send, input } = setup();
    await user.type(input(), '/p on my way{Enter}');
    expect(send).toHaveBeenCalledWith('chatSend', {
      channel: ChatChannel.PARTY,
      text: 'on my way',
    });
  });

  it('121 characters is blocked with the counter red; an empty line sends nothing', async () => {
    const { send, input } = setup();
    input().focus();
    await user.paste('x'.repeat(MAX_CHAT_CHARS)); // a key at a time, 120 renders took a second on a busy machine
    await user.keyboard('x');
    expect(screen.getByLabelText('Characters')).toHaveTextContent('121/120');
    expect(screen.getByLabelText('Characters')).toHaveClass('chat-over');
    await user.keyboard('{Enter}');
    expect(send).not.toHaveBeenCalled();
    await user.clear(input());
    await user.keyboard('{Enter}');
    expect(send).not.toHaveBeenCalled();
  });

  it('counts an emoji as one character, as the server does, so a full line of them sends', async () => {
    const { send, input } = setup();
    input().focus();
    await user.paste('🙂'.repeat(MAX_CHAT_CHARS));
    expect(screen.getByLabelText('Characters')).toHaveTextContent('120/120');
    expect(screen.getByLabelText('Characters')).not.toHaveClass('chat-over');
    await user.keyboard('{Enter}');
    expect(send).toHaveBeenCalledWith('chatSend', {
      channel: ChatChannel.MAP,
      text: '🙂'.repeat(MAX_CHAT_CHARS),
    });
  });

  it('the Enter key (through the ui store) opens it with the input focused, which then has the keyboard; Escape closes it', async () => {
    const { input } = setup({ open: false });
    expect(isOpen()).toBe(false);
    act(() => ui.getState().openChat());
    expect(isOpen()).toBe(true);
    expect(input()).toHaveFocus();
    expect(isTyping(document.activeElement)).toBe(true);
    await user.keyboard('{Escape}');
    expect(isOpen()).toBe(false);
    expect(isTyping(document.activeElement)).toBe(false);
    expect(ui.getState().chatOpen).toBe(false);
  });

  it('a draft half typed is still there when the chat opens again', async () => {
    const { input } = setup();
    await user.type(input(), 'half a');
    await user.keyboard('{Escape}');
    act(() => ui.getState().openChat());
    expect(input()).toHaveValue('half a');
  });

  describe('idle', () => {
    beforeEach(() => {
      act(() => {
        for (let i = 1; i <= 5; i++) {
          chat.getState().applyChatMessage(line(ChatChannel.MAP, 'Robin', `l${i}`));
        }
        chat.getState().applyChatMessage(line(ChatChannel.PARTY, 'Ayla', 'go'));
      });
    });

    it('shows only the last three lines, with no tabs or input', () => {
      setup({ open: false });
      expect(shown()).toEqual(['[Map] Robin: l4', '[Map] Robin: l5', '[Party] Ayla: go']);
      expect(isOpen()).toBe(false);
      expect(chatInput()).toBeNull();
    });

    it('a click on the lines opens the chat with the input focused', async () => {
      setup({ open: false });
      await user.click(screen.getByLabelText('Chat'));
      expect(isOpen()).toBe(true);
      expect(chatInput()).toHaveFocus();
      expect(shown()).toHaveLength(6);
    });

    it('shows the tab chosen while it was open', async () => {
      setup();
      await user.click(tab('Party'));
      await user.keyboard('{Escape}');
      expect(shown()).toEqual(['[Party] Ayla: go']);
    });
  });

  describe('pin', () => {
    const pin = () => screen.getByRole('button', { name: 'Pin' });

    it('keeps the chat open after Escape and after sending, with the keys back to the game, and is saved', async () => {
      const { send, putSettings, input } = setup();
      await user.click(pin());
      expect(pin()).toHaveAttribute('aria-pressed', 'true');
      expect(input()).toHaveFocus();
      expect(settings.getState().chatPinned).toBe(true);
      expect(putSettings).toHaveBeenCalledWith({
        control_preset: 'classic',
        auto_assign_points: false,
        chat_pinned: true,
      });
      await user.keyboard('{Escape}');
      expect(isOpen()).toBe(true);
      expect(isTyping(document.activeElement)).toBe(false);
      await user.type(input(), 'still here{Enter}');
      expect(send).toHaveBeenCalledWith('chatSend', {
        channel: ChatChannel.MAP,
        text: 'still here',
      });
      expect(isOpen()).toBe(true);
      expect(input()).not.toHaveFocus();
      expect(isTyping(document.activeElement)).toBe(false);
    });

    it('the account’s pinned chat is open from the start, and unpinning it closes it', async () => {
      act(() => settings.getState().setChatPinned(true));
      const { putSettings } = setup({ open: false });
      expect(isOpen()).toBe(true);
      expect(chatInput()).not.toHaveFocus();
      await user.click(pin());
      expect(isOpen()).toBe(false);
      expect(putSettings).toHaveBeenCalledWith(expect.objectContaining({ chat_pinned: false }));
    });

    it('a pin that fails to save says so, and stays for now', async () => {
      const { putSettings } = setup();
      putSettings.mockRejectedValueOnce(new Error('offline'));
      await user.click(pin());
      await waitFor(() =>
        expect(toasts.getState().toasts.map((t) => t.text)).toEqual([
          'Could not save the setting.',
        ]),
      );
      expect(settings.getState().chatPinned).toBe(true);
      expect(isOpen()).toBe(true);
    });
  });

  it('open, the log scrolls, oldest line first', () => {
    const style = document.createElement('style');
    style.textContent = readFileSync(path.resolve(process.cwd(), 'src/styles/global.css'), 'utf8');
    document.head.append(style);
    try {
      setup();
      act(() => chat.getState().applyChatMessage(line(ChatChannel.MAP, 'Robin', 'hi')));
      const list = screen.getByRole('list');
      expect(getComputedStyle(list)).toMatchObject({ overflowY: 'auto', display: 'block' });
      expect(screen.getByRole('listitem').style.order).toBe('');
    } finally {
      style.remove();
    }
  });

  it('the log draws the newest line at the bottom and moves any line that does not fit whole out of view', () => {
    const style = document.createElement('style');
    style.textContent = readFileSync(path.resolve(process.cwd(), 'src/styles/global.css'), 'utf8');
    document.head.append(style);
    try {
      setup({ open: false });
      act(() => {
        chat.getState().applyChatMessage(line(ChatChannel.MAP, 'Robin', 'a long line '.repeat(20)));
        chat.getState().applyChatMessage(line(ChatChannel.MAP, 'Robin', 'newest'));
      });
      const list = screen.getByRole('list');
      // jsdom lays nothing out, so this checks the rules the browser draws by.
      expect(getComputedStyle(list)).toMatchObject({
        display: 'flex',
        flexDirection: 'column-reverse',
        flexWrap: 'wrap',
        overflow: 'hidden',
      });
      const items = screen.getAllByRole('listitem');
      expect(items.at(-1)).toHaveTextContent('newest'); // the markup stays oldest first, for screen readers
      expect(items.map((li) => li.style.order)).toEqual(['0', '-1']);
      expect(items.map((li) => getComputedStyle(li).width)).toEqual(['100%', '100%']);
      // Idle, a line taller than the box shows its first three rows, tag included, rather than its last.
      expect(getComputedStyle(items[0]!).getPropertyValue('-webkit-line-clamp')).toBe('3');
    } finally {
      style.remove();
    }
  });

  it('an incoming ChatMessage renders with the right tag, and the open log keeps every line the store does', () => {
    setup();
    act(() => {
      chat.getState().applyChatMessage(line(ChatChannel.MAP, 'Robin', 'hi'));
      chat.getState().applyChatMessage(line(ChatChannel.PARTY, 'Ayla', 'go'));
    });
    const items = screen.getAllByRole('listitem');
    expect(items[0]).toHaveTextContent('[Map] Robin: hi');
    expect(items[1]).toHaveTextContent('[Party] Ayla: go');
    expect(items[1]).toHaveClass('chat-party');
    act(() => {
      for (let i = 0; i < MAX_LINES; i++) {
        chat.getState().applyChatMessage(line(ChatChannel.MAP, 'N', `l${i}`));
      }
    });
    expect(shown()).toHaveLength(MAX_LINES);
    expect(shown()[0]).toBe('[Map] N: l0');
  });

  describe('tabs', () => {
    beforeEach(() => {
      act(() => {
        chat.getState().applyChatMessage(line(ChatChannel.MAP, 'Robin', 'hi'));
        chat.getState().applyChatMessage(line(ChatChannel.PARTY, 'Ayla', 'go'));
        combatLog.getState().applyExpGained(create(ExpGainedSchema, { amount: 12n, exp: 40n }));
      });
    });

    it('General shows map and party lines and notices, Party only party lines, Combat the combat log', async () => {
      setup();
      act(() => {
        chat.getState().notice('King Slime has appeared in the Forest!');
      });
      expect(tab('General')).toHaveAttribute('aria-selected', 'true');
      expect(shown()).toEqual([
        '[Map] Robin: hi',
        '[Party] Ayla: go',
        '[Notice] King Slime has appeared in the Forest!',
      ]);
      expect(screen.getAllByRole('listitem')[2]).toHaveClass('chat-notice');
      await user.click(tab('Party'));
      expect(tab('Party')).toHaveAttribute('aria-selected', 'true');
      expect(tab('General')).toHaveAttribute('aria-selected', 'false');
      expect(shown()).toEqual(['[Party] Ayla: go']);
      await user.click(tab('Combat'));
      expect(shown()).toEqual(['You gained 12 EXP.']);
    });

    it('new lines never switch the tab', async () => {
      setup();
      await user.click(tab('Party'));
      act(() => {
        chat.getState().applyChatMessage(line(ChatChannel.MAP, 'Robin', 'again'));
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
      await user.click(tab(from));
      await user.type(input(), `${typed}{Enter}`);
      expect(send).toHaveBeenCalledWith('chatSend', { channel: want, text });
    });

    it('each tab says where a line goes', async () => {
      const { input } = setup();
      expect(input()).toHaveAttribute('placeholder', 'Enter to chat; /p for the party');
      await user.click(tab('Party'));
      expect(input()).toHaveAttribute('placeholder', 'Enter to chat with the party');
      await user.click(tab('Combat'));
      expect(input()).toHaveAttribute('placeholder', 'Enter to chat on the map; /p for the party');
    });

    it('switching tabs while typing keeps the draft and the focus', async () => {
      const { input } = setup();
      await user.type(input(), 'half a');
      await user.click(tab('Party'));
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
        name: 'a promotion',
        add: () =>
          combatLog.getState().applyPromoted(
            create(PromotedSchema, {
              character: create(CharacterViewSchema, { class: 'warrior' }),
            }),
          ),
        text: 'You became a Warrior!',
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
      {
        name: 'a skill evolved',
        add: () => combatLog.getState().applySkillPoints({ strike: 9 }, { strike: 10 }),
        text: 'Strike evolved into Heavy Strike!',
      },
      {
        name: 'a potion drunk',
        add: () => combatLog.getState().drank('red_potion', { hp: 50, mp: 0 }),
        text: 'Drank a Red Potion ★.',
      },
      {
        name: 'a quest taken',
        add: () =>
          combatLog
            .getState()
            .applyQuestChanged(questChanged(QuestChange.ACCEPTED, 'slime_cleanup')),
        text: 'Quest taken: Slime Cleanup.',
      },
      {
        name: 'a kill counted',
        add: () =>
          combatLog
            .getState()
            .applyQuestChanged(questChanged(QuestChange.PROGRESSED, 'slime_cleanup', [3])),
        text: 'Slime Cleanup: Green Slime 3/10',
      },
      {
        name: 'a quest ready to hand in',
        add: () => combatLog.getState().questReady('sticky_samples'),
        text: 'Sticky Samples is ready to hand in to Town Guide.',
      },
      {
        name: 'a quest finished',
        add: () =>
          combatLog
            .getState()
            .applyQuestChanged(questChanged(QuestChange.HANDED_IN, 'slime_cleanup')),
        text: 'Quest finished: Slime Cleanup! You got Red Potion ★ ×10.',
      },
      {
        name: 'a quest abandoned',
        add: () =>
          combatLog
            .getState()
            .applyQuestChanged(questChanged(QuestChange.ABANDONED, 'slime_cleanup')),
        text: 'Quest abandoned: Slime Cleanup.',
      },
      {
        name: 'a quest this client does not know',
        add: () =>
          combatLog.getState().applyQuestChanged(questChanged(QuestChange.ACCEPTED, 'later')),
        text: 'Quest taken: later.',
      },
    ])('$name reads "$text"', async ({ add, text }) => {
      setup();
      await user.click(tab('Combat'));
      act(add);
      expect(shown()).toEqual([text]);
    });

    it('an item name takes its star colour', async () => {
      setup();
      await user.click(tab('Combat'));
      act(() => combatLog.getState().applyLoot(loot(LootKind.COLLECTED, 'gold_ring', 1)));
      expect(screen.getByText('Gold Ring ★★★★')).toHaveClass('stars-4');
    });

    it('the tab keeps every line the store does, a boss kill’s drops never pushing out its EXP', async () => {
      setup();
      await user.click(tab('Combat'));
      act(() => {
        combatLog.getState().applyExpGained(create(ExpGainedSchema, { amount: 900n }));
        for (let i = 0; i < 11; i++) {
          combatLog
            .getState()
            .applyLoot(loot(LootKind.COLLECTED, 'red_potion', 1, i % 2 ? 'Robin' : 'Ayla'));
        }
      });
      expect(shown()).toHaveLength(12);
      expect(shown()[0]).toBe('You gained 900 EXP.');
      act(() => {
        for (let i = 1; i <= MAX_COMBAT_LINES; i++) {
          combatLog.getState().applyExpGained(create(ExpGainedSchema, { amount: BigInt(i) }));
        }
      });
      expect(shown()).toHaveLength(MAX_COMBAT_LINES);
      expect(shown()[0]).toBe('You gained 1 EXP.');
    });

    it('the open log follows the newest line until the player scrolls up, and again once they scroll back down', async () => {
      // jsdom lays nothing out: a log 1000 px tall in a box 112 px tall.
      const tops = new WeakMap<Element, number>();
      vi.spyOn(Element.prototype, 'scrollTop', 'get').mockImplementation(function (this: Element) {
        return tops.get(this) ?? 0;
      });
      vi.spyOn(Element.prototype, 'scrollTop', 'set').mockImplementation(function (
        this: Element,
        top: number,
      ) {
        tops.set(this, top);
      });
      vi.spyOn(Element.prototype, 'scrollHeight', 'get').mockReturnValue(1000);
      vi.spyOn(Element.prototype, 'clientHeight', 'get').mockReturnValue(112);
      const gain = (amount: number) =>
        act(() =>
          combatLog.getState().applyExpGained(create(ExpGainedSchema, { amount: BigInt(amount) })),
        );
      gain(1);
      setup();
      await user.click(tab('Combat'));
      const log = screen.getByRole('list');
      expect(log.scrollTop).toBe(1000); // opened at the newest
      log.scrollTop = 200;
      fireEvent.scroll(log);
      gain(2);
      expect(log.scrollTop).toBe(200); // reading back, it stays put
      log.scrollTop = 1000 - 112;
      fireEvent.scroll(log);
      gain(3);
      expect(log.scrollTop).toBe(1000);
    });
  });
});
