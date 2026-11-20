// The settings a screen changes, saved a moment after the last of a run of changes, the last value winning, as the skill bar's are.
import { useEffect, useMemo, useRef } from 'react';

import { saveQueue } from '@/lib/save-queue';
import type { RestClient } from '@/net/rest';

import { holdReadsFor, saveSettings } from './settings';

export const SETTINGS_SAVE_DELAY_MS = 500; // a run of flips or rebinds settles before its one PUT

/** The change of a queue saving the settings, which a read waits for and leaving the screen or the page saves at once; told hears each save's problem, or null. */
export function useSettingsSaves(
  rest: Pick<RestClient, 'putSettings'>,
  told: (problem: string | null) => void,
): () => void {
  const tell = useRef(told);
  useEffect(() => {
    tell.current = told;
  });
  const queue = useMemo(
    () =>
      saveQueue(
        () => () => saveSettings(rest).then((p) => tell.current(p)),
        SETTINGS_SAVE_DELAY_MS,
      ),
    [rest],
  );
  useEffect(() => {
    window.addEventListener('pagehide', queue.flush);
    const held = holdReadsFor(queue);
    return () => {
      held();
      window.removeEventListener('pagehide', queue.flush);
      queue.flush();
    };
  }, [queue]);
  return queue.changed;
}
