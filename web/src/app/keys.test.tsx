import { act, render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { CharacterWindow } from '@/features/character/CharacterWindow';
import { ChatBox } from '@/features/chat/ChatBox';
import { InventoryWindow } from '@/features/inventory/InventoryWindow';
import { bindKeyboard } from '@/game/systems/keyboard';
import { isTyping } from '@/input/typing';
import { settings } from '@/state/settings';
import { ui } from '@/state/ui';
import { resetStores } from '@/testing/reset-stores';

// The world's keys and windows together, as the world scene binds the keys and the overlay mounts the windows.
function Overlay() {
  const open = ui.use((s) => s.windows.includes('inventory'));
  const characterOpen = ui.use((s) => s.windows.includes('character'));
  return (
    <>
      <ChatBox socket={{ send: vi.fn(() => true) } as never} rest={{ putSettings: vi.fn() }} />
      {open && (
        <InventoryWindow
          socket={{ request: vi.fn() } as never}
          onClose={() => ui.getState().closeWindow('inventory')}
        />
      )}
      {characterOpen && (
        <CharacterWindow
          socket={{ request: vi.fn() } as never}
          onClose={() => ui.getState().closeWindow('character')}
        />
      )}
    </>
  );
}

describe('the world’s keys with windows open', () => {
  let unbind: (() => void) | undefined;
  const target = { press: vi.fn(), release: vi.fn(), releaseAll: vi.fn() };

  beforeEach(() => {
    resetStores();
    vi.clearAllMocks();
    unbind = bindKeyboard({
      target,
      preset: () => 'classic',
      typing: isTyping,
      onAction: (action) => {
        if (action === 'inventory' || action === 'character') {
          ui.getState().toggleWindow(action);
        } else if (action === 'chat') {
          ui.getState().openChat();
        } else if (action === 'close') {
          ui.getState().closeTopWindow();
        }
      },
    });
    render(<Overlay />);
  });

  afterEach(() => {
    unbind?.();
  });

  const inventoryWindow = () => screen.queryByRole('dialog', { name: 'Inventory' });
  const walks = async () => {
    target.press.mockClear();
    await userEvent.keyboard('{ArrowRight>}');
    await userEvent.keyboard('{/ArrowRight}');
    return target.press.mock.calls.some(([a]) => a === 'right');
  };

  it('C opens the character window beside the inventory, the player still walks, and C closes it again', async () => {
    const characterWindow = () => screen.queryByRole('dialog', { name: 'Character' });
    await userEvent.keyboard('i');
    await userEvent.keyboard('c');
    expect(characterWindow()).toHaveFocus();
    expect(inventoryWindow()).not.toBeNull();
    expect(await walks()).toBe(true);
    await userEvent.keyboard('c');
    expect(characterWindow()).toBeNull();
    expect(inventoryWindow()).not.toBeNull();
  });

  it('the player walks with the inventory open, and I closes it again', async () => {
    await userEvent.keyboard('i');
    expect(inventoryWindow()).toHaveFocus();
    expect(await walks()).toBe(true);
    await userEvent.keyboard('i');
    expect(inventoryWindow()).toBeNull();
  });

  it('closing the inventory with Esc leaves the player walking', async () => {
    await userEvent.keyboard('i');
    await userEvent.keyboard('{Escape}');
    expect(inventoryWindow()).toBeNull();
    expect(await walks()).toBe(true);
  });

  it('Esc closes the focused window, then the one left open once nothing has focus', async () => {
    const characterWindow = () => screen.queryByRole('dialog', { name: 'Character' });
    await userEvent.keyboard('i');
    await userEvent.keyboard('c');
    await userEvent.keyboard('{Escape}');
    expect(characterWindow()).toBeNull();
    expect(document.activeElement).toBe(document.body);
    await userEvent.keyboard('{Escape}');
    expect(inventoryWindow()).toBeNull();
  });

  it('a pressed window comes to the top, and Esc closes it first with nothing focused', async () => {
    const characterWindow = () => screen.queryByRole('dialog', { name: 'Character' });
    await userEvent.keyboard('i');
    await userEvent.keyboard('c');
    expect(inventoryWindow()!.style.zIndex).toBe('1');
    expect(characterWindow()!.style.zIndex).toBe('2');
    await userEvent.click(screen.getByRole('tab', { name: 'Etc' }));
    expect(inventoryWindow()!.style.zIndex).toBe('2');
    expect(characterWindow()!.style.zIndex).toBe('1');
    act(() => (document.activeElement as HTMLElement).blur());
    await userEvent.keyboard('{Escape}');
    expect(inventoryWindow()).toBeNull();
    expect(characterWindow()).not.toBeNull();
  });

  it('× closes the inventory, and the player walks after', async () => {
    await userEvent.keyboard('i');
    await userEvent.click(screen.getByRole('button', { name: 'Close Inventory' }));
    expect(inventoryWindow()).toBeNull();
    expect(await walks()).toBe(true);
  });

  it('Space with an inventory tab focused jumps, and the tab stays unpressed', async () => {
    await userEvent.keyboard('i');
    const etc = screen.getByRole('tab', { name: 'Etc' });
    act(() => etc.focus());
    await userEvent.keyboard(' ');
    expect(target.press).toHaveBeenCalledWith('jump', false);
    expect(etc).toHaveAttribute('aria-selected', 'false');
    await userEvent.keyboard('{Enter}');
    expect(etc).toHaveAttribute('aria-selected', 'false');
    expect(screen.getByLabelText('Chat message')).toHaveFocus();
  });

  it('Enter in the chat sends the line and leaves the chat closed, the keys back with the game', async () => {
    await userEvent.keyboard('{Enter}');
    await userEvent.keyboard('hello{Enter}');
    expect(screen.queryByLabelText('Chat message')).toBeNull();
    expect(ui.getState().chatOpen).toBe(false);
    expect(await walks()).toBe(true);
  });

  it('Esc in the chat closes the chat only, not a window open behind it', async () => {
    await userEvent.keyboard('i');
    await userEvent.keyboard('{Enter}');
    await userEvent.keyboard('{Escape}');
    expect(screen.queryByLabelText('Chat message')).toBeNull();
    expect(inventoryWindow()).not.toBeNull();
  });

  it('a pinned chat stays open: Esc gives the keys back to the game, and Enter takes the input again', async () => {
    act(() => settings.getState().setChatPinned(true));
    await userEvent.keyboard('{Enter}');
    expect(screen.getByLabelText('Chat message')).toHaveFocus();
    await userEvent.keyboard('{Escape}');
    expect(screen.getByLabelText('Chat message')).not.toHaveFocus();
    expect(await walks()).toBe(true);
    await userEvent.keyboard('{Enter}');
    expect(screen.getByLabelText('Chat message')).toHaveFocus();
    await userEvent.keyboard('again{Enter}');
    expect(screen.getByLabelText('Chat message')).not.toHaveFocus();
    await userEvent.keyboard('{Enter}');
    expect(screen.getByLabelText('Chat message')).toHaveFocus();
  });

  it('typing in the chat keeps keys from the game, and leaving the chat gives them back', async () => {
    await userEvent.keyboard('{Enter}');
    expect(screen.getByLabelText('Chat message')).toHaveFocus();
    expect(await walks()).toBe(false);
    await userEvent.keyboard('i');
    expect(inventoryWindow()).toBeNull();
    await userEvent.keyboard('{Escape}');
    expect(await walks()).toBe(true);
  });
});
