import { useEffect, useState } from 'react';

import { Button } from '@/components/Button';
import { loadSettings, saveSettings } from '@/features/shared/settings';
import type { RestClient } from '@/net/rest';
import { settings } from '@/state/settings';

import { Credits } from './Credits';
import { KeyBindings } from './KeyBindings';

export interface SettingsPanelProps {
  readonly rest: RestClient;
  readonly onClose: () => void;
}

export function SettingsPanel({ rest, onClose }: SettingsPanelProps) {
  const autoAssign = settings.use((s) => s.autoAssignPoints);
  const minimap = settings.use((s) => s.showMinimap);
  const [problem, setProblem] = useState<string | null>(null);
  const [bindingKeys, setBindingKeys] = useState(false);

  useEffect(() => {
    void loadSettings(rest);
  }, [rest]);

  const save = async () => {
    setProblem(null);
    setProblem(await saveSettings(rest));
  };

  if (bindingKeys) {
    return (
      <div className="panel">
        <KeyBindings rest={rest} onBack={() => setBindingKeys(false)} />
      </div>
    );
  }

  return (
    <div className="panel">
      <h2>Controls</h2>
      <Button variant="quiet" onClick={() => setBindingKeys(true)}>
        Key bindings
      </Button>
      <h2>Stat points</h2>
      <label className="field field-check">
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
      <h2>Map</h2>
      <label className="field field-check">
        <input
          type="checkbox"
          checked={minimap}
          onChange={(e) => {
            settings.getState().setShowMinimap(e.target.checked);
            void save();
          }}
        />
        Show the minimap
      </label>
      <Credits />
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
