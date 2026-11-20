// Sends an Input every tick even when nothing changed, with the mode; a jump tapped within a tick still counts.
import { heldInput } from '@/input/actions';
import type { Action } from '@/input/actions';
import type { Input } from '@/sim/input';

export interface InputSenderOptions {
  readonly send: (input: Input, tick: number, view: number) => boolean;
  /** Whether walk mode is on, which every input carries (large-regions.md section 7). */
  readonly walk: () => boolean;
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

  private letGo(): Input {
    const input = heldInput(new Set());
    return this.opts.walk() ? { ...input, walk: true } : input;
  }

  private sample(): Input {
    const input = heldInput(this.held);
    const jumped = this.jumpLatched ? { ...input, jump: true } : input;
    return this.opts.walk() ? { ...jumped, walk: true } : jumped;
  }

  /** Sends this tick's input, with view, the tick of the mobs drawn as it's predicted; every key let go while held, the walk mode kept. */
  tickStart(view: number, held = false): Input {
    const input = held ? this.letGo() : this.sample();
    this.jumpLatched = false;
    this.tick++;
    this.opts.send(input, this.tick, view);
    return input;
  }
}
