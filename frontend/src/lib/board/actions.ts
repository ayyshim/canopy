"use client";

// Things people do on the board. Each calls the hub; the board updates from the realtime
// events that follow, so these only report failures (and a few confirmations).
import { api, ApiError } from "../api";
import type { ChatMessageView, GoalView, LogLineView, NoteView, TaskView } from "../types";
import { useBoard, type Comment } from "./store";
import { useUI } from "./ui";

const toast = (text: string, kind?: "ok" | "info" | "warn" | "error") => useUI.getState().toast(text, kind);

/** run calls the hub and shows its error message as a toast. Returns undefined on failure. */
async function run<T>(p: Promise<T>, done?: string): Promise<T | undefined> {
  try {
    const out = await p;
    if (done) toast(done, "ok");
    return out;
  } catch (e) {
    toast(e instanceof ApiError ? e.message : "Something went wrong. Try again.", "error");
    return undefined;
  }
}

export const resolveSignal = (id: string, action: string, extra: Record<string, unknown> = {}) =>
  run(api("POST", `/signals/${id}/resolve`, { action, ...extra }));

export type GoalBody = { manager_id: string; title: string; description?: string; done_when?: string; runner_id?: string; base?: string; snapshot?: boolean; draft?: boolean };

export async function giveGoal(wsId: string, body: GoalBody) {
  return api<{ goal?: GoalView; queued?: boolean; reason?: string }>("POST", `/workspaces/${wsId}/goals`, body);
}

export const saveDraft = (goalId: string, body: GoalBody) => api<{ goal: GoalView }>("PUT", `/goals/${goalId}/draft`, body);
export const giveDraft = (goalId: string) => api<{ goal: GoalView }>("POST", `/goals/${goalId}/give`);
export const deleteDraft = (goalId: string) => run(api("DELETE", `/goals/${goalId}`), "Draft deleted.");

export async function createTask(wsId: string, goalId: string | undefined, body: { title: string; description?: string; member_id: string; depends_on?: string[]; steps?: string[] }) {
  return api<{ task: TaskView }>("POST", goalId ? `/goals/${goalId}/tasks` : `/workspaces/${wsId}/tasks`, body);
}

export type ShipResult = { status: "done" | "conflicts" | "dirty"; message: string; pr_urls?: string[] };

export async function shipGoal(goalId: string, action: "pr" | "merge" | "keep") {
  const res = await run(api<ShipResult>("POST", `/goals/${goalId}/ship`, { action }));
  if (!res) return;
  toast(res.message, res.status === "done" ? "ok" : "warn");
  return res;
}

export const markGoalDone = (goalId: string) => run(api("POST", `/goals/${goalId}/done`), "Goal marked done. Its tasks are hidden from the board.");
export const moveGoal = (goalId: string, runnerId: string, name: string) =>
  run(api("POST", `/goals/${goalId}/move`, { runner_id: runnerId }), `Moved to ${name}. Its sessions restart there from the goal’s notes and progress.`);
export const showGoalTasks = (goalId: string, show: boolean) => run(api("PATCH", `/goals/${goalId}`, { show_tasks: show }));

export async function loadChat(wsId: string, memberId: string) {
  const res = await run(api<{ messages: ChatMessageView[] }>("GET", `/workspaces/${wsId}/members/${memberId}/chat`));
  if (res) useBoard.getState().setChat(memberId, res.messages);
}

export async function sendChat(wsId: string, memberId: string, text: string, asGoal = false) {
  return run(api<{ message?: ChatMessageView; queued?: boolean; reason?: string }>("POST", `/workspaces/${wsId}/members/${memberId}/chat`, { text, as_goal: asGoal }));
}

export async function markChatRead(wsId: string, memberId: string) {
  await api("POST", `/workspaces/${wsId}/members/${memberId}/chat/read`).catch(() => {});
  const b = useBoard.getState().board;
  if (b) useBoard.setState({ board: { ...b, unread: { ...b.unread, [memberId]: 0 } } });
}

export async function loadActivity(wsId: string, memberId: string) {
  const res = await run(api<{ log: LogLineView[] }>("GET", `/workspaces/${wsId}/members/${memberId}/activity`));
  if (res) useBoard.getState().setLog(memberId, (res.log ?? []).slice().reverse());
}

export async function loadTask(taskId: string) {
  const res = await run(api<{ comments: Comment[] }>("GET", `/tasks/${taskId}`));
  if (res) useBoard.getState().setComments(taskId, res.comments ?? []);
}

export const postComment = (taskId: string, text: string) => run(api("POST", `/tasks/${taskId}/comments`, { text }));

export async function markSeen(outputId: string, version: number) {
  await api("POST", `/outputs/${outputId}/seen`).catch(() => {});
  const b = useBoard.getState().board;
  if (b) useBoard.setState({ board: { ...b, seen: { ...b.seen, [outputId]: Math.max(version, b.seen[outputId] ?? 0) } } });
}

export const patchMember = (wsId: string, memberId: string, body: Record<string, unknown>) =>
  run(api("PATCH", `/workspaces/${wsId}/members/${memberId}`, body));
export const putGrants = (wsId: string, memberId: string, grants: { directory_id: string; mode: string }[], allDirectories?: boolean) =>
  run(api("PUT", `/workspaces/${wsId}/members/${memberId}/grants`, { grants, all_directories: allDirectories }), "Directory access saved.");
export const suggestGrants = (wsId: string, memberId: string) =>
  run(api<{ grants: { directory_id: string; mode: string; reason: string }[] }>("POST", `/workspaces/${wsId}/members/${memberId}/suggest-grants`));
export const patchWorkspace = (wsId: string, body: Record<string, unknown>) => run(api("PATCH", `/workspaces/${wsId}`, body));

export const saveNote = (wsId: string, body: { title: string; body: string; kind: string }, id?: string) =>
  run(id ? api<{ note: NoteView }>("PATCH", `/notes/${id}`, body) : api<{ note: NoteView }>("POST", `/workspaces/${wsId}/notes`, body), "Note saved.");
export const deleteNote = (id: string) => run(api("DELETE", `/notes/${id}`), "Note deleted.");

export const setDirAccess = (wsId: string, dirId: string, userId: string, mode: string) =>
  run(api("PUT", `/workspaces/${wsId}/directories/${dirId}/access/${userId}`, { mode }));
