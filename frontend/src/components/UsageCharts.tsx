"use client";

import { fmtTokens } from "@/lib/board/derive";

const PALETTE = ["var(--green)", "var(--teal)", "var(--violet)", "var(--amber)", "var(--blue)"];
const DAYS = ["Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"];

export const meterClass = (p: number) => (p >= 90 ? "red" : p >= 70 ? "amber" : "");

/** DayBars draws tokens per day, stacked by workspace. */
export function DayBars({ days, names, today }: { days: { day: string; by_workspace: Record<string, number> | null }[]; names: Record<string, string>; today?: string }) {
  const ws = Object.keys(names);
  const totals = days.map((d) => Object.values(d.by_workspace ?? {}).reduce((a, b) => a + b, 0));
  const yMax = Math.max(1, Math.ceil(Math.max(...totals, 1) / 1000) * 1000);
  const H = 160, W = 520, bw = 46, gap = (W - 40 - bw * 7) / 6;
  return (
    <>
      <div className="row" style={{ fontSize: 12, flexWrap: "wrap", gap: 10 }}>
        {ws.map((id, i) => <span key={id} className="row" style={{ gap: 4 }}><span className="dot" style={{ background: PALETTE[i % PALETTE.length] }} />{names[id]}</span>)}
      </div>
      <div className="scroll-x">
        <svg viewBox={`0 0 ${W} ${H + 34}`} style={{ width: "100%", minWidth: 420 }} role="img" aria-label="Tokens per day by workspace">
          {[0, 0.5, 1].map((f) => (
            <g key={f}>
              <line x1="34" x2={W} y1={10 + H - f * H} y2={10 + H - f * H} stroke="var(--edge)" />
              <text x="30" y={14 + H - f * H} textAnchor="end" fontSize="10.5" fill="var(--muted)">{fmtTokens(yMax * f)}</text>
            </g>
          ))}
          {days.map((d, i) => {
            const x0 = 40 + i * (bw + gap);
            let y = 10 + H;
            return (
              <g key={d.day}>
                {ws.map((id, k) => {
                  const h = ((d.by_workspace?.[id] ?? 0) / yMax) * H;
                  y -= h;
                  return <rect key={id} x={x0} y={y} width={bw} height={h} fill={PALETTE[k % PALETTE.length]} opacity={k ? 0.8 : 1} />;
                })}
                <text x={x0 + bw / 2} y={H + 26} textAnchor="middle" fontSize="11" fill="var(--muted)">{d.day === today ? "Today" : DAYS[(new Date(d.day + "T12:00:00").getDay() + 6) % 7]}</text>
              </g>
            );
          })}
        </svg>
      </div>
    </>
  );
}

/** Heatmap shows tokens by weekday and hour. */
export function Heatmap({ cells }: { cells: number[][] }) {
  const max = Math.max(1, ...cells.flat());
  return (
    <div className="heat" role="img" aria-label="Tokens by day and hour">
      {cells.map((row, i) => (
        <span key={i} style={{ display: "contents" }}>
          <span>{DAYS[i]}</span>
          {row.map((v, h) => <i key={h} title={fmtTokens(v)} style={{ background: `color-mix(in srgb, var(--green) ${Math.round((v / max) * 100)}%, var(--paper))` }} />)}
        </span>
      ))}
    </div>
  );
}

export function Bars({ rows }: { rows: { label: string; value: number }[] }) {
  const total = Math.max(1, rows.reduce((a, r) => a + r.value, 0));
  return (
    <>
      {rows.map((r) => (
        <div className="stack-sm" style={{ gap: 3 }} key={r.label}>
          <div className="row"><span>{r.label}</span><span className="spacer" /><span className="tnum muted">{fmtTokens(r.value)}</span></div>
          <div className="meter blue"><i style={{ width: `${(r.value / total) * 100}%` }} /></div>
        </div>
      ))}
      {!rows.length && <p className="hint">Nothing yet.</p>}
    </>
  );
}
