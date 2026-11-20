"use client";

import { useEffect } from "react";
import { goal as findGoal, task as findTask } from "@/lib/board/derive";
import { useUI, type Side } from "@/lib/board/ui";
import type { Board } from "@/lib/types";
import { useB } from "../context";
import { shortTitle } from "./bits";
import { DirsPanel, PoolPanel } from "./SmallPanels";
import { GoalPanel } from "./GoalPanel";
import { NotesPanel } from "./NotesPanel";
import { PeoplePanel } from "./PeoplePanel";
import { TaskPanel } from "./TaskPanel";
import { WorkspacePanel } from "./WorkspacePanel";

function sideLabel(b: Board, s: Side): string {
  switch (s.type) {
    case "goal": return "Goal: " + shortTitle(findGoal(b, s.id)?.title ?? "");
    case "task": return "Task: " + shortTitle(findTask(b, s.id)?.title ?? "");
    case "people": return "People";
    case "pool": return "Independent staff";
    case "dirs": return "Directories";
    case "notes": return "Notes";
  }
  return "Workspace";
}

const exists = (b: Board, s: Side) => (s.type === "goal" ? !!findGoal(b, s.id) : s.type === "task" ? !!findTask(b, s.id) : true);

/** Panel is the details sidebar, with a back button through the panels opened before. */
export function Panel() {
  const b = useB();
  const side = useUI((s) => s.side);
  const nav = useUI((s) => s.nav);
  const back = useUI((s) => s.back);
  const openSide = useUI((s) => s.openSide);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.altKey && e.key === "ArrowLeft") { e.preventDefault(); back(); }
      if (e.key === "Escape" && !document.querySelector(".overlay")) openSide(null);
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [back, openSide]);

  useEffect(() => {
    document.body.classList.toggle("side-open", !!side);
    return () => document.body.classList.remove("side-open");
  }, [side]);

  if (!side || !exists(b, side)) return null;
  const history = nav.filter((s) => exists(b, s));
  const Body = { ws: WorkspacePanel, goal: GoalPanel, task: TaskPanel, people: PeoplePanel, pool: PoolPanel, dirs: DirsPanel, notes: NotesPanel }[side.type];
  return (
    <aside className="sidebar" aria-label="Details" key={side.type + (side.id ?? "")}>
      {history.length > 0 && (
        <div className="pback-wrap">
          <button className="pback" onClick={back} title="Back (Alt+Left)">
            <svg width="14" height="14" viewBox="0 0 14 14" aria-hidden="true"><path d="M8.5 2.5L4 7l4.5 4.5" stroke="currentColor" strokeWidth="1.8" fill="none" strokeLinecap="round" /></svg>
            <span>{sideLabel(b, history[history.length - 1])}</span>
          </button>
          {history.length > 1 && <span className="pback-n">{history.length} back</span>}
        </div>
      )}
      <Body id={side.id ?? ""} />
    </aside>
  );
}

/** Parts every panel uses: a head with a close button, a scrolling body, a footer. */
export function PanelHead({ children }: { children: React.ReactNode }) {
  const openSide = useUI((s) => s.openSide);
  return (
    <div className="sb-head">
      <div className="row" style={{ alignItems: "flex-start" }}>
        <div className="spacer" style={{ minWidth: 0 }}>{children}</div>
        <button className="icon-btn" onClick={() => openSide(null)} aria-label="Close details">✕</button>
      </div>
    </div>
  );
}
export const PanelBody = ({ children }: { children: React.ReactNode }) => <div className="sb-body">{children}</div>;
export const PanelFoot = ({ children }: { children: React.ReactNode }) => <div className="sb-foot">{children}</div>;
