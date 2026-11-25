import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';

import { OWN_LOOK } from '@/state/wardrobe';
import type { Picks } from '@/state/wardrobe';
import { fakePortrait } from '@/testing/portrait';

import { FigureView } from './FigureView';

function setup(cls: string, look: Picks = OWN_LOOK) {
  const figure = fakePortrait();
  const figureIn = vi.fn(() => figure);
  const view = render(
    <FigureView figureIn={figureIn} cls={cls} look={look} label="Ayla" className="fig" />,
  );
  const redraw = (c: string, l: Picks) =>
    view.rerender(<FigureView figureIn={figureIn} cls={c} look={l} label="Ayla" className="fig" />);
  return { figure, figureIn, view, redraw, canvas: screen.getByLabelText('Ayla') };
}

describe('a figure view (outfits.md section 7)', () => {
  it('draws the character into its own canvas, dressed again as the look changes, once its class is known', () => {
    const { figure, figureIn, redraw, canvas } = setup('');
    expect(figureIn).toHaveBeenCalledWith(canvas);
    expect(figure.show).not.toHaveBeenCalled();
    redraw('warrior', OWN_LOOK);
    expect(figure.show).toHaveBeenLastCalledWith('warrior', OWN_LOOK);
    const hat = { ...OWN_LOOK, headwear: 'pointed_hat' };
    redraw('warrior', hat);
    expect(figure.show).toHaveBeenLastCalledWith('warrior', hat);
    expect(figureIn).toHaveBeenCalledOnce();
  });

  it('turns as it’s dragged, holding the pointer, and not once it’s let go', () => {
    const { figure, canvas } = setup('warrior');
    const capture = vi.fn();
    canvas.setPointerCapture = capture; // jsdom has none
    fireEvent.pointerDown(canvas, { clientX: 100, buttons: 1 });
    expect(capture).toHaveBeenCalledOnce();
    fireEvent.pointerMove(canvas, { clientX: 130, buttons: 1 });
    fireEvent.pointerMove(canvas, { clientX: 150, buttons: 0 }); // released outside: no turn
    fireEvent.pointerUp(canvas);
    fireEvent.pointerMove(canvas, { clientX: 170, buttons: 1 }); // a press elsewhere carried in: starts, doesn't turn
    expect(figure.turn).toHaveBeenCalledOnce();
    expect(figure.turn.mock.calls[0]![0]).toBeCloseTo(0.3);
  });

  it('lets the figure go as it unmounts, which stops its drawing', () => {
    const { figure, view } = setup('warrior');
    expect(figure.close).not.toHaveBeenCalled();
    view.unmount();
    expect(figure.close).toHaveBeenCalledOnce();
  });
});
