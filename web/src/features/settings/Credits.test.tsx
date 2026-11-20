import { readFileSync } from 'node:fs';
import path from 'node:path';

import { render, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';

import { skills } from '@/gamedata/skills';

import { Credits } from './Credits';

describe('Credits', () => {
  it('names each game-icons.net author whose icon a skill uses, once, under CC BY 3.0', () => {
    render(<Credits />);
    const credits = screen.getByRole('region', { name: 'Credits' });
    expect(credits).toHaveTextContent(
      'Icons from game-icons.net by DarkZaitzev, Delapouite, Lorc and Zeromancer, under CC BY 3.0.',
    );
    const authors = new Set(skills.flatMap((s) => s.stages.map((st) => st.icon.split('/')[0]!)));
    for (const a of authors) {
      expect([a, credits.textContent?.match(new RegExp(`\\b${a}\\b`, 'gi'))?.length]).toEqual([
        a,
        1,
      ]);
    }
    expect(screen.getByRole('link', { name: 'CC BY 3.0' })).toHaveAttribute(
      'href',
      'https://creativecommons.org/licenses/by/3.0/',
    );
  });

  it('spells each icon author as public/icons/LICENSES.md does', () => {
    render(<Credits />);
    const table = readFileSync(path.resolve(process.cwd(), 'public/icons/LICENSES.md'), 'utf8');
    const authors = new Set(
      table
        .split('\n')
        .filter((l) => /^\| (skills|statuses|hud)\//.test(l))
        .map((l) => l.split('|')[2]!.trim()),
    );
    expect(authors.size).toBeGreaterThan(0);
    for (const a of authors) {
      expect(screen.getByRole('region', { name: 'Credits' })).toHaveTextContent(a);
    }
  });

  it('thanks each maker public/models/LICENSES.md lists, our own work aside', () => {
    render(<Credits />);
    const table = readFileSync(path.resolve(process.cwd(), 'public/models/LICENSES.md'), 'utf8');
    const makers = new Set(
      table
        .split('\n')
        .filter((l) => l.startsWith('| ') && l.includes('.glb'))
        .map((l) => l.split('|').map((c) => c.trim()))
        .filter((c) => c[4] !== 'Own work')
        .map((c) => c[3]!),
    );
    expect(makers.size).toBeGreaterThan(0);
    for (const m of makers) {
      expect(screen.getByRole('region', { name: 'Credits' })).toHaveTextContent(m);
    }
  });
});
