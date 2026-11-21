"use client";

import { RefText } from "../IdTag";
import { RefAssist } from "../RefAssist";
import { useCatalog, skillLabel } from "@/lib/catalog";
import { useEffect, useLayoutEffect, useRef, useState } from "react";
import { loadActivity, loadChat, markChatRead, sendChat } from "@/lib/board/actions";
import { chatLocked, clock, computer, computerFor, fmtTokens, memberStatus, visibleMember } from "@/lib/board/derive";
import { useBoard } from "@/lib/board/store";
import { useUI, type ChatWin } from "@/lib/board/ui";
import { isHidden, type ChatMessageView, type MemberView } from "@/lib/types";
import { useB, useBoardCtx } from "../context";
import { Avatar } from "../icons";
import { ChatSettings } from "./ChatSettings";
import { OutputThumb } from "../OutputThumb";

const ListIcon = () => <svg width="14" height="14" viewBox="0 0 14 14" aria-hidden="true"><path d="M2 3.5h10M2 7h10M2 10.5h6" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" /></svg>;
const FullIcon = () => <svg width="15" height="15" viewBox="0 0 16 16" aria-hidden="true"><path d="M2 6V2h4M14 6V2h-4M2 10v4h4M14 10v4h-4" stroke="currentColor" strokeWidth="1.6" fill="none" /></svg>;
const ExitIcon = () => <svg width="15" height="15" viewBox="0 0 16 16" aria-hidden="true"><path d="M6 2v4H2M10 2v4h4M6 14v-4H2M10 14v-4h4" stroke="currentColor" strokeWidth="1.6" fill="none" /></svg>;

/** greeting is shown in an empty thread, from what the member is doing now. */
function greeting(b: ReturnType<typeof useB>, m: MemberView): string {
  const t = b.tasks.find((x) => x.member_id === m.id && x.status === "doing");
  if (m.kind === "manager") {
    const goals = b.goals.filter((g) => g.manager_id === m.id && g.status !== "done" && g.status !== "draft");
    const staff = b.members.filter((x) => x.reports_to === m.id).map((x) => visibleMember(b, x.id)?.name).filter(Boolean);
    return `Hi, I’m ${m.name}. Give me a goal and I’ll plan it with ${staff.length ? staff.join(" and ") : "independent staff"}. ${goals.length ? `Right now I’m on “${goals[0].title}”.` : "I’m free right now."}`;
  }
  const boss = m.reports_to ? visibleMember(b, m.reports_to)?.name : undefined;
  return `Hi, I’m ${m.name}, ${boss ? `a ${skillLabel(m.skill ?? "")?.toLowerCase()} on ${boss}’s staff` : `independent ${skillLabel(m.skill ?? "")?.toLowerCase()}`}. ${t ? `I’m working on “${t.title}”.` : "I’m waiting for a task."}`;
}

