import { fireEvent, render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';

import { Bar } from './Bar';
import { Button } from './Button';
import { Modal } from './Modal';
import { Window } from './Window';

describe('Window', () => {
  it('takes focus when it opens and closes on Escape', async () => {
    const onClose = vi.fn();
    render(
      <Window title="Inventory" onClose={onClose}>
        <p>items</p>
      </Window>,
    );
    expect(screen.getByRole('dialog', { name: 'Inventory' })).toHaveFocus();
    await userEvent.keyboard('{Escape}');
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it('draws at its layer, and a press or focus inside asks to bring it to the top', async () => {
    const onRaise = vi.fn();
    render(
      <Window title="Inventory" onClose={vi.fn()} layer={3} onRaise={onRaise}>
        <button type="button">Etc</button>
      </Window>,
    );
    const w = screen.getByRole('dialog', { name: 'Inventory' });
    expect(w.style.zIndex).toBe('3');
    expect(onRaise).toHaveBeenCalledTimes(1); // the focus it takes on open
    await userEvent.click(screen.getByRole('button', { name: 'Etc' }));
    expect(onRaise.mock.calls.length).toBeGreaterThan(1);
  });

  it('closes from its title-bar button, whose press never starts a drag', async () => {
    const onClose = vi.fn();
    render(
      <Window title="Party" onClose={onClose}>
        <p>members</p>
      </Window>,
    );
    const capture = vi.fn();
    screen.getByText('Party').parentElement!.setPointerCapture = capture;
    await userEvent.click(screen.getByRole('button', { name: 'Close Party' }));
    expect(capture).not.toHaveBeenCalled(); // a captured pointer would send the click to the title bar, not the button
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it('moves when the title is dragged', () => {
    render(
      <Window title="Party" onClose={vi.fn()} initialX={100} initialY={50}>
        <p>members</p>
      </Window>,
    );
    const dialog = screen.getByRole('dialog', { name: 'Party' });
    expect(dialog).toHaveStyle({ left: '100px', top: '50px' });
    const title = screen.getByText('Party');
    const bar = title.parentElement!;
    bar.setPointerCapture = vi.fn();
    fireEvent.pointerDown(bar, { clientX: 110, clientY: 60, pointerId: 1 });
    fireEvent.pointerMove(bar, { clientX: 150, clientY: 90, pointerId: 1 });
    fireEvent.pointerUp(bar, { pointerId: 1 });
    fireEvent.pointerMove(bar, { clientX: 500, clientY: 500, pointerId: 1 }); // not dragging any more
    expect(dialog).toHaveStyle({ left: '140px', top: '80px' });
  });

  it('is never taller than the viewport below it, less the idle chat, and its body scrolls the rest', () => {
    render(
      <Window title="Character" onClose={vi.fn()} initialX={24} initialY={80}>
        <p>stats</p>
      </Window>,
    );
    // 80 from the top and 84 kept clear of the idle chat at the bottom, never under 160; the engine may reorder the sum.
    const max = screen.getByRole('dialog', { name: 'Character' }).style.maxHeight;
    expect(max).toMatch(/^max\(160px, /);
    expect(max).toMatch(/100vh/);
    expect(max).toMatch(/-\s?164px/);
  });

  it('keeps its footer out of the scrolling body, so a notice stays in view', () => {
    render(
      <Window
        title="Character"
        onClose={vi.fn()}
        footer={<p role="alert">Your Equip tab is full.</p>}
      >
        <p>stats</p>
      </Window>,
    );
    const notice = screen.getByRole('alert');
    expect(notice.closest('.window-footer')).not.toBeNull();
    expect(notice.closest('.window-body')).toBeNull();
  });
});

describe('Modal', () => {
  it('closes on Escape and on a backdrop click, but not on a click inside', async () => {
    const onClose = vi.fn();
    const { container } = render(
      <Modal title="Kicked" onClose={onClose}>
        <p>Logged in elsewhere</p>
      </Modal>,
    );
    expect(screen.getByRole('dialog', { name: 'Kicked' })).toHaveFocus();
    await userEvent.keyboard('{Escape}');
    fireEvent.click(screen.getByText('Logged in elsewhere'));
    expect(onClose).toHaveBeenCalledTimes(1);
    fireEvent.click(container.querySelector('.modal-backdrop')!);
    expect(onClose).toHaveBeenCalledTimes(2);
  });

  it('a blocking notice has no way to dismiss it', async () => {
    const { container } = render(
      <Modal title="Reconnecting">
        <p>Hold on</p>
      </Modal>,
    );
    await userEvent.keyboard('{Escape}');
    fireEvent.click(container.querySelector('.modal-backdrop')!);
    expect(screen.getByRole('dialog', { name: 'Reconnecting' })).toBeInTheDocument();
  });
});

describe('Bar and Button', () => {
  it.each([
    { name: 'half full', value: 102, max: 204, width: '50.0%', now: 102 },
    { name: 'empty', value: 0, max: 204, width: '0.0%', now: 0 },
    { name: 'over the maximum is clamped', value: 300, max: 204, width: '100.0%', now: 204 },
    { name: 'a zero maximum is empty', value: 5, max: 0, width: '0.0%', now: 0 },
  ])('a bar $name', ({ value, max, width, now }) => {
    const { container } = render(<Bar label="HP" value={value} max={max} kind="hp" />);
    const meter = screen.getByRole('meter', { name: 'HP' });
    expect(meter).toHaveAttribute('aria-valuenow', String(now));
    expect(container.querySelector('.bar-fill')).toHaveStyle({ width });
    expect(meter).toHaveTextContent(`HP ${value} / ${max}`);
  });

  it('a bar can show its own text, as the EXP bar does at the cap', () => {
    render(<Bar label="EXP" value={0} max={0} kind="exp" text="MAX" />);
    expect(screen.getByRole('meter', { name: 'EXP' })).toHaveTextContent('EXP MAX');
  });

  it('a button is type button by default, carries its variant, and fires', () => {
    const onClick = vi.fn();
    render(
      <Button variant="danger" onClick={onClick}>
        Discard
      </Button>,
    );
    const button = screen.getByRole('button', { name: 'Discard' });
    expect(button).toHaveAttribute('type', 'button');
    expect(button).toHaveClass('btn', 'btn-danger');
    fireEvent.click(button);
    expect(onClick).toHaveBeenCalledTimes(1);
  });
});
