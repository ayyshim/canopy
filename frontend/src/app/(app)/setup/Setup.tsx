"use client";

import Link from "next/link";
import { useCallback, useEffect, useRef, useState } from "react";
import { Brand } from "@/components/board/icons";
import { api, ApiError, currentOrg, hardNavigate, isOrgAdmin } from "@/lib/api";
import { useMe } from "@/lib/me";
import { emptyPayload, type PickedDir, type SetupPayload } from "@/lib/setup/state";
import type { CLIInfo, Computer, WorkspaceSummary } from "@/lib/types";
import { ComputerStep } from "./steps/ComputerStep";
import { FirstGoal } from "./steps/FirstGoal";
import { Members, seedMembers, tidyGrants } from "./steps/Members";
import { People, type OrgPerson } from "./steps/People";
import { Review } from "./steps/Review";
import { Welcome } from "./steps/Welcome";
import { WorkspaceStep } from "./steps/WorkspaceStep";

export const STEPS = ["Welcome", "Your computer", "Name your workspace", "Your team", "Invite people", "First goal", "Check and create"];

/** What every step gets: the payload being built and helpers to change it. */
export type StepProps = {
  p: SetupPayload;
  set: (fn: (p: SetupPayload) => SetupPayload) => void;
  dirs: PickedDir[];
  setDirs: (d: PickedDir[]) => void;
  computer: Computer | null;
  setComputer: (c: Computer | null) => void;
  clis: CLIInfo[];
  people: OrgPerson[];
  errors: Record<string, string>;
  orgId: string;
  orgName: string;
  go: (step: number) => void;
};

/** Workspace setup (SPEC "Workspace setup", DECISIONS "Workspace setup order"). Nothing is
 * created until Create workspace; the POST carries exactly what the review page shows. */