export function ChatWindow({ win, index }: { win: ChatWin; index: number }) {
  useCatalog();
  const b = useB();
  const { wsId, me, send } = useBoardCtx();
  const ui = useUI();
  const msgs = useBoard((s) => s.chats[win.id]);
  const logs = useBoard((s) => s.logs[win.id]);
  const typing = useBoard((s) => s.typing[win.id]);
  const [draft, setDraft] = useState("");
  const [asGoal, setAsGoal] = useState(false);
  const [jump, setJump] = useState(false);
  const [pos, setPos] = useState<{ x: number; y: number } | null>(null);
  const threadRef = useRef<HTMLDivElement>(null);
  const boxRef = useRef<HTMLElement>(null);
  const m = visibleMember(b, win.id);
  const tab = win.tab ?? "chat";

  useEffect(() => {
    loadChat(wsId, win.id);
    markChatRead(wsId, win.id);
  }, [wsId, win.id]);
  useEffect(() => {
    if (tab === "activity") loadActivity(wsId, win.id);
  }, [tab, wsId, win.id]);
  useEffect(() => {
    if (tab === "chat" && msgs?.length) markChatRead(wsId, win.id);
  }, [msgs?.length, tab, wsId, win.id]);
  useLayoutEffect(() => {
    const th = threadRef.current;
    if (th && tab === "chat") th.scrollTop = th.scrollHeight;
  }, [msgs?.length, typing, tab]);
  useEffect(() => {
    document.body.classList.toggle("chat-full", !!win.full);
    return () => document.body.classList.remove("chat-full");
  }, [win.full]);

  // Open beside the clicked node, inside the board.
  const anchor = win.anchor;
  // Refs so other windows opening or closing don't move this one.
  const live = useRef({ index, full: !!win.full });
  useLayoutEffect(() => {
    live.current = { index, full: !!win.full };
  });
  useLayoutEffect(() => {
    const el = boxRef.current, host = el?.offsetParent;
    if (!anchor || !el || !host || live.current.full || innerWidth <= 860) return;
    const h = host.getBoundingClientRect(), gap = 14, off = live.current.index * 30;
    const w = el.offsetWidth, ht = el.offsetHeight;
    let x = anchor.right - h.left + gap;
    if (x + w > h.width - 12) x = anchor.left - h.left - gap - w;
    const y = anchor.top - h.top + off;
    setPos({
      x: Math.max(12, Math.min(x + off, h.width - w - 12)),
      y: Math.max(12, Math.min(y, h.height - ht - 12)),
    });
  }, [anchor]);

  if (!m) return null;
  const st = memberStatus(b, m);
  const boss = m.reports_to ? visibleMember(b, m.reports_to) : undefined;
  const mine = (msgs ?? []).filter((x) => x.kind === "human" && x.author_user_id === me);
  const runsOn = computer(b, computerFor(b, me));

  const submit = async () => {
    const text = draft.trim();
    if (!text) return;
    setDraft("");
    const res = await sendChat(wsId, m.id, text, asGoal);
    if (res?.queued) ui.toast(res.reason ?? "You’re at your usage limit. It runs when your limit resets.", "warn");
    else if (asGoal) ui.toast(`Goal given to ${m.name}.`, "ok");
    setAsGoal(false);
  };
  const keyDown = (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
    if (e.key === "Enter" && !e.shiftKey && !e.nativeEvent.isComposing) { e.preventDefault(); submit(); }
    else if (e.key === "Escape") {
      e.stopPropagation();
      if (jump) setJump(false);
      else if (win.full) ui.updateChat(m.id, { full: false });
      else ui.closeChat(m.id);
    } else send("chat.typing", { member_id: m.id });
  };
  // Drag the window by its header.
  const drag = (e: React.PointerEvent) => {
    if ((e.target as HTMLElement).closest("button") || win.full || innerWidth <= 860 || !boxRef.current) return;
    const el = boxRef.current, sx = e.clientX, sy = e.clientY, l = el.offsetLeft, t = el.offsetTop;
    const move = (ev: PointerEvent) => setPos({ x: l + ev.clientX - sx, y: t + ev.clientY - sy });
    const up = () => { window.removeEventListener("pointermove", move); window.removeEventListener("pointerup", up); };
    window.addEventListener("pointermove", move);
    window.addEventListener("pointerup", up);
  };
  const style: React.CSSProperties = win.full ? {} : pos ? { left: pos.x, top: pos.y } : { right: 16 + index * 30, top: 16 + index * 30 };

  return (
    <section className={"chat" + (win.full ? " full" : "")} ref={boxRef} style={style} aria-label={`Chat with ${m.name}`}>
      {win.full && <MemberList current={m.id} />}
      <div className="chat-head" onPointerDown={drag} onDoubleClick={() => setPos(null)}>
        <Avatar name={m.name} color={m.avatar_color} />
        <div style={{ flex: 1, minWidth: 0 }}>
          <b>{m.name}</b>
          <div className="sub">{m.kind === "manager" ? "Manager" : skillLabel(m.skill ?? "")}{boss ? ", works for " + boss.name : m.kind === "staff" ? ", independent" : ""}</div>
          <div className="stat" style={{ marginTop: 2 }}><span className={`dot ${st.k}`} /> {st.t}</div>
        </div>
        <button className="mine" aria-expanded={jump} onClick={() => setJump(!jump)} title="Jump to a message you sent"><ListIcon /><b>{mine.length}</b></button>
        <button className="icon-btn" aria-pressed={!!win.full} aria-label={win.full ? "Exit full screen" : "Full screen"} title={win.full ? "Exit full screen" : "Full screen"}
          onClick={() => ui.updateChat(m.id, { full: !win.full })}>{win.full ? <ExitIcon /> : <FullIcon />}</button>
        <button className="icon-btn" onClick={() => ui.closeChat(m.id)} aria-label="Close chat">✕</button>
      </div>
      <div className="chat-tabs" role="tablist" aria-label="Chat sections">
        {(["chat", "activity", "settings"] as const).map((t) => (
          <button role="tab" key={t} aria-selected={tab === t} onClick={() => ui.updateChat(m.id, { tab: t })}>{t[0].toUpperCase() + t.slice(1)}</button>
        ))}
      </div>
      {chatLocked(m, me) ? (
        <div className="thread"><p className="hint" style={{ padding: 16 }}>🔒 {m.name} is isolated. Only the person who added them can chat.</p></div>
      ) : <>
      <div className="thread" ref={threadRef} aria-live="polite" role="log" aria-label={`Conversation with ${m.name}`}>
        {tab === "chat" && <Thread m={m} msgs={msgs ?? []} typing={!!typing} jump={jump} mine={mine} greet={greeting(b, m)} />}
        {tab === "activity" && (
          <>
            <div className="term">
              {b.sessions.filter((z) => z.member_id === m.id).map((z) => (
                <div key={z.id}><span className="sys2">Session {z.number} · {z.status} on {computer(b, z.runner_id)?.name ?? "—"}{z.goal_id ? ` for “${b.goals.find((g) => g.id === z.goal_id)?.title ?? ""}”` : ""} · {fmtTokens(z.tokens_in + z.tokens_out)} tokens{z.recovered_from ? " · Recovered" : ""}</span></div>
              ))}
              {!b.sessions.some((z) => z.member_id === m.id) && <span className="sys2">No session yet. It starts when this member has work.</span>}
              {(logs ?? []).slice(-80).map((l) => (
                <div key={l.id}><span className="sys2">{clock(l.at)}</span> <span className={l.kind === "human" ? "you" : l.kind === "error" ? "err" : ""}><RefText text={l.text} /></span></div>
              ))}
            </div>
            {b.outputs.some((o) => o.member_id === m.id) && (
              <>
                <span className="lbl" style={{ marginTop: 10 }}>Outputs</span>
                <div className="thumbs">{b.outputs.filter((o) => o.member_id === m.id).map((o) => <button key={o.id} className="fruit" onClick={() => ui.openViewer(o.id)} title={o.title}><OutputThumb o={o} /></button>)}</div>
              </>
            )}
          </>
        )}
        {tab === "settings" && <ChatSettings m={m} />}
      </div>
      {tab === "chat" && (
        <div className={"composer" + (asGoal ? " goal" : "")}>
          {!draft && (
            <div className="sugg" aria-label="Suggestions">
              {(m.kind === "manager" ? ["How is it going?", "What’s blocking you?", "Summarize today"] : ["What are you working on?", "Anything blocking you?"]).map((s) => (
                <button key={s} onClick={() => sendChat(wsId, m.id, s)}>{s}</button>
              ))}
            </div>
          )}
          <div className="cbox">
            <RefAssist><textarea rows={1} value={draft} onChange={(e) => setDraft(e.target.value)} onKeyDown={keyDown}
              placeholder={asGoal ? `Describe the goal for ${m.name}…` : `Message ${m.name}…`} aria-label={`Message to ${m.name}`}
              style={{ height: Math.min(120, 22 + 20 * (draft.split("\n").length - 1)) }} /></RefAssist>
            <button className="sendb" onClick={submit} aria-label={asGoal ? "Give goal" : "Send message"} disabled={!draft.trim()}>
              <svg width="16" height="16" viewBox="0 0 16 16" aria-hidden="true"><path d="M8 13V3M3.5 7.5 8 3l4.5 4.5" stroke="currentColor" strokeWidth="1.9" fill="none" strokeLinecap="round" strokeLinejoin="round" /></svg>
            </button>
          </div>
          <div className="cfoot">
            {m.kind === "manager" ? (
              <label className="gswitch"><input type="checkbox" checked={asGoal} onChange={(e) => setAsGoal(e.target.checked)} /> Send as a new goal</label>
            ) : <span>Usually you’d go through {boss?.name ?? "a manager"}</span>}
            <span>Enter to send, Shift+Enter for a new line · runs on {runsOn?.name ?? "no computer yet"}</span>
          </div>
        </div>
      )}
      </>}
    </section>
  );
}

