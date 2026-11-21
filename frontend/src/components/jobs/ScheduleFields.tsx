"use client";

import { useEffect, useId, useRef, useState } from "react";
import { api, ApiError } from "@/lib/api";
import { DEFAULT_PICK, formatWhen, pickText, WEEKDAYS, type Pick, type SchedulePreview } from "@/lib/jobs";

type State = { status: "idle" | "loading" | "ok" | "bad"; data?: SchedulePreview; error?: string };

const ZONES: string[] = (() => {
  try { return (Intl as unknown as { supportedValuesOf: (k: string) => string[] }).supportedValuesOf("timeZone"); } catch { return []; }
})();

/** The When step: a picker, a text box and a raw field that all fill one schedule, with a live preview. */
export function ScheduleFields({ wsId, value, zone, onChange, onZone, onValid }: {
  wsId: string; value: string; zone: string; onChange: (s: string) => void; onZone: (z: string) => void; onValid: (ok: boolean) => void;
}) {
  const id = useId();
  const [pick, setPick] = useState<Pick>(DEFAULT_PICK);
  const [p, setP] = useState<State>({ status: "idle" });
  const seq = useRef(0);

  useEffect(() => {
    const text = value.trim();
    const n = ++seq.current;
    if (!text) { setP({ status: "idle" }); onValid(false); return; } // eslint-disable-line react-hooks/set-state-in-effect
    setP((s) => ({ ...s, status: "loading" }));
    const t = setTimeout(() => {
      api<SchedulePreview>("POST", "/schedules/preview", { schedule: text, timezone: zone, workspace_id: wsId }).then(
        (data) => { if (n === seq.current) { setP({ status: "ok", data }); onValid(true); } },
        (e) => { if (n === seq.current) { setP({ status: "bad", error: e instanceof ApiError ? e.message : "Couldn’t check that schedule." }); onValid(false); } },
      );
    }, 300);
    return () => clearTimeout(t);
  }, [value, zone, wsId, onValid]);

  const set = (next: Partial<Pick>) => {
    const n = { ...pick, ...next };
    setPick(n);
    onChange(pickText(n));
  };
  const cron = p.data?.cron ?? "";

  return (
    <div className="stack">
      <div className="field">
        <span>How often</span>
        <div className="seg" role="group" aria-label="How often">
          {([["day", "Every day"], ["weekdays", "Weekdays"], ["week", "Weekly"], ["month", "Monthly"]] as const).map(([f, label]) => (
            <button key={f} type="button" aria-pressed={pick.freq === f} onClick={() => set({ freq: f })}>{label}</button>
          ))}
        </div>
      </div>
      <div className="row" style={{ flexWrap: "wrap", alignItems: "flex-end" }}>
        {pick.freq === "week" && (
          <label className="field" style={{ minWidth: 140 }}><span>Day</span>
            <select className="input" value={pick.weekday} onChange={(e) => set({ weekday: Number(e.target.value) })}>
              {WEEKDAYS.map((d, i) => <option key={d} value={i}>{d}</option>)}
            </select></label>
        )}
        {pick.freq === "month" && (
          <label className="field" style={{ minWidth: 140 }}><span>Day of the month</span>
            <select className="input" value={pick.monthday} onChange={(e) => set({ monthday: Number(e.target.value) })}>
              {Array.from({ length: 28 }, (_, i) => i + 1).map((d) => <option key={d} value={d}>{d}</option>)}
            </select></label>
        )}
        <label className="field" style={{ minWidth: 120 }}><span>At</span>
          <input className="input" type="time" value={pick.time} onChange={(e) => set({ time: e.target.value || "09:00" })} /></label>
        <label className="field grow" style={{ minWidth: 200 }}><span>Time zone</span>
          <input className="input" list={`${id}-zones`} value={zone} onChange={(e) => onZone(e.target.value)} placeholder="Asia/Kathmandu" autoComplete="off" /></label>
        <datalist id={`${id}-zones`}>{ZONES.map((z) => <option key={z} value={z} />)}</datalist>
      </div>
      <label className="field"><span>Or write it</span>
        <input className="input" value={value} onChange={(e) => onChange(e.target.value)} placeholder="every weekday at 9" aria-invalid={p.status === "bad"} autoComplete="off" /></label>

      <div className="banner" role="status" aria-live="polite" style={{ minHeight: 64 }}>
        {p.status === "bad" ? <span className="err">{p.error}</span> : p.data ? (
          <>
            <b>{p.data.sentence}</b>
            <ol className="plain-list" style={{ margin: "6px 0 0", paddingLeft: 18 }}>
              {p.data.next.map((t) => <li key={t}>{formatWhen(t, p.data!.zone)}</li>)}
            </ol>
          </>
        ) : <span className="muted">{p.status === "loading" ? "Checking…" : "Pick a time or write one to see when it runs."}</span>}
      </div>

      <details>
        <summary className="hint" style={{ cursor: "pointer" }}>Advanced</summary>
        <label className="field" style={{ marginTop: 8 }}><span>Raw schedule (five fields: minute hour day month weekday)</span>
          <input className="input" value={cron || (/^[\d*/,\-\s]+$/.test(value) ? value : "")} onChange={(e) => onChange(e.target.value)} placeholder="0 9 * * 1-5" autoComplete="off" spellCheck={false} /></label>
      </details>
    </div>
  );
}
