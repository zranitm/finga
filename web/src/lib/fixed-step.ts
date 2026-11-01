// Integer time in thirds of a µs so a 50 ms frame yields exactly three steps; float ms would yield two.
const unitsPerStep = 50_000;
const stepsPerTick = 3;

export interface Step {
  readonly tickStart: boolean;
}

export class FixedStep {
  private accumulated = 0;
  private stepCount = 0;

  /** Time beyond maxStepsPerFrame (a background tab coming back) is dropped, not simulated in a burst. */
  constructor(readonly maxStepsPerFrame = 15) {
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
        steps.push({ tickStart: this.stepCount % stepsPerTick === 0 });
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
