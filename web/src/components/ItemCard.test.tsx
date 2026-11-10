import { create } from '@bufbuild/protobuf';
import { render, screen, within } from '@testing-library/react';
import { describe, expect, it } from 'vitest';

import { items } from '@/gamedata/items';
import { ItemViewSchema } from '@/protocol/messages';

import { ItemCard, NothingWornCard } from './ItemCard';
import type { ItemTemplate } from './ItemCard';

const templates: readonly ItemTemplate[] = items;
const template = (id: string) => templates.find((t) => t.id === id)!;
const view = (itemId: string, tpl: string, stats?: Record<string, number>, quantity = 1) =>
  create(ItemViewSchema, { itemId, template: tpl, quantity, ...(stats ? { stats } : {}) });

const lines = (label: string) =>
  within(screen.getByRole('list', { name: label }))
    .getAllByRole('listitem')
    .map((li) => li.textContent);

describe('ItemCard', () => {
  it('shows a copy of equipment: name in its star colour, stars and type, requirements, its roll beside the range, the description', () => {
    render(
      <ItemCard
        template={template('iron_sword')}
        view={view('i-1', 'iron_sword', { physicalAttack: 18 })}
      />,
    );
    const card = screen.getByRole('group', { name: 'Iron Sword' });
    expect(within(card).getByText('Iron Sword')).toHaveClass('item-card-name', 'stars-2');
    expect(within(card).getByLabelText('2 stars')).toHaveTextContent('★★');
    expect(card).toHaveTextContent('★★ Weapon');
    expect(lines('Requirements')).toEqual(['Level 8', 'Warrior', 'STR 20']);
    expect(lines('Stats')).toEqual(['Physical attack 18 (16–20)']);
    expect(card).toHaveTextContent('A plain blade that holds an edge.');
    expect(card).not.toHaveTextContent('Quantity');
  });

  it('marks in red each requirement the character misses, stat minimums included', () => {
    render(
      <ItemCard
        template={{ ...template('hunting_bow'), requires: { dex: 20, agi: 12 } }}
        view={view('i-1', 'hunting_bow', { physicalAttack: 15 })}
        unmet={{ level: true, class: true, stats: ['dex'] }}
      />,
    );
    const reqs = within(screen.getByRole('list', { name: 'Requirements' })).getAllByRole(
      'listitem',
    );
    expect(reqs.map((r) => [r.textContent, r.className])).toEqual([
      ['Level 8', 'unmet'],
      ['Bowman', 'unmet'],
      ['DEX 20', 'unmet'],
      ['AGI 12', ''],
    ]);
  });

  it('marks what wearing it instead would change: gains in green, losses in red, a stat only the worn item has as a full loss', () => {
    render(
      <ItemCard
        template={template('leather_cap')}
        view={view('i-1', 'leather_cap', { physicalDefense: 4 })}
        changes={{ physicalDefense: 1, maxHp: -30 }}
      />,
    );
    expect(lines('Stats')).toEqual(['Physical defense 4 (3–4) +1', 'Max HP −30']);
    expect(screen.getByText('+1', { exact: false })).toHaveClass('stat-gain');
    expect(screen.getByText('−30', { exact: false })).toHaveClass('stat-loss');
  });

  it('reads rates in tenths of a percent, and names a ring by its kind', () => {
    const ring: ItemTemplate = {
      id: 'opal_ring',
      name: 'Opal Ring',
      tab: 'equip',
      slot: 'ring',
      min_level: 10,
      stars: 4,
      description: 'A milky stone.',
      stats: { crit_rate: [5, 15], max_mp: [0, 20] },
    };
    render(
      <ItemCard
        template={ring}
        view={view('i-1', 'opal_ring', { critRate: 10 })}
        changes={{ critRate: 5 }}
      />,
    );
    expect(screen.getByRole('group', { name: 'Opal Ring' })).toHaveTextContent('★★★★ Ring');
    expect(lines('Stats')).toEqual(['Max MP 0 (0–20)', 'Crit rate 1% (0.5%–1.5%) +0.5%']);
    expect(lines('Requirements')).toEqual(['Level 10']);
  });

  it('lists the new stats in their units: regen a point a second, speeds and penetration in percent', () => {
    const boots: ItemTemplate = {
      id: 'swift_boots',
      name: 'Swift Boots',
      tab: 'equip',
      slot: 'shoes',
      stars: 3,
      description: '',
      stats: {
        magic_defense: [1, 3],
        penetration: [0, 50],
        hp_regen: [1, 5],
        move_speed: [10, 30],
      },
    };
    render(
      <ItemCard
        template={boots}
        view={view('i-1', 'swift_boots', {
          magicDefense: 2,
          penetration: 25,
          hpRegen: 5,
          moveSpeed: 20,
        })}
      />,
    );
    expect(lines('Stats')).toEqual([
      'Magic defense 2 (1–3)',
      'Penetration 2.5% (0%–5%)',
      'HP regen 0.5/s (0.1/s–0.5/s)',
      'Move speed 2% (1%–3%)',
    ]);
  });

  it('shows a Use item’s effect and a stack’s quantity, an Etc item its tab, and neither any requirements', () => {
    render(
      <ItemCard template={template('red_potion')} view={view('i-1', 'red_potion', undefined, 5)} />,
    );
    const potion = screen.getByRole('group', { name: 'Red Potion' });
    expect(potion).toHaveTextContent('★ Use');
    expect(potion).toHaveTextContent('Restores 50 HP');
    expect(potion).toHaveTextContent('Quantity 5');
    expect(screen.queryByRole('list', { name: 'Requirements' })).toBeNull();
    render(
      <ItemCard
        template={template('blue_potion')}
        view={view('i-2', 'blue_potion')}
        heading="Worn"
      />,
    );
    expect(screen.getByRole('group', { name: 'Worn: Blue Potion' })).toHaveTextContent(
      'Restores 30 MP',
    );
    render(<ItemCard template={template('slime_gel')} view={view('i-3', 'slime_gel')} />);
    expect(screen.getByRole('group', { name: 'Slime Gel' })).toHaveTextContent('★ Etc');
  });

  it('says when nothing is worn', () => {
    render(<NothingWornCard />);
    expect(screen.getByRole('group', { name: 'Worn: nothing' })).toHaveTextContent(
      'Nothing is worn there.',
    );
  });
});
