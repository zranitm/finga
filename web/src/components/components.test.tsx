import { fireEvent, render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';

import { Bar } from './Bar';
import { Button } from './Button';
import { Modal } from './Modal';
import { Window } from './Window';

describe('Window', () => {
  it('takes focus when it opens, reports focus changes, and closes on Escape', async () => {
    const onClose = vi.fn();
    const onFocusChange = vi.fn();
    render(
      <Window title="Inventory" onClose={onClose} onFocusChange={onFocusChange}>
        <input aria-label="search" />
      </Window>,
    );
    const dialog = screen.getByRole('dialog', { name: 'Inventory' });
    expect(dialog).toHaveFocus();
    expect(onFocusChange).toHaveBeenLastCalledWith(true);
    await userEvent.keyboard('{Escape}');
    expect(onClose).toHaveBeenCalledTimes(1);
    // Focus moving inside the window is not a loss of focus.
    screen.getByLabelText('search').focus();
    expect(onFocusChange).not.toHaveBeenCalledWith(false);
    screen.getByLabelText('search').blur();
    expect(onFocusChange).toHaveBeenLastCalledWith(false);
  });

  it('closes from its title-bar button and moves when the title is dragged', () => {
    const onClose = vi.fn();
    render(
      <Window title="Party" onClose={onClose} initialX={100} initialY={50}>
        <p>members</p>
      </Window>,
    );
    fireEvent.click(screen.getByRole('button', { name: 'Close Party' }));
    expect(onClose).toHaveBeenCalledTimes(1);
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
