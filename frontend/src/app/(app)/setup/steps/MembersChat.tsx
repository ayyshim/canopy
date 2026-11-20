"use client";

import { useState } from "react";
import { api, ApiError } from "@/lib/api";
import { SKILL_LABEL } from "@/lib/types";
import type { StepProps } from "../Setup";

export type ChatMember = { key: string; name: string; kind: "manager" | "staff"; skill: string; reports_to_key: string; instructions: string; skill_keys: string[] };
type Reply = { reply: string; team: { members: ChatMember[] } | null };
type Msg = { role: "user" | "assistant"; text: string };

/** A chat where the person describes their needs and gets a suggested team. */
export function MembersChat({ p, orgId, computerId, online, onUse }: Pick<StepProps, "p" | "orgId"> & { computerId: string | undefined; online: boolean; onUse: (m: ChatMember[]) => void }) {
  const [msgs, setMsgs] = useState<Msg[]>([]);
  const [text, setText] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [team, setTeam] = useState<ChatMember[] | null>(null);
  const [used, setUsed] = useState(false);

  const send = async () => {
    const t = text.trim();
    if (!t || busy) return;
    const next: Msg[] = [...msgs, { role: "user", text: t }];
    setMsgs(next);
    setText("");
    setError("");
    setBusy(true);
    try {
      const r = await api<Reply>("POST", `/orgs/${orgId}/setup/chat`, {
        runner_id: computerId,
        workspace: { Name: p.workspace.name, About: p.workspace.about },
        directories: p.directories.map((d) => ({ name: d.name, kind: d.kind })),
        skills: p.skills.map((k) => ({ key: k.key, name: k.name })),
        messages: next,
      });
      setMsgs([...next, { role: "assistant", text: r.reply || "Here is what I suggest." }]);
      if (r.team) { setTeam(r.team.members); setUsed(false); }
    } catch (e) {
      setError(e instanceof ApiError && e.status === 409 ? e.message : "Something went wrong. Please try again.");
    } finally {
      setBusy(false);
    }
  };

  const nameOf = (k: string) => team?.find((m) => m.key === k)?.name;
  return (
    <div className="stack-sm" aria-label="Chat about your team">
      {!online && <div className="err" style={{ fontSize: 13.5 }}>Your computer isn’t connected right now. Connect it to chat, or build the team by hand.</div>}
      <div className="stack-sm" aria-live="polite">
        {msgs.length === 0 && <p className="hint">Tell me what you want to get done. For example: “I run a small shop and need help with my website and writing.”</p>}
        {msgs.map((m, i) => (
          <div key={i} className="card" style={{ padding: "8px 12px", marginLeft: m.role === "user" ? 40 : 0, marginRight: m.role === "user" ? 0 : 40, whiteSpace: "pre-wrap" }}>
            <b style={{ fontSize: 12 }}>{m.role === "user" ? "You" : "Canopy"}</b><div>{m.text}</div>
          </div>
        ))}
        {busy && <p className="hint">Thinking…</p>}
      </div>
      {error && <div className="err" style={{ fontSize: 13.5 }}>{error}</div>}
      <div className="row" style={{ gap: 8, alignItems: "flex-end" }}>
        <textarea className="input" style={{ flex: 1 }} rows={2} value={text} aria-label="Your message" placeholder="What do you need help with?" disabled={!online}
          onChange={(e) => setText(e.target.value)} onKeyDown={(e) => { if (e.key === "Enter" && !e.shiftKey) { e.preventDefault(); void send(); } }} />
        <button type="button" className="btn btn-primary" onClick={send} disabled={busy || !online || !text.trim()}>Send</button>
      </div>
      {team && (
        <div className="mgroup">
          <h3 style={{ margin: 0 }}>Suggested team</h3>
          <ul style={{ margin: "6px 0", paddingLeft: 18 }}>
            {team.map((m) => (
              <li key={m.key}><b>{m.name}</b>{m.kind === "manager" ? " (manager)" : ` · ${SKILL_LABEL[m.skill] ?? "Staff"}${m.reports_to_key && nameOf(m.reports_to_key) ? `, reports to ${nameOf(m.reports_to_key)}` : ", helps anyone"}`}</li>
            ))}
          </ul>
          <button type="button" className="btn btn-primary" onClick={() => { onUse(team); setUsed(true); }}>Use this team</button>
          {used && <span className="hint" style={{ marginLeft: 8 }}>Done. Switch to “Build by hand” to change anyone.</span>}
        </div>
      )}
    </div>
  );
}
