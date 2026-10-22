// The last 100 chat lines, map and party, oldest first.
import type { ChatChannel, Data } from '@/protocol/messages';

import { defineStore } from './store';

export const MAX_LINES = 100;

export interface ChatLine {
  readonly channel: ChatChannel;
  readonly sender: string;
  readonly sender_name: string;
  readonly text: string;
  readonly at: number; // Unix milliseconds, when it arrived
}

export interface ChatState {
  readonly lines: readonly ChatLine[];
  applyChatMessage(d: Data<'ChatMessage'>, at?: number): void;
  clear(): void;
}

export const chat = defineStore<ChatState>((set, get) => ({
  lines: [],
  applyChatMessage: (d, at = Date.now()) => {
    const lines = [...get().lines, { ...d, at }];
    set({ lines: lines.length > MAX_LINES ? lines.slice(lines.length - MAX_LINES) : lines });
  },
  clear: () => {
    set({ lines: [] });
  },
}));
