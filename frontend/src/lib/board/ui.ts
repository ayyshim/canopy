"use client";

import { create } from "zustand";

export type Side = { type: "ws" | "goal" | "task" | "people" | "pool" | "dirs" | "notes"; id?: string };
export type ChatWin = { id: string; full?: boolean; min?: boolean; tab?: "chat" | "activity" | "settings" };
export type Dialog =
  | { kind: "goal"; managerId?: string }
  | { kind: "task"; goalId?: string; memberId?: string }
  | { kind: "member"; mode: "manager" | "staff" | "pool"; bossId?: string; memberId?: string }
  | { kind: "invite" }
  | { kind: "access"; userId: string }
  | { kind: "summary" }
  | { kind: "cap"; signalId: string }
  | { kind: "shortcuts" };
export type Toast = { id: number; text: string; kind: "ok" | "info" | "warn" | "error" };

type UIState = {
  side: Side | null;
  nav: Side[]; // panels to go back to
  chats: ChatWin[];
  tray: boolean;
  viewer: { outputId: string; version?: number } | null;
  dialog: Dialog | null;
  toasts: Toast[];
  glow: string | null; // a node briefly highlighted by "Show on the tree"
  focus: { id: string; at: number } | null; // the canvas centers on this node when it changes
  centerOn: (id: string) => void;

  openSide: (s: Side | null) => void;
  back: () => void;
  openChat: (id: string, opts?: Partial<ChatWin>) => void;
  updateChat: (id: string, patch: Partial<ChatWin>) => void;
  closeChat: (id: string) => void;
  setTray: (v: boolean) => void;
  openViewer: (outputId: string, version?: number) => void;
  closeViewer: () => void;
  openDialog: (d: Dialog | null) => void;
  toast: (text: string, kind?: Toast["kind"]) => void;
  dropToast: (id: number) => void;
  setGlow: (id: string | null) => void;
};

const same = (a: Side | null, b: Side | null) => !!a && !!b && a.type === b.type && a.id === b.id;
let toastSeq = 0;

export const useUI = create<UIState>((set, get) => ({
  side: null,
  nav: [],
  chats: [],
  tray: false,
  viewer: null,
  dialog: null,
  toasts: [],
  glow: null,
  focus: null,
  centerOn: (id) => set({ focus: { id, at: Date.now() } }),

  openSide: (s) =>
    set((st) => {
      if (!s) return { side: null, nav: [] };
      if (same(st.side, s)) return {};
      return { side: s, nav: st.side ? [...st.nav, st.side].slice(-10) : st.nav };
    }),
  back: () =>
    set((st) => {
      const nav = st.nav.slice();
      const prev = nav.pop() ?? null;
      return { side: prev, nav };
    }),
  openChat: (id, opts) =>
    set((st) => {
      const others = st.chats.filter((c) => c.id !== id);
      const existing = st.chats.find((c) => c.id === id);
      const win = { ...existing, id, min: false, ...opts };
      return { chats: [...others.slice(-2), win] };
    }),
  updateChat: (id, patch) => set((st) => ({ chats: st.chats.map((c) => (c.id === id ? { ...c, ...patch } : c)) })),
  closeChat: (id) => set((st) => ({ chats: st.chats.filter((c) => c.id !== id) })),
  setTray: (tray) => set({ tray }),
  openViewer: (outputId, version) => set({ viewer: { outputId, version } }),
  closeViewer: () => set({ viewer: null }),
  openDialog: (dialog) => set({ dialog }),
  toast: (text, kind = "ok") => {
    const id = ++toastSeq;
    set((st) => ({ toasts: [...st.toasts, { id, text, kind }].slice(-4) }));
    setTimeout(() => get().dropToast(id), kind === "error" ? 7000 : 4200);
  },
  dropToast: (id) => set((st) => ({ toasts: st.toasts.filter((t) => t.id !== id) })),
  setGlow: (glow) => {
    set({ glow });
    if (glow) setTimeout(() => get().glow === glow && set({ glow: null }), 1800);
  },
}));
