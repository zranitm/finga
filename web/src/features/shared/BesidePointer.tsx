// Cards beside the pointer, above every window and never catching it, kept fully on screen: the item cards and the skill card.
import { useLayoutEffect, useRef, useState } from 'react';
import type { ReactNode } from 'react';
import { createPortal } from 'react-dom';

import { besidePointer } from '@/lib/placement';
import type { Point, Size } from '@/lib/placement';

/** Measured before paint, after each change of what it holds, so it places by its real size. */
export function BesidePointer({
  at,
  children,
}: {
  readonly at: Point;
  readonly children: ReactNode;
}) {
  const box = useRef<HTMLDivElement>(null);
  const [size, setSize] = useState<Size>({ width: 0, height: 0 });
  useLayoutEffect(() => {
    const r = box.current?.getBoundingClientRect();
    if (r && (r.width !== size.width || r.height !== size.height)) {
      setSize({ width: r.width, height: r.height });
    }
  }, [children, size.width, size.height]);
  const place = besidePointer(at, size, { width: window.innerWidth, height: window.innerHeight });
  return createPortal(
    <div ref={box} className="item-cards" style={{ left: place.x, top: place.y }}>
      {children}
    </div>,
    document.body,
  );
}
