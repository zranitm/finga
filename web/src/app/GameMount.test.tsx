import { render } from '@testing-library/react';
import { StrictMode } from 'react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import type { GameView } from '@/game/view';

import { GameMount } from './GameMount';

/** A view that counts what is attached and still mounted. */
function countingView() {
  const parents: HTMLElement[] = [];
  let live = 0;
  const view: GameView = {
    attach: (parent) => {
      parents.push(parent);
      live++;
      return () => {
        live--;
      };
    },
    enter: vi.fn(),
    leave: vi.fn(),
    portrait: vi.fn(),
  };
  return { view, parents, live: () => live };
}

describe('GameMount', () => {
  let parent: HTMLDivElement;
  beforeEach(() => {
    parent = document.createElement('div');
    parent.id = 'game';
    document.body.appendChild(parent);
  });
  afterEach(() => {
    parent.remove();
  });

  it('leaves the view attached once in #game after a StrictMode double mount, and detaches it on unmount', () => {
    const v = countingView();
    const { unmount } = render(
      <StrictMode>
        <GameMount view={v.view} />
      </StrictMode>,
    );
    expect(v.live()).toBe(1);
    expect(new Set(v.parents)).toEqual(new Set([parent]));
    unmount();
    expect(v.live()).toBe(0);
  });

  it('attaches nothing when the parent element is missing', () => {
    parent.remove();
    const v = countingView();
    render(<GameMount view={v.view} />);
    expect(v.parents).toEqual([]);
  });
});
