import type { Metadata } from "next";
import { Suspense } from "react";
import { OrgDetail } from "./OrgDetail";

export const metadata: Metadata = { title: "Organization" };

export function generateStaticParams() {
  return [{ id: "_" }];
}

export default function AdminOrgPage() {
  return (
    <Suspense>
      <OrgDetail />
    </Suspense>
  );
}
