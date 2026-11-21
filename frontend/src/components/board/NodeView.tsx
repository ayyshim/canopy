"use client";

import { UsageBadge } from "../ui";
import { useCatalog, skillLabel } from "@/lib/catalog";
import { resolveSignal } from "@/lib/board/actions";
import {
  account, blockReason, chatLocked, blockedOnPerson, computer, fmtTokens, goalProgress, goalRepos, isAdmin, memberStatus, needsMe, payload,
  runnerForTask, signInLabel, signalsForTask, task as findTask, whyWaiting, workTasks, firstName, clock, plural,
} from "@/lib/board/derive";
import type { Node } from "@/lib/board/layout";
import { useUI } from "@/lib/board/ui";
import { skillNames } from "@/lib/skills";
import { useB, useBoardCtx } from "./context";
import { Avatar, KIND_LABEL } from "./icons";
import { SignalCard } from "./SignalCard";
import { OutputThumb } from "./OutputThumb";

/** NodeView draws one card on the tree. */
export function NodeView({ n }: { n: Node }) {
  useCatalog();
  const b = useB();
  const { me } = useBoardCtx();
  const ui = useUI();
  const style: React.CSSProperties = { left: n.x, top: n.y, width: n.w, height: n.h };
  const focusGoal = ui.side?.type === "goal" ? ui.side.id : null;
  const dimFor = (taskId?: string) => (focusGoal && taskId && findTask(b, taskId)?.goal_id !== focusGoal ? " dim" : "");
  const glow = ui.glow === n.id ? " glow" : "";
  const keyOpen = (fn: () => void) => (e: React.KeyboardEvent) => (e.key === "Enter" || e.key === " ") && (e.preventDefault(), fn());

  switch (n.type) {
    case "ws": {
      const mg = b.members.filter((m) => m.kind === "manager").length;
      const done = b.tasks.filter((t) => t.status === "done").length;
      const all = Math.max(b.tasks.length, 1);
      const hosts = [...new Set(b.goals.filter((g) => g.status !== "done" && g.status !== "draft").map((g) => g.runner_id).filter(Boolean))] as string[];
      const open = () => ui.openSide({ type: "ws" });
      return (
        <div className={`node team${ui.side?.type === "ws" ? " sel" : ""}${glow}`} style={style} role="button" tabIndex={0} onClick={open} onKeyDown={keyOpen(open)}
          aria-label={`Workspace ${b.workspace.name}`}>
          <h2>{b.workspace.name}</h2>
          <p>{b.workspace.about}</p>
          <div className="bar">
            <span className="tr"><i style={{ width: `${(done / all) * 100}%` }} /></span>
            <span>{mg} manager{mg === 1 ? "" : "s"}, {b.members.length - mg} staff, {b.goals.length} goal{b.goals.length === 1 ? "" : "s"}</span>
          </div>
          <div className="use">
            {hosts.slice(0, 3).map((id) => {
              const r = computer(b, id);
              const why = whyWaiting(b, id);
              const a = account(b, id);
              if (a?.mode === "api_key" && a.cap_tokens_per_day && !why) {
                const pc = Math.min(1, a.used_tokens / a.cap_tokens_per_day);
                return (
                  <span className="u" key={id}><span className={`dot ${pc >= 0.8 ? "need" : "done"}`} />{r?.name}: {Math.round(pc * 100)}% of cap
                    <span className="mb"><i style={{ width: `${pc * 100}%` }} /></span></span>
                );
              }
              return <span className="u" key={id}><span className={`dot ${why ? "need" : "done"}`} />{r?.name ?? "No computer"}: {why ? why[0].toLowerCase() + why.slice(1) : "available"}</span>;
            })}
          </div>
        </div>
      );
    }
    case "goal": {
      const g = n.g;
      const m = b.members.find((x) => x.id === g.manager_id);
      const ts = workTasks(b, g.id);
      const dn = ts.filter((t) => t.status === "done").length;
      const sel = focusGoal === g.id;
      const hasRepos = goalRepos(b, g.id).length > 0;
      const status = g.status === "ready" ? (hasRepos ? "Ready to ship" : "Ready to close") : g.status === "done" ? "Done ✓" : g.status === "planning" ? "Planning" : g.status === "draft" ? "Draft" : `${dn} of ${ts.length}`;
      const open = () => ui.openSide({ type: "goal", id: g.id });
      const mName = m && "name" in m ? m.name : "Manager";
      const mColor = m && "avatar_color" in m ? m.avatar_color : "var(--line)";
      return (
        <div className={`node goal${g.status === "done" ? " done" : g.status === "ready" ? " ready" : g.status === "draft" ? " draft" : ""}${sel ? " sel" : ""}${focusGoal && !sel ? " dim" : ""}${glow}`}
          style={style} role="button" tabIndex={0} onClick={open} onKeyDown={keyOpen(open)}>
          <span className="gt"><span className="gdot" style={{ background: g.color }} /><span>{g.title}</span></span>
          <span className="gb">
            <span className="gm"><i style={{ background: mColor }}>{mName.slice(0, 1)}</i>{mName}</span>
            <span className="tr"><i style={{ width: `${goalProgress(b, g)}%`, background: g.color }} /></span>
            <span>{status}</span>
            <UsageBadge usage={g.usage} compact />
          </span>
        </div>
      );
    }
    case "addgoal":
      return <button className="node addgoal" style={style} onClick={() => ui.openDialog({ kind: "goal" })}>+ Give a goal</button>;
    case "note": {
      const done = b.goals.filter((g) => g.status === "done").length;
      return (
        <button className="node note" style={style} onClick={() => ui.openDialog({ kind: "summary" })}>
          <b>Workspace summary</b>{done} of {plural(b.goals.length, "goal")} done, {needsMe(b, me).length} waiting on you, {plural(b.outputs.length, "output")}.
        </button>
      );
    }
    case "dirs":
      return (
        <div className="node dircard" style={style} role="button" tabIndex={0} onClick={() => ui.openSide({ type: "dirs" })} onKeyDown={keyOpen(() => ui.openSide({ type: "dirs" }))}>
          <span className="ah">Directories</span>
          {b.directories.length === 0 && <span className="dr muted">None yet</span>}
          {b.directories.map((d) => {
            const r = computer(b, d.machine_id);
            const q = b.mirrors[d.id]?.queued_writes ?? 0;
            return (
              <span className="dr" key={d.id}>
                <span className={`dot ${r?.online ? "done" : "need"}`} />
                <code>{d.name}</code>
                <span className="muted">{firstName(b, d.owner_user_id)}</span>
                <span className="spacer" />
                {d.is_repo ? <span className="pill">git</span> : q ? <span className="pill info">{q} waiting</span> : r?.online ? <span className="pill ok">synced</span> : <span className="pill warn">owner offline</span>}
              </span>
            );
          })}
        </div>
      );
    case "acct": {
      const s = n.s;
      const p = payload(s);
      const r = computer(b, p.runner_id);
      const canAct = r ? r.owner_user_id === me || (r.shared && isAdmin(b)) : false;
      if (s.type === "usage_limit") {
        return (
          <div className="node acct" style={style}>
            <span className="ah">Usage limit</span>
            <b>{signInLabel(b, p.runner_id)} is paused</b>
            <span className="ab">Goals on {p.runner_name} wait until {clock(p.resets_at)}. Everyone else’s keep going.</span>
            {canAct && (
              <span className="bs">
                <button className="mini dark" onClick={() => resolveSignal(s.id, "wait")}>Wait for reset</button>
                <button className="mini" onClick={() => resolveSignal(s.id, "use_key")}>Use an API key</button>
              </span>
            )}
          </div>
        );
      }
      const full = p.level >= 100;
      return (
        <div className="node acct" style={style}>
          <span className="ah">{full ? "Cap reached" : "Nearing the daily cap"}</span>
          <b>{p.runner_name}, {Math.round((p.used / Math.max(1, p.cap)) * 100)}% used</b>
          <span className="ab">{full ? "Goals on it are paused. Raise the cap to continue." : "Goals on it pause when it reaches the cap."}</span>
          {canAct && (
            <span className="bs">
              <button className="mini dark" onClick={() => ui.openDialog({ kind: "cap", signalId: s.id })}>Raise cap</button>
              <button className="mini" onClick={() => resolveSignal(s.id, "keep")}>{full ? "Stay paused" : "Keep cap"}</button>
            </span>
          )}
        </div>
      );
    }
    case "member": {
      const m = n.m;
      if (!m) {
        return (
          <div className={`node agent locked${n.mgr ? " mgr" : ""}`} style={style} aria-label="Not shared with you"
            onClick={() => ui.toast("This part of the tree isn’t shared with you. Ask a workspace admin for access.", "info")}>
            <span className="avatar" style={{ background: "var(--line)" }} />
            <span style={{ minWidth: 0 }}><span className="nm">{n.mgr ? "Manager" : "Staff"}</span><span className="md">Not shared with you</span></span>
          </div>
        );
      }
      const s = memberStatus(b, m);
      const unread = b.unread[m.id] ?? 0;
      const warn = s.k === "need";
      const sel = ui.chats.some((c) => c.id === m.id);
      const locked = chatLocked(m, me);
      const open = (e: React.SyntheticEvent<HTMLElement>) => {
        if (locked) return ui.toast(`${m.name} is isolated. Only the person who added them can chat.`, "info");
        const r = e.currentTarget.getBoundingClientRect();
        ui.openChat(m.id, { anchor: { left: r.left, top: r.top, right: r.right, bottom: r.bottom } });
      };
      return (
        <div className={`node agent${n.mgr ? " mgr" : ""}${warn ? " warn" : ""}${sel ? " sel" : ""}${glow}`} style={style} role="button" tabIndex={0}
          onClick={open} onKeyDown={(e) => (e.key === "Enter" || e.key === " ") && (e.preventDefault(), open(e))} aria-label={`${m.name}, ${s.t}`}>
          <Avatar name={m.name} color={m.avatar_color} />
          <span style={{ minWidth: 0 }}>
            <span className="nm">{m.name}{m.kind === "manager" ? <span className="tag">MANAGER</span> : !m.reports_to ? <span className="tag ind">INDEPENDENT</span> : null}{m.isolated && <span className="tag ind" title="Isolated: only its creator can chat, managers can’t borrow it" aria-label="Isolated">🔒</span>}</span>
            <span className="md" title={skillNames(b.member_skills?.[m.id], b.skills ?? []).join(", ") || undefined}>{m.kind === "staff" && m.skill ? skillLabel(m.skill) + ", " : ""}{m.model}{(b.member_skills?.[m.id]?.length ?? 0) > 0 ? ` · ${b.member_skills[m.id].length} skill${b.member_skills[m.id].length === 1 ? "" : "s"}` : ""}</span>
            <span className={`stat${warn ? " warn" : s.k === "error" ? " err" : ""}`}><span className={`dot ${s.k}`} />{s.t}</span>
          </span>
          {unread > 0 && <span className="ubadge" aria-label={`${unread} new in chat`}>{unread}</span>}
        </div>
      );
    }
    case "pool": {
      const open = () => ui.openSide({ type: "pool" });
      return (
        <div className={`node pool${ui.side?.type === "pool" ? " sel" : ""}`} style={style} role="button" tabIndex={0} onClick={open} onKeyDown={keyOpen(open)}>
          <b>Independent staff</b><span>Helps any manager when needed</span><span>{n.n} member{n.n === 1 ? "" : "s"}</span>
        </div>
      );
    }
    case "addm": {
      const [label, sub] = n.mode === "manager" ? ["+ Add manager", "Takes goals and leads staff"] : n.mode === "pool" ? ["+ Add independent staff", "Helps any manager"] : ["+ Add staff", "Reports to " + n.boss?.name];
      return (
        <button className="node addm" style={style} onClick={() => ui.openDialog({ kind: "member", mode: n.mode, bossId: n.boss?.id })}>
          <span>{label}</span><small>{sub}</small>
        </button>
      );
    }
    case "session": {
      const z = n.s;
      const r = computer(b, z.runner_id);
      const g = b.goals.find((x) => x.id === z.goal_id);
      const why = whyWaiting(b, z.runner_id, n.m.cli);
      const [word, cls] = ({ running: ["Running", "working"], starting: ["Starting", "working"], gate: ["Waiting", ""], review: ["Waiting", ""], waiting: ["Idle", ""], asleep: ["Asleep", ""], crashed: ["Crashed", "error"], error: ["Error", "error"] } as Record<string, [string, string]>)[z.status] ?? [z.status, ""];
      const mins = Math.max(1, Math.round((new Date(z.last_active_at).getTime() - new Date(z.started_at).getTime()) / 60000));
      const working = z.status === "running" || z.status === "starting";
      const what = g ? `“${g.title}”` : z.person_id ? `${firstName(b, z.person_id)} (chat)` : "";
      return (
        <div className={`node sess${z.status === "asleep" ? " dim2" : ""}`} style={{ ...style, borderLeft: `3px solid ${g?.color ?? "var(--term-sub)"}` }} role="button" tabIndex={0}
          onClick={() => ui.openChat(n.m.id, { tab: "activity" })} title={`${n.m.name} for ${what} on ${r?.name ?? "no computer"} with ${signInLabel(b, z.runner_id)}`}>
          <span className="a"><span className={`dot ${why ? "need" : cls}`} /><b>Session {z.number}</b>
            <span className={z.recovered_from && !why ? "rec" : ""}>{why && working ? "Paused" : z.recovered_from ? "Recovered" : word}</span></span>
          <span className="b"><span>{r?.hostname || r?.name || "none"}</span><span>{mins}m · {fmtTokens(z.tokens_in + z.tokens_out)}</span></span>
        </div>
      );
    }
    case "task": {
      const t = n.t;
      const g = b.goals.find((x) => x.id === t.goal_id);
      const open = signalsForTask(b, t.id);
      const q = open.some((s) => s.type === "question");
      const rv = open.some((s) => s.type === "review");
      const oc = open.find((s) => s.type === "owner_change");
      const halted = whyWaiting(b, runnerForTask(b, t));
      const red = t.status === "waiting" && t.depends_on.some((d) => { const x = findTask(b, d); return !!x && x.status !== "done" && blockedOnPerson(b, x); });
      const waits = t.depends_on.filter((d) => { const x = findTask(b, d); return x && x.status !== "done"; }).length;
      let ft: React.ReactNode;
      if (t.status === "done") ft = <span>Done</span>;
      else if (rv || t.status === "review") ft = <span className="blk" style={{ color: "var(--amber-text)" }}>Waiting for your review</span>;
      else if (oc) ft = <span className="blk" style={{ color: "var(--amber-text)" }}>Needs {firstName(b, payload(oc).owner_user_id)}’s approval</span>;
      else if (t.status === "doing" && halted) ft = <><span className="pg"><i style={{ width: `${t.progress}%` }} /></span><span className="blk" style={{ color: "var(--amber-text)" }}>Paused</span></>;
      else if (t.status === "doing") ft = <><span className="pg"><i style={{ width: `${t.progress}%` }} /></span><span>{t.progress}%</span></>;
      else if (t.status === "todo" && halted) ft = <span className="blk" style={{ color: "var(--amber-text)" }}>{halted}</span>;
      else if (red) ft = <span className="blk">{blockReason(b, t)}</span>;
      else if (waits) ft = <span>Waits on {waits}</span>;
      else ft = <span>To do</span>;
      const sel = ui.side?.type === "task" && ui.side.id === t.id;
      const openTask = () => ui.openSide({ type: "task", id: t.id });
      return (
        <div className={`node task ${t.status}${red ? " blocked" : ""}${dimFor(t.id)}${sel ? " sel" : ""}${glow}`} style={{ ...style, ["--gc" as string]: g?.color ?? "transparent" }}
          role="button" tabIndex={0} onClick={openTask} onKeyDown={keyOpen(openTask)} aria-label={`Task ${t.title}`}>
          <span className="tt">{t.title}</span><span className="ft">{ft}<UsageBadge usage={t.usage} compact /></span>
          {q && <span className="pin" aria-label="Open question">?</span>}
        </div>
      );
    }
    case "fruit": {
      const o = n.o;
      const isNew = (b.seen[o.id] ?? 0) < o.versions.length;
      const cls = o.status === "review" ? " review" : o.status === "approved" ? " approved" : isNew ? " new" : "";
      return (
        <button className={`node fruit${cls}${dimFor(n.taskId)}`} style={style} onClick={() => ui.openViewer(o.id)}
          aria-label={`${KIND_LABEL[o.kind] ?? "Output"}: ${o.title}${o.status === "review" ? ", waiting for your review" : ""}`}>
          <OutputThumb o={o} />
        </button>
      );
    }
    case "more":
      return <button className={`node fruit more${dimFor(n.taskId)}`} style={style} onClick={() => ui.setTray(true)} aria-label="Show all outputs for this task">+{n.n}</button>;
    case "sig": {
      const { height: _h, ...rest } = style;
      void _h;
      return <SignalCard s={n.s} style={rest} dim={!!dimFor(n.taskId)} />;
    }
  }
}
