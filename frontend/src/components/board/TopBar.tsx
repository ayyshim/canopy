"use client";

import Link from "next/link";
import { useEffect, useRef, useState } from "react";
import { LogoMark } from "@/components/LogoMark";
import { api, currentOrg, hardNavigate, initials, isOrgAdmin } from "@/lib/api";
import { isAdmin, needsMe, payload, plural, unseen } from "@/lib/board/derive";
import { useBoard } from "@/lib/board/store";
import { useUI } from "@/lib/board/ui";
import { useMe } from "@/lib/me";
import { setTheme, useTheme, type Theme } from "@/lib/theme";
import type { components } from "@/lib/api-types";
import type { WorkspaceSummary } from "@/lib/types";
import { useB, useBoardCtx } from "./context";
import { Avatar, Brand, Chevron, DotsIcon, GridIcon, LogoutIcon } from "./icons";

type ClaudeUsage = components["schemas"]["ClaudeComputerUsage"];
type ClaudeWindow = components["schemas"]["ClaudeWindow"];
const win = (c: ClaudeUsage, key: string): ClaudeWindow | undefined => c.windows.find((w) => w.key === key);

/** useMenu opens one menu at a time and closes it on Escape or an outside click. */
function useMenu() {
  const [open, setOpen] = useState<string | null>(null);
  const ref = useRef<HTMLElement>(null);
  useEffect(() => {
    if (!open) return;
    const key = (e: KeyboardEvent) => e.key === "Escape" && setOpen(null);
    const click = (e: MouseEvent) => !(e.target as HTMLElement).closest(".mw") && setOpen(null);
    document.addEventListener("keydown", key);
    document.addEventListener("mousedown", click);
    return () => { document.removeEventListener("keydown", key); document.removeEventListener("mousedown", click); };
  }, [open]);
  return { open, toggle: (id: string) => setOpen((o) => (o === id ? null : id)), close: () => setOpen(null), ref };
}

const resetIn = (iso?: string) => {
  if (!iso) return "";
  const mins = Math.max(0, Math.round((new Date(iso).getTime() - Date.now()) / 60000));
  return mins >= 60 ? `${Math.floor(mins / 60)}h ${mins % 60}m` : `${mins}m`;
};
const ago = (iso: string) => {
  const mins = Math.max(0, Math.round((Date.now() - new Date(iso).getTime()) / 60000));
  return mins < 1 ? "just now" : mins < 60 ? `${mins}m ago` : `${Math.floor(mins / 60)}h ago`;
};
const col = (p: number) => (p >= 90 ? "var(--red)" : p >= 70 ? "var(--amber)" : "var(--green)");

function Meter({ label, pct, sub }: { label: string; pct: number; sub: string }) {
  return (
    <div className="stack-sm" style={{ gap: 5 }}>
      <div className="row"><b className="spacer" style={{ fontSize: 13.5 }}>{label}</b><span className="hint tnum">{Math.round(pct)}% used</span></div>
      <div className={`meter ${pct >= 90 ? "red" : pct >= 70 ? "amber" : ""}`} style={{ height: 10, borderRadius: 5 }}><i style={{ width: `${Math.min(100, pct)}%` }} /></div>
      <span className="hint">{sub}</span>
    </div>
  );
}

