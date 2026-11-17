import { act, render } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { skillById } from '@/progression/skills';

import { SkillIcon } from './SkillIcon';

const strike = skillById('strike')!;
const tile = (c: HTMLElement) => c.querySelector<HTMLElement>('.skill-icon')!;
const look = (c: HTMLElement, name: string) => tile(c).style.getPropertyValue(name);
const count = (c: HTMLElement) => c.querySelector('.skill-icon-count')?.textContent;
const shaded = (c: HTMLElement) =>
  c.querySelector<HTMLElement>('.skill-icon-sweep')?.style.getPropertyValue('--left');
const flashing = (c: HTMLElement) => c.querySelector('.skill-icon-flash') !== null;

describe('SkillIcon', () => {
  afterEach(() => {
    vi.useRealTimers();
    vi.restoreAllMocks();
  });

  it('draws its stage’s glyph on the skill’s colour, with a pip a stage reached for a skill that evolves', () => {
    const { container, rerender } = render(<SkillIcon skill={strike} points={1} size="bar" />);
    expect([look(container, '--skill'), look(container, '--icon')]).toEqual([
      '#ff6a3d',
      'url("/icons/skills/lorc/sword-wound.svg")',
    ]);
    expect(container.querySelectorAll('.skill-icon-pip')).toHaveLength(1);
    rerender(<SkillIcon skill={strike} points={25} size="bar" />);
    expect(look(container, '--icon')).toBe('url("/icons/skills/lorc/hammer-drop.svg")');
    expect(container.querySelectorAll('.skill-icon-pip')).toHaveLength(3);
    rerender(<SkillIcon skill={strike} points={0} size="window" />);
    expect(look(container, '--icon')).toBe('url("/icons/skills/lorc/sword-wound.svg")');
    expect(container.querySelector('.skill-icon-pips')).toBeNull(); // no stage reached yet
    rerender(<SkillIcon skill={skillById('heal')!} points={1} size="window" />);
    expect(tile(container).className).toBe('skill-icon skill-icon-window');
    expect(look(container, '--skill')).toBe('#4fd8ff');
    expect(container.querySelector('.skill-icon-pips')).toBeNull(); // one stage: nothing to count
    rerender(<SkillIcon skill={skillById('spark')!} points={10} size="bar" off />);
    expect(tile(container)).toHaveClass('skill-icon-off');
    expect(look(container, '--skill')).toBe('#b070f0');
  });

  it('greys only its face when off, so a cooldown’s sweep and count stay full over it', () => {
    vi.spyOn(performance, 'now').mockReturnValue(1000);
    const { container } = render(
      <SkillIcon skill={strike} points={1} size="bar" off cooldown={{ from: 0, until: 59_000 }} />,
    );
    expect(tile(container)).toHaveClass('skill-icon-off');
    const face = container.querySelector('.skill-icon-face')!;
    expect(face.querySelector('.skill-icon-glyph')).not.toBeNull();
    expect([
      face.querySelector('.skill-icon-count'),
      face.querySelector('.skill-icon-sweep'),
    ]).toEqual([null, null]);
    expect(count(container)).toBe('58');
  });

  it('counts whole seconds rounded up, and tenths rounded up below one', () => {
    const now = vi.spyOn(performance, 'now');
    const at = (left: number) => {
      now.mockReturnValue(10_000);
      const { container, unmount } = render(
        <SkillIcon
          skill={strike}
          points={1}
          size="bar"
          cooldown={{ from: 5000, until: 10_000 + left }}
        />,
      );
      const text = count(container);
      unmount();
      return text;
    };
    expect([at(4200), at(1000), at(660), at(40)]).toEqual(['5', '1', '0.7', '0.1']);
  });

  it('shades from 360° down to 0°, sweeping clockwise from the top: half-way, 180°', () => {
    vi.useFakeTimers();
    const t = performance.now();
    const { container } = render(
      <SkillIcon skill={strike} points={1} size="bar" cooldown={{ from: t, until: t + 4000 }} />,
    );
    expect([shaded(container), count(container)]).toEqual(['360deg', '4']);
    act(() => {
      vi.advanceTimersByTime(2000);
    });
    expect([shaded(container), count(container)]).toEqual(['180deg', '2']);
    act(() => {
      vi.advanceTimersByTime(1200);
    }); // frames fall every 16 ms: 3200 ms is one
    expect([shaded(container), count(container)]).toEqual(['72deg', '0.8']);
  });

  it('a cooldown restored part-way through starts part-way: 10 s left of 60 shows a sixth shaded', () => {
    vi.spyOn(performance, 'now').mockReturnValue(500_000);
    const { container } = render(
      <SkillIcon
        skill={strike}
        points={1}
        size="bar"
        cooldown={{ from: 450_000, until: 510_000 }}
      />,
    );
    expect([shaded(container), count(container)]).toEqual(['60deg', '10']);
  });

  it('flashes white for 150 ms at the end, whether the last frame or the store ends it', () => {
    vi.useFakeTimers();
    const t = performance.now();
    const cooling = { from: t, until: t + 1000 };
    const { container, rerender } = render(
      <SkillIcon skill={strike} points={1} size="bar" cooldown={cooling} />,
    );
    act(() => {
      vi.advanceTimersByTime(1020);
    }); // a frame past the end
    expect(flashing(container)).toBe(true);
    rerender(<SkillIcon skill={strike} points={1} size="bar" />);
    act(() => {
      vi.advanceTimersByTime(150);
    });
    expect(flashing(container)).toBe(false);

    const again = { from: t + 1170, until: t + 2170 };
    rerender(<SkillIcon skill={strike} points={1} size="bar" cooldown={again} />);
    act(() => {
      vi.advanceTimersByTime(1000);
    }); // at the end, before its frame: the store's timer ends it
    rerender(<SkillIcon skill={strike} points={1} size="bar" />);
    expect(flashing(container)).toBe(true);
  });

  it('does not flash for a cooldown taken away part-way', () => {
    vi.useFakeTimers();
    const t = performance.now();
    const { container, rerender } = render(
      <SkillIcon skill={strike} points={1} size="bar" cooldown={{ from: t, until: t + 1000 }} />,
    );
    act(() => {
      vi.advanceTimersByTime(500);
    });
    rerender(<SkillIcon skill={strike} points={1} size="bar" />);
    act(() => {
      vi.advanceTimersByTime(600);
    });
    expect([flashing(container), container.querySelector('.skill-icon-sweep')]).toEqual([
      false,
      null,
    ]);
  });
});
