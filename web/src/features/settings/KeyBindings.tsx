// The key-binding window (key-bindings.md section 4): every action's key, rebound by a click and a key press, applied at once and saved to the account a moment after the last change.
import { useEffect, useMemo, useRef, useState } from 'react';

import { Button } from '@/components/Button';
import { saveSettings } from '@/features/shared/settings';
import { useKeys } from '@/features/shared/useKeys';
import type { Action } from '@/input/actions';
import { fixedActions, keyName, rebind, reservedKeys } from '@/input/bindings';
import type { KeyTable } from '@/input/bindings';
import { saveQueue } from '@/lib/save-queue';
import type { RestClient } from '@/net/rest';
import { keyboardLayout } from '@/state/keyboard-layout';
import { settings } from '@/state/settings';

export const KEYS_SAVE_DELAY_MS = 500;

/** How long a refused key shakes its chip. */
const REFUSED_MS = 400;

const names: Readonly<Record<Action, string>> = {
  up: 'Walk up',
  down: 'Walk down',
  left: 'Walk left',
  right: 'Walk right',
  jump: 'Jump',
  walk: 'Walk or sprint',
  attack: 'Attack',
  nextTarget: 'Lock the next target',
  hpPotion: 'HP potion',
  skill1: 'Skill 1',
  skill2: 'Skill 2',
  skill3: 'Skill 3',
  skill4: 'Skill 4',
  skill5: 'Skill 5',
  skill6: 'Skill 6',
  skill7: 'Skill 7',
  skill8: 'Skill 8',
  talk: 'Talk, or enter a portal',
  inventory: 'Inventory',
  character: 'Character',
  skills: 'Skills',
  party: 'Party',
  quests: 'Quest log',
  map: 'Map',
  arena: 'Arena',
  zoomIn: 'Zoom in',
  zoomOut: 'Zoom out',
  chat: 'Chat',
  close: 'Close the top window',
};

const groups: readonly { readonly name: string; readonly actions: readonly Action[] }[] = [
  { name: 'Movement', actions: ['up', 'down', 'left', 'right', 'jump', 'walk'] },
  {
    name: 'Combat',
    actions: [
      'attack',
      'nextTarget',
      'hpPotion',
      'skill1',
      'skill2',
      'skill3',
      'skill4',
      'skill5',
      'skill6',
      'skill7',
      'skill8',
    ],
  },
  { name: 'World', actions: ['talk'] },
  {
    name: 'Windows',
    actions: [
      'inventory',
      'character',
      'skills',
      'party',
      'quests',
      'map',
      'arena',
      'zoomIn',
      'zoomOut',
    ],
  },
  { name: 'Fixed', actions: ['chat', 'close'] },
];

/** Why a reserved key can't be bound. */
function refusal(code: string): string {
  if (code === 'Escape' || code === 'Enter' || code === 'NumpadEnter') {
    return 'Enter and Esc stay on the chat and closing windows.';
  }
  if (code === 'MetaLeft' || code === 'MetaRight' || code === 'ContextMenu') {
    return 'The system keeps that key.';
  }
  return 'The browser keeps F5, F11 and F12.';
}

export interface KeyBindingsProps {
  readonly rest: Pick<RestClient, 'putSettings'>;
  readonly onBack: () => void;
}

/** Whether an action takes a Ctrl key, which the browser may read with the next key pressed. */
const ctrlBound = (table: KeyTable) =>
  Object.values(table).some((codes) =>
    codes.some((c) => c === 'ControlLeft' || c === 'ControlRight'),
  );

