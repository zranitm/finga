import { useEffect, useState } from 'react';

import { Button } from '@/components/Button';
import { loadSettings, saveSettings } from '@/features/shared/settings';
import type { RestClient } from '@/net/rest';
import { presets, settings } from '@/state/settings';
import type { Preset } from '@/state/settings';

export interface SettingsPanelProps {
  readonly rest: RestClient;
  readonly onClose: () => void;
}

const labels: Record<Preset, string> = {
  classic: 'Classic: arrows, Space, X attack, Z talk',
  wasd: 'WASD: W A S D, Space, J attack, E talk',
};

export function SettingsPanel({ rest, onClose }: SettingsPanelProps) {
  const preset = settings.use((s) => s.preset);
  const autoAssign = settings.use((s) => s.autoAssignPoints);
  const [problem, setProblem] = useState<string | null>(null);

  useEffect(() => {
    void loadSettings(rest);
  }, [rest]);

  const save = async () => {
    setProblem(null);
    setProblem(await saveSettings(rest));
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
        Auto-assign at every level-up, by the class’s split (a Beginner spends its own)
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
