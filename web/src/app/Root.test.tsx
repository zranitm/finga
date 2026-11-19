import { render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { resetStores } from '@/testing/reset-stores';

import { watchKeyboardLayout } from './keyboard-layout';
import type { Root as RootOf } from './Root';

vi.mock('./keyboard-layout', () => ({ watchKeyboardLayout: vi.fn(() => () => undefined) }));

/** Root imported afresh: a worker keeps its modules across files, so an App another file imported first would still call the real watcher, not this mock. */
async function freshRoot(): Promise<typeof RootOf> {
  vi.resetModules();
  return (await import('./Root')).Root;
}

describe('Root', () => {
  afterEach(() => {
    vi.restoreAllMocks();
    resetStores();
  });

  it('renders the app', async () => {
    const Root = await freshRoot();
    render(<Root />);
    expect(screen.getByRole('form', { name: 'Log in' })).toBeInTheDocument();
  });

  // render() runs the mount effects and the boundary's fallback inside act, so both are there as it returns: no wait, which a loaded runner's clock could run out.
  it('an error thrown while the app mounts shows a reload screen, not a blank page, and reports it to the console', async () => {
    const Root = await freshRoot();
    const crash = new Error('keyboard.addEventListener is not a function');
    vi.mocked(watchKeyboardLayout).mockImplementationOnce(() => {
      throw crash;
    });
    const logged = vi.spyOn(console, 'error').mockImplementation(() => undefined);
    render(<Root />);
    expect(screen.getByRole('alert')).toHaveTextContent(
      'Something went wrongReload the page to play again.',
    );
    expect(screen.getByRole('button', { name: 'Reload' })).toBeInTheDocument();
    expect(screen.queryByRole('form', { name: 'Log in' })).toBeNull();
    expect(logged).toHaveBeenCalledWith('finga: the page crashed', crash, expect.any(String));
  });
});