export function KeyBindings({ rest, onBack }: KeyBindingsProps) {
  const keys = useKeys();
  const printed = keyboardLayout.use((s) => s.printed);
  const changed = settings.use((s) => Object.keys(s.keys).length > 0);
  const [listening, setListening] = useState<Action | null>(null);
  const [line, setLine] = useState<string | null>(null);
  const [refused, setRefused] = useState<{ readonly action: Action; readonly n: number } | null>(
    null,
  );
  const [confirming, setConfirming] = useState(false);
  const [problem, setProblem] = useState<string | null>(null);
  const bound = useRef<string | null>(null); // the key just bound, whose key up the game mustn't see
  const queue = useMemo(
    () =>
      saveQueue(
        () => () =>
          saveSettings(rest).then((p) => {
            setProblem(p);
          }),
        KEYS_SAVE_DELAY_MS,
      ),
    [rest],
  );
  useEffect(() => {
    window.addEventListener('pagehide', queue.flush);
    return () => {
      window.removeEventListener('pagehide', queue.flush);
      queue.flush(); // leaving the window saves a change still waiting
    };
  }, [queue]);

  useEffect(() => {
    const up = (e: KeyboardEvent) => {
      if (e.code === bound.current) {
        bound.current = null;
        e.preventDefault();
        e.stopImmediatePropagation();
      }
    };
    window.addEventListener('keyup', up, { capture: true });
    return () => window.removeEventListener('keyup', up, { capture: true });
  }, []);

  useEffect(() => {
    if (refused === null) {
      return undefined;
    }
    const t = setTimeout(() => setRefused(null), REFUSED_MS);
    return () => clearTimeout(t);
  }, [refused]);

  // While listening, the next key is the window's alone: taken before the game or the modal sees it.
  useEffect(() => {
    if (listening === null) {
      return undefined;
    }
    const down = (e: KeyboardEvent) => {
      e.preventDefault();
      e.stopImmediatePropagation();
      if (e.repeat) {
        return;
      }
      if (e.code === 'Escape') {
        setListening(null);
        return;
      }
      if (reservedKeys.has(e.code)) {
        setLine(refusal(e.code));
        setRefused((r) => ({ action: listening, n: (r?.n ?? 0) + 1 }));
        return;
      }
      setListening(null);
      setRefused(null);
      bound.current = e.code;
      const old = keys.table[listening][0] ?? '';
      if (old === e.code) {
        setLine(null);
        return;
      }
      const next = rebind(settings.getState().keys, listening, e.code);
      settings.getState().setKeys(next.changes);
      queue.changed();
      const now = `${keyName(e.code, printed)} is now ${names[listening]}`;
      setLine(
        next.moved ? `${now}; ${names[next.moved]} moved to ${keyName(old, printed)}.` : `${now}.`,
      );
    };
    const up = (e: KeyboardEvent) => {
      e.preventDefault();
      e.stopImmediatePropagation();
    };
    const away = (e: PointerEvent) => {
      if (!(e.target instanceof Element) || !e.target.closest(`[data-action="${listening}"]`)) {
        setListening(null);
      }
    };
    window.addEventListener('keydown', down, { capture: true });
    window.addEventListener('keyup', up, { capture: true });
    window.addEventListener('pointerdown', away, { capture: true });
    return () => {
      window.removeEventListener('keydown', down, { capture: true });
      window.removeEventListener('keyup', up, { capture: true });
      window.removeEventListener('pointerdown', away, { capture: true });
    };
  }, [listening, keys.table, printed, queue]);

  const reset = () => {
    setConfirming(false);
    settings.getState().setKeys({});
    queue.changed();
    setLine('Every key is back on its default.');
  };

  return (
    <div className="key-bindings">
      <h3>Key bindings</h3>
      <p className="muted">Click a key, then press the one you want. Esc cancels.</p>
      <div className="key-groups">
        {groups.map((g) => (
          <section key={g.name} aria-label={g.name}>
            <h4>{g.name}</h4>
            <ul className="key-rows">
              {g.actions.map((a) => {
                const fixed = fixedActions.has(a);
                const on = listening === a;
                const shown = fixed
                  ? keys.table[a].map((code) => keyName(code, printed)).join(' or ')
                  : keys.name(a);
                return (
                  <li key={a} className={`key-row${fixed ? ' key-row-fixed' : ''}`}>
                    <span>{names[a]}</span>
                    <button
                      type="button"
                      data-action={a}
                      className={`key-chip${on ? ' key-chip-listening' : ''}${refused?.action === a ? ' key-chip-refused' : ''}`}
                      aria-label={`${names[a]}: ${on ? 'press a key' : shown}`}
                      aria-pressed={on}
                      disabled={fixed}
                      onClick={() => {
                        setLine(null);
                        setListening(on ? null : a);
                      }}
                    >
                      {on ? 'Press a key' : shown}
                    </button>
                  </li>
                );
              })}
            </ul>
          </section>
        ))}
      </div>
      {ctrlBound(keys.table) && (
        <p className="muted key-note">Ctrl with other keys can trigger browser shortcuts.</p> // Chrome keeps Ctrl+W, T and N
      )}
      {line && (
        <p className="key-line" role="status">
          {line}
        </p>
      )}
      {problem && (
        <p className="error" role="alert">
          {problem}
        </p>
      )}
      <div className="key-actions">
        {confirming ? (
          <>
            <span>Put every key back on its default?</span>
            <Button variant="danger" onClick={reset}>
              Reset
            </Button>
            <Button variant="quiet" onClick={() => setConfirming(false)}>
              Cancel
            </Button>
          </>
        ) : (
          <>
            <Button variant="quiet" disabled={!changed} onClick={() => setConfirming(true)}>
              Reset to default
            </Button>
            <Button variant="quiet" onClick={onBack}>
              Back
            </Button>
          </>
        )}
      </div>
    </div>
  );
}
