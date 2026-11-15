/** The keys held during one sub-step: up walks north (+z), right east (+x). */
export interface Input {
  readonly left: boolean;
  readonly right: boolean;
  readonly up: boolean;
  readonly down: boolean;
  readonly jump: boolean;
}
