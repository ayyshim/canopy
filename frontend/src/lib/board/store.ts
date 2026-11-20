"use client";

import { create } from "zustand";
import type {
  AccountView, AnyMember, Board, ChatMessageView, Cursor, GoalRepoView, GrantView, LogLineView, NoteView, OutputView,
  PresenceUser, SessionView, SignalView, SkillView,
} from "../types";

export type Comment = { id: string; task_id: string; author_user_id?: string | null; author_member_id?: string | null; text: string; created_at: string };

type BoardState = {
  board: Board | null;
  connected: boolean;
  presence: PresenceUser[];
  cursors: Record<string, Cursor>;
  typing: Record<string, { name: string; at: number }>; // member id → who's typing to it
  chats: Record<string, ChatMessageView[]>; // member id → loaded thread
  logs: Record<string, LogLineView[]>; // member id → loaded activity, newest last
  comments: Record<string, Comment[]>; // task id → loaded comments
  gone: "" | "deleted" | "lost"; // the workspace went away, or the viewer lost access

  setSnapshot: (b: Board, presence: PresenceUser[]) => void;
  apply: (type: string, data: unknown) => void;
  setConnected: (v: boolean) => void;
  setChat: (memberId: string, msgs: ChatMessageView[]) => void;
  setLog: (memberId: string, lines: LogLineView[]) => void;
  setComments: (taskId: string, list: Comment[]) => void;
  reset: () => void;
};

/** upsert replaces the item with the same key or adds it. */
function upsert<T>(list: T[], item: T, key: (x: T) => string): T[] {
  const k = key(item);
  const i = list.findIndex((x) => key(x) === k);
  if (i < 0) return [...list, item];
  const next = list.slice();
  next[i] = item;
  return next;
}

const byId = <T extends { id: string }>(x: T) => x.id;
const sessionKey = (s: SessionView) => `${s.member_id}|${s.goal_id ?? ""}|${s.person_id ?? ""}`;
const accountKey = (a: AccountView) => `${a.runner_id}|${a.cli}`;
const repoKey = (r: GoalRepoView) => `${r.goal_id}|${r.directory_id}`;

const empty = { board: null, connected: false, presence: [], cursors: {}, typing: {}, chats: {}, logs: {}, comments: {}, gone: "" as const };

export const useBoard = create<BoardState>((set) => ({
  ...empty,
  setSnapshot: (board, presence) => set({ board, presence, gone: "" }),
  setConnected: (connected) => set({ connected }),
  setChat: (memberId, msgs) => set((s) => ({ chats: { ...s.chats, [memberId]: msgs } })),
  setLog: (memberId, lines) => set((s) => ({ logs: { ...s.logs, [memberId]: lines } })),
  setComments: (taskId, list) => set((s) => ({ comments: { ...s.comments, [taskId]: list } })),
  reset: () => set(empty),
  apply: (type, data) => set((s) => applyEvent(s, type, data)),
}));

