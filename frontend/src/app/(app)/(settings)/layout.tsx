"use client";

import { usePathname } from "next/navigation";
import { Suspense } from "react";
import { PageShell } from "@/components/PageShell";

const TITLES: [string, string][] = [
  ["/org/usage", "Usage and analytics"], ["/org/activity", "Activity log"], ["/org/general", "Organization"], ["/org/people", "People"],
  ["/org/workspaces", "Workspaces"], ["/org/computers", "Computers and sign-ins"], ["/account", "Account"],
];

export default function SettingsLayout({ children }: { children: React.ReactNode }) {
  const path = usePathname();
  const title = TITLES.find(([p]) => path.startsWith(p))?.[1] ?? "Settings";
  return <Suspense><PageShell title={title}>{children}</PageShell></Suspense>;
}
