import { act, render, screen } from '@testing-library/react';
import { beforeEach, describe, expect, it } from 'vitest';

import { limits } from '@/gamedata/limits';
import { teleport } from '@/state/teleport';
import { resetStores } from '@/testing/reset-stores';

import { TeleportBar } from './TeleportBar';

describe('TeleportBar', () => {
  beforeEach(() => {
    resetStores();
  });

  it('shows while a channel runs, naming the map and filling over the channel, and goes as it ends', () => {
    render(<TeleportBar />);
    expect(screen.queryByRole('status')).not.toBeInTheDocument();
    act(() => {
      teleport.getState().start('forest');
    });
    const bar = screen.getByRole('status');
    expect(bar).toHaveTextContent('Teleporting to Forest');
    const fill = bar.querySelector<HTMLElement>('.teleport-bar-fill')!;
    expect([limits.teleport_channel_ms, fill.style.animationDuration]).toEqual([3000, '3000ms']);
    act(() => {
      teleport.getState().end();
    });
    expect(screen.queryByRole('status')).not.toBeInTheDocument();
  });

  it('draws a new channel’s fill afresh, from empty', () => {
    render(<TeleportBar />);
    act(() => {
      teleport.getState().start('forest');
    });
    const first = document.querySelector('.teleport-bar-fill');
    act(() => {
      teleport.getState().end();
      teleport.getState().start('cave');
    });
    expect(screen.getByRole('status')).toHaveTextContent('Teleporting to Cave');
    expect(document.querySelector('.teleport-bar-fill')).not.toBe(first);
  });
});
