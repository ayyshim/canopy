"use client";

import Link from "next/link";
import { usePathname, useSearchParams } from "next/navigation";
import { useEffect, useState } from "react";
import { Brand } from "@/components/board/icons";
import { api, currentOrg, hardNavigate, isOrgAdmin } from "@/lib/api";
import { useMe } from "@/lib/me";
import type { WorkspaceSummary } from "@/lib/types";

/** The workspace the person last opened, for the workspace part of the settings nav. */
export function useLastWorkspace(): WorkspaceSummary | null {
  const { me } = useMe();
  const org = currentOrg(me);
  const [ws, setWs] = useState<WorkspaceSummary | null>(null);
  useEffect(() => {
    if (!org) return;
    api<{ workspaces: WorkspaceSummary[] }>("GET", `/orgs/${org.id}/workspaces`).then((r) => {
      let last = "";
      try { last = localStorage.getItem("canopy-last-ws") ?? ""; } catch { /* storage blocked */ }
      setWs(r.workspaces.find((w) => w.id === last) ?? r.workspaces[0] ?? null);
    }, () => {});
  }, [org]);
  return ws;
}

/** PageShell is the layout of every page beside the board: a top bar, section nav and main column. */
export function PageShell({ title, children, workspace }: { title: string; children: React.ReactNode; workspace?: WorkspaceSummary | null }) {
  const { me } = useMe();
  const path = usePathname();
  const params = useSearchParams();
  const org = currentOrg(me);
  const orgAdmin = isOrgAdmin(org?.role);
  const last = useLastWorkspace();
  const ws = workspace ?? last;
  const wsAdmin = !!ws && (orgAdmin || ws.role === "admin");
  const link = (href: string, label: string, current?: boolean) => {
    const [p, q] = href.split("?");
    const here = current ?? (p === path && (!q || params.toString() === q || (q === "tab=general" && !params.get("tab"))));
    return <Link key={href} href={href} aria-current={here ? "page" : undefined}>{label}</Link>;
  };
  const logout = async () => {
    await api("POST", "/auth/logout").catch(() => {});
    hardNavigate("/login");
  };

  return (
    <div className="page">
      <header className="ptop">
        <Link className="brand" href="/board"><Brand /><b>Canopy</b></Link>
        <span className="crumb">{title}</span>
        <span className="grow" />
        {org && <Link className="btn" href={ws ? `/w/${ws.id}` : "/board"}>Back to the board</Link>}
        <button className="btn btn-ghost hide-sm" onClick={logout}>Log out</button>
      </header>
      <div className="pwrap">
        <nav className="pnav" aria-label="Sections">
          {org && (
            <>
              <h2>{org.name}</h2>
              {orgAdmin ? (
                <>
                  {link("/org/usage", "Usage and analytics", path.startsWith("/org/usage"))}
                  {link("/org/activity", "Activity log")}
                  {link("/org/general", "General")}
                  {link("/org/people", "People")}
                  {link("/org/workspaces", "Workspaces")}
                  {link("/org/computers", "Computers and sign-ins")}
                </>
              ) : (
                <>
                  {link(`/org/usage/${me.user.id}`, "Your usage", path.startsWith("/org/usage"))}
                  {link("/org/computers", "Your computers")}
                </>
              )}
            </>
          )}
          {wsAdmin && ws && (
            <>
              <h2 style={{ marginTop: 18 }}>Workspace: {ws.name}</h2>
              {link(`/w/${ws.id}/settings?tab=general`, "General")}
              {link(`/w/${ws.id}/settings?tab=directories`, "Directories")}
              {link(`/w/${ws.id}/settings?tab=members`, "Members")}
              {link(`/w/${ws.id}/settings?tab=skills`, "Skills")}
              {link(`/w/${ws.id}/settings?tab=access`, "Access")}
              {orgAdmin && link(`/w/${ws.id}/settings?tab=danger`, "Danger zone")}
            </>
          )}
          {me.user.is_hub_admin && (
            <>
              <h2 style={{ marginTop: 18 }}>This hub</h2>
              {link("/admin", "Overview")}
              {link("/admin/organizations", "Organizations", path.startsWith("/admin/organizations"))}
              {link("/admin/users", "People")}
              {link("/admin/settings", "Hub settings")}
            </>
          )}
          <h2 style={{ marginTop: 18 }}>You</h2>
          {link("/account", "Account and notifications")}
          <button className="btn btn-ghost btn-sm show-sm" onClick={logout}>Log out</button>
        </nav>
        <main className="pmain"><div className="page-in">{children}</div></main>
      </div>
    </div>
  );
}

/** Section is a titled card on a settings page. */
export function Section({ title, sub, children, danger }: { title: string; sub?: React.ReactNode; children: React.ReactNode; danger?: boolean }) {
  return (
    <section className={"card panel" + (danger ? " danger" : "")}>
      <div><h2>{title}</h2>{sub && <p className="hint" style={{ marginTop: 2 }}>{sub}</p>}</div>
      {children}
    </section>
  );
}

/** Denied is shown to people who may not open a page. */
export function Denied({ title }: { title: string }) {
  return (
    <>
      <h1>{title}</h1>
      <div className="note-box">Only admins can open this page. Ask an organization admin if you need it.</div>
      <div><Link className="btn" href="/board">Back to the board</Link></div>
    </>
  );
}
