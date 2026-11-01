// Sends an Input every tick even when nothing changed; a jump tapped within a tick still counts.
import { heldInput } from '@/input/actions';
import type { Action } from '@/input/actions';
import { noInput } from '@/sim/input';
import type { Input } from '@/sim/input';

export interface InputSenderOptions {
  readonly send: (input: Input, tick: number) => boolean;
}

export class InputSender {
  private readonly held = new Set<Action>();
  private jumpLatched = false;
  private captured = false;
  private tick = 0;

  constructor(private readonly opts: InputSenderOptions) {}

  press(action: Action): void {
    if (this.captured) {
      return;
    }
    this.held.add(action);
    if (action === 'jump') {
      this.jumpLatched = true;
    }
  }

  /** Honoured even while captured, so no key sticks. */
  release(action: Action): void {
    this.held.delete(action);
  }

  releaseAll(): void {
    this.held.clear();
  }

  /** Starting a capture sends an all-released Input at once. */
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

  get currentTick(): number {
    return this.tick;
  }

  sample(): Input {
    if (this.captured) {
      return noInput;
    }
    const input = heldInput(this.held);
    return this.jumpLatched ? { ...input, jump: true } : input;
  }

  tickStart(): Input {
    const input = this.sample();
    this.jumpLatched = false;
    this.tick++;
    this.opts.send(input, this.tick);
    return input;
  }
}
