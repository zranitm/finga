// Sends an Input every tick even when nothing changed; a jump tapped within a tick still counts.
import { heldInput } from '@/input/actions';
import type { Action } from '@/input/actions';
import type { Input } from '@/sim/input';

export interface InputSenderOptions {
  readonly send: (input: Input, tick: number, view: number) => boolean;
}

export class InputSender {
  private readonly held = new Set<Action>();
  private jumpLatched = false;
  private tick = 0;

  constructor(private readonly opts: InputSenderOptions) {}

  press(action: Action): void {
    this.held.add(action);
    if (action === 'jump') {
      this.jumpLatched = true;
    }
  }

  release(action: Action): void {
    this.held.delete(action);
  }

  /** Lets go of every key, a jump tapped this tick too, as when typing starts or the page loses focus. */
  releaseAll(): void {
    this.held.clear();
    this.jumpLatched = false;
  }

  private sample(): Input {
    const input = heldInput(this.held);
    return this.jumpLatched ? { ...input, jump: true } : input;
  }

  /** Sends this tick's input, with view, the tick of the mobs drawn as it's predicted. */
  tickStart(view = 0): Input {
    const input = this.sample();
    this.jumpLatched = false;
    this.tick++;
    this.opts.send(input, this.tick, view);
    return input;
  }
}
