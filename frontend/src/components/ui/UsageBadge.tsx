import type { UsageTotals } from "@/lib/core-types";
import { fmtCost, fmtTokens, totalTokens } from "@/lib/usageFormat";

/** UsageBadge shows what a goal or task used: "12.4k tokens · $0.31". It is a dash until there is usage. */
export function UsageBadge({ usage, compact }: { usage?: UsageTotals | null; compact?: boolean }) {
  const tokens = usage ? totalTokens(usage) : 0;
  if (!usage || tokens + usage.cost_micros === 0) {
    return <span className="ui-usage ui-usage-empty" title="No usage yet">–</span>;
  }
  const label = `${fmtTokens(tokens)}${compact ? "" : " tokens"} · ${fmtCost(usage.cost_micros)}`;
  return <span className="ui-usage" title="Estimated cost at list prices">{label}</span>;
}
