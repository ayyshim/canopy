// Sets up a demo org and workspace over the API, pairs a machine that runs fakeclaude, then
// records the board in a browser while a goal runs. Writes <out>/board.webm.
import { chromium } from "playwright";
import { spawn, execFileSync } from "node:child_process";
import fs from "node:fs";
import path from "node:path";
import os from "node:os";

const { BASE, CANOPY_BIN, FAKECLAUDE_BIN, WORK, OUT } = process.env;
const env = {
  ...process.env,
  HOME: path.join(WORK, "home"),
  CANOPY_HOME: path.join(WORK, "home", "config"),
  CANOPY_DATA: path.join(WORK, "home", "data"),
  CANOPY_CLAUDE_BIN: FAKECLAUDE_BIN,
  FAKECLAUDE_STATE: path.join(WORK, "fakeclaude-state"),
  XDG_DATA_HOME: "",
  XDG_CONFIG_HOME: "",
};
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

const setupCtx = await (await chromium.launch()).newContext({ baseURL: BASE });
const api = setupCtx.request;
let csrf = "";
async function call(method, url, data) {
  const r = await api.fetch(url, { method, data, headers: { "X-CSRF-Token": csrf } });
  if (!r.ok()) throw new Error(`${method} ${url}: ${r.status()} ${await r.text()}`);
  return r.status() === 204 ? {} : r.json();
}
csrf = (await call("GET", "/api/v1/auth/csrf")).token;

await call("POST", "/api/v1/hub/setup", {
  token: "record-board",
  hub: { name: "Demo hub" },
  admin: { name: "Hub Admin", email: "admin@demo.test", password: "demo-pass-12345" },
});
const me = await call("POST", "/api/v1/auth/signup", {
  org: { name: "Acme", size: "2_10" },
  user: { name: "Riley Owner", email: "riley@demo.test", password: "demo-pass-12345", accept_terms: true },
});
const org = me.current_org_id;

const { code } = await call("POST", `/api/v1/orgs/${org}/runners/pairing`, { name: "Demo laptop", kind: "computer" });
execFileSync(CANOPY_BIN, ["pair", "--hub", BASE, "--code", code], { env, stdio: "inherit" });
const runner = spawn(CANOPY_BIN, ["run", "--foreground", "--sandbox", "none", "--unsafe-no-sandbox"], { env, stdio: "inherit" });
process.on("exit", () => runner.kill("SIGINT"));

let runnerId = "";
for (let i = 0; i < 100 && !runnerId; i++) {
  const { runners } = await call("GET", `/api/v1/orgs/${org}/runners`);
  runnerId = runners.find((r) => r.name === "Demo laptop" && r.online)?.id ?? "";
  if (!runnerId) await sleep(200);
}
if (!runnerId) throw new Error("the machine never came online");

const code1 = path.join(WORK, "payments-api");
fs.mkdirSync(code1, { recursive: true });
fs.writeFileSync(path.join(code1, "main.go"), "package main\n");
const { workspace } = await call("POST", `/api/v1/orgs/${org}/workspaces`, {
  workspace: { name: "Payments", goal_completion: "auto" },
  runner_id: runnerId,
  directories: [{ key: "code", path: code1 }],
  clis: [{ cli: "claude", enabled: true, mode: "subscription" }],
  members: [
    { key: "m", name: "Riley", kind: "manager", cli: "claude", all_directories: true },
    { key: "d", name: "Dev", kind: "staff", skill: "developer", reports_to_key: "m", cli: "claude",
      grants: [{ directory_key: "code", mode: "read_write", source: "suggested" }] },
    { key: "w", name: "Wren", kind: "staff", skill: "writer", reports_to_key: "m", cli: "claude" },
  ],
});
const ws = workspace.id;
const board = () => call("GET", `/api/v1/workspaces/${ws}/board`);
const manager = (await board()).members.find((m) => m.name === "Riley").id;

// Record in a fresh context that shares the signed-in cookies.
const storage = await setupCtx.storageState();
const W = 1100, H = 640;
const browser = await chromium.launch();
const ctx = await browser.newContext({
  baseURL: BASE, storageState: storage, viewport: { width: W, height: H },
  recordVideo: { dir: OUT, size: { width: W, height: H } },
});
const page = await ctx.newPage();
// Session cards show this computer's host name; swap it for a demo one.
await page.addInitScript((host) => {
  const fix = (n) => { if (n.nodeValue?.includes(host)) n.nodeValue = n.nodeValue.replaceAll(host, "demo-laptop"); };
  new MutationObserver((ms) => ms.forEach((m) => {
    if (m.type === "characterData") fix(m.target);
    m.addedNodes.forEach((n) => { const w = document.createTreeWalker(n, NodeFilter.SHOW_TEXT); for (let t = w.currentNode; t; t = w.nextNode()) fix(t); });
  })).observe(document, { subtree: true, childList: true, characterData: true });
}, os.hostname());
await page.goto(`/w/${ws}`);
await page.waitForLoadState("networkidle");
await page.getByRole("button", { name: "Got it" }).click({ timeout: 3000 }).catch(() => {});
await sleep(2000);

await call("POST", `/api/v1/workspaces/${ws}/goals`, { manager_id: manager, title: "Ship the CSV export [tasks:3] [ask] [slow:2]" });

// Answer each question a few seconds after it appears, until the goal is done.
const answered = new Set();
for (let i = 0; i < 240; i++) {
  const b = await board();
  if (answered.size && b.goals.every((g) => g.status === "done")) break;
  const q = b.signals.find((s) => s.type === "question" && !answered.has(s.id));
  if (q) {
    answered.add(q.id);
    await sleep(3500);
    const option = q.payload.options?.[0];
    const label = typeof option === "string" ? option : option?.label ?? option?.text ?? "Option A";
    await call("POST", `/api/v1/signals/${q.id}/resolve`, { action: "answer", text: label });
  }
  await sleep(500);
}
await sleep(3500);

const video = page.video();
await ctx.close();
fs.renameSync(await video.path(), path.join(OUT, "board.webm"));
await browser.close();
runner.kill("SIGINT");
process.exit(0);
