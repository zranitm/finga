import { readdirSync, readFileSync } from 'node:fs';
import path from 'node:path';

import { describe, expect, it } from 'vitest';

import { skills } from '@/gamedata/skills';

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
