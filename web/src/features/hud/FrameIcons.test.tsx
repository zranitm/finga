import { create } from '@bufbuild/protobuf';
import { act, fireEvent, render, screen, within } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { chainsOf } from '@/progression/chains';
import { colorOf, skillById } from '@/progression/skills';
import { statusColor } from '@/progression/statuses';
import { PartyStateSchema } from '@/protocol/messages';
import type { FrameBuff } from '@/state/character';
import { party } from '@/state/party';
import { settings } from '@/state/settings';
import type { ShownStatus } from '@/state/statuses';
import { resetStores } from '@/testing/reset-stores';

import { FrameIcons } from './FrameIcons';
import type { HiddenCooldown } from './hiddenCooldowns';

/** A status shown now with ms left of its whole time. */
const on = (id: string, left: number, whole = left): ShownStatus => {
  const now = performance.now();
  return { id, from: now - (whole - left), until: now + left };
};

const names = () =>
  within(screen.getByRole('list', { name: 'Statuses' }))
    .getAllByRole('listitem')
    .map((li) => li.getAttribute('aria-label'));

describe('FrameIcons', () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  it('shows nothing with no statuses', () => {
    const { container } = render(<FrameIcons statuses={[]} />);
    expect(container).toBeEmptyDOMElement();
  });

  it('puts hard control first, then by group, the soonest to end first within one', () => {
    render(
      <FrameIcons
        statuses={[
          on('poison', 4000),
          on('slow', 2000),
          on('stun', 1500),
          on('taunt', 900),
          on('burn', 3000),
        ]}
      />,
    );
    expect(names()).toEqual(['Stun', 'Slow', 'Burn', 'Poison', 'Taunt']);
  });

  it('shows six, and the rest as "+N"', () => {
    render(
      <FrameIcons
        statuses={['stun', 'slow', 'silence', 'blind', 'poison', 'burn', 'bleed', 'weaken'].map(
          (id, i) => on(id, 1000 * (i + 1)),
        )}
      />,
    );
    expect(names()).toEqual(['Stun', 'Slow', 'Silence', 'Blind', 'Poison', 'Burn', '2 more']);
    expect(screen.getByText('+2')).toBeInTheDocument();
  });

  it('draws each tile in its status’s colour with its glyph, its time left counted and swept', () => {
    render(<FrameIcons statuses={[on('burn', 2500, 5000)]} />);
    const tile = screen
      .getByRole('listitem', { name: 'Burn' })
      .querySelector<HTMLElement>('.skill-icon')!;
    const hex = `#${statusColor('burn').toString(16).padStart(6, '0')}`;
    expect(tile.style.getPropertyValue('--skill')).toBe(hex);
    expect(tile.style.getPropertyValue('--icon')).toContain(
      'icons/statuses/lorc/burning-embers.svg',
    );
    expect(tile.querySelector('.skill-icon-glyph')).not.toBeNull();
    expect(tile.querySelector('.skill-icon-count')).toHaveTextContent('3');
    const left = parseFloat(
      tile.querySelector<HTMLElement>('.skill-icon-sweep')!.style.getPropertyValue('--left'),
    );
    expect(left).toBeCloseTo(180, 0); // half its time left
  });

  it('draws a status this client doesn’t know as a plain tile', () => {
    render(<FrameIcons statuses={[on('frenzy', 1000)]} />);
    const tile = screen.getByRole('listitem').querySelector<HTMLElement>('.skill-icon')!;
    expect(tile.style.getPropertyValue('--icon')).toBe('');
    expect(tile.querySelector('.skill-icon-glyph')).toBeNull();
  });

  describe('on the player’s own frame (frame-icons.md section 2)', () => {
    beforeEach(() => {
      resetStores();
    });
    /** A buff shown now with ms left of its whole time, from giver ('' for the player's own). */
    const buff = (skill: string, left: number, giver = '', whole = left): FrameBuff => {
      const now = performance.now();
      return { skill, points: 1, giver, from: now - (whole - left), until: now + left };
    };
    /** A follow-up of the Warrior's Strike cooling with ms left. */
    const cooling = (skill: string, left: number): HiddenCooldown => {
      const now = performance.now();
      const when = chainsOf('warrior')
        .find((c) => c.root === 'strike')!
        .follow.find((f) => f.skill === skill)!.when;
      return {
        skill,
        on: [{ root: 'strike', when }],
        cooldown: { from: now - 1000, until: now + left },
      };
    };
    const own = () => screen.getByRole('list', { name: 'Buffs, statuses and cooldowns' });
    const labels = () =>
      within(own())
        .getAllByRole('listitem')
        .map((li) => li.getAttribute('aria-label'));

    it('shows buffs, then statuses, then hidden cooldowns, each kind soonest to end first', () => {
      render(
        <FrameIcons
          statuses={[on('burn', 3000), on('stun', 4000)]}
          own={{
            walk: false,
            buffs: [buff('war_cry', 9000), buff('iron_body', 2000)],
            cooldowns: [cooling('ground_slam', 7000)],
          }}
        />,
      );
      expect(labels()).toEqual(['Iron Body', 'War Cry', 'Stun', 'Burn', 'Ground Slam, cooling']);
    });

    it('shows walk mode first while it’s on, alone too, its hover naming the key that switches it as bound', () => {
      const { rerender } = render(
        <FrameIcons
          statuses={[on('burn', 3000)]}
          own={{ walk: true, buffs: [buff('war_cry', 9000)], cooldowns: [] }}
        />,
      );
      expect(labels()).toEqual(['Walk mode', 'War Cry', 'Burn']);
      const walk = within(own()).getByRole('listitem', { name: 'Walk mode' });
      expect(walk.title).toBe('Walk mode: no sprint. Left Shift to switch.');
      expect(walk.querySelector<HTMLElement>('.skill-icon')!.style.getPropertyValue('--icon')).toBe(
        'url("/icons/hud/delapouite/walk.svg")',
      );
      act(() => settings.getState().setKeys({ walk: 'KeyG' }));
      expect(walk.title).toBe('Walk mode: no sprint. G to switch.');
      rerender(<FrameIcons statuses={[]} own={{ walk: true, buffs: [], cooldowns: [] }} />);
      expect(labels()).toEqual(['Walk mode']);
      rerender(<FrameIcons statuses={[]} own={{ walk: false, buffs: [], cooldowns: [] }} />);
      expect(screen.queryByRole('list')).toBeNull();
    });

    it('draws a buff as its skill’s icon at its stage in its colour, edged ally green, its time left swept and counted', () => {
      render(
        <FrameIcons
          statuses={[]}
          own={{ walk: false, buffs: [buff('iron_body', 15000, '', 30000)], cooldowns: [] }}
        />,
      );
      const tile = within(own())
        .getByRole('listitem', { name: 'Iron Body' })
        .querySelector<HTMLElement>('.skill-icon')!;
      const s = skillById('iron_body')!;
      expect(tile).toHaveClass('frame-icon-buff');
      expect(tile.style.getPropertyValue('--skill')).toBe(
        `#${colorOf(s, s.stages[0]!).toString(16).padStart(6, '0')}`,
      );
      expect(tile.style.getPropertyValue('--icon')).toContain(
        `icons/skills/${s.stages[0]!.icon}.svg`,
      );
      expect(tile.querySelector('.skill-icon-count')).toHaveTextContent('15');
      expect(
        parseFloat(
          tile.querySelector<HTMLElement>('.skill-icon-sweep')!.style.getPropertyValue('--left'),
        ),
      ).toBeCloseTo(180, 0);
    });

    it('draws a hidden cooldown as its skill’s icon dimmed and edged grey, its seconds to ready counted', () => {
      render(
        <FrameIcons
          statuses={[]}
          own={{ walk: false, buffs: [], cooldowns: [cooling('ground_slam', 6500)] }}
        />,
      );
      const tile = within(own())
        .getByRole('listitem', { name: 'Ground Slam, cooling' })
        .querySelector<HTMLElement>('.skill-icon')!;
      expect(tile).toHaveClass('frame-icon-cooling');
      expect(tile.style.getPropertyValue('--icon')).toContain(
        `icons/skills/${skillById('ground_slam')!.stages[0]!.icon}.svg`,
      );
      expect(tile.querySelector('.skill-icon-count')).toHaveTextContent('7');
    });

    it('shows twelve of each kind, then "+N" for that kind, where other frames show six', () => {
      const ids = Array.from({ length: 13 }, (_, k) => `buff_${k}`);
      const many = ['stun', 'slow', 'silence', 'blind', 'poison', 'burn', 'bleed', 'weaken'];
      render(
        <FrameIcons
          statuses={many.map((id, k) => on(id, 1000 * (k + 1)))}
          own={{
            walk: false,
            buffs: ids.map((id, k) => buff(id, 1000 * (k + 1))),
            cooldowns: ['ground_slam', 'charge'].map((id, k) => cooling(id, 1000 * (k + 1))),
          }}
        />,
      );
      expect(labels().slice(12)).toEqual([
        '1 more buffs',
        'Stun',
        'Slow',
        'Silence',
        'Blind',
        'Poison',
        'Burn',
        'Bleed',
        'Weaken',
        'Ground Slam, cooling',
        'Charge, cooling',
      ]);
      expect(within(own()).getByText('+1')).toBeInTheDocument();
    });

    it('tells a buff’s seconds left and who gave it, a member by name and one who left the party as an ally', () => {
      party.getState().applyPartyState(
        create(PartyStateSchema, {
          party: { members: [{ characterId: 'c-robin', name: 'Robin' }] },
        }),
      );
      render(
        <FrameIcons
          statuses={[]}
          own={{
            walk: false,
            buffs: [
              buff('iron_body', 11200),
              buff('war_cry', 8000, 'c-robin'),
              buff('focus', 4000, 'c-gone'),
            ],
            cooldowns: [],
          }}
        />,
      );
      const tip = (name: string) => {
        const li = within(own()).getByRole('listitem', { name });
        fireEvent.mouseEnter(li);
        return li.title;
      };
      expect(tip('Iron Body')).toBe('Iron Body: 12 s left');
      expect(tip('War Cry')).toBe('War Cry: 8 s left, from Robin');
      expect(tip('Focus')).toBe('Focus: 4 s left, from an ally');
    });

    it('tells a hidden cooldown’s seconds to ready, and on which key and while what it comes up', () => {
      render(
        <FrameIcons
          statuses={[]}
          own={{ walk: false, buffs: [], cooldowns: [cooling('ground_slam', 6500)] }}
        />,
      );
      expect(within(own()).getByRole('listitem', { name: 'Ground Slam, cooling' }).title).toBe(
        'Ground Slam: ready in 7 s. On Strike’s key while the target is stunned or frozen.',
      );
    });
  });
});