export function Setup({ later = false }: { later?: boolean }) {
  const { me } = useMe();
  const org = currentOrg(me);
  const [step, setStep] = useState(0);
  const [done, setDone] = useState<Set<number>>(new Set());
  const [p, setP] = useState<SetupPayload>(emptyPayload);
  const [dirs, setDirList] = useState<PickedDir[]>([]);
  const [computer, setComputer] = useState<Computer | null>(null);
  const [clis, setClis] = useState<CLIInfo[]>([]);
  const [people, setPeople] = useState<OrgPerson[]>([]);
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [busy, setBusy] = useState(false);
  const [loaded, setLoaded] = useState(false);
  const main = useRef<HTMLElement>(null);

  useEffect(() => {
    if (!org || !isOrgAdmin(org.role)) return;
    (async () => {
      const [ws, c, r, pp] = await Promise.all([
        api<{ workspaces: WorkspaceSummary[] }>("GET", `/orgs/${org.id}/workspaces`),
        api<{ clis: CLIInfo[] }>("GET", "/clis"),
        api<{ runners: Computer[]; my_computer: string | null }>("GET", `/orgs/${org.id}/runners`),
        api<{ people: OrgPerson[] }>("GET", `/orgs/${org.id}/people`),
      ]);
      const mine = r.runners.find((x) => x.id === r.my_computer) ?? r.runners.find((x) => x.owner_user_id === me.user.id) ?? null;
      setClis(c.clis);
      setPeople(pp.people.filter((x) => x.id !== me.user.id));
      setComputer(mine);
      setP((x) => seedMembers({ ...x, runner_id: mine?.id ?? null, clis: [{ cli: "claude", enabled: true, mode: "subscription", cap_tokens_per_day: null, api_key: "" }] }));
      // Later workspaces skip the welcome, and the computer step when one is already connected.
      if (later || ws.workspaces.length > 0) {
        setStep(mine?.online ? 2 : 1);
        setDone(new Set(mine?.online ? [0, 1] : [0]));
      }
      setLoaded(true);
    })().catch(() => setLoaded(true));
  }, [org, me.user.id, later]);

  const set = useCallback((fn: (p: SetupPayload) => SetupPayload) => setP((x) => tidyGrants(fn(x), dirs)), [dirs]);
  // Picking folders also updates the payload's directory list and the members' grants.
  const setDirs = useCallback((d: PickedDir[]) => {
    setDirList(d);
    setP((x) => tidyGrants({ ...x, directories: d.map(({ key, path, name, kind }) => ({ key, path, name, kind })) }, d));
  }, []);

  if (!org) return null;
  if (!isOrgAdmin(org.role)) {
    return (
      <div className="center-page"><div className="empty card"><h1>Only admins can create workspaces</h1><p className="muted">Ask an admin to create one and add you to it.</p><Link className="btn" href="/board">Back</Link></div></div>
    );
  }

  const problem = (i: number): Record<string, string> => {
    const e: Record<string, string> = {};
    if (i === 1 && !computer?.online) e.computer = "Connect your computer first.";
    if (i === 1) {
      const c = p.clis.find((x) => x.cli === "claude");
      if (c?.mode === "api_key" && !c.api_key.trim()) e["clis.0.api_key"] = "Paste the key, or switch back to your Claude subscription.";
    }
    if (i === 2 && !p.workspace.name.trim()) e["workspace.name"] = "Give the workspace a name.";
    if (i === 3) {
      if (!p.members.some((m) => m.kind === "manager")) e.members = "Add at least one manager.";
      p.members.forEach((m, k) => { if (!m.name.trim()) e[`members.${k}.name`] = "Give everyone a name."; });
    }
    if (i === 4) p.people.forEach((x, k) => { if (!x.user_id && x.email && !/^\S+@\S+\.\S+$/.test(x.email)) e[`people.${k}.email`] = "That email doesn’t look right."; });
    return e;
  };
  const goTo = (i: number) => {
    setErrors({});
    setStep(i);
    main.current?.scrollTo?.(0, 0);
  };
  const next = () => {
    const e = problem(step);
    setErrors(e);
    if (Object.keys(e).length) return;
    setDone((d) => new Set(d).add(step));
    goTo(step + 1);
  };
  const create = async () => {
    setBusy(true);
    setErrors({});
    try {
      const body = {
        ...p,
        people: p.people.filter((x) => x.user_id || x.email.trim()).map(({ user_id, email, role, manager_keys, independent_staff }) => ({ user_id, email, role, manager_keys, independent_staff })),
        members: p.members.map(({ grants_by: _g, ...m }) => (void _g, m)),
        clis: computer ? p.clis : [],
        first_goal: p.first_goal?.title.trim() ? p.first_goal : null,
      };
      const r = await api<{ workspace: { id: string } }>("POST", `/orgs/${org.id}/workspaces`, body);
      hardNavigate(`/w/${r.workspace.id}`);
    } catch (e) {
      setBusy(false);
      if (e instanceof ApiError && Object.keys(e.fields).length) {
        setErrors(e.fields);
        const f = Object.keys(e.fields)[0];
        const back = f.startsWith("workspace") || f.startsWith("directories") ? 2 : f.startsWith("clis") ? 1 : f.startsWith("members") ? 3 : f.startsWith("people") ? 4 : f.startsWith("first_goal") ? 5 : 6;
        setStep(back);
      } else setErrors({ create: e instanceof ApiError ? e.message : "Something went wrong. Please try again." });
    }
  };

  const props: StepProps = { p, set, dirs, setDirs, computer, setComputer, clis, people, errors, orgId: org.id, orgName: org.name, go: goTo };
  const Body = [Welcome, ComputerStep, WorkspaceStep, Members, People, FirstGoal, Review][step];
  const logout = async () => { await api("POST", "/auth/logout").catch(() => {}); hardNavigate("/login"); };

  return (
    <div className="onb">
      <aside className="onb-rail">
        <span className="brand"><Brand size={26} /><b>Canopy</b></span>
        <ol className="steps">
          {STEPS.map((s, k) => {
            const ok = done.has(k) && k !== step;
            return (
              <li key={s}>
                <button aria-current={k === step ? "step" : "false"} disabled={k > step && !done.has(k - 1)} onClick={() => goTo(k)}>
                  <span className={"sn" + (ok ? " ok" : "")}>{ok ? "✓" : k + 1}</span>{s}
                </button>
              </li>
            );
          })}
        </ol>
        <span className="grow" />
        <div className="row wrap" style={{ gap: 6, marginBottom: 12 }}>
          <Link className="btn btn-ghost btn-sm" href="/board">Cancel</Link>
          <button className="btn btn-ghost btn-sm" onClick={logout}>Log out</button>
        </div>
        <p className="fine">Your passwords and keys stay on your computer. Canopy never sees them.</p>
      </aside>
      <main className="onb-main" ref={main}>
        <div className="onb-body">{loaded ? <Body {...props} /> : <p className="hint">Loading…</p>}</div>
        <footer className="onb-foot">
          {step > 0 ? <button className="btn" onClick={() => goTo(step - 1)}>Back</button> : <span />}
          <div className="row">
            {errors.create && <span className="err">{errors.create}</span>}
            {step === 4 && <button className="btn btn-ghost" onClick={() => { set((x) => ({ ...x, people: [] })); setDone((d) => new Set(d).add(4)); goTo(5); }}>Skip for now</button>}
            {step === 5 && <button className="btn btn-ghost" onClick={() => { set((x) => ({ ...x, first_goal: null })); setDone((d) => new Set(d).add(5)); goTo(6); }}>Skip for now</button>}
            {step === 6 ? <button className="btn btn-primary" disabled={busy} onClick={create}>{busy ? "Creating…" : "Create workspace"}</button>
              : <button className="btn btn-primary" onClick={next} disabled={step === 1 && !computer?.online} title={step === 1 && !computer?.online ? "Connect your computer first" : undefined}>
                {step === 0 ? "Get started" : step === 5 ? "Check everything" : "Continue"}</button>}
          </div>
        </footer>
      </main>
    </div>
  );
}
