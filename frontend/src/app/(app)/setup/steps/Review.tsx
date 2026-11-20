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
    if (m.kind === "manager" && m.all_directories) return "All folders";
    return m.grants.map((g) => `${dirs.find((d) => d.key === g.directory_key)?.name} (${g.mode === "read_write" ? "can edit" : "view only"})`).join(", ") || "Notes only";
  };
  const notesOnly = p.members.filter((m) => m.kind === "staff" && !m.grants.length);
    const invited = p.people.filter((x) => x.user_id || x.email.trim());
  return (
    <>
      <h1>Ready to create?</h1>
      <p className="lead">Here’s what you chose. You can change any of it later.</p>
      <div className="rv">
        <h2 className="row">Workspace<span className="spacer" /><Edit step={2} go={go} /></h2>
        <span className="hint">In {orgName}</span><br /><b>{p.workspace.name}</b>{p.workspace.about && <span className="hint">, {p.workspace.about}</span>}
        <p className="hint" style={{ marginTop: 6 }}>The work happens on {computer?.name ?? "your computer"}.</p>
      </div>
      <div className="rv">
        <h2 className="row">Your files<span className="spacer" /><Edit step={2} go={go} /></h2>
        {dirs.length ? (
          <div className="scroll-x">
            <table className="table">
              <thead><tr><th>Folder</th><th>Who can use it</th></tr></thead>
              <tbody>
                {dirs.map((d) => {
                  const who = p.members.filter((m) => (m.kind === "manager" && m.all_directories) || m.grants.some((g) => g.directory_key === d.key));
                  return (
                    <tr key={d.key}><td><code>{d.path}</code></td>                      <td>{who.length ? who.map((m) => m.name).join(", ") : <span className="hint">No one yet</span>}</td></tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        ) : <p className="hint">No folders chosen. You can add some later.</p>}
      </div>
      <div className="rv">
        <h2 className="row">Skills<span className="spacer" /><Edit step={3} go={go} /></h2>
        {p.skills.length ? p.skills.map((x) => <div key={x.key} style={{ padding: "3px 0", fontSize: 13.5 }}><b>{x.name}</b> <span className="hint">{x.source_kind === "github" ? "from GitHub" : "Markdown"}</span></div>)
          : <p className="hint">No skills yet. You can add them any time in workspace settings.</p>}
      </div>
      <div className="rv">
        <h2 className="row">Your team<span className="spacer" /><Edit step={4} go={go} /></h2>
        {p.members.map((m) => (
          <div className="row" key={m.key} style={{ alignItems: "flex-start", padding: "6px 0", paddingLeft: m.kind === "staff" && m.reports_to_key ? 26 : 0 }}>
            <Avatar name={m.name || "?"} color={m.kind === "manager" ? "#14322A" : "#0E9AA7"} size={28} />
            <span className="spacer"><b>{m.name}</b> <span className="hint">{m.kind === "manager" ? "Manager" : SKILL_LABEL[m.skill] + (m.reports_to_key ? "" : ", shared")}</span><br />
              <code style={{ fontSize: 12 }}>{access(m.key)}</code>
              {m.skill_keys.length > 0 && <><br /><span className="hint">Skills: {m.skill_keys.map((k) => p.skills.find((x) => x.key === k)?.name).filter(Boolean).join(", ")}</span></>}</span>
            {m.kind === "staff" && (m.grants_by === "you" ? <span className="pill">Set by you</span> : <span className="pill info">Chosen by {m.reports_to_key ? name(m.reports_to_key) : "Canopy"}</span>)}
          </div>
        ))}
        {notesOnly.length > 0 && <div className="note-box" style={{ background: "var(--amber-soft)", marginTop: 8 }}>{notesOnly.map((m) => m.name).join(" and ")} can only read the notes until you give them folders.</div>}
      </div>
      <div className="rv">
        <h2 className="row">Teammates<span className="spacer" /><Edit step={5} go={go} /></h2>
        {invited.map((x, i) => (
          <div style={{ padding: "4px 0", fontSize: 13.5 }} key={i}>
            <b>{x.user_id ? people.find((o) => o.id === x.user_id)?.name : x.email}</b>{" "}
            <span className="hint">{x.role === "admin" ? "Admin" : "Member, can see " + ([...x.manager_keys.map((k) => name(k) + "’s team"), ...(x.independent_staff ? ["shared staff"] : [])].join(", ") || "nothing yet")}{x.user_id ? "" : " · will get an invitation email"}</span>
          </div>
        ))}
        {!invited.length && <p className="hint">Just you for now.</p>}
      </div>
      <div className="rv">
        <h2 className="row">First goal<span className="spacer" /><Edit step={6} go={go} /></h2>
        {p.first_goal?.title.trim() ? <><b>{p.first_goal.title}</b> <span className="hint">for {name(p.first_goal.manager_key)}</span></> : <p className="hint">No goal yet. You can add one after.</p>}
      </div>
    </>
  );
}
