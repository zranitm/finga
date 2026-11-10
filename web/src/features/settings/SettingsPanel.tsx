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
  classic: 'Classic: arrows, Space, X attack',
  wasd: 'WASD: W A S D, Space, J attack',
};

/** The account's saved settings win over the local ones once they arrive; until then, the cached preset and no auto-assign stay in force. */
export async function loadSettings(rest: Pick<RestClient, 'getSettings'>): Promise<void> {
  try {
    const saved = await rest.getSettings();
    settings.getState().setPreset(saved.control_preset);
    settings.getState().setAutoAssignPoints(saved.auto_assign_points ?? false);
  } catch {
    // Kept as they are until the next try.
  }
}

export function SettingsPanel({ rest, onClose }: SettingsPanelProps) {
  const preset = settings.use((s) => s.preset);
  const autoAssign = settings.use((s) => s.autoAssignPoints);
  const [problem, setProblem] = useState<string | null>(null);

  useEffect(() => {
    void loadSettings(rest);
  }, [rest]);

  // A PUT replaces every setting, so it sends them all as they now stand.
  const save = async () => {
    const s = settings.getState();
    setProblem(null);
    try {
      await rest.putSettings({ control_preset: s.preset, auto_assign_points: s.autoAssignPoints });
    } catch (err) {
      setProblem(err instanceof RestError ? err.message : 'Could not save the setting.');
    }
  };

  const choose = (next: Preset) => {
    settings.getState().setPreset(next);
    void save();
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
            onChange={() => choose(p)}
          />
          {labels[p]}
        </label>
      ))}
      <h2>Stat points</h2>
      <label className="field">
        <input
          type="checkbox"
          checked={autoAssign}
          onChange={(e) => {
            settings.getState().setAutoAssignPoints(e.target.checked);
            void save();
          }}
        />
        Auto-assign at every level-up, by the class’s split
      </label>
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
