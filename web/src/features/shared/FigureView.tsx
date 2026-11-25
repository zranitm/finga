// A character drawn in 3D into a canvas of its own and turned by a drag, as the wardrobe and the character window show one (outfits.md section 7).
import { useEffect, useRef } from 'react';
import type { PointerEvent as ReactPointerEvent } from 'react';

import type { Picks, Portrait } from '@/state/wardrobe';

const TURN_PER_PX = 0.01; // radians a pixel of drag turns the figure

export interface FigureViewProps {
  /** The figure drawn into canvas till it's closed. */
  readonly figureIn: (canvas: HTMLCanvasElement) => Portrait;
  readonly cls: string;
  readonly look: Picks;
  readonly label: string;
  readonly className: string;
}

/** Drawn while mounted: the figure closes as it unmounts, which stops its drawing. */
export function FigureView({ figureIn, cls, look, label, className }: FigureViewProps) {
  const canvas = useRef<HTMLCanvasElement>(null);
  const figure = useRef<Portrait | null>(null);
  const dragged = useRef<number | null>(null);

  useEffect(() => {
    const f = figureIn(canvas.current!);
    figure.current = f;
    return () => {
      figure.current = null;
      f.close();
    };
  }, [figureIn]);

  useEffect(() => {
    if (cls) {
      figure.current?.show(cls, look);
    }
  }, [figureIn, cls, look]);

  const drag = (e: ReactPointerEvent<HTMLCanvasElement>) => {
    if (dragged.current !== null && e.buttons & 1) {
      figure.current?.turn((e.clientX - dragged.current) * TURN_PER_PX);
    }
    dragged.current = e.buttons & 1 ? e.clientX : null;
  };

  return (
    <canvas
      ref={canvas}
      className={className}
      aria-label={label}
      onPointerDown={(e) => {
        e.currentTarget.setPointerCapture(e.pointerId); // the drag keeps turning off the canvas
        drag(e);
      }}
      onPointerMove={drag}
      onPointerUp={() => (dragged.current = null)}
    />
  );
}
