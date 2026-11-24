// Pan and zoom on a region's plan (large-regions.md section 10): a drag pans, the wheel zooms about the pointer and the buttons about the middle, the view held inside the map.
import { useEffect, useRef, useState } from 'react';
import type { MouseEvent, PointerEvent, RefObject } from 'react';

import type { MapBox } from './MapMarks';

const MAX_PX_PER_M = 4; // zoomed in no further than a tile's metre at this many screen pixels
export const ZOOM_STEP = 2; // a button's zoom
const WHEEL_PX = 400; // the wheel's scroll that zooms by ZOOM_STEP
const DRAG_PX = 4; // a press that moves less is a click

/** A view of a map size metres across, zoom times its whole, its west and north edges in metres. */
export interface Zoomed {
  readonly zoom: number;
  readonly x: number;
  readonly y: number;
}

/** v with its corner held so the view stays inside a map size across. */
function held(v: Zoomed, [sx, sz]: readonly [number, number]): Zoomed {
  const w = sx / v.zoom;
  const h = sz / v.zoom;
  return {
    zoom: v.zoom,
    x: Math.min(Math.max(v.x, 0), sx - w),
    y: Math.min(Math.max(v.y, 0), sz - h),
  };
}

/** v zoomed by factor, from 1 to most, keeping the spot at (ax, ay) metres where it is on screen. */
export function zoomedBy(
  v: Zoomed,
  size: readonly [number, number],
  factor: number,
  most: number,
  ax: number,
  ay: number,
): Zoomed {
  const zoom = Math.min(Math.max(v.zoom * factor, 1), most);
  const k = v.zoom / zoom;
  return held({ zoom, x: ax - (ax - v.x) * k, y: ay - (ay - v.y) * k }, size);
}

/** v moved dx and dy metres, held inside the map. */
export function pannedBy(
  v: Zoomed,
  size: readonly [number, number],
  dx: number,
  dy: number,
): Zoomed {
  return held({ ...v, x: v.x + dx, y: v.y + dy }, size);
}

/** The pan and zoom of a plan drawn at scale pixels a metre when whole, on a map size across; off, it stays whole. */
export function usePanZoom(
  svg: RefObject<SVGSVGElement | null>,
  size: readonly [number, number],
  scale: number,
  on: boolean,
) {
  const [v, setV] = useState<Zoomed>({ zoom: 1, x: 0, y: 0 });
  const most = on ? Math.max(1, MAX_PX_PER_M / scale) : 1;
  const dragged = useRef(false);
  const [sx, sz] = size;
  useEffect(() => {
    const el = svg.current;
    if (!el || !on) {
      return;
    }
    const wheel = (e: WheelEvent) => {
      e.preventDefault(); // the wheel zooms the plan, not the page
      const box = el.getBoundingClientRect();
      setV((now) =>
        zoomedBy(
          now,
          [sx, sz],
          ZOOM_STEP ** (-e.deltaY / WHEEL_PX),
          most,
          now.x + (e.clientX - box.left) / (scale * now.zoom),
          now.y + (e.clientY - box.top) / (scale * now.zoom),
        ),
      );
    };
    el.addEventListener('wheel', wheel, { passive: false });
    return () => el.removeEventListener('wheel', wheel);
  }, [svg, on, sx, sz, scale, most]);
  const view: MapBox = { x: v.x, y: v.y, w: sx / v.zoom, h: sz / v.zoom };
  /** A press on the plan: a drag past DRAG_PX pans it till the button's up, and the click that ends it picks nothing. */
  const onPointerDown = (e: PointerEvent<SVGSVGElement>) => {
    if (!on || e.button !== 0) {
      return;
    }
    const from = { x: e.clientX, y: e.clientY, v };
    dragged.current = false;
    const move = (m: globalThis.PointerEvent) => {
      const dx = m.clientX - from.x;
      const dy = m.clientY - from.y;
      if (!dragged.current && Math.hypot(dx, dy) < DRAG_PX) {
        return;
      }
      dragged.current = true;
      const k = scale * from.v.zoom;
      setV(pannedBy(from.v, size, -dx / k, -dy / k));
    };
    const up = () => {
      window.removeEventListener('pointermove', move);
      window.removeEventListener('pointerup', up);
    };
    window.addEventListener('pointermove', move);
    window.addEventListener('pointerup', up);
  };
  const onClickCapture = (e: MouseEvent<SVGSVGElement>) => {
    if (dragged.current) {
      dragged.current = false;
      e.stopPropagation();
    }
  };
  const zoomBy = (factor: number) => {
    setV((now) =>
      zoomedBy(now, size, factor, most, now.x + sx / now.zoom / 2, now.y + sz / now.zoom / 2),
    );
  };
  return { view, zoom: v.zoom, most, zoomBy, onPointerDown, onClickCapture };
}
