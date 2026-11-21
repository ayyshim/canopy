// Board geometry (SPEC "Layout rules"): the workspace card on top with its goals beside it,
// managers in a row below with their staff to the right, independent staff last. Each member's
// column holds its tasks (with outputs and questions), approvals and its sessions.
import type { Board, GoalView, MemberView, OutputView, SessionView, SignalView, TaskView } from "../types";
import { isHidden } from "../types";
import { blockedOnPerson, hiddenTask, independentStaff, isAdmin, managers, outputsOf, staffOf, task } from "./derive";

export const GEO = { teamW: 290, teamH: 136, agW: 240, agH: 84, colW: 320, cx: 36, cw: 220, gap: 14, fruit: 50 };
const SIZE = { task: 64, sess: 58, fork: 150, gate: 118, errc: 118, acct: 140 };

export type Node =
  | { id: string; type: "ws"; x: number; y: number; w: number; h: number }
  | { id: string; type: "goal"; g: GoalView; x: number; y: number; w: number; h: number }
  | { id: string; type: "addgoal" | "note" | "dirs"; x: number; y: number; w: number; h: number }
  | { id: string; type: "acct"; s: SignalView; x: number; y: number; w: number; h: number }
  | { id: string; type: "member"; m: MemberView | null; memberId: string; mgr: boolean; x: number; y: number; w: number; h: number }
  | { id: string; type: "pool"; n: number; x: number; y: number; w: number; h: number }
  | { id: string; type: "addm"; mode: "manager" | "staff" | "pool"; boss?: MemberView; x: number; y: number; w: number; h: number }
  | { id: string; type: "session"; m: MemberView; s: SessionView; x: number; y: number; w: number; h: number }
  | { id: string; type: "task"; t: TaskView; x: number; y: number; w: number; h: number }
  | { id: string; type: "fruit"; o: OutputView; taskId: string; x: number; y: number; w: number; h: number }
  | { id: string; type: "more"; n: number; taskId: string; x: number; y: number; w: number; h: number }
  | { id: string; type: "sig"; kind: "fork" | "gate" | "errc"; s: SignalView; taskId?: string; x: number; y: number; w: number; h: number };

export type Edge = { d: string; cls?: string; stroke?: string; opacity?: number; marker?: "vi" | "red" };
export type Layout = { nodes: Node[]; byId: Record<string, Node>; edges: Edge[]; box: { x0: number; y0: number; x1: number; y1: number } };

const GATE_LIKE = ["gate", "errc"];
const sessionOrder: Record<string, number> = { crashed: 0, error: 0, running: 1, starting: 1, gate: 1, review: 1, waiting: 2, asleep: 3 };

