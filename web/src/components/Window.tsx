import { useCallback, useEffect, useLayoutEffect, useRef, useState } from 'react';
import type { KeyboardEvent, PointerEvent, ReactNode } from 'react';

import { Button } from './Button';

export interface WindowProps {
  readonly title: string;
  readonly onClose: () => void;
  readonly initialX?: number;
  readonly initialY?: number;
  /** Its place among the open windows; a higher one draws on top. */
  readonly layer?: number;
  /** A press or focus inside asks to bring it to the top. */
  readonly onRaise?: () => void;
  /** Below the scrolling body, so it stays in view however long the body: a refusal's notice. */
  readonly footer?: ReactNode;
  readonly children: ReactNode;
}

// A window ends this far above the bottom, clear of the idle chat's lines, and scrolls what doesn't fit.
const BOTTOM_CLEAR_PX = 84;
const MIN_HEIGHT_PX = 160;
const EDGE_PX = 8;

/** Takes focus on open, so Escape closes it; the game's keys still reach the game. */
export function Window({
  title,
  onClose,
  initialX = 80,
  initialY = 80,
  layer,
  onRaise,
  footer,
  children,
}: WindowProps) {
  const [pos, setPos] = useState({ x: initialX, y: initialY });
  const drag = useRef<{ dx: number; dy: number } | null>(null);
  const root = useRef<HTMLDivElement>(null);

  useEffect(() => {
    root.current?.focus();
  }, []);

  // Opened past the right edge of a narrow screen, it moves left until it fits.
  useLayoutEffect(() => {
    const width = root.current?.getBoundingClientRect().width ?? 0;
    const over = initialX + width + EDGE_PX - window.innerWidth;
    if (over > 0) {
      setPos((p) => ({ ...p, x: Math.max(EDGE_PX, p.x - over) }));
    }
  }, [initialX]);

  const onKeyDown = useCallback(
    (e: KeyboardEvent<HTMLDivElement>) => {
      if (e.key === 'Escape') {
        e.stopPropagation();
        onClose();
      }
    },
    [onClose],
  );

  const onPointerDown = (e: PointerEvent<HTMLDivElement>) => {
    if (e.target instanceof Element && e.target.closest('button')) {
      return; // a captured pointer would send the button's click to the title bar
    }
    drag.current = { dx: e.clientX - pos.x, dy: e.clientY - pos.y };
    e.currentTarget.setPointerCapture(e.pointerId);
  };
  const onPointerMove = (e: PointerEvent<HTMLDivElement>) => {
    if (drag.current) {
      setPos({ x: e.clientX - drag.current.dx, y: e.clientY - drag.current.dy });
    }
  };
  const onPointerUp = () => {
    drag.current = null;
  };

  return (
    <div
      ref={root}
      className="window"
      role="dialog"
      aria-label={title}
      tabIndex={-1}
      style={{
        left: pos.x,
        top: pos.y,
        zIndex: layer,
        maxHeight: `max(${MIN_HEIGHT_PX}px, calc(100vh - ${pos.y + BOTTOM_CLEAR_PX}px))`,
      }}
      onKeyDown={onKeyDown}
      onPointerDownCapture={onRaise}
      onFocus={onRaise}
    >
      <div
        className="window-title"
        onPointerDown={onPointerDown}
        onPointerMove={onPointerMove}
        onPointerUp={onPointerUp}
      >
        <span>{title}</span>
        <Button variant="quiet" aria-label={`Close ${title}`} onClick={onClose}>
          ×
        </Button>
      </div>
      <div className="window-body">{children}</div>
      {footer && <div className="window-footer">{footer}</div>}
    </div>
  );
}
