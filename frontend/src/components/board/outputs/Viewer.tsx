"use client";

import { useEffect, useState } from "react";
import { markSeen, resolveSignal } from "@/lib/board/actions";
import { clock, computer, goal as findGoal, payload, runnerForTask, task as findTask, visibleMember } from "@/lib/board/derive";
import { useUI } from "@/lib/board/ui";
import type { OutputView } from "@/lib/types";
import { useB } from "../context";
import { Avatar, KIND_LABEL } from "../icons";
import { Markdown } from "../panels/bits";

export const fileURL = (o: OutputView, n: number) => `/api/v1/outputs/${o.id}/versions/${n}/file`;

/** useText fetches a text output's content. */
export function useText(url: string, enabled: boolean) {
  const [got, setGot] = useState<{ url: string; text: string } | null>(null);
  useEffect(() => {
    if (!enabled) return;
    let live = true;
    fetch(url, { credentials: "same-origin" }).then((r) => (r.ok ? r.text() : "")).then((t) => live && setGot({ url, text: t }), () => live && setGot({ url, text: "" }));
    return () => { live = false; };
  }, [url, enabled]);
  return got?.url === url ? got.text : null;
}

function Preview({ o, n }: { o: OutputView; n: number }) {
  const v = o.versions[n - 1];
  const isText = !/^(image|video|audio)\//.test(v?.mime ?? "");
  const text = useText(fileURL(o, n), isText);
  // Outputs are files from the hub, so next/image's optimizer doesn't apply.
  // eslint-disable-next-line @next/next/no-img-element
  if (v?.mime.startsWith("image/")) return <img src={fileURL(o, n)} alt={o.title} style={{ maxWidth: "100%", borderRadius: 8 }} />;
  if (v?.mime.startsWith("video/") || v?.mime.startsWith("audio/")) return <video src={fileURL(o, n)} controls style={{ maxWidth: "100%", borderRadius: 10 }} />;
  if (text === null) return <p className="hint">Loading…</p>;
  if (o.kind === "diff" || /\.(diff|patch)$/.test(o.name)) {
    return (
      <div className="diff">
        {text.split("\n").slice(0, 4000).map((l, i) => (
          <div key={i} className={l.startsWith("+++") || l.startsWith("---") || l.startsWith("diff ") || l.startsWith("@@") ? "hd" : l.startsWith("+") ? "add" : l.startsWith("-") ? "del" : ""}>{l || " "}</div>
        ))}
      </div>
    );
  }
  if (/\.md$/.test(o.name) || v?.mime === "text/markdown") return <div className="doc"><Markdown text={text} className="md" /></div>;
  return <div className="doc"><pre style={{ whiteSpace: "pre-wrap", margin: 0 }}>{text}</pre></div>;
}

