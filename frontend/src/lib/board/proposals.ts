"use client";

import { useEffect } from "react";
import { api, ApiError } from "../api";
import type { StaffProposalView } from "../types";
import { useBoard } from "./store";
import { useUI } from "./ui";

export type DraftEdit = Partial<Pick<StaffProposalView, "name" | "role" | "instructions" | "skills">>;

export const pendingDrafts = (list: StaffProposalView[]) => list.filter((p) => p.status === "pending").sort((a, b) => a.created_at.localeCompare(b.created_at));

/** useStaffDrafts loads the workspace's drafts for admins (the hub only lists them to admins);
 * everyone else gets drafts as they arrive over the socket. */
export function useStaffDrafts(wsId: string, admin: boolean) {
  const connected = useBoard((s) => s.connected);
  useEffect(() => {
    if (!admin || !connected) return;
    api<{ proposals: StaffProposalView[] }>("GET", `/workspaces/${wsId}/staff-proposals?status=pending`)
      .then((r) => useBoard.getState().setProposals(r.proposals), () => {});
  }, [wsId, admin, connected]);
}

async function decide(wsId: string, id: string, action: "approve" | "reject", body: Record<string, unknown>, done: string) {
  const ui = useUI.getState();
  try {
    await api("POST", `/workspaces/${wsId}/staff-proposals/${id}/${action}`, body);
    ui.toast(done, "ok");
    return true;
  } catch (e) {
    ui.toast(e instanceof ApiError ? e.message : "Something went wrong. Try again.", "error");
    return false;
  }
}

export const approveDraft = (wsId: string, id: string, edit: DraftEdit) => decide(wsId, id, "approve", edit, "Draft approved.");
export const rejectDraft = (wsId: string, id: string, reason: string) => decide(wsId, id, "reject", { reason }, "Draft rejected.");