function Thread({ m, msgs, typing, jump, mine, greet }: { m: MemberView; msgs: ChatMessageView[]; typing: boolean; jump: boolean; mine: ChatMessageView[]; greet: string }) {
  const b = useB();
  const { me } = useBoardCtx();
  const ui = useUI();
  const [flash, setFlash] = useState<string | null>(null);
  type Group = { author: string; you: boolean; name: string; at: string; items: ChatMessageView[] };
  const out: React.ReactNode[] = [];
  let grp: Group | null = null;
  const flush = () => {
    if (!grp) return;
    const g = grp;
    out.push(
      <div className={`grp ${g.you ? "you" : "them"}`} key={"g" + g.items[0].id}>
        <div className="gmeta"><b>{g.name}</b>{clock(g.at)}</div>
        {g.items.map((x) => (
          <div className={"bub" + (flash === x.id ? " flash" : "")} key={x.id} data-mid={x.id}>{x.as_goal && <span className="gtag">GOAL</span>}<RefText text={x.text} /></div>
        ))}
      </div>,
    );
    grp = null;
  };
  for (const x of msgs) {
    if (x.kind === "event") {
      flush();
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      const refs = (x.refs ?? {}) as Record<string, any>;
      const sig = refs.signal_id ? b.signals.find((s) => s.id === refs.signal_id) : undefined;
      const open = () => {
        if (refs.output_id) ui.openViewer(refs.output_id);
        else if (refs.goal_id) ui.openSide({ type: "goal", id: refs.goal_id });
        else if (refs.task_id) ui.openSide({ type: "task", id: refs.task_id });
        else if (sig?.task_id) ui.openSide({ type: "task", id: sig.task_id });
      };
      const action = refs.output_id ? "Open" : refs.goal_id ? "Details" : refs.task_id || sig ? (sig ? "Respond" : "Open") : null;
      out.push(
        <div className={"sys" + (sig ? " warn" : "")} key={x.id}>
          <span className="ic" aria-hidden="true">{refs.signal_id ? "?" : refs.output_id ? "▤" : refs.goal_id ? "◎" : "▸"}</span>
          <span>{x.text}</span>
          {action && <button className="act" onClick={open}>{refs.signal_id && !sig ? "Answered" : action}</button>}
        </div>,
      );
      continue;
    }
    const author = x.kind === "human" ? x.author_user_id ?? "" : m.id;
    const you = x.kind === "human" && x.author_user_id === me;
    const name = x.kind === "human" ? b.people.find((p) => p.id === x.author_user_id)?.name ?? "Someone" : m.name;
    const g = grp as Group | null;
    if (!g || g.author !== author || new Date(x.created_at).getTime() - new Date(g.items[g.items.length - 1].created_at).getTime() > 180000) {
      flush();
      grp = { author, you, name, at: x.created_at, items: [] };
    }
    grp!.items.push(x);
  }
  flush();
  return (
    <>
      {!msgs.length && <div className="grp them"><div className="gmeta"><b>{m.name}</b></div><div className="bub">{greet}</div></div>}
      {out}
      {typing && <div className="typing" aria-label={`${m.name} is typing`}><i /><i /><i /></div>}
      {jump && (
        <div className="jump" aria-label="Your messages">
          {mine.length ? mine.slice().reverse().map((x) => (
            <button key={x.id} onClick={() => {
              document.querySelector(`[data-mid="${x.id}"]`)?.scrollIntoView({ behavior: "smooth", block: "center" });
              setFlash(x.id);
              setTimeout(() => setFlash(null), 1600);
            }}>
              {x.as_goal && <span className="gtag">GOAL</span>} {x.text.slice(0, 70)}<br /><span className="muted">{clock(x.created_at)}</span>
            </button>
          )) : <p className="muted" style={{ padding: 8 }}>You haven’t sent anything yet.</p>}
        </div>
      )}
    </>
  );
}

