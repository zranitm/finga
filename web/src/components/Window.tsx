import { useCallback, useEffect, useRef, useState } from 'react';
import type { KeyboardEvent, PointerEvent, ReactNode } from 'react';

import { Button } from './Button';

export interface WindowProps {
  readonly title: string;
  readonly onClose: () => void;
  /** The feature wires it to ui.captureKeyboard. */
  readonly onFocusChange?: (focused: boolean) => void;
  readonly initialX?: number;
  readonly initialY?: number;
  readonly children: ReactNode;
}

/** Takes focus on open so its keys stop reaching the game. */
export function Window({
  title,
  onClose,
  onFocusChange,
  initialX = 80,
  initialY = 80,
  children,
}: WindowProps) {
  const [pos, setPos] = useState({ x: initialX, y: initialY });
  const drag = useRef<{ dx: number; dy: number } | null>(null);
  const root = useRef<HTMLDivElement>(null);

  useEffect(() => {
    root.current?.focus();
  }, []);

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
      style={{ left: pos.x, top: pos.y }}
      onKeyDown={onKeyDown}
      onFocus={() => onFocusChange?.(true)}
      onBlur={(e) => {
        if (!e.currentTarget.contains(e.relatedTarget)) {
          onFocusChange?.(false);
        }
      }}
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
    </div>
  );
}
