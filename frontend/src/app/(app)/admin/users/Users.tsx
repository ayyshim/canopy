"use client";

import { useState } from "react";
import { SearchTable } from "@/components/SearchTable";
import { api, ApiError, formatDate, timeAgo, type AdminUser } from "@/lib/api";
import { useMe } from "@/lib/me";

export function Users() {
  const { me } = useMe();
  const [version, setVersion] = useState(0);
  const [error, setError] = useState("");

  async function setAdmin(u: AdminUser, value: boolean) {
    setError("");
    try {
      await api("PATCH", "/admin/users/" + u.id, { is_hub_admin: value });
      if (u.id === me.user.id && !value) return window.location.reload();
      setVersion((v) => v + 1);
    } catch (e) {
      setError(e instanceof ApiError ? e.message : "Something went wrong. Try again.");
    }
  }

  return (
    <>
      <h1>Users</h1>
      {error && <div className="banner bad" role="alert">{error}</div>}
      <SearchTable<AdminUser>
        path="/admin/users"
        label="Search people by name or email"
        rowKey={(u) => u.id}
        version={version}
        columns={[
          { header: "Name", cell: (u) => <>{u.name}{u.is_hub_admin && <> <span className="pill">Hub admin</span></>}</> },
          { header: "Email", cell: (u) => u.email },
          { header: "Organizations", cell: (u) => u.organizations, align: "right" },
          { header: "Last active", cell: (u) => timeAgo(u.last_active_at) },
          { header: "Joined", cell: (u) => formatDate(u.created_at) },
          {
            header: "Hub admin",
            cell: (u) => (
              <button className="btn btn-sm" onClick={() => void setAdmin(u, !u.is_hub_admin)}>
                {u.is_hub_admin ? "Remove hub admin" : "Make hub admin"}
              </button>
            ),
          },
        ]}
      />
    </>
  );
}
