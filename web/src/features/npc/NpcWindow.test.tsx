import { create } from '@bufbuild/protobuf';
import { act, render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import { RequestError } from '@/net/socket';
import {
  InventoryChangedSchema,
  NpcDialogSchema,
  QuestConditionsSchema,
  QuestOfferState,
  QuestStatus,
  WelcomeSchema,
} from '@/protocol/messages';
import { character } from '@/state/character';
import { inventory } from '@/state/inventory';
import { quests } from '@/state/quests';
import { resetStores } from '@/testing/reset-stores';

import { NpcWindow, unmetText } from './NpcWindow';

function fakeSocket(answers: readonly (object | Error)[] = []) {
  const calls: { t: string; d: unknown }[] = [];
  const request = vi.fn((t: string, d: unknown) => {
    calls.push({ t, d });
    const a = answers[calls.length - 1] ?? { ok: true };
    return a instanceof Error ? Promise.reject(a) : Promise.resolve(a);
  });
  return { socket: { request } as never, calls };
}

function welcome(
  level: number,
  log: { questId: string; status: QuestStatus; progress?: number[] }[],
) {
  const d = create(WelcomeSchema, {
    character: { id: 'c-ayla', name: 'Ayla', class: 'warrior', level },
    quests: log,
  });
  character.getState().applyWelcome(d);
  inventory.getState().applyWelcome(d);
  quests.getState().applyWelcome(d);
}

const gels = (quantity: number) =>
  create(InventoryChangedSchema, {
    revision: 2n,
    slots: [
      {
        tab: 'etc',
        slot: 0,
        item: { itemId: 'g', template: 'slime_gel', tab: 'etc', slot: 0, quantity },
      },
    ],
  });

describe('NpcWindow', () => {
  beforeEach(() => {
    resetStores();
  });

  it('lists what the NPC gives: one to accept with its rewards, one greyed with what it needs', () => {
    welcome(1, []);
    quests.getState().applyNpcDialog(
      create(NpcDialogSchema, {
        npcId: 'town_guide',
        gives: [
          { questId: 'slime_cleanup', state: QuestOfferState.AVAILABLE },
          {
            questId: 'sticky_samples',
            state: QuestOfferState.CONDITIONS_UNMET,
            unmet: { level: 2, quests: ['slime_cleanup'] },
          },
        ],
      }),
    );
    render(<NpcWindow socket={fakeSocket().socket} onClose={vi.fn()} />);
    expect(screen.getByRole('dialog', { name: 'Town Guide' })).toBeInTheDocument();
    const offered = within(screen.getByRole('list', { name: 'Quests offered' })).getAllByRole(
      'listitem',
    );
    expect(offered[0]).toHaveTextContent('Slime Cleanup');
    expect(offered[0]).toHaveTextContent('Rewards: 30 EXP, Red Potion ×10');
    expect(within(offered[0]!).getByRole('button', { name: 'Accept' })).toBeEnabled();
    expect(offered[1]).toHaveAttribute('aria-disabled', 'true');
    expect(offered[1]).toHaveTextContent('Needs level 2 · Finish Slime Cleanup first');
    expect(within(offered[1]!).queryByRole('button')).toBeNull();
  });

  it('Accept sends the NPC and the quest, and closes the dialog once taken', async () => {
    welcome(1, []);
    quests.getState().applyNpcDialog(
      create(NpcDialogSchema, {
        npcId: 'town_guide',
        gives: [{ questId: 'slime_cleanup', state: QuestOfferState.AVAILABLE }],
      }),
    );
    const { socket, calls } = fakeSocket();
    const onClose = vi.fn();
    render(<NpcWindow socket={socket} onClose={onClose} />);
    await userEvent.click(screen.getByRole('button', { name: 'Accept' }));
    expect(calls).toEqual([
      { t: 'acceptQuest', d: { npcId: 'town_guide', questId: 'slime_cleanup' } },
    ]);
    expect(onClose).toHaveBeenCalledOnce();
  });

  it('a refusal stays open and says why', async () => {
    welcome(1, []);
    quests.getState().applyNpcDialog(
      create(NpcDialogSchema, {
        npcId: 'town_guide',
        gives: [{ questId: 'slime_cleanup', state: QuestOfferState.AVAILABLE }],
      }),
    );
    const { socket } = fakeSocket([new RequestError('npc_out_of_reach', 'move closer')]);
    const onClose = vi.fn();
    render(<NpcWindow socket={socket} onClose={onClose} />);
    await userEvent.click(screen.getByRole('button', { name: 'Accept' }));
    expect(screen.getByRole('alert')).toHaveTextContent('move closer');
    expect(onClose).not.toHaveBeenCalled();
  });

  it('Hand in waits for every objective, the items held included, then sends the quest', async () => {
    welcome(2, [{ questId: 'sticky_samples', status: QuestStatus.ACTIVE, progress: [0] }]);
    quests.getState().applyNpcDialog(
      create(NpcDialogSchema, {
        npcId: 'town_guide',
        gives: [{ questId: 'sticky_samples', state: QuestOfferState.ACTIVE }],
        takes: [{ questId: 'sticky_samples', ready: true }],
      }),
    );
    const { socket, calls } = fakeSocket();
    const onClose = vi.fn();
    render(<NpcWindow socket={socket} onClose={onClose} />);
    const handIn = screen.getByRole('button', { name: 'Hand in' });
    expect(handIn).toBeDisabled();
    expect(screen.getByRole('list', { name: 'Sticky Samples objectives' })).toHaveTextContent(
      'Slime Gel 0/5',
    );
    act(() => inventory.getState().applyInventoryChanged(gels(5)));
    expect(screen.getByText('Ready to hand in to Town Guide')).toBeInTheDocument();
    await userEvent.click(handIn);
    expect(calls).toEqual([
      { t: 'handInQuest', d: { npcId: 'town_guide', questId: 'sticky_samples' } },
    ]);
    expect(onClose).toHaveBeenCalledOnce();
    // Under way and taken back here, it shows once, under Hand in.
    expect(screen.queryByText('In progress')).toBeNull();
    expect(screen.getAllByText('Sticky Samples')).toHaveLength(1);
  });

  it('an NPC with nothing to give or take says so', () => {
    welcome(1, []);
    quests.getState().applyNpcDialog(create(NpcDialogSchema, { npcId: 'town_guide' }));
    render(<NpcWindow socket={fakeSocket().socket} onClose={vi.fn()} />);
    expect(screen.getByText('Nothing for you right now.')).toBeInTheDocument();
  });

  it.each([
    { u: { level: 10, class: '', quests: [] }, text: 'Needs level 10' },
    { u: { level: 0, class: 'bowman', quests: [] }, text: 'Bowman only' },
    {
      u: { level: 0, class: '', quests: ['slime_cleanup', 'later'] },
      text: 'Finish Slime Cleanup, later first',
    },
    { u: undefined, text: 'Not available yet' },
  ])('unmet conditions read "$text"', ({ u, text }) => {
    expect(unmetText(u && create(QuestConditionsSchema, u))).toBe(text);
  });
});
