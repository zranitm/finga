import type { ChatChannel, Data } from '@/protocol/messages';

import { defineStore } from './store';

export const MAX_LINES = 100;

export interface ChatLine {
  readonly channel: ChatChannel;
  readonly sender: string;
  readonly senderName: string;
  readonly text: string;
  readonly at: number; // Unix ms
}

export interface ChatState {
  readonly lines: readonly ChatLine[];
  applyChatMessage(d: Data<'chatMessage'>, at?: number): void;
  clear(): void;
}

export const chat = defineStore<ChatState>((set, get) => ({
  lines: [],
  applyChatMessage: (d, at = Date.now()) => {
    const { channel, sender, senderName, text } = d;
    const lines = [...get().lines, { channel, sender, senderName, text, at }];
    set({ lines: lines.length > MAX_LINES ? lines.slice(lines.length - MAX_LINES) : lines });
  },
  clear: () => {
    set({ lines: [] });
  },
}));
