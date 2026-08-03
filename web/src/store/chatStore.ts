import { create } from 'zustand';
import type { Channel } from '../lib/types';

interface ChatState {
  activeChannel: Channel | null;
  setActiveChannel: (channel: Channel | null) => void;
}

export const useChatStore = create<ChatState>((set) => ({
  activeChannel: null,
  setActiveChannel: (channel) => set({ activeChannel: channel }),
}));