/** applyEvent folds one realtime event into the board (SPEC "Board WebSocket"). */
function applyEvent(s: BoardState, type: string, data: unknown): Partial<BoardState> {
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  const d = data as any;
  const b = s.board;
  switch (type) {
    case "presence.joined":
      return { presence: upsert(s.presence, d as PresenceUser, byId) };
    case "presence.left": {
      const cursors = { ...s.cursors };
      delete cursors[d.id];
      return { presence: s.presence.filter((p) => p.id !== d.id), cursors };
    }
    case "presence.cursor":
      return { cursors: { ...s.cursors, [d.user_id]: { ...d, at: Date.now() } } };
    case "chat.typing":
      return { typing: { ...s.typing, [d.member_id]: { name: d.name, at: Date.now() } } };
    case "workspace.deleted":
      return { gone: "deleted" };
    case "access.lost":
      return { gone: "lost" };
  }
  if (!b) return {};
  const nb = (patch: Partial<Board>): Partial<BoardState> => ({ board: { ...b, ...patch } });
  switch (type) {
    case "workspace.updated":
      return nb({ workspace: d });
    case "task.created":
    case "task.updated":
      return nb({ tasks: upsert(b.tasks, d, byId) });
    case "task.deleted":
      return nb({ tasks: b.tasks.filter((t) => t.id !== d.id) });
    case "goal.updated":
      return nb({ goals: upsert(b.goals, d, byId) });
    case "goal.deleted":
      return nb({ goals: b.goals.filter((g) => g.id !== d.id), goal_events: b.goal_events.filter((e) => e.goal_id !== d.id) });
    case "attachment.created":
      return nb({ attachments: upsert(b.attachments ?? [], d, byId) });
    case "attachment.deleted":
      return nb({ attachments: (b.attachments ?? []).filter((a) => a.id !== d.id) });
    case "goal.event":
      return nb({ goal_events: [...b.goal_events, d] });
    case "goal.repo":
      return nb({ goal_repos: upsert(b.goal_repos, d as GoalRepoView, repoKey) });
    case "signal.opened":
    case "signal.updated":
      return nb({ signals: upsert(b.signals, d as SignalView, byId) });
    case "signal.resolved":
      return nb({ signals: b.signals.filter((x) => x.id !== d.id) });
    case "session.updated":
      return nb({ sessions: upsert(b.sessions, d as SessionView, sessionKey) });
    case "session.log": {
      const l = d as LogLineView;
      const id = l.member_id ?? "";
      if (!s.logs[id]) return {};
      return { logs: { ...s.logs, [id]: [...s.logs[id], l].slice(-500) } };
    }
    case "chat.message": {
      const m = d as ChatMessageView;
      const open = s.chats[m.member_id];
      const typing = { ...s.typing };
      if (m.kind === "agent") delete typing[m.member_id];
      return {
        chats: open ? { ...s.chats, [m.member_id]: upsert(open, m, byId) } : s.chats,
        typing,
        board: m.kind === "event" ? b : { ...b, unread: { ...b.unread, [m.member_id]: (b.unread[m.member_id] ?? 0) + (m.kind === "agent" ? 1 : 0) } },
      };
    }
    case "task.comment": {
      const c = d as Comment;
      const list = s.comments[c.task_id];
      return list ? { comments: { ...s.comments, [c.task_id]: upsert(list, c, byId) } } : {};
    }
    case "output.created":
    case "output.version":
      return nb({ outputs: upsert(b.outputs, d as OutputView, byId) });
    case "member.updated": {
      const m = d.member as AnyMember;
      let grants = b.grants;
      if (Array.isArray(d.grants)) grants = [...b.grants.filter((g) => g.member_id !== m.id), ...(d.grants as GrantView[])];
      return nb({ members: upsert(b.members, m, byId), grants });
    }
    case "member.deleted":
      return nb({ members: b.members.filter((m) => m.id !== d.id), grants: b.grants.filter((g) => g.member_id !== d.id) });
    case "directory.updated": {
      const old = b.directories.find((x) => x.id === d.id);
      return nb({ directories: upsert(b.directories, { owner_name: "", your_mode: "read", ...old, ...d }, byId) });
    }
    case "directory.deleted":
      return nb({ directories: b.directories.filter((x) => x.id !== d.id) });
    case "directory.clone":
      return nb({ clones: upsert(b.clones, d, (c) => `${c.directory_id}|${c.runner_id}`) });
    case "directory.synced":
      return nb({ mirrors: { ...b.mirrors, [d.directory_id]: { ...b.mirrors[d.directory_id], ...d } } });
    case "note.saved":
      return nb({ notes: upsert(b.notes, d as NoteView, byId) });
    case "note.deleted":
      return nb({ notes: b.notes.filter((n) => n.id !== d.id) });
    case "skill.saved":
      return nb({ skills: upsert(b.skills ?? [], d as SkillView, byId) });
    case "skill.deleted": {
      const member_skills = Object.fromEntries(Object.entries(b.member_skills ?? {}).map(([k, v]) => [k, v.filter((id) => id !== d.id)]));
      return nb({ skills: (b.skills ?? []).filter((x) => x.id !== d.id), member_skills });
    }
    case "member.skills":
      return nb({
        skills: (d.skills as SkillView[]).reduce((list, x) => upsert(list, x, byId), b.skills ?? []),
        member_skills: { ...b.member_skills, [d.member_id]: (d.skills as SkillView[]).map((x) => x.id) },
      });
    case "account.updated":
      return nb({ accounts: upsert(b.accounts, d as AccountView, accountKey) });
    case "runner.updated": {
      if (d.revoked) return nb({ runners: b.runners.filter((r) => r.id !== d.id) });
      const old = b.runners.find((r) => r.id === d.id);
      return nb({ runners: upsert(b.runners, { ...old, ...d }, byId) });
    }
  }
  return {};
}