/** layout places every node. heights holds measured heights of signal cards (they vary). */
export function layout(b: Board, heights: Record<string, number>, focusGoal: string | null): Layout {
  const nodes: Node[] = [];
  const byId: Record<string, Node> = {};
  const edges: Edge[] = [];
  const add = <N extends Node>(n: N): N => {
    nodes.push(n);
    byId[n.id] = n;
    return n;
  };
  const canAdd = isAdmin(b);
  const mgrs = managers(b);
  const pool = independentStaff(b);

  // Workspace card, its goals beside it, the summary note and the directories card.
  const ws = add({ id: "__ws", type: "ws", x: 0, y: 0, w: GEO.teamW, h: GEO.teamH });
  const gx = GEO.teamW + 40;
  let gy = 0;
  // Done goals lose their card; their tasks stay hidden unless show_tasks is on (hiddenTask).
  const goals = b.goals.filter((g) => g.status !== "done");
  for (const g of goals) {
    add({ id: g.id, type: "goal", g, x: gx, y: gy, w: 236, h: 60 });
    gy += 68;
  }
  const addGoal = add({ id: "__addgoal", type: "addgoal", x: gx, y: gy, w: 236, h: 40 });
  add({ id: "__note", type: "note", x: gx + 262, y: 0, w: 190, h: 80 });
  const dirs = add({ id: "__dirs", type: "dirs", x: gx + 262, y: 96, w: 236, h: 36 + Math.max(1, b.directories.length) * 25 });

  // Account cards (usage limits, caps) and conflicts not tied to a task, left of the workspace.
  let ly = 0;
  const accts: Node[] = [];
  for (const s of b.signals) {
    if (s.type === "usage_limit" || s.type === "cap") {
      accts.push(add({ id: "acct_" + s.id, type: "acct", s, x: -266, y: ly, w: 236, h: SIZE.acct }));
      ly += SIZE.acct + 16;
    } else if (s.type === "sync_conflict" && !s.task_id) {
      const h = heights[s.id] ?? 150;
      add({ id: s.id, type: "sig", kind: "gate", s, x: -266, y: ly, w: 236, h });
      ly += h + 16;
    }
  }
  const L1 = Math.max(GEO.teamH + 70, gy + 100, dirs.y + dirs.h + 50);
  const L2 = L1 + GEO.agH + 70;

  const kids: Record<string, Node[]> = {};
  const column = (m: MemberView | null, id: string, x: number, y0: number) => {
    const list: Node[] = (kids[id] = []);
    if (!m) return;
    let y = y0;
    for (const t of b.tasks.filter((z) => z.member_id === id && !hiddenTask(b, z))) {
      const tn = add({ id: t.id, type: "task", t, x: x + GEO.cx, y, w: GEO.cw, h: SIZE.task });
      list.push(tn);
      y += SIZE.task + GEO.gap;
      const outs = outputsOf(b, t.id);
      outs.slice(0, 4).forEach((o, k) => {
        const more = k === 3 && outs.length > 4;
        const pos = { x: tn.x + 14 + k * 46, y: tn.y + tn.h + 10, w: 40, h: 40, taskId: t.id };
        if (more) add({ id: "more_" + t.id, type: "more", n: outs.length - 3, ...pos });
        else add({ id: o.id, type: "fruit", o, ...pos });
        edges.push({ cls: "e-stem", d: `M${pos.x + 20},${tn.y + tn.h} V${pos.y}` });
      });
      if (outs.length) y += GEO.fruit;
      for (const s of b.signals.filter((z) => z.task_id === t.id && !["usage_limit", "cap", "goal_ready"].includes(z.type))) {
        const kind = s.type === "question" ? "fork" : s.type === "error" ? "errc" : "gate";
        const h = heights[s.id] ?? SIZE[kind];
        const sn = add({ id: s.id, type: "sig", kind, s, taskId: t.id, x: x + GEO.cx, y, w: GEO.cw, h });
        if (kind === "fork") edges.push({ cls: "e-ask", d: `M${tn.x + 190},${tn.y + tn.h} V${sn.y}` });
        else list.push(sn);
        y += h + GEO.gap;
      }
    }
    // Items from a chat, not a task (such as a chat that stopped on an error), sit under the agent.
    for (const s of b.signals.filter((z) => z.member_id === id && !z.task_id && !["usage_limit", "cap", "goal_ready", "sync_conflict"].includes(z.type))) {
      const kind = s.type === "question" ? "fork" : s.type === "error" ? "errc" : "gate";
      const h = heights[s.id] ?? SIZE[kind];
      list.push(add({ id: s.id, type: "sig", kind, s, x: x + GEO.cx, y, w: GEO.cw, h }));
      y += h + GEO.gap;
    }
    const sessions = b.sessions
      .filter((s) => s.member_id === id && s.status !== "ended")
      .sort((a, c) => (sessionOrder[a.status] ?? 4) - (sessionOrder[c.status] ?? 4) || c.last_active_at.localeCompare(a.last_active_at))
      .slice(0, 2);
    for (const s of sessions) {
      list.push(add({ id: "ses_" + s.id, type: "session", m, s, x: x + GEO.cx, y, w: GEO.cw, h: SIZE.sess }));
      y += SIZE.sess + GEO.gap;
    }
  };
  const memberNode = (id: string, x: number, y: number, mgr: boolean) => {
    const raw = b.members.find((z) => z.id === id);
    const m = raw && !isHidden(raw) ? (raw as MemberView) : null;
    add({ id, type: "member", m, memberId: id, mgr, x, y, w: GEO.agW, h: GEO.agH });
    column(m, id, x, y + GEO.agH + 24);
  };

  let X = 0;
  const top: [Node, boolean][] = [];
  for (const mg of mgrs) {
    memberNode(mg.id, X, L1, true);
    top.push([byId[mg.id], false]);
    const staff = staffOf(b, mg.id);
    staff.forEach((s, i) => memberNode(s.id, X + (i + 1) * GEO.colW, L2, false));
    const boss = isHidden(mg) ? undefined : (mg as MemberView);
    if (canAdd && boss) add({ id: "add_" + mg.id, type: "addm", mode: "staff", boss, x: X + (staff.length + 1) * GEO.colW, y: L2, w: 180, h: GEO.agH });
    X += (staff.length + 1) * GEO.colW + 180 + 100;
  }
  const poolNode = add({ id: "__pool", type: "pool", n: pool.length, x: X, y: L1, w: GEO.agW, h: GEO.agH });
  top.push([poolNode, false]);
  pool.forEach((s, i) => memberNode(s.id, X + i * GEO.colW, L2, false));
  const addPool = canAdd ? add({ id: "__addind", type: "addm", mode: "pool", x: X + pool.length * GEO.colW, y: L2, w: 180, h: GEO.agH }) : null;
  X += Math.max(pool.length * GEO.colW + 180, GEO.agW) + 100;
  if (canAdd) top.push([add({ id: "__addmgr", type: "addm", mode: "manager", x: X, y: L1, w: 180, h: GEO.agH }), true]);

  // Edges: the workspace bus down to managers and the pool, manager → staff branches.
  const branch = (dashed: boolean) => (dashed ? "e-add" : "e-tree");
  const bus = (from: Node, targets: [Node, boolean][]) => {
    const fx = from.x + from.w / 2, fb = from.y + from.h;
    for (const [q, dashed] of targets) {
      const by = fb + (q.y - fb) / 2;
      edges.push({ cls: branch(dashed), d: `M${fx},${fb} V${by} H${q.x + q.w / 2} V${q.y}` });
    }
  };
  bus(ws, top);
  for (const mg of mgrs) {
    const q = byId[mg.id], mx = q.x + q.w, my = q.y + q.h / 2;
    const targets: [Node, boolean][] = staffOf(b, mg.id).map((s) => [byId[s.id], false]);
    if (byId["add_" + mg.id]) targets.push([byId["add_" + mg.id], true]);
    for (const [s2, dashed] of targets) {
      const cx = s2.x + s2.w / 2;
      edges.push({ cls: branch(dashed), d: `M${mx},${my} H${cx - 8} Q${cx},${my} ${cx},${my + 8} V${s2.y}` });
    }
  }
  const poolTargets: [Node, boolean][] = pool.map((s) => [byId[s.id], false]);
  if (addPool) poolTargets.push([addPool, true]);
  bus(poolNode, poolTargets);

  // Borrowed independent staff: a dotted arc from the manager whose goal they help.
  const borrows = new Set<string>();
  for (const t of b.tasks) {
    if (!t.borrowed || t.status === "done") continue;
    const g = b.goals.find((x) => x.id === t.goal_id);
    const key = `${g?.manager_id}>${t.member_id}`;
    if (!g?.manager_id || !t.member_id || borrows.has(key)) continue;
    borrows.add(key);
    const a = byId[g.manager_id], c = byId[t.member_id];
    if (!a || !c) continue;
    const sx = a.x + a.w / 2, sy = a.y + a.h, ex = c.x + 30, ey = c.y;
    edges.push({ cls: "e-borrow", d: `M${sx},${sy} C${sx},${sy + 120} ${ex},${ey - 120} ${ex},${ey - 4}` });
  }
  for (const a of accts) edges.push({ cls: "e-ask", d: `M${a.x + a.w},${a.y + a.h / 2} C${a.x + a.w + 15},${a.y + a.h / 2} ${ws.x - 15},${ws.y + ws.h / 2} ${ws.x},${ws.y + ws.h / 2}` });
  {
    const tx = ws.x + ws.w, ty = ws.y + ws.h / 2, mx = tx + 20;
    for (const g of goals) {
      const q = byId[g.id];
      edges.push({ d: `M${tx},${ty} H${mx} V${q.y + q.h / 2} H${q.x}`, stroke: g.color, opacity: 0.7 });
    }
    edges.push({ cls: "e-add", d: `M${tx},${ty} H${mx} V${addGoal.y + addGoal.h / 2} H${addGoal.x}` });
  }
  // A trunk down each member's column with a curved branch to each item; approvals sit on it.
  for (const id of Object.keys(kids)) {
    const p = byId[id], list = kids[id];
    if (!p || !list.length) continue;
    const tx = p.x + 20, y0 = p.y + p.h;
    let last = y0;
    for (const q of list) {
      if (q.type === "sig" && GATE_LIKE.includes(q.kind)) {
        last = Math.max(last, q.y + 25);
        continue;
      }
      const cy = q.y + q.h / 2;
      edges.push({ cls: "e-tree", d: `M${tx},${cy - 8} Q${tx},${cy} ${tx + 8},${cy} H${q.x}` });
      last = Math.max(last, cy - 8);
    }
    edges.push({ cls: "e-tree", d: `M${tx},${y0} V${last}` });
  }
  // Dependencies: violet dashed; solid red while the task it waits on needs a person.
  for (const t of b.tasks) {
    for (const did of t.depends_on) {
      const s = byId[did], e = byId[t.id], dt = task(b, did);
      if (!s || !e || s.type !== "task" || !dt || dt.kind === "plan" || (dt.status === "done" && t.status === "done")) continue;
      const red = t.status === "waiting" && dt.status !== "done" && blockedOnPerson(b, dt);
      const sy = s.y + s.h / 2, ey = e.y + e.h / 2;
      let d: string;
      if (e.x > s.x + s.w + 20) d = `M${s.x + s.w},${sy} C${s.x + s.w + 70},${sy} ${e.x - 70},${ey} ${e.x - 2},${ey}`;
      else if (e.x + e.w < s.x - 20) d = `M${s.x},${sy} C${s.x - 70},${sy} ${e.x + e.w + 70},${ey} ${e.x + e.w + 2},${ey}`;
      else {
        const k = Math.max(s.x + s.w, e.x + e.w) + 60;
        d = `M${s.x + s.w},${sy} C${k},${sy} ${k},${ey} ${e.x + e.w + 2},${ey}`;
      }
      const opacity = focusGoal && t.goal_id !== focusGoal ? 0.18 : dt.status === "done" ? 0.45 : undefined;
      edges.push({ cls: red ? "e-red" : "e-dep", d, marker: red ? "red" : "vi", opacity });
    }
  }
  let x0 = 1e9, y0 = 1e9, x1 = -1e9, y1 = -1e9;
  for (const n of nodes) {
    x0 = Math.min(x0, n.x - 40);
    y0 = Math.min(y0, n.y);
    x1 = Math.max(x1, n.x + n.w);
    y1 = Math.max(y1, n.y + n.h);
  }
  return { nodes, byId, edges, box: { x0, y0, x1, y1 } };
}
