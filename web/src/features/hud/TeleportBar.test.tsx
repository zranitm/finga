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

  it('shows while a channel runs, naming the waypoint and filling over the channel, and goes as it ends', () => {
    render(<TeleportBar />);
    expect(screen.queryByRole('status')).not.toBeInTheDocument();
    act(() => {
      teleport.getState().start({ waypoint: 'west_pass', name: 'Hoarfell' });
    });
    const bar = screen.getByRole('status');
    expect(bar).toHaveTextContent('Teleporting to Hoarfell');
    const fill = bar.querySelector<HTMLElement>('.teleport-bar-fill')!;
    expect([limits.teleport_channel_ms, fill.style.animationDuration]).toEqual([3000, '3000ms']);
    act(() => {
      teleport.getState().end();
    });
    expect(screen.queryByRole('status')).not.toBeInTheDocument();
  });

  it('names the channel a switch takes the player to (large-regions.md section 4)', () => {
    render(<TeleportBar />);
    act(() => {
      teleport.getState().start({ channel: 3 });
    });
    expect(screen.getByRole('status')).toHaveTextContent('Switching to Channel 3');
  });

  it('draws a new channel’s fill afresh, from empty', () => {
    render(<TeleportBar />);
    act(() => {
      teleport.getState().start({ waypoint: 'west_pass', name: 'Hoarfell' });
    });
    const first = document.querySelector('.teleport-bar-fill');
    act(() => {
      teleport.getState().end();
      teleport.getState().start({ waypoint: 'greyfang_cave', name: 'Greyfang Cave' });
    });
    expect(screen.getByRole('status')).toHaveTextContent('Teleporting to Greyfang Cave');
    expect(document.querySelector('.teleport-bar-fill')).not.toBe(first);
  });
});
