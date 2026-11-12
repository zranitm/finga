// Integer time, 50 000 units to a step, so a 50 ms frame yields exactly three 1/60 s steps; float ms would yield two.
const unitsPerStep = 50_000;

/** How the client paces itself by the world's tick rate: each tick holds stepsPerTick steps of the movement dt. */
export interface TickTiming {
  readonly tickMs: number;
  readonly stepsPerTick: number;
  readonly stepsPerSecond: number;
}

/** A rate that does not split into whole steps of dt, or 0 from an older server, counts as 20 Hz. */
export function tickTiming(hz: number, dt: number): TickTiming {
  const stepsPerSecond = Math.round(1 / dt);
  const rate = Number.isInteger(hz) && hz > 0 && stepsPerSecond % hz === 0 ? hz : 20;
  return { tickMs: 1000 / rate, stepsPerTick: stepsPerSecond / rate, stepsPerSecond };
}

/** How near, relative to its count, a value must be to whole ticks to count as them: 1000 / hz is not exact in floating point. */
export const WHOLE_TICKS = 1e-6;

/** How many ticks of tickMs a duration lasts, rounded up and at least one, as the server counts it; a duration of whole ticks counts as exactly that many. */
export function ticksIn(ms: number, tickMs: number): number {
  const ticks = ms / tickMs;
  const whole = Math.round(ticks);
  return Math.max(Math.abs(ticks - whole) <= whole * WHOLE_TICKS ? whole : Math.ceil(ticks), 1);
}

export interface Step {
  readonly tickStart: boolean;
}

export class FixedStep {
  private accumulated = 0;
  private stepCount = 0;
  private readonly stepsPerTick: number;
  private readonly unitsPerMs: number;

  /** Time beyond maxStepsPerFrame (a background tab coming back) is dropped, not simulated in a burst. */
  constructor(
    timing: TickTiming,
    readonly maxStepsPerFrame = 15,
  ) {
    const { stepsPerTick, stepsPerSecond } = timing;
    if (!Number.isInteger(stepsPerTick) || stepsPerTick < 1) {
      throw new RangeError(`stepsPerTick must be a positive integer, got ${stepsPerTick}`);
    }
    if (!Number.isInteger(stepsPerSecond) || stepsPerSecond < 1) {
      throw new RangeError(`stepsPerSecond must be a positive integer, got ${stepsPerSecond}`);
    }
    this.stepsPerTick = stepsPerTick;
    this.unitsPerMs = (unitsPerStep * stepsPerSecond) / 1000;
    if (!Number.isInteger(maxStepsPerFrame) || maxStepsPerFrame < 1) {
      throw new RangeError(`maxStepsPerFrame must be a positive integer, got ${maxStepsPerFrame}`);
    }
  }

  advance(frameMs: number): Step[] {
    if (!(frameMs >= 0)) {
      throw new RangeError(`frame time must be a non-negative number, got ${frameMs}`);
    }
    this.accumulated += Math.round(frameMs * this.unitsPerMs);
    const steps: Step[] = [];
    while (this.accumulated >= unitsPerStep) {
      this.accumulated -= unitsPerStep;
      if (steps.length < this.maxStepsPerFrame) {
        steps.push({ tickStart: this.stepCount % this.stepsPerTick === 0 });
        this.stepCount++;
      }
    }
    if (steps.length === this.maxStepsPerFrame) {
      this.accumulated = 0;
    }
    return steps;
  }
}
