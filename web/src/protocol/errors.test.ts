import { readFileSync } from 'node:fs';
import path from 'node:path';

import { describe, expect, it } from 'vitest';

import { ERR, errorText, isErrorCode } from './errors';

// npm runs the scripts from web/, so the server's catalog is in ../server.
const file = path.resolve(process.cwd(), '..', 'server', 'testdata', 'errors', 'codes.json');
const catalog = JSON.parse(readFileSync(file, 'utf8')) as { code: number; name: string }[];

describe('error codes', () => {
  it('every code the server can send has the client’s own words', () => {
    for (const { code, name } of catalog) {
      expect(isErrorCode(code), name).toBe(true);
      expect(errorText(code), name).toMatch(/^[A-Z].*\.$/);
      expect(errorText(code), name).not.toContain(String(code));
    }
  });

  it('names each code as the catalog does', () => {
    expect(Object.entries(ERR)).toEqual(catalog.map(({ code, name }) => [name, code]));
  });

  it('a code this client doesn’t know shows its number, so it reaches support', () => {
    expect(isErrorCode(220999)).toBe(false);
    expect(errorText(220999)).toBe('Something went wrong (error 220999).');
  });
});
