// Drawn by its place among the open windows, so the one Escape closes is the one on top.
import { Window } from '@/components/Window';
import type { WindowProps } from '@/components/Window';
import { ui } from '@/state/ui';
import type { WindowName } from '@/state/ui';

export type WorldWindowProps = Omit<WindowProps, 'layer' | 'onRaise'> & {
  readonly name: WindowName;
};

/** A window over the world, brought to the top when pressed or focused. */
export function WorldWindow({ name, ...props }: WorldWindowProps) {
  const layer = ui.use((s) => s.windows.indexOf(name) + 1);
  return <Window {...props} layer={layer} onRaise={() => ui.getState().openWindow(name)} />;
}
