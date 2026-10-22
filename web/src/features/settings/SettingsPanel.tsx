import { useEffect, useState } from 'react';

import { Button } from '@/components/Button';
import { RestError } from '@/net/rest';
import type { RestClient } from '@/net/rest';
import { presets, settings } from '@/state/settings';
import type { Preset } from '@/state/settings';

export interface SettingsPanelProps {
  readonly rest: RestClient;
  readonly onClose: () => void;
}

const labels: Record<Preset, string> = {
  classic: 'Classic: arrows, Space, X attack, Z pick up',
  wasd: 'WASD: W A S D, Space, J attack, K pick up',
};

/** Choose a control preset (B6): cached at once, saved to the account. */
export function SettingsPanel({ rest, onClose }: SettingsPanelProps) {
  const preset = settings.use((s) => s.preset);
  const [problem, setProblem] = useState<string | null>(null);

  // The account's saved preset wins over the local cache once it arrives.
  useEffect(() => {
    let alive = true;
    rest
      .getSettings()
      .then((saved) => {
        if (alive) settings.getState().setPreset(saved.control_preset);
      })
      .catch(() => {
        // The cached preset stays in force until the next try.
      });
    return () => {
      alive = false;
    };
  }, [rest]);

  const choose = async (next: Preset) => {
    settings.getState().setPreset(next); // the cache: the next frame already uses it
    setProblem(null);
    try {
      await rest.putSettings({ control_preset: next });
    } catch (err) {
      setProblem(err instanceof RestError ? err.message : 'Could not save the setting.');
    }
  };

  return (
    <div className="panel" aria-label="Settings">
      <h2>Controls</h2>
      {presets.map((p) => (
        <label key={p} className="field">
          <input
            type="radio"
            name="preset"
            value={p}
            checked={preset === p}
            onChange={() => void choose(p)}
          />
          {labels[p]}
        </label>
      ))}
      {problem && (
        <p className="error" role="alert">
          {problem}
        </p>
      )}
      <Button variant="quiet" onClick={onClose}>
        Done
      </Button>
    </div>
  );
}
