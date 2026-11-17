import { render, screen, within } from '@testing-library/react';
import { describe, expect, it } from 'vitest';

import { statusColor } from '@/progression/statuses';
import type { ShownStatus } from '@/state/statuses';

import { StatusIcons } from './StatusIcons';

/** A status shown now with ms left of its whole time. */
const on = (id: string, left: number, whole = left): ShownStatus => {
  const now = performance.now();
  return { id, from: now - (whole - left), until: now + left };
};

const names = () =>
  within(screen.getByRole('list', { name: 'Statuses' }))
    .getAllByRole('listitem')
    .map((li) => li.getAttribute('aria-label'));

describe('StatusIcons', () => {
  it('shows nothing with no statuses', () => {
    const { container } = render(<StatusIcons statuses={[]} />);
    expect(container).toBeEmptyDOMElement();
  });

  it('puts hard control first, then by group, the soonest to end first within one', () => {
    render(
      <StatusIcons
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
      <StatusIcons
        statuses={['stun', 'slow', 'silence', 'blind', 'poison', 'burn', 'bleed', 'weaken'].map(
          (id, i) => on(id, 1000 * (i + 1)),
        )}
      />,
    );
    expect(names()).toEqual(['Stun', 'Slow', 'Silence', 'Blind', 'Poison', 'Burn', '2 more']);
    expect(screen.getByText('+2')).toBeInTheDocument();
  });

  it('draws each tile in its status’s colour with its glyph, its time left counted and swept', () => {
    render(<StatusIcons statuses={[on('burn', 2500, 5000)]} />);
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
    render(<StatusIcons statuses={[on('frenzy', 1000)]} />);
    const tile = screen.getByRole('listitem').querySelector<HTMLElement>('.skill-icon')!;
    expect(tile.style.getPropertyValue('--icon')).toBe('');
    expect(tile.querySelector('.skill-icon-glyph')).toBeNull();
  });
});
