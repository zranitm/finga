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
  readonly heardIn: number; // the region's channel the line was heard in; 0 off regions
}

export interface NoticeLine {
  readonly kind: 'notice';
  readonly text: string;
  readonly at: number; // Unix ms
}

export interface ChatState {
  readonly lines: readonly ChatLine[];
  /** A line heard in heardIn, the region's channel the player was in, 0 off regions. */
  applyChatMessage(d: Data<'chatMessage'>, heardIn: number): void;
  notice(text: string): void;
  clear(): void;
}

export const chat = defineStore<ChatState>((set, get) => {
  const add = (line: ChatLine) => {
    const lines = [...get().lines, line];
    set({ lines: lines.length > MAX_LINES ? lines.slice(lines.length - MAX_LINES) : lines });
  };
  return {
    lines: [],
    applyChatMessage: (d, heardIn) => {
      const { channel, sender, senderName, text } = d;
      add({ kind: 'said', channel, sender, senderName, text, at: Date.now(), heardIn });
    },
    notice: (text) => {
      add({ kind: 'notice', text, at: Date.now() });
    },
    clear: () => {
      set({ lines: [] });
    },
  };
});
