"use client";

import Link from "next/link";
import { LogoutButton } from "@/components/LogoutButton";
import { AuthShell } from "@/components/AuthShell";
import { currentOrg } from "@/lib/api";
import { useMe } from "@/lib/me";

/** For members with no workspace access (SPEC "Sessions and logout"). */
export function NoWorkspace() {
  const { me } = useMe();
  const org = currentOrg(me);
  const admin = org?.role === "owner" || org?.role === "admin";
  return (
    <AuthShell>
      <div className="authcard">
        <h1>{admin ? "No workspaces yet" : "Nothing shared with you yet"}</h1>
        <p className="lead">{admin ? `${org?.name} has no workspace you can open. Create one to give your agents something to do.` : `You’re in ${org?.name ?? "the organization"}, but no workspace is shared with you. Ask an admin to give you access.`}</p>
        {admin && <Link className="btn btn-primary wide" href="/setup">Create a workspace</Link>}
        <Link className="btn wide" href="/new-org">Create an organization</Link>
        <LogoutButton className="btn btn-ghost wide" />
      </div>
    </AuthShell>
  );
}
