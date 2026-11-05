// Integer time in thirds of a µs so a 50 ms frame yields exactly three steps; float ms would yield two.
const unitsPerStep = 50_000;

/** How the client paces itself by the world's tick rate: each tick holds stepsPerTick steps of 1/60 s. */
export interface TickTiming {
  readonly tickMs: number;
  readonly stepsPerTick: number;
}

/** A rate that does not split into whole 1/60 s steps, or 0 from an older server, counts as 20 Hz. */
export function tickTiming(hz: number): TickTiming {
  const rate = Number.isInteger(hz) && hz > 0 && 60 % hz === 0 ? hz : 20;
  return { tickMs: 1000 / rate, stepsPerTick: 60 / rate };
}

export interface Step {
  readonly tickStart: boolean;
}

export class FixedStep {
  private accumulated = 0;
  private stepCount = 0;

  /** Time beyond maxStepsPerFrame (a background tab coming back) is dropped, not simulated in a burst. */
  constructor(
    readonly stepsPerTick: number,
    readonly maxStepsPerFrame = 15,
  ) {
    if (!Number.isInteger(stepsPerTick) || stepsPerTick < 1) {
      throw new RangeError(`stepsPerTick must be a positive integer, got ${stepsPerTick}`);
    }
    if (!Number.isInteger(maxStepsPerFrame) || maxStepsPerFrame < 1) {
      throw new RangeError(`maxStepsPerFrame must be a positive integer, got ${maxStepsPerFrame}`);
    }
  }

  advance(frameMs: number): Step[] {
    if (!(frameMs >= 0)) {
      throw new RangeError(`frame time must be a non-negative number, got ${frameMs}`);
    }
    this.accumulated += Math.round(frameMs * 3000);
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

  static readonly stepMs = 1000 / 60;
}
