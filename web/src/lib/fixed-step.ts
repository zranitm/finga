// Turns variable frame time into whole 1/60 s simulation steps, and flags
// every third step as the start of a 50 ms tick (C08 samples keys there).
//
// Time is counted as an integer, in thirds of a microsecond: one step is
// then exactly 50,000 units and a 50 ms frame exactly 150,000, so three
// steps come out of a 50 ms frame with nothing left over. In floating-point
// milliseconds, 50 - 1000/60 - 1000/60 is a hair under one step, and a
// 50 ms frame would yield two.
const unitsPerStep = 50_000; // 1/60 s in thirds of a microsecond
const stepsPerTick = 3;

export interface Step {
  /** True when this step begins a 50 ms tick. */
  readonly tickStart: boolean;
}

export class FixedStep {
  private accumulated = 0; // in thirds of a microsecond
  private stepCount = 0; // steps produced so far, for the tick boundary

  /**
   * maxStepsPerFrame caps a long frame (a background tab coming back): the
   * excess time is dropped rather than simulated in one burst.
   */
  constructor(readonly maxStepsPerFrame = 15) {
    if (!Number.isInteger(maxStepsPerFrame) || maxStepsPerFrame < 1) {
      throw new RangeError(`maxStepsPerFrame must be a positive integer, got ${maxStepsPerFrame}`);
    }
  }

  /** Adds a frame's duration and returns the steps it yields, in order. */
  advance(frameMs: number): Step[] {
    if (!(frameMs >= 0)) {
      throw new RangeError(`frame time must be a non-negative number, got ${frameMs}`);
    }
    this.accumulated += Math.round(frameMs * 3000); // ms → thirds of a µs
    const steps: Step[] = [];
    while (this.accumulated >= unitsPerStep) {
      this.accumulated -= unitsPerStep;
      if (steps.length < this.maxStepsPerFrame) {
        steps.push({ tickStart: this.stepCount % stepsPerTick === 0 });
        this.stepCount++;
      }
    }
    if (steps.length === this.maxStepsPerFrame) {
      this.accumulated = 0; // the remainder of a very long frame is dropped
    }
    return steps;
  }

  /** The step length in milliseconds, for the simulation. */
  static readonly stepMs = 1000 / 60;
}
