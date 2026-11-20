"use client";

import Link from "next/link";
import { SearchTable } from "@/components/SearchTable";
import { formatDate, ORG_SIZES, type AdminOrg } from "@/lib/api";

const sizeLabel = (s: string) => ORG_SIZES.find((o) => o.value === s)?.label ?? s;

export function Organizations() {
  return (
    <>
      <h1>Organizations</h1>
      <SearchTable<AdminOrg>
        path="/admin/organizations"
        label="Search organizations"
        rowKey={(o) => o.id}
        columns={[
          { header: "Name", cell: (o) => <Link href={`/admin/organizations/${o.id}`}>{o.name}</Link> },
          { header: "Owner", cell: (o) => <>{o.owner.name} <span className="muted">{o.owner.email}</span></> },
          { header: "Members", cell: (o) => o.members, align: "right" },
          { header: "Size", cell: (o) => sizeLabel(o.size) },
          { header: "Created", cell: (o) => formatDate(o.created_at) },
        ]}
      />
    </>
  );
}
