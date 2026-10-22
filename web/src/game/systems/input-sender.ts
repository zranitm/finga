// The input sender (page 7, Sending): samples the held keys once per 50 ms
// tick and sends that sample as the tick's Input, every tick, even when
// nothing changed. A jump pressed and released inside a tick still counts
// for that tick. While the keyboard is captured by a text field or window,
// an all-released Input goes out at once and no keys are sampled.
import { heldInput } from '@/input/actions';
import type { Action } from '@/input/actions';
import { noInput } from '@/sim/input';
import type { Input } from '@/sim/input';

export interface InputSenderOptions {
  /** GameSocket.sendInput: one Input per tick number. */
  readonly send: (input: Input, tick: number) => boolean;
}

export class InputSender {
  private readonly held = new Set<Action>();
  private jumpLatched = false;
  private captured = false;
  private tick = 0;

  constructor(private readonly opts: InputSenderOptions) {}

  /** A movement key went down; ignored while captured. */
  press(action: Action): void {
    if (this.captured) {
      return;
    }
    this.held.add(action);
    if (action === 'jump') {
      this.jumpLatched = true;
    }
  }

  /** A movement key went up; always honoured so no key sticks. */
  release(action: Action): void {
    this.held.delete(action);
  }

  /** Every key up (the window lost focus). */
  releaseAll(): void {
    this.held.clear();
  }

  /** Keyboard capture starts or ends; starting sends an all-released Input at once. */
  setCaptured(on: boolean): void {
    if (on === this.captured) {
      return;
    }
    this.captured = on;
    if (on) {
      this.held.clear();
      this.jumpLatched = false;
      this.tick++;
      this.opts.send(noInput, this.tick);
    }
  }

  get isCaptured(): boolean {
    return this.captured;
  }

  /** The tick number of the last sample, for actions that go once per tick. */
  get currentTick(): number {
    return this.tick;
  }

  /** The keys held right now, as the sim sees them, with a latched jump. */
  sample(): Input {
    if (this.captured) {
      return noInput;
    }
    const input = heldInput(this.held);
    return this.jumpLatched ? { ...input, jump: true } : input;
  }

  /** At a tick boundary: samples, sends with the next tick number, returns the Input for prediction. */
  tickStart(): Input {
    const input = this.sample();
    this.jumpLatched = false;
    this.tick++;
    this.opts.send(input, this.tick);
    return input;
  }
}
