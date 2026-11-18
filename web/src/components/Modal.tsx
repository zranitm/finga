import { useEffect, useRef } from 'react';
import type { KeyboardEvent as ReactKeyboardEvent, ReactNode } from 'react';

import { openModal } from '@/lib/modals';
import type { OpenModal } from '@/lib/modals';

export interface ModalProps {
  readonly title: string;
  readonly onClose: () => void;
  readonly children: ReactNode;
}

export function Modal({ title, onClose, children }: ModalProps) {
  const root = useRef<HTMLDivElement>(null);
  const close = useRef(onClose);
  const modal = useRef<OpenModal | null>(null);
  useEffect(() => {
    close.current = onClose;
  });
  useEffect(() => {
    root.current?.focus();
  }, []);
  // Esc on the window too: a panel swapped inside drops the focus to the page. The game takes no keys meanwhile.
  useEffect(() => {
    const open = openModal();
    modal.current = open;
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape' && open.newest()) {
        e.preventDefault();
        close.current();
      }
    };
    window.addEventListener('keydown', onKeyDown);
    return () => {
      window.removeEventListener('keydown', onKeyDown);
      open.close();
    };
  }, []);
  // Esc with the focus inside stops here, so a window the modal sits in, and the window listener, don't close too.
  const onKeyDown = (e: ReactKeyboardEvent<HTMLDivElement>) => {
    if (e.key === 'Escape') {
      e.stopPropagation();
      if (modal.current?.newest()) {
        close.current();
      }
    }
  };
  return (
    <div
      className="modal-backdrop"
      onClick={(e) => {
        if (e.target === e.currentTarget) {
          onClose();
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
