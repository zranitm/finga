import { create } from '@bufbuild/protobuf';
import { act, render, screen } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import { ERR } from '@/protocol/errors';
import { QuestChangedSchema, QuestStatus, WelcomeSchema } from '@/protocol/messages';
import { character } from '@/state/character';
import { inventory } from '@/state/inventory';
import { quests } from '@/state/quests';
import { refusal } from '@/testing/refusal';
import { resetStores } from '@/testing/reset-stores';
import { user } from '@/testing/user';

import { QuestLog } from './QuestLog';

function fakeSocket(answers: readonly (object | Error)[] = []) {
  const calls: { t: string; d: unknown }[] = [];
  const request = vi.fn((t: string, d: unknown) => {
    calls.push({ t, d });
    const a = answers[calls.length - 1] ?? { ok: true };
    return a instanceof Error ? Promise.reject(a) : Promise.resolve(a);
  });
  return { socket: { request } as never, calls };
}

function enter(
  log: { questId: string; status: QuestStatus; progress?: number[]; timesFinished?: number }[],
  gels = 0,
) {
  const d = create(WelcomeSchema, {
    character: { id: 'c-ayla', name: 'Ayla', class: 'warrior', level: 2 },
    inventory:
      gels > 0 ? [{ itemId: 'g', template: 'slime_gel', tab: 'etc', slot: 0, quantity: gels }] : [],
    quests: log,
  });
  character.getState().applyWelcome(d);
  inventory.getState().applyWelcome(d);
  quests.getState().applyWelcome(d);
}

describe('QuestLog', () => {
  beforeEach(() => {
    resetStores();
  });

  it('shows each active quest with its counts, items from the inventory, and which is ready', () => {
    enter(
      [
        { questId: 'slime_cleanup', status: QuestStatus.ACTIVE, progress: [10] },
        { questId: 'sticky_samples', status: QuestStatus.ACTIVE, progress: [0] },
      ],
      3,
    );
    render(<QuestLog socket={fakeSocket().socket} onClose={vi.fn()} />);
    const active = screen.getByRole('list', { name: 'Active quests' });
    expect([...active.querySelectorAll('strong')].map((s) => s.textContent)).toEqual([
      'Slime Cleanup',
      'Sticky Samples',
    ]);
    expect(screen.getByRole('list', { name: 'Slime Cleanup objectives' })).toHaveTextContent(
      'Green Slime 10/10 ✓Ready to hand in to Town Guide',
    );
    expect(screen.getByRole('list', { name: 'Sticky Samples objectives' })).toHaveTextContent(
      'Slime Gel 3/5',
    );
  });

  it('follows the log: a kill counted updates its line', () => {
    enter([{ questId: 'slime_cleanup', status: QuestStatus.ACTIVE, progress: [2] }]);
    render(<QuestLog socket={fakeSocket().socket} onClose={vi.fn()} />);
    act(() =>
      quests.getState().applyQuestChanged(
        create(QuestChangedSchema, {
          quest: { questId: 'slime_cleanup', status: QuestStatus.ACTIVE, progress: [3] },
        }),
      ),
    );
    expect(screen.getByRole('list', { name: 'Slime Cleanup objectives' })).toHaveTextContent(
      'Green Slime 3/10',
    );
  });

  it('Abandon asks first, then sends the quest', async () => {
    enter([{ questId: 'slime_cleanup', status: QuestStatus.ACTIVE, progress: [2] }]);
    const { socket, calls } = fakeSocket();
    render(<QuestLog socket={socket} onClose={vi.fn()} />);
    await user.click(screen.getByRole('button', { name: 'Abandon' }));
    expect(screen.getByText('Abandon Slime Cleanup? Its progress is lost.')).toBeInTheDocument();
    await user.click(screen.getByRole('button', { name: 'Keep' }));
    expect(calls).toEqual([]);
    await user.click(screen.getByRole('button', { name: 'Abandon' }));
    const confirm = screen.getAllByRole('button', { name: 'Abandon' });
    await user.click(confirm.at(-1)!);
    expect(calls).toEqual([{ t: 'abandonQuest', d: { questId: 'slime_cleanup' } }]);
  });

  it('a refused abandon says why', async () => {
    enter([{ questId: 'slime_cleanup', status: QuestStatus.ACTIVE, progress: [2] }]);
    const { socket } = fakeSocket([refusal(ERR.UNAVAILABLE)]);
    render(<QuestLog socket={socket} onClose={vi.fn()} />);
    await user.click(screen.getByRole('button', { name: 'Abandon' }));
    await user.click(screen.getAllByRole('button', { name: 'Abandon' }).at(-1)!);
    expect(screen.getByRole('alert')).toHaveTextContent('The server is busy. Try again.');
  });

  it('with nothing under way it says where quests come from, and counts the finished ones', () => {
    enter([{ questId: 'slime_cleanup', status: QuestStatus.FINISHED, timesFinished: 1 }]);
    render(<QuestLog socket={fakeSocket().socket} onClose={vi.fn()} />);
    expect(
      screen.getByText('No quests under way. Talk to the people in town.'),
    ).toBeInTheDocument();
    expect(screen.getByText('Quests finished: 1')).toBeInTheDocument();
  });
});
