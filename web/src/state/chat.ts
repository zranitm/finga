import type { ChatChannel, Data } from '@/protocol/messages';

import { defineStore } from './store';

export const MAX_LINES = 100;

/** A player's line on the map or to the party, or a notice the game posts in the General tab. */
export type ChatLine = SaidLine | NoticeLine;

export interface SaidLine {
  readonly kind: 'said';
  readonly channel: ChatChannel;
  readonly sender: string;
  readonly senderName: string;
  readonly text: string;
  readonly at: number; // Unix ms
}

export interface NoticeLine {
  readonly kind: 'notice';
  readonly text: string;
  readonly at: number; // Unix ms
}

export interface ChatState {
  readonly lines: readonly ChatLine[];
  applyChatMessage(d: Data<'chatMessage'>, at?: number): void;
  notice(text: string, at?: number): void;
  clear(): void;
}

export const chat = defineStore<ChatState>((set, get) => {
  const add = (line: ChatLine) => {
    const lines = [...get().lines, line];
    set({ lines: lines.length > MAX_LINES ? lines.slice(lines.length - MAX_LINES) : lines });
  };
  return {
    lines: [],
    applyChatMessage: (d, at = Date.now()) => {
      const { channel, sender, senderName, text } = d;
      add({ kind: 'said', channel, sender, senderName, text, at });
    },
    notice: (text, at = Date.now()) => {
      add({ kind: 'notice', text, at });
    },
    clear: () => {
      set({ lines: [] });
    },
  };
});
