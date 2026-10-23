import { readFileSync } from 'node:fs';
import path from 'node:path';

import { describe, expect, it } from 'vitest';

import { errorCodes } from './errors';

// npm runs the scripts from web/, so the server's list is in ../server.
const file = path.resolve(process.cwd(), '..', 'server', 'testdata', 'errors', 'codes.json');

describe('errorCodes', () => {
  it('is exactly the list the server writes, so a new server code cannot go unnoticed', () => {
    const server = (JSON.parse(readFileSync(file, 'utf8')) as { code: string }[]).map(
      (c) => c.code,
    );
    expect([...errorCodes]).toEqual(server);
  });
});
