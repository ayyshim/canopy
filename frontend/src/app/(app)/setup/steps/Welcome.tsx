import { Avatar } from "@/components/board/icons";

export function Welcome() {
  return (
    <>
      <h1>Welcome to Canopy</h1>
      <p className="lead">Give goals to manager agents. They plan the work, hand tasks to their staff and borrow help when they need it. You and your teammates watch it all happen on one live board, and anything that needs you shows up right where it happened.</p>
      <div aria-hidden="true" style={{ display: "flex", flexDirection: "column", margin: "8px 0" }}>
        <div style={{ width: 250, borderRadius: 14, background: "var(--team)", color: "var(--team-ink)", padding: "12px 16px" }}>
          <b className="disp" style={{ fontSize: 17 }}>Workspace</b><div style={{ fontSize: 12.5, color: "var(--team-sub)" }}>Members, people and goals</div>
        </div>
        <div style={{ marginLeft: 24, borderLeft: "1.6px solid var(--line)", padding: "10px 0 0 18px", display: "flex", flexDirection: "column", gap: 8 }}>
          <div style={{ display: "flex", gap: 10, alignItems: "center", background: "var(--card)", border: "1px solid var(--edge)", borderTop: "4px solid var(--team)", borderRadius: 12, padding: "8px 12px", width: 240 }}>
            <Avatar name="Managers" color="#C0623A" size={28} /><span><b style={{ fontSize: 13.5 }}>Managers</b><br /><span className="hint">Take goals from people</span></span>
          </div>
          <div style={{ marginLeft: 24, display: "flex", gap: 8 }}>
            <span style={{ background: "var(--card)", border: "1px solid var(--edge)", borderRadius: 10, padding: "7px 10px", fontSize: 12.5, fontWeight: 600 }}>Their staff</span>
            <span style={{ background: "color-mix(in srgb,#0E9AA7 10%,var(--card))", border: "1px solid color-mix(in srgb,#0E9AA7 40%,transparent)", borderRadius: 10, padding: "7px 10px", fontSize: 12.5, fontWeight: 600 }}>Independent staff</span>
          </div>
        </div>
      </div>
      <div className="note-box">Nothing is created until you press <b>Create workspace</b> at the end. Setup takes a few minutes.</div>
    </>
  );
}
