"use client";

import { Avatar } from "@/components/board/icons";
import { SKILL_LABEL } from "@/lib/types";
import type { StepProps } from "../Setup";

function Edit({ step, go }: { step: number; go: (step: number) => void }) {
  return <button className="linkbtn" onClick={() => go(step)}>Edit</button>;
}

/** Review and create: exactly what Create workspace sends. */
export function Review({ p, dirs, computer, people, orgName, go }: StepProps) {
  const name = (key: string) => p.members.find((m) => m.key === key)?.name || "";
  const access = (key: string) => {
    const m = p.members.find((x) => x.key === key)!;
    if (m.kind === "manager" && m.all_directories) return "All workspace directories";
    return m.grants.map((g) => `${dirs.find((d) => d.key === g.directory_key)?.name} (${g.mode === "read_write" ? "read & write" : "read"})`).join(", ") || "Only the workspace notes";
  };
  const notesOnly = p.members.filter((m) => m.kind === "staff" && !m.grants.length);
  const signIn = p.clis.find((c) => c.cli === "claude")?.mode === "api_key" ? "your API key" : "your Claude subscription";
  const invited = p.people.filter((x) => x.user_id || x.email.trim());
  return (
    <>
      <h1>Review and create</h1>
      <p className="lead">Check who can reach what before the workspace goes live. You can change any of this later from the board.</p>
      <div className="rv">
        <h2 className="row">Workspace<span className="spacer" /><Edit step={2} go={go} /></h2>
        <span className="hint">In {orgName}</span><br /><b>{p.workspace.name}</b>{p.workspace.about && <span className="hint">, {p.workspace.about}</span>}
        <p className="hint" style={{ marginTop: 6 }}>Your goals run on {computer?.name ?? "your computer"} with {signIn}. Notes, decisions and outputs are kept in the hub.</p>
      </div>
      <div className="rv">
        <h2 className="row">Repositories and folders<span className="spacer" /><Edit step={2} go={go} /></h2>
        {dirs.length ? (
          <div className="scroll-x">
            <table className="table">
              <thead><tr><th>Directory</th><th>Shared through</th><th>Members with access</th></tr></thead>
              <tbody>
                {dirs.map((d) => {
                  const who = p.members.filter((m) => (m.kind === "manager" && m.all_directories) || m.grants.some((g) => g.directory_key === d.key));
                  return (
                    <tr key={d.key}><td><code>{d.path}</code></td><td>{d.is_repo ? "Git: each person’s own clone" : `Hub copy of the folder on ${computer?.name ?? "your computer"}`}</td>
                      <td>{who.length ? who.map((m) => m.name).join(", ") : <span className="hint">No one yet</span>}</td></tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        ) : <p className="hint">No directories. Members can only read the workspace notes.</p>}
      </div>
      <div className="rv">
        <h2 className="row">Skills<span className="spacer" /><Edit step={4} go={go} /></h2>
        {p.skills.length ? p.skills.map((x) => <div key={x.key} style={{ padding: "3px 0", fontSize: 13.5 }}><b>{x.name}</b> <span className="hint">{x.source_kind === "github" ? "from GitHub" : "Markdown"}</span></div>)
          : <p className="hint">No skills yet. You can add them any time in workspace settings.</p>}
      </div>
      <div className="rv">
        <h2 className="row">Team and their access<span className="spacer" /><Edit step={5} go={go} /></h2>
        {p.members.map((m) => (
          <div className="row" key={m.key} style={{ alignItems: "flex-start", padding: "6px 0", paddingLeft: m.kind === "staff" && m.reports_to_key ? 26 : 0 }}>
            <Avatar name={m.name || "?"} color={m.kind === "manager" ? "#14322A" : "#0E9AA7"} size={28} />
            <span className="spacer"><b>{m.name}</b> <span className="hint">{m.kind === "manager" ? "Manager" : SKILL_LABEL[m.skill] + (m.reports_to_key ? "" : ", independent")}, {m.model}</span><br />
              <code style={{ fontSize: 12 }}>{access(m.key)}</code>
              {m.skill_keys.length > 0 && <><br /><span className="hint">Skills: {m.skill_keys.map((k) => p.skills.find((x) => x.key === k)?.name).filter(Boolean).join(", ")}</span></>}</span>
            {m.kind === "staff" && (m.grants_by === "you" ? <span className="pill">Changed by you</span> : <span className="pill info">Suggested by {m.reports_to_key ? name(m.reports_to_key) : "Canopy"}</span>)}
          </div>
        ))}
        {notesOnly.length > 0 && <div className="note-box" style={{ background: "var(--amber-soft)", marginTop: 8 }}>{notesOnly.map((m) => m.name).join(" and ")} can only read the workspace notes.</div>}
      </div>
      <div className="rv">
        <h2 className="row">People<span className="spacer" /><Edit step={6} go={go} /></h2>
        {invited.map((x, i) => (
          <div style={{ padding: "4px 0", fontSize: 13.5 }} key={i}>
            <b>{x.user_id ? people.find((o) => o.id === x.user_id)?.name : x.email}</b>{" "}
            <span className="hint">{x.role === "admin" ? "Admin, full access" : "Staff, can see " + ([...x.manager_keys.map((k) => name(k) + "’s team"), ...(x.independent_staff ? ["independent staff"] : [])].join(", ") || "nothing yet")}{x.user_id ? "" : " · gets an invitation email"}</span>
          </div>
        ))}
        {!invited.length && <p className="hint">Just you for now.</p>}
      </div>
      <div className="rv">
        <h2 className="row">First goal<span className="spacer" /><Edit step={7} go={go} /></h2>
        {p.first_goal?.title.trim() ? <><b>{p.first_goal.title}</b> <span className="hint">for {name(p.first_goal.manager_key)}</span></> : <p className="hint">No goal yet. The workspace starts idle.</p>}
      </div>
    </>
  );
}
