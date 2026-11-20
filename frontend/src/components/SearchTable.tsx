"use client";

import { useEffect, useState } from "react";
import { api, type Page } from "@/lib/api";

type Props<T> = {
  path: string; // e.g. "/admin/users"
  label: string; // search box label
  columns: { header: string; cell: (row: T) => React.ReactNode; align?: "right" }[];
  rowKey: (row: T) => string;
  version?: number; // bump to reload
};

const PAGE = 50;

/** A searchable, paged table over an admin list endpoint ({ items, total }). */
export function SearchTable<T>({ path, label, columns, rowKey, version = 0 }: Props<T>) {
  const [q, setQ] = useState("");
  const [offset, setOffset] = useState(0);
  const [data, setData] = useState<Page<T> | null>(null);

  useEffect(() => {
    let live = true;
    const t = setTimeout(() => {
      const qs = new URLSearchParams({ q, limit: String(PAGE), offset: String(offset) });
      api<Page<T>>("GET", `${path}?${qs}`).then((d) => live && setData(d));
    }, 200);
    return () => {
      live = false;
      clearTimeout(t);
    };
  }, [path, q, offset, version]);

  return (
    <div className="stack">
      <input className="input" type="search" aria-label={label} placeholder={label} value={q}
        onChange={(e) => { setQ(e.target.value); setOffset(0); }} />
      <div className="card" style={{ overflowX: "auto" }}>
        <table className="table">
          <thead>
            <tr>{columns.map((c) => <th key={c.header} style={{ textAlign: c.align ?? "left" }}>{c.header}</th>)}</tr>
          </thead>
          <tbody>
            {data?.items.map((row) => (
              <tr key={rowKey(row)}>
                {columns.map((c) => <td key={c.header} style={{ textAlign: c.align ?? "left" }}>{c.cell(row)}</td>)}
              </tr>
            ))}
            {data && data.items.length === 0 && (
              <tr><td colSpan={columns.length} className="muted">No matches.</td></tr>
            )}
          </tbody>
        </table>
      </div>
      {data && data.total > PAGE && (
        <div className="row">
          <span className="muted">{offset + 1}–{Math.min(offset + PAGE, data.total)} of {data.total}</span>
          <span className="spacer" />
          <button className="btn btn-sm" disabled={offset === 0} onClick={() => setOffset(offset - PAGE)}>Previous</button>
          <button className="btn btn-sm" disabled={offset + PAGE >= data.total} onClick={() => setOffset(offset + PAGE)}>Next</button>
        </div>
      )}
    </div>
  );
}
