import { render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { resetStores } from '@/testing/reset-stores';

import { watchKeyboardLayout } from './keyboard-layout';
import { Root } from './Root';

vi.mock('./keyboard-layout', () => ({ watchKeyboardLayout: vi.fn(() => () => undefined) }));

describe('Root', () => {
  afterEach(() => {
    vi.restoreAllMocks();
    resetStores();
  });

  it('renders the app', async () => {
    render(<Root />);
    expect(await screen.findByRole('form', { name: 'Log in' })).toBeInTheDocument();
  });

  it('an error thrown while the app mounts shows a reload screen, not a blank page, and reports it to the console', async () => {
    const crash = new Error('keyboard.addEventListener is not a function');
    vi.mocked(watchKeyboardLayout).mockImplementationOnce(() => {
      throw crash;
    });
    const logged = vi.spyOn(console, 'error').mockImplementation(() => undefined);
    render(<Root />);
    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Something went wrongReload the page to play again.',
    );
    expect(screen.getByRole('button', { name: 'Reload' })).toBeInTheDocument();
    expect(screen.queryByRole('form', { name: 'Log in' })).toBeNull();
    expect(logged).toHaveBeenCalledWith('finga: the page crashed', crash, expect.any(String));
  });
});
