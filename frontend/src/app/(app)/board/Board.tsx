"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useEffect, useState } from "react";
import { TopBar } from "@/components/TopBar";
import { api, currentOrg, isOrgAdmin } from "@/lib/api";
import { useMe } from "@/lib/me";
import type { WorkspaceSummary } from "@/lib/types";

/** /board opens the workspace you used last (or the first one). With none, admins are sent to set
 * one up and members see the no-workspace page (SPEC "Empty and error states"). */
export function Board() {
  const { me } = useMe();
  const router = useRouter();
  const org = currentOrg(me);
  const admin = isOrgAdmin(org?.role);
  const [empty, setEmpty] = useState(false);

  useEffect(() => {
    if (!org) {
      router.replace(me.user.is_hub_admin ? "/admin" : "/new-org");
      return;
    }
    api<{ workspaces: WorkspaceSummary[] }>("GET", `/orgs/${org.id}/workspaces`).then((r) => {
      let last = "";
      try { last = localStorage.getItem("canopy-last-ws") ?? ""; } catch { /* storage blocked */ }
      const pick = r.workspaces.find((w) => w.id === last) ?? r.workspaces[0];
      if (pick) router.replace(`/w/${pick.id}`);
      else if (admin) setEmpty(true);
      else router.replace("/no-workspace");
    }, () => setEmpty(true));
  }, [org, admin, router, me.user.is_hub_admin]);

  if (!empty) return <div className="loading" role="status">Opening your workspace…</div>;
  return (
    <div id="app">
      <TopBar />
      <main className="center-page">
        <div className="empty card">
          <h1>No workspaces yet</h1>
          <p className="muted">A workspace holds your agents, goals and the repositories and folders they can use. Set one up to see it here as a live tree.</p>
          <Link className="btn btn-primary" href="/setup">Set up a workspace</Link>
        </div>
      </main>
    </div>
  );
}
