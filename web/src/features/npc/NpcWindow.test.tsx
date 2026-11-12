import { create } from '@bufbuild/protobuf';
import { act, render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import { RequestError } from '@/net/socket';
import { npcById } from '@/progression/quests';
import { ERR } from '@/protocol/errors';
import {
  InventoryChangedSchema,
  LootKind,
  LootSchema,
  NpcDialogSchema,
  PromotionConditionKind,
  PromotionConditionSchema,
  QuestConditionsSchema,
  QuestOfferState,
  QuestStatus,
  StatsChangedSchema,
  WelcomeSchema,
} from '@/protocol/messages';
import { character } from '@/state/character';
import { combatLog } from '@/state/combat-log';
import { inventory } from '@/state/inventory';
import { quests } from '@/state/quests';
import { refusal } from '@/testing/refusal';
import { resetStores } from '@/testing/reset-stores';

import { NpcWindow, conditionText, unmetText } from './NpcWindow';

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

/** A level 10 Beginner with ten points in Strike, the Warrior's Trial finished, and str of their own. */
function trainee(str: number) {
  const d = create(WelcomeSchema, {
    character: {
      id: 'c-ayla',
      name: 'Ayla',
      class: 'beginner',
      level: 10,
      stats: { str, dex: 7, int: 7, agi: 7, vit: 7 },
      sheet: { skills: { unspent: 0, invested: { strike: 10 } } },
    },
    quests: [{ questId: 'warriors_trial', status: QuestStatus.FINISHED, timesFinished: 1 }],
  });
  character.getState().applyWelcome(d);
  inventory.getState().applyWelcome(d);
  quests.getState().applyWelcome(d);
}

// The Warrior Trainer's offer as the world counted it at the talk, its STR condition at have.
const warriorOffer = (str: number) =>
  create(NpcDialogSchema, {
    npcId: 'warrior_trainer',
    promotion: {
      class: 'warrior',
      conditions: [
        { kind: PromotionConditionKind.LEVEL, need: 10, have: 10 },
        { kind: PromotionConditionKind.STAT, id: 'str', need: 30, have: str },
        { kind: PromotionConditionKind.SKILL, id: 'strike', need: 10, have: 10 },
        { kind: PromotionConditionKind.QUEST, id: 'warriors_trial', need: 1, have: 1 },
      ],
    },
  });

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

/** Equip slots from..to-1 holding leather caps, or emptied. */
const caps = (from: number, to: number, held = true) =>
  create(InventoryChangedSchema, {
    revision: 3n + BigInt(held ? 0 : 1),
    slots: Array.from({ length: to - from }, (_, k) => ({
      tab: 'equip',
      slot: from + k,
      ...(held && {
        item: {
          itemId: `cap-${from + k}`,
          template: 'leather_cap',
          tab: 'equip',
          slot: from + k,
          quantity: 1,
        },
      }),
    })),
  });

/** A cap of Ayla's on the ground, waiting for room in her Equip tab. */
const waitingCap = () =>
  create(LootSchema, {
    kind: LootKind.WAITING,
    mapId: 'forest',
    dropId: 7,
    ownerId: 'c-ayla',
    ownerName: 'Ayla',
    template: 'leather_cap',
    quantity: 1,
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
    const { socket } = fakeSocket([refusal(ERR.NPC_OUT_OF_REACH)]);
    const onClose = vi.fn();
    render(<NpcWindow socket={socket} onClose={onClose} />);
    await userEvent.click(screen.getByRole('button', { name: 'Accept' }));
    expect(screen.getByRole('alert')).toHaveTextContent('Move closer.');
    expect(onClose).not.toHaveBeenCalled();
  });

  it("a request with no answer says only that, not the socket's own words", async () => {
    welcome(1, []);
    quests.getState().applyNpcDialog(
      create(NpcDialogSchema, {
        npcId: 'town_guide',
        gives: [{ questId: 'slime_cleanup', state: QuestOfferState.AVAILABLE }],
      }),
    );
    const { socket } = fakeSocket([
      new RequestError('timeout', 'acceptQuest: no response within 5000 ms'),
    ]);
    render(<NpcWindow socket={socket} onClose={vi.fn()} />);
    await userEvent.click(screen.getByRole('button', { name: 'Accept' }));
    expect(screen.getByRole('alert')).toHaveTextContent('No answer yet. Try again.');
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

  describe('room for the reward', () => {
    const readySamples = () => {
      welcome(2, [{ questId: 'sticky_samples', status: QuestStatus.ACTIVE, progress: [0] }]);
      quests.getState().applyNpcDialog(
        create(NpcDialogSchema, {
          npcId: 'town_guide',
          takes: [{ questId: 'sticky_samples', ready: true }],
        }),
      );
      act(() => inventory.getState().applyInventoryChanged(gels(5)));
    };

    it("a hand-in whose reward doesn't fit isn't asked for: it says how many slots to free, a drop waiting for room counted in", async () => {
      readySamples();
      act(() => {
        inventory.getState().applyInventoryChanged(caps(0, 24));
        combatLog.getState().applyLoot(waitingCap());
      });
      const { socket, calls } = fakeSocket();
      const onClose = vi.fn();
      render(<NpcWindow socket={socket} onClose={onClose} />);
      await userEvent.click(screen.getByRole('button', { name: 'Hand in' }));
      expect(calls).toEqual([]);
      expect(screen.getByRole('alert')).toHaveTextContent(
        'Free 2 slots in your Equip tab to take this reward.',
      );
      act(() => inventory.getState().applyInventoryChanged(caps(22, 24, false)));
      await userEvent.click(screen.getByRole('button', { name: 'Hand in' }));
      expect(calls).toEqual([
        { t: 'handInQuest', d: { npcId: 'town_guide', questId: 'sticky_samples' } },
      ]);
      expect(onClose).toHaveBeenCalledOnce();
    });

    it('a hand-in the item service refuses for a full tab says what to free, counted again', async () => {
      readySamples();
      act(() => {
        inventory.getState().applyInventoryChanged(caps(0, 23));
        combatLog.getState().applyLoot(waitingCap()); // it landed in the last slot as the hand-in went
      });
      const { socket, calls } = fakeSocket([refusal(ERR.EQUIP_TAB_FULL)]);
      render(<NpcWindow socket={socket} onClose={vi.fn()} />);
      await userEvent.click(screen.getByRole('button', { name: 'Hand in' }));
      expect(calls).toHaveLength(1);
      expect(screen.getByRole('alert')).toHaveTextContent(
        'Free 1 slot in your Equip tab to take this reward.',
      );
    });

    it("a promotion whose weapon doesn't fit isn't asked for, and says so", async () => {
      trainee(30);
      quests.getState().applyNpcDialog(warriorOffer(30));
      act(() => inventory.getState().applyInventoryChanged(caps(0, 24)));
      const { socket, calls } = fakeSocket();
      render(<NpcWindow socket={socket} onClose={vi.fn()} />);
      await userEvent.click(screen.getByRole('button', { name: 'Become a Warrior' }));
      await userEvent.click(
        within(screen.getByRole('dialog', { name: 'Become a Warrior?' })).getByRole('button', {
          name: 'Become a Warrior',
        }),
      );
      expect(calls).toEqual([]);
      expect(screen.getByRole('alert')).toHaveTextContent(
        'Free 1 slot in your Equip tab to take your weapon.',
      );
    });
  });

  it('an NPC with nothing to give or take says so', () => {
    welcome(1, []);
    quests.getState().applyNpcDialog(create(NpcDialogSchema, { npcId: 'town_guide' }));
    render(<NpcWindow socket={fakeSocket().socket} onClose={vi.fn()} />);
    expect(screen.getByText('Nothing for you right now.')).toBeInTheDocument();
  });

  it('an NPC with lines says them in place of having nothing for you', () => {
    welcome(15, []);
    quests.getState().applyNpcDialog(create(NpcDialogSchema, { npcId: 'crossroads_guide' }));
    render(<NpcWindow socket={fakeSocket().socket} onClose={vi.fn()} />);
    const lines = npcById('crossroads_guide')!.lines;
    expect(lines.length).toBeGreaterThan(0);
    for (const line of lines) {
      expect(screen.getByText(line)).toBeInTheDocument();
    }
    expect(screen.queryByText('Nothing for you right now.')).toBeNull();
  });

  it("a trainer's promotion lists each condition, and its button waits for every one", () => {
    trainee(27);
    quests.getState().applyNpcDialog(warriorOffer(27));
    render(<NpcWindow socket={fakeSocket().socket} onClose={vi.fn()} />);
    const offer = screen.getByRole('region', { name: 'Promotion' });
    expect(within(offer).getByRole('heading')).toHaveTextContent('Become a Warrior');
    const conditions = within(offer)
      .getAllByRole('listitem')
      .map((li) => li.textContent);
    expect(conditions).toEqual([
      'Level 10 ✓',
      'STR 30 without gear · yours 27',
      '10 points in Strike ✓',
      "The Warrior's Trial finished ✓",
    ]);
    expect(within(offer).getByRole('button', { name: 'Become a Warrior' })).toBeDisabled();
    expect(screen.queryByText('Nothing for you right now.')).toBeNull();
  });

  it('an open dialog follows a spend that meets the last condition, without talking again', () => {
    trainee(27);
    quests.getState().applyNpcDialog(warriorOffer(27));
    const { socket, calls } = fakeSocket();
    render(<NpcWindow socket={socket} onClose={vi.fn()} />);
    expect(screen.getByRole('button', { name: 'Become a Warrior' })).toBeDisabled();
    act(() =>
      character.getState().applyStatsChanged(
        create(StatsChangedSchema, {
          sheet: {
            primary: { str: 30, dex: 7, int: 7, agi: 7, vit: 7 },
            skills: { unspent: 0, invested: { strike: 10 } },
          },
          hp: 50,
          maxHp: 50,
        }),
      ),
    );
    expect(screen.getByText('STR 30 without gear ✓')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Become a Warrior' })).toBeEnabled();
    expect(calls).toEqual([]);
  });

  it("promoting asks first, saying it's for good, then sends the class and closes once done", async () => {
    trainee(30);
    quests.getState().applyNpcDialog(warriorOffer(30));
    const { socket, calls } = fakeSocket();
    const onClose = vi.fn();
    render(<NpcWindow socket={socket} onClose={onClose} />);
    await userEvent.click(screen.getByRole('button', { name: 'Become a Warrior' }));
    const confirm = screen.getByRole('dialog', { name: 'Become a Warrior?' });
    expect(confirm).toHaveTextContent('A promotion is for good');
    expect(confirm).toHaveTextContent('The trainer hands you your first weapon.');
    await userEvent.click(within(confirm).getByRole('button', { name: 'Not yet' }));
    expect(screen.queryByRole('dialog', { name: 'Become a Warrior?' })).toBeNull();
    expect(calls).toEqual([]);

    await userEvent.click(screen.getByRole('button', { name: 'Become a Warrior' }));
    await userEvent.click(
      within(screen.getByRole('dialog', { name: 'Become a Warrior?' })).getByRole('button', {
        name: 'Become a Warrior',
      }),
    );
    expect(calls).toEqual([{ t: 'promote', d: { npcId: 'warrior_trainer', class: 'warrior' } }]);
    expect(onClose).toHaveBeenCalledOnce();
  });

  it('a refused promotion stays open and says why', async () => {
    trainee(30);
    quests.getState().applyNpcDialog(warriorOffer(30));
    const { socket } = fakeSocket([refusal(ERR.PROMOTION_CONDITIONS_UNMET)]);
    const onClose = vi.fn();
    render(<NpcWindow socket={socket} onClose={onClose} />);
    await userEvent.click(screen.getByRole('button', { name: 'Become a Warrior' }));
    await userEvent.click(
      within(screen.getByRole('dialog', { name: 'Become a Warrior?' })).getByRole('button', {
        name: 'Become a Warrior',
      }),
    );
    expect(screen.getByRole('alert')).toHaveTextContent('You don’t meet every condition yet.');
    expect(onClose).not.toHaveBeenCalled();
  });

  it.each([
    { c: { kind: PromotionConditionKind.LEVEL, need: 10, have: 9 }, text: "Level 10 · you're 9" },
    { c: { kind: PromotionConditionKind.LEVEL, need: 10, have: 12 }, text: 'Level 10' },
    {
      c: { kind: PromotionConditionKind.STAT, id: 'agi', need: 30, have: 31 },
      text: 'AGI 30 without gear',
    },
    {
      c: { kind: PromotionConditionKind.SKILL, id: 'spark', need: 10, have: 4 },
      text: '10 points in Spark · you have 4',
    },
    {
      c: { kind: PromotionConditionKind.QUEST, id: 'warriors_trial', need: 1, have: 0 },
      text: "The Warrior's Trial finished · not yet",
    },
    {
      c: { kind: 9 as PromotionConditionKind, need: 1, have: 0 },
      text: "A condition this client doesn't know",
    },
  ])('a condition reads "$text"', ({ c, text }) => {
    expect(conditionText(create(PromotionConditionSchema, c))).toBe(text);
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
