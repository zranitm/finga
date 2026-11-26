// A leaver's line follows the mode's rated flag, not its being a battle: here 3v3 is unrated, as any round mode could be.
import { afterAll, beforeAll, describe, expect, it, vi } from 'vitest';

import type * as OutcomeModule from './outcome';

type Loose = Readonly<Record<string, unknown>>;

vi.mock('@/gamedata/pvp', async (importOriginal) => {
  const real = await importOriginal<{ pvp: { readonly modes: readonly Loose[] } }>();
  const modes = real.pvp.modes.map((m) => (m.id === 'team' ? { ...m, rated: false } : m));
  return { pvp: { ...real.pvp, modes } };
});

// A worker shares its modules across files, and one before this may have loaded the real modes: this file loads outcome.ts afresh, under the mock, and leaves the next file to load its own.
let outcome: typeof OutcomeModule;
beforeAll(async () => {
  vi.resetModules();
  outcome = await import('./outcome');
});
afterAll(() => {
  vi.resetModules();
});

describe('quitLine', () => {
  it.each([
    ['duel', 'You left the match: it counts as a loss. Your rating updates when the match ends.'],
    ['team', 'You left the match: it counts as a loss.'],
    ['battle', 'You left the match: it counts as a loss.'],
    ['a mode this client doesn’t know', 'You left the match: it counts as a loss.'],
  ])('%s', (mode, line) => {
    expect(outcome.quitLine(mode)).toBe(line);
  });
});
