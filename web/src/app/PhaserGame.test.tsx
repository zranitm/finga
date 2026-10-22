import { render } from '@testing-library/react';
import { StrictMode } from 'react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { GameHandle } from '@/game/main';
import { GameSocket } from '@/net/socket';
import { FakeGame } from '@/testing/fake-phaser';
import { fakeWebSocketClass } from '@/testing/fake-socket';

import { PhaserGame } from './PhaserGame';

vi.mock('phaser', async () => (await import('@/testing/fake-phaser')).fakePhaser());

describe('PhaserGame', () => {
  let parent: HTMLDivElement;
  beforeEach(() => {
    FakeGame.reset();
    parent = document.createElement('div');
    parent.id = 'game';
    document.body.appendChild(parent);
  });
  afterEach(() => {
    parent.remove();
  });

  const handle = () =>
    new GameHandle({
      socket: new GameSocket({ url: 'ws://game.test/ws', WebSocket: fakeWebSocketClass }),
    });

  it('creates one game in #game after a StrictMode double mount, and destroys it on unmount', () => {
    const { unmount } = render(
      <StrictMode>
        <PhaserGame handle={handle()} />
      </StrictMode>,
    );
    expect(FakeGame.live()).toHaveLength(1);
    expect(FakeGame.live()[0]?.config.parent).toBe(parent);
    unmount();
    expect(FakeGame.live()).toHaveLength(0);
  });

  it('creates nothing when the parent element is missing', () => {
    render(<PhaserGame handle={handle()} parentId="nowhere" />);
    expect(FakeGame.instances).toHaveLength(0);
  });
});