/** Viewer shows one output full screen, with versions and the review decision. */
export function Viewer() {
  const b = useB();
  const ui = useUI();
  const vw = ui.viewer;
  const [changes, setChanges] = useState(false);
  const [note, setNote] = useState("");
  const o = vw ? b.outputs.find((x) => x.id === vw.outputId) : undefined;
  const n = o ? vw?.version ?? o.versions.length : 0;

  useEffect(() => {
    if (o) markSeen(o.id, o.versions.length);
  }, [o?.id, o?.versions.length]); // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => {
    if (!vw) return;
    const list = b.outputs;
    const onKey = (e: KeyboardEvent) => {
      if ((e.target as HTMLElement).closest("textarea, input")) return;
      const i = list.findIndex((x) => x.id === vw.outputId);
      if (e.key === "Escape") ui.closeViewer();
      if (e.key === "ArrowLeft" && i > 0) ui.openViewer(list[i - 1].id);
      if (e.key === "ArrowRight" && i < list.length - 1) ui.openViewer(list[i + 1].id);
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [vw, b.outputs, ui]);

  if (!vw || !o) return null;
  const list = b.outputs;
  const idx = list.indexOf(o);
  const t = findTask(b, o.task_id);
  const m = visibleMember(b, o.member_id);
  const g = findGoal(b, t?.goal_id);
  const v = o.versions[n - 1];
  const sig = b.signals.find((s) => s.type === "review" && payload(s).output_id === o.id);
  const latest = n === o.versions.length;
  const show = () => { if (t) { ui.closeViewer(); ui.centerOn(t.id); ui.setGlow(t.id); } };
  const copy = async () => {
    try {
      const text = await (await fetch(fileURL(o, n), { credentials: "same-origin" })).text();
      await navigator.clipboard.writeText(text);
      ui.toast("Copied.", "ok");
    } catch {
      ui.toast("Couldn’t copy. Select the text instead.", "warn");
    }
  };

  return (
    <div className="overlay" onClick={(e) => e.target === e.currentTarget && ui.closeViewer()}>
      <div className="viewer" role="dialog" aria-modal="true" aria-label={o.title}>
        <div className="stage">
          <div className="stage-top">
            <button aria-label="Previous output" disabled={idx <= 0} onClick={() => ui.openViewer(list[idx - 1].id)}>‹</button>
            <span className="t">{o.title}</span>
            <button aria-label="Next output" disabled={idx >= list.length - 1} onClick={() => ui.openViewer(list[idx + 1].id)}>›</button>
          </div>
          <div className="stage-in"><Preview o={o} n={n} /></div>
        </div>
        <aside className="vpanel">
          <div className="row"><span className="pill">{KIND_LABEL[o.kind]}</span><span className="spacer" /><button className="icon-btn" onClick={ui.closeViewer} aria-label="Close viewer">✕</button></div>
          <h2>{o.title}</h2>
          {o.versions.length > 1 && (
            <div className="sb-sec">
              <span className="lbl">Versions</span>
              <div className="vers">{o.versions.map((x) => <button key={x.number} aria-pressed={x.number === n} onClick={() => ui.openViewer(o.id, x.number)}>Version {x.number}</button>)}</div>
            </div>
          )}
          {sig && latest ? (
            <div className="box warn">
              <b>{m?.name ?? "The agent"} is waiting for your review</b>
              <span style={{ fontSize: 13 }}>The task finishes once you approve.</span>
              {changes ? (
                <>
                  <label className="field"><span>What should change?</span><textarea className="textarea" rows={3} value={note} onChange={(e) => setNote(e.target.value)} /></label>
                  <div className="row">
                    <button className="btn btn-primary" disabled={!note.trim()} onClick={async () => { if (await resolveSignal(sig.id, "changes", { note })) { setChanges(false); setNote(""); } }}>Send to {m?.name}</button>
                    <button className="btn" onClick={() => setChanges(false)}>Cancel</button>
                  </div>
                </>
              ) : (
                <div className="row">
                  <button className="btn btn-primary" onClick={() => resolveSignal(sig.id, "approve")}>Approve</button>
                  <button className="btn" onClick={() => setChanges(true)}>Ask for changes</button>
                </div>
              )}
            </div>
          ) : o.status === "approved" ? (
            <div className="box ok"><b>Approved</b><span style={{ fontSize: 13 }}>Version {o.versions.length} was approved.</span></div>
          ) : o.status === "changes" ? (
            <div className="box"><b>Changes asked</b><span style={{ fontSize: 13 }}>{m?.name} is making a new version.</span></div>
          ) : null}
          <dl className="kv">
            <dt>Made by</dt><dd>{m ? <span className="row" style={{ gap: 6 }}><Avatar name={m.name} color={m.avatar_color} size={22} />{m.name}</span> : "An agent"}</dd>
            <dt>Model</dt><dd>{v?.model || m?.model}</dd>
            {t && <><dt>Task</dt><dd><button className="linkbtn" style={{ padding: 0, textAlign: "left" }} onClick={show}>{t.title}</button></dd></>}
            {o.kind === "diff" && g?.branch && <><dt>Branch</dt><dd className="mono" style={{ fontSize: 12 }}>{g.branch}</dd></>}
            {t && <><dt>Made on</dt><dd>{computer(b, runnerForTask(b, t))?.name ?? "—"}</dd></>}
            <dt>Made at</dt><dd>{clock(v?.created_at)}</dd>
            {v?.note && <><dt>Note</dt><dd>{v.note}</dd></>}
          </dl>
          <div className="row wrap" style={{ marginTop: "auto" }}>
            {t && <button className="btn btn-sm" onClick={show}>Show on the tree</button>}
            {["document", "diff", "report"].includes(o.kind) && <button className="btn btn-sm" onClick={copy}>Copy</button>}
            <a className="btn btn-sm" href={fileURL(o, n)} target="_blank" rel="noreferrer">Open the file</a>
          </div>
          <span className="hint"><span className="kbd">←</span> <span className="kbd">→</span> move between outputs · <span className="kbd">Esc</span> closes</span>
        </aside>
      </div>
    </div>
  );
}
