import type { UsageTotals } from "@/lib/core-types";

/** Tokens as 950, 12.4k or 1.2M. */
export function fmtTokens(n: number): string {
  if (n >= 1e6) return (n / 1e6).toFixed(1) + "M";
  if (n >= 1e3) return (n / 1e3).toFixed(1) + "k";
  return String(Math.round(n));
}

/** Cost in millionths of a dollar as $0.31, $12.40 or <$0.01. Zero shows as a dash. */
export function fmtCost(micros: number): string {
  if (micros <= 0) return "–";
  if (micros < 10_000) return "<$0.01";
  return "$" + (micros / 1e6).toFixed(2);
}

/** Tokens in and out together; cache reads are shown on their own. */
export function totalTokens(u: Pick<UsageTotals, "tokens_in" | "tokens_out">): number {
  return u.tokens_in + u.tokens_out;
}
