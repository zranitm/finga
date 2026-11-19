/** The keys held during one sub-step: up walks north (+z), right east (+x). */
export interface Input {
  readonly left: boolean;
  readonly right: boolean;
  readonly up: boolean;
  readonly down: boolean;
  readonly jump: boolean;
  /** Walk mode, which never sprints (large-regions.md section 7); absent in sprint mode. */
  readonly walk?: boolean;
}
