// What lets an element show a skill's card, a grid tile or an icon in the chains panel alike: on hover beside the pointer, on focus beside the element, hidden as it leaves, blurs or drags.
import type { DragEvent, FocusEvent, MouseEvent } from 'react';

import { dragTile } from '@/components/SkillIcon';
import type { Point } from '@/lib/placement';
import { SKILL_DRAG_TYPE } from '@/state/skill-bar';

/** One element's say over the card: whether it's the one showing it, and showing or hiding it from there. */
export interface CardTarget {
  readonly shown: boolean;
  readonly onShow: (at: Point) => void;
  readonly onHide: () => void;
}

/** The element's focus, pointer and description props for the card cardId. */
export function cardProps(t: CardTarget, cardId: string) {
  const at = (e: MouseEvent) => t.onShow({ x: e.clientX, y: e.clientY });
  return {
    tabIndex: 0,
    'aria-describedby': t.shown ? cardId : undefined,
    onMouseEnter: at,
    onMouseMove: at,
    onMouseLeave: t.onHide,
    onFocus: (e: FocusEvent<HTMLElement>) => {
      const r = e.currentTarget.getBoundingClientRect();
      t.onShow({ x: r.right, y: r.top });
    },
    onBlur: t.onHide,
  };
}

/** Drags skill onto the skill bar, its tile under the pointer, hiding the card. */
export function dragSkill(skill: string, t: CardTarget) {
  return (e: DragEvent<HTMLElement>) => {
    t.onHide();
    e.dataTransfer.setData(SKILL_DRAG_TYPE, skill);
    e.dataTransfer.effectAllowed = 'copy';
    dragTile(e);
  };
}
