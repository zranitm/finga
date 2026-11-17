import { create } from '@bufbuild/protobuf';
import { act, render, screen, within } from '@testing-library/react';
import { beforeEach, describe, expect, it } from 'vitest';

import { PartyStateSchema, PartyViewSchema } from '@/protocol/messages';
import { party } from '@/state/party';
import { resetStores } from '@/testing/reset-stores';

import { PartyPanel } from './PartyPanel';
import { UnitFrame } from './UnitFrame';

describe('UnitFrame', () => {
  it('draws an ally with a green stripe and a dot, an enemy with a red stripe and a cross, so the shapes tell them apart', () => {
    render(
      <>
        <UnitFrame side="ally" name="Ayla" level={7} hp={40} maxHp={50} label="Status" />
        <UnitFrame
          side="enemy"
          name="Green Slime"
          level={1}
          hp={20}
          maxHp={34}
          label="Target: Green Slime, enemy"
        />
      </>,
    );
    const ally = screen.getByRole('group', { name: 'Status' });
    const enemy = screen.getByRole('group', { name: 'Target: Green Slime, enemy' });
    expect([ally.className, enemy.className]).toEqual([
      'unit-frame side-ally',
      'unit-frame side-enemy',
    ]);
    // a dot and a cross: the shapes tell the sides apart without colour
    expect([
      [ally.querySelector('.side-mark circle') !== null, ally.querySelector('.side-mark path')],
      [enemy.querySelector('.side-mark circle'), enemy.querySelector('.side-mark path') !== null],
    ]).toEqual([
      [true, null],
      [null, true],
    ]);
    expect(ally).toHaveTextContent(/Ayla.*Lv\. 7/);
    expect(within(ally).getByRole('meter', { name: 'HP' })).toHaveAttribute('aria-valuenow', '40');
  });

  it('shows a badge in the header, and its children under the HP bar', () => {
    render(
      <UnitFrame
        side="enemy"
        name="Mossback Snail"
        level={16}
        hp={725}
        maxHp={725}
        badge="Elite"
        label="Target"
        hpName="Mossback Snail HP"
      >
        <p>under</p>
      </UnitFrame>,
    );
    const frame = screen.getByLabelText('Target');
    expect(within(frame).getByText('Elite').closest('.unit-frame-head')).not.toBeNull();
    const hp = within(frame).getByRole('meter', { name: 'Mossback Snail HP' });
    expect(hp.nextElementSibling).toBe(within(frame).getByText('under'));
  });
});

describe('PartyPanel', () => {
  beforeEach(() => {
    resetStores();
  });

  it('draws each member as a compact ally frame with their level, the leader starred, an offline one dimmed', () => {
    render(<PartyPanel />);
    act(() =>
      party.getState().applyPartyState(
        create(PartyStateSchema, {
          party: create(PartyViewSchema, {
            leader: 'c-robin',
            members: [
              { characterId: 'c-robin', name: 'Robin', level: 12, hp: 40, maxHp: 50, online: true },
              { characterId: 'c-jade', name: 'Jade', level: 9, hp: 0, maxHp: 30, online: false },
            ],
          }),
        }),
      ),
    );
    expect(screen.getByRole('group', { name: 'Party' })).toBeInTheDocument();
    const robin = screen.getByRole('group', { name: 'Party member Robin' });
    const jade = screen.getByRole('group', { name: 'Party member Jade' });
    expect([robin.className, jade.className]).toEqual([
      'unit-frame side-ally unit-frame-compact',
      'unit-frame side-ally unit-frame-compact unit-frame-dimmed',
    ]);
    expect(robin).toHaveTextContent(/★ Robin.*Lv\. 12/);
    expect(jade).toHaveTextContent(/Jade.*Lv\. 9/);
    expect(within(robin).getByRole('meter', { name: 'Robin HP' })).toHaveAttribute(
      'aria-valuenow',
      '40',
    );
  });
});
