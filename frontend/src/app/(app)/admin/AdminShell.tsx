"use client";

import Link from "next/link";
import { Suspense } from "react";
import { PageShell } from "@/components/PageShell";
import { useMe } from "@/lib/me";

/** Hub admin pages: only hub admins get past this. */
export function AdminShell({ children }: { children: React.ReactNode }) {
  const { me } = useMe();
  return (
    <Suspense>
      <PageShell title="Hub admin">
        {me.user.is_hub_admin ? children : (
          <>
            <h1>Only hub admins can open this</h1>
            <p className="lead">Ask a hub admin if you need access.</p>
            <div><Link className="btn" href="/">Go back</Link></div>
          </>
        )}
      </PageShell>
    </Suspense>
  );
}
