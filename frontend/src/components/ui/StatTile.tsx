import type { ReactNode } from "react";

/** StatTile is one number with a label and a short note under it. */
export function StatTile({ label, value, sub, cost }: { label: string; value: ReactNode; sub?: ReactNode; cost?: boolean }) {
  return (
    <div className={"ui-tile" + (cost ? " is-cost" : "")}>
      <div className="ui-tile-l">{label}</div>
      <div className="ui-tile-v">{value}</div>
      {sub && <div className="ui-tile-s">{sub}</div>}
    </div>
  );
}

/** StatTiles lays tiles out two across on phones and four across on wide screens. */
export function StatTiles({ children }: { children: ReactNode }) {
  return <div className="ui-tiles">{children}</div>;
}