/** MemberList is the left column of the full-screen chat. */
function MemberList({ current }: { current: string }) {
  useCatalog();
  const b = useB();
  const ui = useUI();
  const ms = b.members.filter((x) => !isHidden(x)) as MemberView[];
  const row = (x: MemberView) => {
    const st = memberStatus(b, x);
    const un = b.unread[x.id] ?? 0;
    return (
      <li key={x.id}>
        <button aria-current={x.id === current} onClick={() => { if (x.id !== current) { ui.closeChat(current); ui.openChat(x.id, { full: true }); } }}>
          <Avatar name={x.name} color={x.avatar_color} size={28} />
          <span style={{ minWidth: 0 }}><span className="nm">{x.name}</span><span className="rl"><span className={`dot ${st.k}`} style={{ marginRight: 5 }} />{x.kind === "manager" ? "Manager" : skillLabel(x.skill ?? "")}</span></span>
          {un > 0 && <span className="ub">{un}</span>}
        </button>
      </li>
    );
  };
  return (
    <aside className="cside" aria-label="Members">
      <h4>Managers</h4><ul>{ms.filter((x) => x.kind === "manager").map(row)}</ul>
      <h4>Staff</h4><ul>{ms.filter((x) => x.kind !== "manager").map(row)}</ul>
    </aside>
  );
}

/** Chats is every open chat window. */
export function Chats() {
  const chats = useUI((s) => s.chats);
  return <>{chats.map((c, i) => <ChatWindow key={c.id} win={c} index={chats.length - 1 - i} />)}</>;
}
