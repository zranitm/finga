// Each unit's status marks (statuses.md section 6): one per status it has, started as a frame lists it and stopped as one no longer does.
import type { StatusOn } from '@/progression/statuses';

import type { Fx } from '../effects/fx';
import { statusEffect } from '../effects/ids';
import type { Body, Stop } from '../effects/types';
import { parseRef } from '../systems/combat-view';
import type { Ref } from '../systems/combat-view';

export class StatusMarks {
  private readonly playing = new Map<string, Map<string, Stop>>(); // by unit ref, by status id

  constructor(
    private readonly fx: Fx,
    private readonly body: (r: Ref) => Body | undefined,
  ) {}

  /** The units drawn now, by ref, with their statuses: a new one's mark starts on its body, an ended one's stops, and a unit not listed loses all of its. */
  show(units: ReadonlyMap<string, readonly StatusOn[]>): void {
    for (const [ref, marks] of this.playing) {
      const on = units.get(ref) ?? [];
      for (const [id, stop] of marks) {
        if (!on.some((s) => s.id === id)) {
          stop();
          marks.delete(id);
        }
      }
      if (marks.size === 0) {
        this.playing.delete(ref);
      }
    }
    for (const [ref, on] of units) {
      const r = parseRef(ref);
      const at = r && this.body(r);
      if (!r || !at || on.length === 0) {
        continue;
      }
      let marks = this.playing.get(ref);
      for (const s of on) {
        if (marks?.has(s.id)) {
          continue;
        }
        marks ??= new Map();
        this.playing.set(ref, marks);
        const size = { radius: at.radius, height: at.height };
        marks.set(
          s.id,
          this.fx.play(statusEffect(s.id), { ...at, yaw: 0, size, follow: () => this.body(r) }),
        );
      }
    }
  }

  /** Every mark stops, as everyone drawn goes. */
  clear(): void {
    for (const marks of this.playing.values()) {
      for (const stop of marks.values()) {
        stop();
      }
    }
    this.playing.clear();
  }
}
