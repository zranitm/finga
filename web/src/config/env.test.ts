import { describe, expect, it } from 'vitest';

import { env, parseEnv } from './env';

describe('env', () => {
  it.each([
    { name: 'unset means same origin', raw: { DEV: true }, want: '' },
    { name: 'empty means same origin', raw: { VITE_API_ORIGIN: '', DEV: false }, want: '' },
    { name: 'blank means same origin', raw: { VITE_API_ORIGIN: '  ', DEV: false }, want: '' },
    {
      name: 'an http origin',
      raw: { VITE_API_ORIGIN: 'http://192.168.1.10:8080', DEV: false },
      want: 'http://192.168.1.10:8080',
    },
    {
      name: 'an https origin',
      raw: { VITE_API_ORIGIN: 'https://play.example', DEV: false },
      want: 'https://play.example',
    },
  ])('$name', ({ raw, want }) => {
    expect(parseEnv(raw)).toEqual({ apiOrigin: want, isDev: raw.DEV, netDelayMs: 0 });
  });

  it.each([
    { name: 'not a URL', origin: 'localhost:8080' },
    { name: 'a path', origin: 'http://localhost:8080/api' },
    { name: 'a trailing slash', origin: 'http://localhost:8080/' },
    { name: 'the wrong scheme', origin: 'ws://localhost:8080' },
  ])('$name throws', ({ origin }) => {
    expect(() => parseEnv({ VITE_API_ORIGIN: origin, DEV: true })).toThrow(/VITE_API_ORIGIN/);
  });

  it.each([
    { name: 'unset is no delay', raw: { DEV: true }, want: 0 },
    { name: 'blank is no delay', raw: { VITE_NET_DELAY_MS: ' ', DEV: true }, want: 0 },
    { name: '100 in development', raw: { VITE_NET_DELAY_MS: '100', DEV: true }, want: 100 },
    { name: 'ignored in a build', raw: { VITE_NET_DELAY_MS: '100', DEV: false }, want: 0 },
  ])('$name', ({ raw, want }) => {
    expect(parseEnv(raw).netDelayMs).toBe(want);
  });

  it.each([
    { name: 'not a number', value: 'slow' },
    { name: 'negative', value: '-5' },
    { name: 'fractional', value: '1.5' },
    { name: 'over 5 s', value: '6000' },
  ])('a delay that is $name throws', ({ value }) => {
    expect(() => parseEnv({ VITE_NET_DELAY_MS: value, DEV: true })).toThrow(/VITE_NET_DELAY_MS/);
  });

  it('the module value comes from import.meta.env', () => {
    expect(env.apiOrigin).toBe('');
    expect(typeof env.isDev).toBe('boolean');
    expect(env.netDelayMs).toBe(0);
  });
});
