// A refused line comes back as a Response with no req; app/wiring shows it as a toast.
import { useEffect, useRef, useState } from 'react';
import type { KeyboardEvent } from 'react';

import type { GameSocket } from '@/net/socket';
import { ChatChannel } from '@/protocol/messages';
import { chat } from '@/state/chat';
import { ui } from '@/state/ui';

export const MAX_CHAT_CHARS = 120;
const SHOWN_LINES = 8;

export interface ChatBoxProps {
  readonly socket: Pick<GameSocket, 'send'>;
}

/** "/p hello" is a party line; anything else is a map line. */
export function parseLine(raw: string): { channel: ChatChannel; text: string } {
  const m = /^\/p(?:\s+|$)(.*)$/s.exec(raw);
  return m
    ? { channel: ChatChannel.PARTY, text: m[1]!.trim() }
    : { channel: ChatChannel.MAP, text: raw.trim() };
}

export function ChatBox({ socket }: ChatBoxProps) {
  const lines = chat.use((s) => s.lines);
  const open = ui.use((s) => s.chatOpen);
  const [draft, setDraft] = useState('');
  const input = useRef<HTMLInputElement>(null);

  useEffect(() => {
    if (open) {
      input.current?.focus();
    }
  }, [open]);

  const { channel, text } = parseLine(draft);
  const over = text.length > MAX_CHAT_CHARS;

  const onKeyDown = (e: KeyboardEvent<HTMLInputElement>) => {
    if (e.key === 'Escape') {
      e.preventDefault();
      input.current?.blur();
      return;
    }
    if (e.key !== 'Enter') {
      return;
    }
    e.preventDefault();
    if (text === '' || over) {
      return;
    }
    socket.send('chatSend', { channel, text });
    setDraft('');
    input.current?.blur();
  };

  return (
    <div className="chat" aria-label="Chat">
      <ul className="chat-lines" aria-live="polite">
        {lines.slice(-SHOWN_LINES).map((l) => (
          <li
            key={`${l.at}-${l.sender}-${l.text}`}
            className={`chat-line chat-${l.channel === ChatChannel.PARTY ? 'party' : 'map'}`}
          >
            <span className="chat-tag">[{l.channel === ChatChannel.PARTY ? 'Party' : 'Map'}]</span>{' '}
            <strong>{l.senderName}:</strong> {l.text}
          </li>
        ))}
      </ul>
      <div className="chat-input-row">
        <input
          ref={input}
          aria-label="Chat message"
          placeholder="Enter to chat; /p for the party"
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
          onKeyDown={onKeyDown}
          onFocus={() => ui.getState().openChat()}
          onBlur={() => ui.getState().closeChat()}
        />
        <span className={`chat-count${over ? ' chat-over' : ''}`} aria-label="Characters">
          {text.length}/{MAX_CHAT_CHARS}
        </span>
      </div>
    </div>
  );
}
