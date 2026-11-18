import { act, fireEvent, render, screen, within } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { Modal } from '@/components/Modal';
import { RestError } from '@/net/rest';
import type { Settings } from '@/net/rest';
import { keyboardLayout } from '@/state/keyboard-layout';
import { settings } from '@/state/settings';
import { resetStores } from '@/testing/reset-stores';

import { KEYS_SAVE_DELAY_MS, KeyBindings } from './KeyBindings';

const chip = (name: RegExp | string) => screen.getByRole('button', { name });
const press = (code: string, init: KeyboardEventInit = {}) =>
  fireEvent.keyDown(document.activeElement ?? document.body, { code, ...init });

function setup(put: (s: Settings) => Promise<Settings> = (s) => Promise.resolve(s)) {
  const putSettings = vi.fn(put);
  const onBack = vi.fn();
  const view = render(<KeyBindings rest={{ putSettings }} onBack={onBack} />);
  return { putSettings, onBack, ...view };
}

/** Clicks an action's chip, which then listens. */
function listen(action: string) {
  const c = chip(new RegExp(`^${action}: `));
  c.focus();
  fireEvent.click(c);
  expect(chip(`${action}: press a key`)).toHaveAttribute('aria-pressed', 'true');
}

describe('KeyBindings', () => {
  beforeEach(() => {
    localStorage.clear();
    resetStores();
  });
  afterEach(() => {
    vi.useRealTimers();
    vi.restoreAllMocks();
  });

  it('lists every action in its group with its key, the fixed ones greyed', () => {
    setup();
    const group = (name: string) =>
      within(screen.getByRole('region', { name }))
        .getAllByRole('button')
        .map((b) => b.getAttribute('aria-label'));
    expect(group('Movement')).toEqual([
      'Walk up: ↑',
      'Walk down: ↓',
      'Walk left: ←',
      'Walk right: →',
      'Jump: Space',
    ]);
    expect(group('Combat')).toEqual([
      'Attack: X',
      'Lock the next target: Tab',
      'HP potion: 1',
      'Skill 1: Q',
      'Skill 2: W',
      'Skill 3: E',
      'Skill 4: R',
      'Skill 5: A',
      'Skill 6: S',
      'Skill 7: D',
      'Skill 8: F',
    ]);
    expect(group('World')).toEqual(['Talk, or enter a portal: Z']);
    expect(group('Windows')).toEqual([
      'Inventory: I',
      'Character: C',
      'Skills: K',
      'Party: P',
      'Quest log: J',
      'Map: M',
      'Arena: V',
    ]);
    expect(group('Fixed')).toEqual(['Chat: Enter or Num Enter', 'Close the top window: Esc']);
    expect(chip('Chat: Enter or Num Enter')).toBeDisabled();
    expect(chip('Close the top window: Esc')).toBeDisabled();
  });

  it('binds the next key pressed, at once, and the game never sees that key', () => {
    const game = vi.fn();
    window.addEventListener('keydown', game);
    window.addEventListener('keyup', game);
    setup();
    listen('Attack');
    press('KeyB');
    fireEvent.keyUp(document.activeElement!, { code: 'KeyB' });
    expect(settings.getState().keys).toEqual({ attack: 'KeyB' });
    expect(chip('Attack: B')).toHaveAttribute('aria-pressed', 'false');
    expect(screen.getByRole('status')).toHaveTextContent('B is now Attack.');
    expect(game).not.toHaveBeenCalled();
    window.removeEventListener('keydown', game);
    window.removeEventListener('keyup', game);
  });

  it('a key another action holds swaps them, and the line names both', () => {
    setup();
    listen('Attack');
    press('KeyQ');
    expect(settings.getState().keys).toEqual({ attack: 'KeyQ', skill1: 'KeyX' });
    expect(screen.getByRole('status')).toHaveTextContent('Q is now Attack; Skill 1 moved to X.');
    expect([chip('Attack: Q'), chip('Skill 1: X')]).toHaveLength(2);
  });

  it('a modifier on its own is a key', () => {
    setup();
    listen('Jump');
    press('ShiftLeft');
    expect(chip('Jump: Left Shift')).toBeInTheDocument();
  });

  it('with a Ctrl key bound, says the browser may take Ctrl with other keys', () => {
    const note = () => screen.queryByText('Ctrl with other keys can trigger browser shortcuts.');
    setup();
    expect(note()).toBeNull();
    listen('Jump');
    press('AltLeft');
    expect(note()).toBeNull(); // Ctrl's alone: Chrome keeps Ctrl+W, T and N
    listen('Attack');
    press('ControlRight');
    expect(note()).toBeInTheDocument();
  });

  it.each([
    { code: 'Enter', why: 'Enter and Esc stay on the chat and closing windows.' },
    { code: 'NumpadEnter', why: 'Enter and Esc stay on the chat and closing windows.' },
    { code: 'MetaLeft', why: 'The system keeps that key.' },
    { code: 'MetaRight', why: 'The system keeps that key.' },
    { code: 'ContextMenu', why: 'The system keeps that key.' },
    { code: 'F5', why: 'The browser keeps F5, F11 and F12.' },
    { code: 'F11', why: 'The browser keeps F5, F11 and F12.' },
    { code: 'F12', why: 'The browser keeps F5, F11 and F12.' },
  ])('$code is refused with a shake and why, and the chip keeps listening', ({ code, why }) => {
    setup();
    listen('Attack');
    press(code);
    expect(settings.getState().keys).toEqual({});
    expect(screen.getByRole('status')).toHaveTextContent(why);
    expect(chip('Attack: press a key')).toHaveClass('key-chip-refused');
    press('KeyB');
    expect(chip('Attack: B')).not.toHaveClass('key-chip-refused');
  });

  it('Esc stops listening and changes nothing, and doesn’t close the window it sits in', () => {
    const onClose = vi.fn();
    render(
      <Modal title="Settings" onClose={onClose}>
        <KeyBindings rest={{ putSettings: vi.fn() }} onBack={vi.fn()} />
      </Modal>,
    );
    listen('Attack');
    press('Escape', { key: 'Escape' });
    expect(chip('Attack: X')).toHaveAttribute('aria-pressed', 'false');
    expect(settings.getState().keys).toEqual({});
    expect(onClose).not.toHaveBeenCalled();
  });

  it('a click anywhere else stops listening', () => {
    setup();
    listen('Attack');
    fireEvent.pointerDown(document.body);
    expect(chip('Attack: X')).toHaveAttribute('aria-pressed', 'false');
    press('KeyB');
    expect(settings.getState().keys).toEqual({});
  });

  it('names keys as the player’s keyboard prints them, where the browser says', () => {
    keyboardLayout.getState().setPrinted(new Map([['KeyQ', 'a']]));
    setup();
    expect(chip('Skill 1: A')).toBeInTheDocument();
    listen('Attack');
    press('KeyQ');
    expect(screen.getByRole('status')).toHaveTextContent('A is now Attack; Skill 1 moved to X.');
  });

  it('Reset to default asks first, then puts every key back', () => {
    setup();
    expect(chip('Reset to default')).toBeDisabled();
    listen('Attack');
    press('KeyB');
    fireEvent.click(chip('Reset to default'));
    fireEvent.click(chip('Cancel'));
    expect(settings.getState().keys).toEqual({ attack: 'KeyB' });
    fireEvent.click(chip('Reset to default'));
    expect(screen.getByText('Put every key back on its default?')).toBeInTheDocument();
    fireEvent.click(chip('Reset'));
    expect(settings.getState().keys).toEqual({});
    expect(chip('Attack: X')).toBeInTheDocument();
  });

  it('the key an action already has changes nothing, and saves nothing', async () => {
    vi.useFakeTimers();
    const { putSettings } = setup();
    listen('Attack');
    press('KeyX');
    expect(chip('Attack: X')).toHaveAttribute('aria-pressed', 'false');
    expect(screen.queryByRole('status')).toBeNull();
    await act(() => vi.advanceTimersByTimeAsync(KEYS_SAVE_DELAY_MS));
    expect(putSettings).not.toHaveBeenCalled();
  });

  it('saves once, a moment after the last of a run of changes', async () => {
    vi.useFakeTimers();
    const { putSettings } = setup();
    listen('Attack');
    press('KeyB');
    listen('Jump');
    press('KeyG');
    await act(() => vi.advanceTimersByTimeAsync(KEYS_SAVE_DELAY_MS - 1));
    expect(putSettings).not.toHaveBeenCalled();
    await act(() => vi.advanceTimersByTimeAsync(1));
    expect(putSettings.mock.calls).toEqual([
      [
        {
          auto_assign_points: false,
          chat_pinned: false,
          hide_minimap: false,
          keys: { attack: 'KeyB', jump: 'KeyG' },
        },
      ],
    ]);
  });

  it('leaving the window saves a change still waiting at once, and Back leaves', () => {
    const { putSettings, onBack, unmount } = setup();
    listen('Attack');
    press('KeyB');
    fireEvent.click(chip('Back'));
    expect(onBack).toHaveBeenCalledTimes(1);
    unmount();
    expect(putSettings).toHaveBeenCalledTimes(1);
  });

  it('a failed save says so and keeps the change on screen', async () => {
    vi.useFakeTimers();
    setup(() => Promise.reject(new RestError(500, 'network', 'Could not reach the server.')));
    listen('Attack');
    press('KeyB');
    await act(() => vi.advanceTimersByTimeAsync(KEYS_SAVE_DELAY_MS));
    expect(screen.getByRole('alert')).toHaveTextContent('Could not reach the server.');
    expect(chip('Attack: B')).toBeInTheDocument();
  });
});
