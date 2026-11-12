import { readdirSync, readFileSync } from 'node:fs';
import path from 'node:path';

import { describe, expect, it } from 'vitest';

import { actionPoints, attackLengthTicks, skillLengthTicks } from './actions';

// The vectors are the contract with the server's AttackLengthTicks, SkillLengthTicks and ActionPoints.
interface Vector {
  readonly name: string;
  readonly kind: 'attack' | 'skill';
  readonly length_ms: number;
  readonly attack_speed: number;
  readonly hit: number;
  readonly cancel: number;
  readonly tick_hz: number;
  readonly expected: { readonly ticks: number; readonly hit: number; readonly cancel: number };
}

const vectorsDir = path.resolve(process.cwd(), '..', 'server', 'testdata', 'actions');
const vectors = readdirSync(vectorsDir)
  .filter((name) => name.endsWith('.json'))
  .map((name) => JSON.parse(readFileSync(path.join(vectorsDir, name), 'utf8')) as Vector);

describe('the action vectors', () => {
  it('are all here', () => {
    expect(vectors.map((v) => v.name)).toEqual(
      expect.arrayContaining([
        'warrior-attack',
        'attack-speed',
        'skill-ignores-attack-speed',
        'share-on-a-whole-tick',
        'wind-up',
        'one-tick',
      ]),
    );
  });

  it.each(vectors.map((v) => [v.name, v] as const))('%s matches the server', (_, v) => {
    const tickMs = 1000 / v.tick_hz;
    const ticks =
      v.kind === 'attack'
        ? attackLengthTicks(v.length_ms, v.attack_speed, tickMs)
        : skillLengthTicks(v.length_ms, tickMs);
    expect({ ticks, ...actionPoints(ticks, v.hit, v.cancel) }).toEqual(v.expected);
  });
});
