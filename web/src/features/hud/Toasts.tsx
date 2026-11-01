import { useEffect } from 'react';

import { toasts } from '@/state/toasts';
import type { Toast } from '@/state/toasts';

export const TOAST_MS = 4000;

export function Toasts() {
  const list = toasts.use((s) => s.toasts);
  if (list.length === 0) {
    return null;
  }
  return (
    <div className="toasts" aria-live="polite">
      {list.map((t) => (
        <ToastItem key={t.id} toast={t} />
      ))}
    </div>
  );
}

function ToastItem({ toast }: { readonly toast: Toast }) {
  useEffect(() => {
    const id = setTimeout(() => toasts.getState().dismiss(toast.id), TOAST_MS);
    return () => clearTimeout(id);
  }, [toast.id]);
  return (
    <button
      type="button"
      className={`toast toast-${toast.kind}`}
      onClick={() => toasts.getState().dismiss(toast.id)}
    >
      {toast.text}
    </button>
  );
}
