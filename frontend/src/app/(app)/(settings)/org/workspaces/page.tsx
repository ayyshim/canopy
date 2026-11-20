"use client";

import Link from "next/link";
import { useEffect, useState } from "react";
import { LogoMark } from "@/components/LogoMark";
import { Denied } from "@/components/PageShell";
import { api, currentOrg, isOrgAdmin } from "@/lib/api";
import { useMe } from "@/lib/me";
import type { WorkspaceSummary } from "@/lib/types";

/** /org/workspaces: every workspace in the organization. */
export default function WorkspacesPage() {
  const { me } = useMe();
  const org = currentOrg(me);
  const [list, setList] = useState<WorkspaceSummary[] | null>(null);
  useEffect(() => {
    if (org) api<{ workspaces: WorkspaceSummary[] }>("GET", `/orgs/${org.id}/workspaces`).then((r) => setList(r.workspaces));
  }, [org]);
  if (!org || !isOrgAdmin(org.role)) return <Denied title="Workspaces" />;
  return (
    <>
      <h1>Workspaces</h1>
      <p className="lead">Each workspace is a team of agents with its own directories, goals and people.</p>
      <div className="grid2">
        {(list ?? []).map((w) => (
          <section className="card panel" key={w.id}>
            <div className="row"><LogoMark name={w.name} url={w.logo_url} /><h2 className="spacer">{w.name}</h2></div>
            <p className="hint">{w.about || "No description."}</p>
            <dl className="kv">
              <dt>Members</dt><dd>{w.members} agents</dd>
              <dt>People</dt><dd>{w.people}</dd>
              <dt>Directories</dt><dd>{w.directories}</dd>
              <dt>Goals finish</dt><dd>{w.goal_completion === "auto" ? "Automatically" : "When their person says so"}</dd>
            </dl>
            <div className="row"><Link className="btn btn-sm" href={`/w/${w.id}`}>Open</Link><Link className="btn btn-sm" href={`/w/${w.id}/settings`}>Settings</Link></div>
          </section>
        ))}
        <Link className="card panel" href="/setup" style={{ border: "1.5px dashed var(--line)", background: "transparent", alignItems: "center", justifyContent: "center", minHeight: 200, fontWeight: 600, color: "var(--muted)", textDecoration: "none" }}>
          + New workspace<span className="hint" style={{ fontWeight: 400 }}>Uses the computer and CLIs you already connected</span>
        </Link>
      </div>
    </>
  );
}
