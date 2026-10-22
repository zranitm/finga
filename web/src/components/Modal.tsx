import { useEffect, useRef } from 'react';
import type { KeyboardEvent, ReactNode } from 'react';

export interface ModalProps {
  readonly title: string;
  readonly onClose?: () => void; // absent: the modal cannot be dismissed (a blocking notice)
  readonly children: ReactNode;
}

/** A centred dialog over a dimmed backdrop; Escape and a backdrop click close it when allowed. */
export function Modal({ title, onClose, children }: ModalProps) {
  const root = useRef<HTMLDivElement>(null);
  useEffect(() => {
    root.current?.focus();
  }, []);
  const onKeyDown = (e: KeyboardEvent<HTMLDivElement>) => {
    if (e.key === 'Escape' && onClose) {
      e.stopPropagation();
      onClose();
    }
  };
  return (
    <div
      className="modal-backdrop"
      onClick={(e) => {
        if (e.target === e.currentTarget) {
          onClose?.();
        }
      }}
    >
      <div
        ref={root}
        className="modal"
        role="dialog"
        aria-modal="true"
        aria-label={title}
        tabIndex={-1}
        onKeyDown={onKeyDown}
      >
        <h2 className="modal-title">{title}</h2>
        {children}
      </div>
    </div>
  );
}
