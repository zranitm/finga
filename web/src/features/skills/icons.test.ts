import { readdirSync, readFileSync } from 'node:fs';
import path from 'node:path';

import { describe, expect, it } from 'vitest';

import { skills } from '@/gamedata/skills';
import { statuses } from '@/gamedata/statuses';
import { statusIcon, statusIcons } from '@/progression/statuses';

const ICONS = path.resolve(process.cwd(), 'public/icons');

describe('skill icons', () => {
  const used = [
    ...new Set(skills.flatMap((s) => s.stages.map((st) => `skills/${st.icon}.svg`))),
  ].sort();
  const files = readdirSync(path.join(ICONS, 'skills'), { recursive: true, encoding: 'utf8' })
    .filter((f) => f.endsWith('.svg'))
    .map((f) => `skills/${f}`)
    .sort();

  it('has an SVG for every icon a skill or a stage names, and none unused', () => {
    expect(used.length).toBeGreaterThan(0);
    expect(files).toEqual(used);
  });

  it('lists every SVG in LICENSES.md, for its credit', () => {
    const licences = readFileSync(path.join(ICONS, 'LICENSES.md'), 'utf8');
    const listed = [...licences.matchAll(/^\| (skills\/\S+\.svg) /gm)].map((m) => m[1]).sort();
    expect(listed).toEqual(files);
  });
});

describe('status icons', () => {
  const used = [...new Set(Object.values(statusIcons).map((i) => `statuses/${i}.svg`))].sort();
  const files = readdirSync(path.join(ICONS, 'statuses'), { recursive: true, encoding: 'utf8' })
    .filter((f) => f.endsWith('.svg'))
    .map((f) => `statuses/${f}`)
    .sort();

  it('gives every status an icon of its own', () => {
    const icons = statuses.map((s) => statusIcon(s.id));
    expect(icons.every(Boolean)).toBe(true);
    expect(new Set(icons).size).toBe(statuses.length);
  });

  it('has an SVG for every status icon, and none unused', () => {
    expect(files).toEqual(used);
  });

  it('lists every SVG in LICENSES.md, for its credit', () => {
    const licences = readFileSync(path.join(ICONS, 'LICENSES.md'), 'utf8');
    const listed = [...licences.matchAll(/^\| (statuses\/\S+\.svg) /gm)].map((m) => m[1]).sort();
    expect(listed).toEqual(files);
  });

  it('keeps only the glyph: path data, no black square', () => {
    for (const f of files) {
      const svg = readFileSync(path.join(ICONS, f), 'utf8');
      expect(svg, f).toMatch(
        /^<svg xmlns="http:\/\/www.w3.org\/2000\/svg" viewBox="0 0 512 512">(<path d="[^"]+"\/>)+<\/svg>\n$/,
      );
      expect(svg, f).not.toContain('M0 0h512v512H0z');
    }
  });
});
