"use client";

import { RefText } from "./IdTag";
import { resolveSignal } from "@/lib/board/actions";
import { directory, firstName, goal as findGoal, payload, task as findTask, personName, visibleMember } from "@/lib/board/derive";
import { useUI } from "@/lib/board/ui";
import { useNow } from "@/lib/useNow";
import type { SignalView } from "@/lib/types";
import { useB, useBoardCtx } from "./context";
import { EyeIcon, LockIcon } from "./icons";

/** Minutes left before a gate counts as Deny. */
function TimeOut({ at }: { at?: string }) {
  const now = useNow(20000);
  if (!at) return null;
  const mins = Math.max(0, Math.ceil((new Date(at).getTime() - now) / 60000));
  return <span className="to tnum">Times out in {mins}m, then counts as Deny</span>;
}

/** SignalCard is a question, approval, error, review or owner decision on the tree. */
export function SignalCard({ s, style, dim }: { s: SignalView; style: React.CSSProperties; dim?: boolean }) {
  const b = useB();
  const { me } = useBoardCtx();
  const ui = useUI();
  const m = visibleMember(b, s.member_id);
  const p = payload(s);
  const cls = (base: string) => `node ${base}${dim ? " dim" : ""}`;
  const act = (a: string, extra?: Record<string, unknown>) => () => resolveSignal(s.id, a, extra);

  switch (s.type) {
    case "question": {
      const mgr = m?.reports_to ? visibleMember(b, m.reports_to) : undefined;
      if (p.delegated) {
        return <div className={cls("fork")} style={style} data-sig={s.id}><div className="fq">{mgr ? mgr.name : "The manager"} is deciding…</div></div>;
      }
      const options: string[] = p.options ?? [];
      const one = options.length > 2 || options.some((o) => o.length > 16);
      return (
        <div className={cls("fork")} style={style} data-sig={s.id}>
          <div className="fq"><RefText text={String(p.question ?? "")} /></div>
          <svg className="flines" viewBox="0 0 220 20" aria-hidden="true">
            <path d={one ? "M110 0 V20" : "M110 0 V8 M52 8 H168 M52 8 V20 M168 8 V20"} stroke="#D08A0E" strokeWidth="1.8" strokeDasharray="4 3" fill="none" />
          </svg>
          <div className={"leaves" + (one ? " one" : "")}>
            {options.map((o, i) => (
              <button key={o} className={"leaf" + (i === p.suggested ? " sugg" : "")} onClick={act("answer", { text: o })}>
                {o}{i === p.suggested && <small>Suggested</small>}
              </button>
            ))}
          </div>
          <div>
            <button className="linkbtn" onClick={() => s.task_id && ui.openSide({ type: "task", id: s.task_id })}>Write your own</button>
            {mgr && p.can_delegate && <button className="linkbtn" onClick={act("manager")}>Let {mgr.name} decide</button>}
          </div>
        </div>
      );
    }
    case "gate":
      return (
        <div className={cls("gate")} style={style} data-sig={s.id}>
          <span className="lock" aria-hidden="true"><LockIcon /></span>
          <span className="h">{m?.name ?? "An agent"} wants to run</span>
          <code title={p.summary}>{p.summary}</code>
          <span className="bs">
            <button className="mini dark" onClick={act("allow")}>Allow once</button>
            <button className="mini" onClick={act("always")} title={`Always allow ${p.action_kind} for ${m?.name ?? "this agent"}`}>Always</button>
            <button className="mini" onClick={act("deny")}>Deny</button>
          </span>
          <TimeOut at={s.expires_at} />
        </div>
      );
    case "dir_access":
      return (
        <div className={cls("gate")} style={style} data-sig={s.id}>
          <span className="lock" aria-hidden="true"><LockIcon /></span>
          <span className="h">{m?.name ?? "An agent"} wants {p.mode === "read_write" ? "to change" : "to read"}</span>
          <code>{p.path}</code>
          <span className="bs">
            <button className="mini dark" onClick={act("grant")}>Grant access</button>
            <button className="mini" onClick={act("deny")}>Deny</button>
          </span>
          <TimeOut at={s.expires_at} />
        </div>
      );
    case "error":
      return (
        <div className={cls("errc")} style={style} data-sig={s.id}>
          <span className="xmark" aria-hidden="true">!</span>
          <span className="h">{m?.name ?? "An agent"} stopped on an error{s.task_id ? "" : " in a chat"}</span>
          <p>{p.message}</p>
          <span className="bs">
            {s.task_id ? (
              <>
                <button className="mini dark" onClick={act("retry")}>Retry</button>
                <button className="mini" onClick={act("skip")}>Skip this step</button>
              </>
            ) : (
              <>
                {m && <button className="mini dark" onClick={() => ui.openChat(m.id)}>Open chat</button>}
                <button className="mini" onClick={act("skip")}>Dismiss</button>
              </>
            )}
          </span>
        </div>
      );
    case "review": {
      const rt = findTask(b, s.task_id);
      const rg = rt?.kind === "handover" ? findGoal(b, rt.goal_id) : undefined;
      if (rg) {
        return (
          <div className={cls("gate")} style={style} data-sig={s.id}>
            <span className="lock" aria-hidden="true"><EyeIcon /></span>
            <span className="h">Review the handover brief for “{rg.title}”</span>
            <p>The goal can’t finish until you approve it.</p>
            <span className="bs"><button className="mini dark" onClick={() => ui.openSide({ type: "goal", id: rg.id })}>Open the goal</button></span>
          </div>
        );
      }
      return (
        <div className={cls("gate")} style={style} data-sig={s.id}>
          <span className="lock" aria-hidden="true"><EyeIcon /></span>
          <span className="h">Review “{p.title}”</span>
          <p>The task waits until you decide.</p>
          <span className="bs"><button className="mini dark" onClick={() => ui.openViewer(p.output_id)}>Open and review</button></span>
        </div>
      );
    }
    case "sync_conflict": {
      const mine = p.owner_user_id === me;
      const d = directory(b, p.directory_id);
      return (
        <div className={cls("gate")} style={style} data-sig={s.id}>
          <span className="lock" aria-hidden="true">!</span>
          <span className="h">Sync conflict in {d?.name ?? "a folder"}</span>
          <code>{p.path}</code>
          <p>You changed this file while it was offline, and an agent changed it too.</p>
          {mine ? (
            <span className="bs">
              <button className="mini dark" onClick={act("keep_owner")}>Keep mine</button>
              <button className="mini" onClick={act("keep_agent")}>Use the agent’s</button>
            </span>
          ) : (
            <span className="to">Only {personName(b, p.owner_user_id)} can resolve it, on their own copy.</span>
          )}
        </div>
      );
    }
    case "owner_change": {
      const mine = p.owner_user_id === me;
      const d = directory(b, p.directory_id);
      return (
        <div className={cls("gate")} style={style} data-sig={s.id}>
          <span className="lock" aria-hidden="true"><LockIcon /></span>
          <span className="h">{mine ? "Your approval" : `${firstName(b, p.owner_user_id)}’s approval`} for {d?.name ?? p.path}</span>
          <p><RefText text={String(p.description ?? "")} /></p>
          {mine ? (
            <span className="bs">
              <button className="mini dark" onClick={act("approve")}>Approve</button>
              <button className="mini" onClick={act("deny")}>Decline</button>
            </span>
          ) : (
            <span className="to">Only {personName(b, p.owner_user_id)} owns this folder. Waiting for them.</span>
          )}
        </div>
      );
    }
  }
  return null;
}
