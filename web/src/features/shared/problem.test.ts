import { describe, expect, it } from 'vitest';

import { RequestError } from '@/net/socket';
import { ERR } from '@/protocol/errors';
import { refusal } from '@/testing/refusal';

import { problemOf } from './problem';

describe('problemOf', () => {
  it('gives the refusal’s words', () => {
    expect(problemOf(refusal(ERR.QUEST_NOT_READY))).toBe('That quest isn’t finished yet.');
  });

  it('says only that there is no answer yet, for a timeout or a dropped connection', () => {
    expect(problemOf(new RequestError('timeout', 'handInQuest: no response within 5000 ms'))).toBe(
      'No answer yet. Try again.',
    );
    expect(problemOf(new RequestError('disconnected', 'acceptQuest: kicked'))).toBe(
      'No answer yet. Try again.',
    );
  });

  it('falls back for anything thrown that is not an error', () => {
    expect(problemOf('nope')).toBe('The server refused.');
  });
});