export function TopBar() {
  const b = useB();
  const { me: meState } = useMe();
  const { me, wsId } = useBoardCtx();
  const presence = useBoard((s) => s.presence);
  const ui = useUI();
  const menu = useMenu();
  const org = currentOrg(meState);
  const orgAdmin = isOrgAdmin(org?.role);
  const [workspaces, setWorkspaces] = useState<WorkspaceSummary[]>([]);
  const [computers, setComputers] = useState<ClaudeUsage[] | null>(null);
  const theme = useTheme();
  const need = needsMe(b, me);
  const [walk, setWalk] = useState(0);

  useEffect(() => {
    if (!org) return;
    api<{ workspaces: WorkspaceSummary[] }>("GET", `/orgs/${org.id}/workspaces`).then((r) => setWorkspaces(r.workspaces), () => {});
  }, [org]);

  // Claude Code usage per connected computer. Refetch (debounced) when the board reports
  // an account/rate-limit change over the socket.
  const orgId = org?.id;
  useEffect(() => {
    if (!orgId) return;
    const t = setTimeout(() => {
      api<{ computers: ClaudeUsage[] }>("GET", `/orgs/${orgId}/usage/claude`).then((r) => setComputers(r.computers), () => {});
    }, 300);
    return () => clearTimeout(t);
  }, [orgId, b.accounts, b.signals]);

  // Walk the items that need me: center the next one and open what it needs.
  const go = (step: number) => {
    if (!need.length) return;
    const i = (((walk + step) % need.length) + need.length) % need.length;
    setWalk(i);
    const s = need[i];
    if (s.type === "review") return ui.openViewer(payload(s).output_id);
    if (s.type === "goal_ready" && s.goal_id) { ui.openSide({ type: "goal", id: s.goal_id }); return ui.centerOn(s.goal_id); }
    if (s.task_id) ui.openSide({ type: "task", id: s.task_id });
    if (s.type === "usage_limit" || s.type === "cap") return ui.centerOn("acct_" + s.id);
    if (!s.task_id) return ui.centerOn(s.id);
    ui.centerOn(s.type === "question" || s.type === "gate" || s.type === "error" || s.type === "dir_access" || s.type === "owner_change" ? s.id : s.task_id);
  };
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.target as HTMLElement).closest("input, textarea, select, [contenteditable]") || e.metaKey || e.ctrlKey || e.altKey) return;
      if (e.key === "]") go(1);
      else if (e.key === "[") go(-1);
      else if (e.key === "g") ui.openDialog({ kind: "goal" });
      else if (e.key === "n") ui.openDialog({ kind: "task" });
      else if (e.key === "o") ui.setTray(!ui.tray);
      else if (e.key === "?") ui.openDialog({ kind: "shortcuts" });
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  });

  const logout = async () => {
    await api("POST", "/auth/logout").catch(() => {});
    hardNavigate("/login");
  };
  const reported = (computers ?? []).filter((c) => c.reported);
  const sp = Math.max(0, ...reported.map((c) => win(c, "five_hour")?.used_percent ?? 0));
  const hasData = reported.length > 0;
  const lastUpdated = reported.map((c) => c.reported_at ?? "").sort().pop();
  const shown = presence.slice(0, 4);
  const newOutputs = unseen(b).length;
  const roleLabel = org?.role === "owner" ? "Owner" : org?.role === "admin" ? "Admin" : "Member";

  return (
    <header className="topbar">
      <Link className="brand" href="/board" aria-label="Canopy board"><Brand /><b className="hide-s">Canopy</b></Link>
      <div className="mw">
        <button className="wsbtn" onClick={() => menu.toggle("org")} aria-haspopup="menu" aria-expanded={menu.open === "org"}>
          <LogoMark name={org?.name ?? ""} url={org?.logo_url} style={{ background: "var(--ink)", color: "var(--paper)" }} />
          <span className="wsn hide-s">{org?.name}</span><Chevron />
        </button>
        {menu.open === "org" && (
          <div className="menu" style={{ left: 0, width: 290 }} role="menu">
            <div className="ml">Your organizations</div>
            {meState.organizations.map((o) => (
              <button key={o.id} className="mi" role="menuitemradio" aria-checked={o.id === org?.id}
                onClick={async () => { if (o.id !== org?.id) { await api("PATCH", "/me", { last_org_id: o.id }); hardNavigate("/board"); } menu.close(); }}>
                <LogoMark name={o.name} url={o.logo_url} style={{ background: o.id === org?.id ? "var(--ink)" : "#8E5BD9", color: o.id === org?.id ? "var(--paper)" : "#fff" }} />
                <span className="spacer"><b>{o.name}</b><br /><small className="hint">{o.role === "owner" ? "Owner" : o.role === "admin" ? "Admin" : "Member"}</small></span>
                {o.id === org?.id && <span aria-hidden="true">✓</span>}
              </button>
            ))}
            <hr />
            {orgAdmin && <><Link className="mi" role="menuitem" href="/org/general">Organization settings</Link><Link className="mi" role="menuitem" href="/org/usage">Usage and analytics</Link></>}
            <Link className="mi" role="menuitem" href="/new-org">+ Create organization</Link>
          </div>
        )}
      </div>
      <div className="mw">
        <button className="wsbtn" onClick={() => menu.toggle("ws")} aria-haspopup="menu" aria-expanded={menu.open === "ws"}>
          <LogoMark name={b.workspace.name} url={b.workspace.logo_url} /><span className="wsn">{b.workspace.name}</span><Chevron />
        </button>
        {menu.open === "ws" && (
          <div className="menu" style={{ left: 0, width: 290 }} role="menu">
            <div className="row" style={{ padding: "8px 12px 6px" }}>
              <span className="spacer"><span className="hint" style={{ fontWeight: 600 }}>Organization</span><br /><b>{org?.name}</b> <span className="hint">· {roleLabel}, {workspaces.length} workspace{workspaces.length === 1 ? "" : "s"}</span></span>
              {orgAdmin && <Link className="btn btn-sm" href="/org/general">Settings</Link>}
            </div>
            <hr />
            <div className="ml">Workspaces</div>
            {workspaces.map((w) => (
              <Link key={w.id} className="mi" role="menuitemradio" aria-checked={w.id === wsId} href={`/w/${w.id}`} onClick={menu.close}>
                <LogoMark name={w.name} url={w.logo_url} />
                <span className="spacer"><b>{w.name}</b><br /><small className="hint">{plural(w.members, "member")}, {plural(w.people, "person", "people")}</small></span>
                {w.id === wsId && <span aria-hidden="true">✓</span>}
              </Link>
            ))}
            <hr />
            {isAdmin(b) && <Link className="mi" role="menuitem" href={`/w/${wsId}/settings`}>Workspace settings</Link>}
            {orgAdmin && <Link className="mi" role="menuitem" href="/setup">+ New workspace</Link>}
          </div>
        )}
      </div>
      <span className="grow" />
      <div className={"needpill" + (need.length ? "" : " none")} role="group" aria-label="Items that need you">
        <button className="a" onClick={() => go(-1)} aria-label="Previous item">‹</button>
        <button className="count" onClick={() => go(0)}>{need.length} need{need.length === 1 ? "s" : ""} you</button>
        <button className="a" onClick={() => go(1)} aria-label="Next item">›</button>
      </div>
      <div className="mw hide-sm">
        <button className="usage-btn" onClick={() => menu.toggle("usage")} aria-haspopup="dialog" aria-expanded={menu.open === "usage"}
          aria-label={hasData ? `Claude Code usage: current session ${Math.round(sp)} percent` : "Claude Code usage: no data yet"}>
          <span className="ub-row"><b>Session</b><span className="tr"><i style={{ width: `${Math.min(100, sp)}%`, background: col(sp) }} /></span><span>{hasData ? `${Math.round(sp)}%` : "–"}</span></span>
        </button>
        {menu.open === "usage" && (
          <div className="menu" style={{ right: 0, width: 340, padding: "14px 16px" }} role="dialog" aria-label="Claude Code usage">
            <div className="row"><b className="spacer" style={{ fontSize: 14 }}>Claude Code usage</b>{lastUpdated && <span className="hint">Updated {ago(lastUpdated)}</span>}</div>
            {!computers?.length ? (
              <p className="hint" style={{ margin: "8px 0 4px" }}>No computer connected yet. <Link className="linkbtn" href="/org/computers" onClick={menu.close}>Connect a computer</Link></p>
            ) : (
              <div className="stack-sm" style={{ gap: 16, marginTop: 10 }}>
                {computers.map((c) => {
                  const five = win(c, "five_hour"), week = win(c, "seven_day");
                  return (
                    <div key={c.runner_id} className="stack-sm" style={{ gap: 10 }}>
                      <div className="row"><b className="spacer">{c.name}</b><span className="hint">{c.online ? "Online" : "Offline"}</span></div>
                      {!c.reported || (!five && !week) ? (
                        <span className="hint">{c.sign_in === "api_key" ? "Signed in with an API key, so there are no plan limits to show." : "No data yet. It appears after Claude Code runs on this computer."}</span>
                      ) : (<>
                        {five && <Meter label="Current session" pct={five.used_percent} sub={five.resets_at ? `Resets in ${resetIn(five.resets_at)}` : ""} />}
                        {week && <Meter label="This week" pct={week.used_percent} sub={week.resets_at ? `Resets ${new Date(week.resets_at).toLocaleString([], { weekday: "long", hour: "2-digit", minute: "2-digit" })}` : ""} />}
                      </>)}
                    </div>
                  );
                })}
              </div>
            )}
          </div>
        )}
      </div>
      <button className="presence hide-sm" onClick={() => ui.openSide({ type: "people" })} aria-label={`${plural(presence.length, "person", "people")} on this board. Open people and access`}>
        {shown.map((p) => <span key={p.id} className="pav on" style={{ background: p.avatar_color }} title={p.name + (p.id === me ? " (you)" : "")}>{initials(p.name)}</span>)}
        {presence.length > shown.length && <span className="pav more">+{presence.length - shown.length}</span>}
      </button>
      <button className="btn out-btn" onClick={() => ui.setTray(!ui.tray)} aria-expanded={ui.tray}><GridIcon /><span className="hide-s">Outputs</span>{newOutputs > 0 && <span className="bdg">{newOutputs}</span>}</button>
      <button className="btn btn-primary" onClick={() => ui.openDialog({ kind: "goal" })}><span aria-hidden="true">+</span><span className="hide-s">Give a goal</span></button>
      <div className="mw">
        <button className="me-btn" onClick={() => menu.toggle("me")} aria-label="Your account" aria-expanded={menu.open === "me"}><Avatar name={meState.user.name} color={meState.user.avatar_color} size={28} /></button>
        {menu.open === "me" && (
          <div className="menu" style={{ right: 0 }} role="menu">
            <div style={{ padding: "8px 12px 6px" }}><b>{meState.user.name}</b><br /><span className="hint">{meState.user.email}</span></div>
            <hr />
            <Link className="mi" role="menuitem" href="/account">Account and notifications</Link>
            <Link className="mi" role="menuitem" href="/org/computers">Your computers</Link>
            <Link className="mi" role="menuitem" href={orgAdmin ? "/org/usage" : `/org/usage/${me}`}>{orgAdmin ? "Usage and analytics" : "Your usage"}</Link>
            {orgAdmin && <Link className="mi" role="menuitem" href="/org/general">Organization settings</Link>}
            {isAdmin(b) && <Link className="mi" role="menuitem" href={`/w/${wsId}/settings`}>Workspace settings</Link>}
            {meState.user.is_hub_admin && <Link className="mi" role="menuitem" href="/admin">Hub admin</Link>}
            <button className="mi" role="menuitem" onClick={() => { menu.close(); ui.openDialog({ kind: "shortcuts" }); }}>Keyboard shortcuts</button>
            <hr />
            <button className="mi" role="menuitem" onClick={logout}>Sign out</button>
          </div>
        )}
      </div>
      <button className="btn btn-ghost btn-sm hide-sm" onClick={logout} title="Log out"><LogoutIcon /><span className="sr">Log out</span></button>
      <div className="mw">
        <button className="icon-btn" onClick={() => menu.toggle("more")} aria-label="Menu" aria-expanded={menu.open === "more"}><DotsIcon /></button>
        {menu.open === "more" && (
          <div className="menu" style={{ right: 0 }} role="menu">
            {isAdmin(b) && <Link className="mi" role="menuitem" href={`/w/${wsId}/settings`}>Workspace settings</Link>}
            <button className="mi" role="menuitem" onClick={() => { menu.close(); ui.openDialog({ kind: "task" }); }}>New task</button>
            <button className="mi" role="menuitem" onClick={() => { menu.close(); ui.openSide({ type: "people" }); }}>People and access</button>
            {isAdmin(b) && <button className="mi" role="menuitem" onClick={() => { menu.close(); ui.openDialog({ kind: "member", mode: "manager" }); }}>Add a manager</button>}
            <button className="mi" role="menuitem" onClick={() => { menu.close(); ui.openSide({ type: "dirs" }); }}>Directories and repositories</button>
            <button className="mi" role="menuitem" onClick={() => { menu.close(); ui.openSide({ type: "notes" }); }}>Notes and decisions</button>
            <button className="mi" role="menuitem" onClick={() => { menu.close(); ui.openDialog({ kind: "shortcuts" }); }}>Keyboard shortcuts</button>
            <hr />
            <div className="theme" role="group" aria-label="Theme">
              {(["system", "light", "dark"] as Theme[]).map((t) => <button key={t} aria-pressed={theme === t} onClick={() => setTheme(t)}>{t[0].toUpperCase() + t.slice(1)}</button>)}
            </div>
            {orgAdmin && <><hr /><Link className="mi" role="menuitem" href="/setup">Set up another workspace</Link></>}
          </div>
        )}
      </div>
    </header>
  );
}
