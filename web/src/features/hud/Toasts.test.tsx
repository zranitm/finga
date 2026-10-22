import { act, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { toasts } from '@/state/toasts';

import { TOAST_MS, Toasts } from './Toasts';

describe('Toasts', () => {
  beforeEach(() => {
    vi.useFakeTimers();
    toasts.reset();
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  it('shows a pushed toast, goes on a click or after the time', () => {
    render(<Toasts />);
    act(() => {
      toasts.getState().push('Your inventory is full.');
      toasts.getState().push('The server is busy. Try again.');
    });
    expect(screen.getAllByRole('button')).toHaveLength(2);
    act(() => {
      screen.getByRole('button', { name: 'Your inventory is full.' }).click();
    });
    expect(
      screen.queryByRole('button', { name: 'Your inventory is full.' }),
    ).not.toBeInTheDocument();
    act(() => {
      vi.advanceTimersByTime(TOAST_MS);
    });
    expect(screen.queryAllByRole('button')).toHaveLength(0);
  });
});
