"use client";

import { useUI } from "@/lib/board/ui";
import type { TaskView } from "@/lib/types";
import type { UsageTotals } from "@/lib/core-types";
import { fmtCost, fmtTokens } from "@/lib/usageFormat";
import { Card, EmptyState, StatTile, StatTiles } from "../../ui";

const empty = (u?: UsageTotals | null) => !u || u.calls === 0;

/** UsageTab shows what a goal or task used: totals, then each model, then (for goals) each task. */
export function UsageTab({ usage, tasks }: { usage?: UsageTotals | null; tasks?: TaskView[] }) {
  const openSide = useUI((s) => s.openSide);
  if (!usage || empty(usage)) return <EmptyState icon="$">No usage yet. Tokens and cost show up here as the agents work.</EmptyState>;
  const rows = (tasks ?? []).filter((t) => (t.usage?.cost_micros ?? 0) > 0 || (t.usage?.calls ?? 0) > 0)
    .sort((a, b) => (b.usage?.cost_micros ?? 0) - (a.usage?.cost_micros ?? 0));
  return (
    <div className="stack">
      <StatTiles>
        <StatTile label="Estimated cost" value={fmtCost(usage.cost_micros)} sub={`${usage.calls} model calls`} cost />
        <StatTile label="Tokens in" value={fmtTokens(usage.tokens_in)} />
        <StatTile label="Tokens out" value={fmtTokens(usage.tokens_out)} />
        <StatTile label="Cache" value={fmtTokens(usage.cache_read)} sub={`read · ${fmtTokens(usage.cache_write)} written`} />
      </StatTiles>
      <Card title="By model" sub="List prices, not what a subscription costs.">
        <div className="scroll-x">
          <table className="ui-ut">
            <thead><tr><th>Model</th><th>In</th><th>Out</th><th>Cache</th><th>Cost</th></tr></thead>
            <tbody>
              {usage.by_model.map((m) => (
                <tr key={m.model}>
                  <td className="mono">{m.model}</td>
                  <td>{fmtTokens(m.tokens_in)}</td>
                  <td>{fmtTokens(m.tokens_out)}</td>
                  <td>{fmtTokens(m.cache_read)}</td>
                  <td className="ui-ut-c">{fmtCost(m.cost_micros)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </Card>
      {rows.length > 0 && (
        <Card title="By task" sub="Most expensive first.">
          <div className="stack-sm">
            {rows.map((t) => {
              const share = usage.cost_micros ? ((t.usage?.cost_micros ?? 0) / usage.cost_micros) * 100 : 0;
              return (
                <button key={t.id} className="trow ui-ut-row" onClick={() => openSide({ type: "task", id: t.id })}>
                  <span className="spacer ui-ut-t">{t.title}</span>
                  <span className="ui-ut-c">{fmtCost(t.usage?.cost_micros ?? 0)}</span>
                  <span className="ui-ut-bar"><i style={{ width: `${share}%` }} /></span>
                </button>
              );
            })}
          </div>
        </Card>
      )}
    </div>
  );
}
