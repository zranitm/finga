import { act, render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { ChatBox } from '@/features/chat/ChatBox';
import { InventoryWindow } from '@/features/inventory/InventoryWindow';
import { bindKeyboard } from '@/game/systems/keyboard';
import { isTyping } from '@/input/typing';
import { ui } from '@/state/ui';
import { resetStores } from '@/testing/reset-stores';

// The world's keys and windows together, as the world scene binds the keys and the overlay mounts the windows.
function Overlay() {
  const open = ui.use((s) => s.windows.inventory);
  return (
    <>
      <ChatBox socket={{ send: vi.fn(() => true) } as never} />
      {open && (
        <InventoryWindow
          socket={{ request: vi.fn() } as never}
          onClose={() => ui.getState().closeWindow('inventory')}
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
        if (action === 'inventory') {
          ui.getState().toggleWindow('inventory');
        } else if (action === 'chat') {
          ui.getState().openChat();
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
    expect(target.press).toHaveBeenCalledWith('jump');
    expect(etc).toHaveAttribute('aria-selected', 'false');
    await userEvent.keyboard('{Enter}');
    expect(etc).toHaveAttribute('aria-selected', 'false');
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
