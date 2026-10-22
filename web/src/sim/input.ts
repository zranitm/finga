// The held keys during one sub-step, as the server expects them (page 4's
// Input), independent of key presets.
export interface Input {
  readonly left: boolean;
  readonly right: boolean;
  readonly up: boolean;
  readonly down: boolean;
  readonly jump: boolean;
}

export const noInput: Input = { left: false, right: false, up: false, down: false, jump: false };

/** True when any key is held. */
export function anyHeld(input: Input): boolean {
  return input.left || input.right || input.up || input.down || input.jump;
}
