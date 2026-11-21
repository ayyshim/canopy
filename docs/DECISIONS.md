# Decisions that override or extend SPEC.md

Where this file and `docs/SPEC.md` disagree, this file wins. Newest decisions at the bottom of each
section; record the date when one is added.

## Stack (2026-11-19)

| Spec package | Here | Tech |
| --- | --- | --- |
| apps/api | `hub/` | Go: chi, pgx v5 + sqlc, goose migrations, coder/websocket, go-redis, aws-sdk-go-v2 (S3) |
| apps/runner | `client/` | Go: one binary `canopy`, Claude Code adapter, sandbox, directory sync |
| apps/web | `frontend/` | Next.js (App Router, TypeScript), Zustand |
| packages/shared | `shared/` | Go module for the runner protocol (hub and client import it); `openapi.yaml` and JSON Schemas for REST and board events, generated into TypeScript |
| packages/ui | `frontend/src/ui` | Design tokens as CSS variables, components |
| Infra | `deploy/` | Postgres 16, Redis 7, S3-compatible storage (RustFS), SMTP (Mailpit in dev), Caddy as the single origin |

- One origin: Caddy sends `/api/*` and `/ws/*` to the hub and everything else to Next.js, so the
  session cookie is first-party.
- Deployment: the same build runs self-hosted and as a hosted multi-organization service.

## Agents (2026-11-19)

- v1 supports **Claude Code only**, signed in with a subscription or an Anthropic API key. Codex,
  Gemini CLI and Pi come later through the same adapter interface.
- Claude Code runs as `claude -p --input-format stream-json --output-format stream-json`. Each
  session gets a Canopy MCP server hosted by the runner for permission prompts (gates), questions,
  directory requests, owner-change requests and outputs.

## Directories (2026-11-19) — answers SPEC open question 1

**Hub mirror.** Each shared directory is mirrored in the hub: owner machine ⇄ hub copy ⇄ the
workspace runner.

- The owner's client watches the directory and uploads changed files only. It honors
  `.gitignore`, `.dockerignore` and `.canopyignore` (nested too), skips build output, never uploads
  common secret files (`.env*`, `*.pem`, `id_*`, keys) and caps file size.
- Files are stored by content hash (object storage) with a per-directory file tree in Postgres.
- Runners materialize directories from the hub copy, so **reads never need the owner online**. This
  replaces the spec's "members that need it pause with Directory unavailable" for reads.
- The owner's disk stays the source of truth. Writes made elsewhere go to the hub; if the owner is
  offline they **queue** and are applied on reconnect after checking each file's base hash. A
  mismatch raises a conflict for the owner to resolve.

**Person-level access.** On top of the spec's per-agent `DirectoryGrant`, a directory has per-person
access (`DirectoryAccess`: read or read_write). The owner always has read_write.

- Assumed default: everyone with access to the workspace can read; write must be granted by the
  owner or a workspace admin.
- An agent's effective access for a task = its `DirectoryGrant` ∩ the access of the person the
  task is attributed to. Sandbox mounts are computed per task.
- If that person lacks write on a directory the task must change, the agent creates a task for a
  member that can write there (spec assignment order) and an **owner approval** item appears on the
  owner's part of the tree. The requesting task waits on it (red "waiting on a person" arrow).
  Approve runs it; Deny makes the requester's agent work around it.
- When a member's next task is attributed to a person who can't see a directory its current
  conversation touched, the member starts a fresh CLI conversation instead of resuming.

## Attribution (2026-11-19)

- Amendment to the spec's attribution rule: work a directory owner approves inside their own
  directory is attributed to **the owner**, not the person who requested it.

## Limits (2026-11-19)

- Default per-person limits for new organizations: 2,000,000 tokens per 5-hour session window and
  20,000,000 tokens per week. Admins change them in organization settings.
- Auth rate limiting (10 per minute per IP) uses Redis when `REDIS_URL` is set and an in-process
  limiter otherwise.

## Hubs (2026-11-19)

- A **hub** is one self-hosted Canopy server (hub + database + web app). Each company runs its own.
- **First-run setup comes before everything else.** On first start the hub prints a one-time setup
  link (`/hub-setup?token=…`, or set `HUB_SETUP_TOKEN`). Setup names the hub, chooses sign-in
  options (GitHub and Google OAuth credentials are stored in the hub's database, not env vars) and
  creates the first **hub admin**. Until then every other API returns `setup_required`.
- Only after setup can organizations register (SPEC "Sign up"). Hub admins can close sign-ups, so
  new people join only by invitation.
- **Hub admin dashboard** (`/admin`): every organization and user on the hub, hub settings
  (name, sign-ups, sign-in providers), hub admin roles (at least one must remain) and hub-wide
  totals. Hub-wide token usage and agent analytics join it once runners report usage (milestone 6).
- Being a hub admin does not make someone a member of any organization; organization data stays
  behind organization membership.
- The `canopy` client binary is the same for everyone and isn't tied to a hub: a person pairs it
  with their hub's URL and a one-time code (milestone 3).

## Deployment (2026-11-19)

Two deliverables, built by `make release` (`scripts/release.sh`) for linux and macOS on amd64 and
arm64:

- **`canopy-hub`**: the API, the web app (the Next.js static export embedded with `go:embed`),
  the runner endpoint, client downloads with `/install.sh`, and its own PostgreSQL 16 (bundled,
  started on a loopback port, data in `<data>/postgres/data`). Redis, S3 and Caddy are optional:
  one hub uses an in-memory event bus and stores files in `<data>/blobs`; `--domain` serves
  HTTPS with automatic certificates. `DATABASE_URL`, `REDIS_URL` and `S3_*` switch to external
  services.
- **`canopy`**: the client. The hub serves it at `/download/canopy-<os>-<arch>`; the setup
  wizard shows a one-line install command that downloads it, pairs it and starts it as a user
  service.

**Upgrading = replacing the binary.** On start the hub compares the schema its data was last
written with (`<data>/hub-state.json`) to the schema it carries:

- older data: the stopped database is copied to `<data>/backups/<time>-before-<version>`, the
  changes are applied, and the hub logs the versions, the list of changes and the backup path;
  hub admins see each upgrade on the dashboard.
- newer data (an older binary): the hub refuses to start and changes nothing.
- `--no-auto-migrate` refuses to start when changes are pending and lists them;
  `canopy-hub migrate status` shows what an upgrade would change; `canopy-hub migrate up` applies.
- The PostgreSQL major version is pinned (16). Data from another major version is refused, never
  re-initialized.
- With `DATABASE_URL`, the hub takes a `pg_dump` backup first when `pg_dump` is installed, and
  warns when it isn't.

Static export notes: dynamic pages (`/reset/<token>`, `/w/<id>`, `/admin/organizations/<id>`, …)
are exported once with a `_` placeholder; the hub maps real URLs to them and pages read the id
from the URL (`usePathSegment`). Cache Components stays on for the dev server only (static export
can't prerender partially).

## Orchestration (2026-11-19)

- The orchestrator runs in the **hub**, not on the runner as SPEC suggests: goals, tasks and
  signals live in the hub's database and people answer through it, so scheduling there keeps one
  source of truth and survives runner restarts. Runners own the CLI processes.
- A machine restart isn't a CLI crash: its sessions sleep and resume their task when it returns.
- Claude Code reports its subscription utilization (`rate_limit_event`: five-hour and weekly
  windows), so subscription accounts show utilization when the CLI provides it (SPEC open
  question 3).
- Turns are fed to a session one at a time; a chat message waits for the current turn to end.

## Workspace knowledge (2026-11-19)

- **No workspace folder.** This replaces the SPEC's workspace folder (Workspace setup step 2,
  `/settings/general`, Concepts). Plans, notes, decisions, summaries and outputs live in the hub:
  notes and decisions in Postgres, output files in object storage. Every member can read them even
  when no computer is on.
- Agents read and write them through the Canopy MCP tools (read notes, add a note or decision,
  submit an output). The runner may put a read-only snapshot of the notes into the sandbox for CLIs
  that work better with files; outputs upload to the hub instead of a folder watcher.
- Backend follow-up: a migration drops `workspaces.folder_path`; the orchestrator's mounts and
  prompts and the client's sandbox and output watcher stop using it.

## Workspace setup order (2026-11-19)

- **Connect your computer comes before Create a workspace.** The person's `canopy` client lists
  folders on their computer, so directories are picked from it (no free-typed paths that the hub
  can't check) and start syncing to the hub as soon as they're added. The same step installs the
  client and waits until the hub can reach it.
- Order: Welcome, Connect your computer, Create a workspace (name, about, directories), Connect
  your CLIs, Members and access, Invite people, First goal, Review. Later workspaces skip the first
  two when a computer is already connected. (Where agents run is no longer a workspace setting;
  see Where work runs.)

## Design system (2026-11-19)

- The UI follows the Canopy design prototype (claude.ai artifact `Qv5vgXvQUP9xVWDGkhQ813`): its
  color tokens for light and dark, Bricolage Grotesque / Inter / JetBrains Mono, 11px control
  radius, 14–16px card radius, the board's card shapes (team card, sticky-note tasks with a goal
  stripe, terminal-style session cards, question forks, approval cards with a lock on the trunk),
  the chat window, toasts, the split sign-in screen, the onboarding step rail and the settings
  pages with section navigation. `docs/prototype.html` carries the full token set and components.

## Where work runs (2026-11-19) — replaces one runner per workspace

One rule: **work runs on the computer of the person it's for, signed in with that person's own
CLI account.** The person it's for is the task's attributed person (see Attribution): whoever gave
the goal, whoever is chatting, and the directory owner for a change they approve.

- Riley's goals run on Riley's computer and use Riley's Claude subscription; Sam's run on Sam's.
- A change Bob approves in his own directory runs on Bob's computer, directly in the real
  directory: no copy and no queued write.
- Each person has one computer for their goals (the first they pair; changeable). Give a goal says
  where it runs and with whose sign-in.
- **Shared computer (optional).** An admin can mark a computer shared, typically a server signed in
  with an organization API key and a daily cap. Only a computer signed in with an API key can be
  shared: a subscription is personal and is never used for someone else's work. People without a computer of their own run their
  goals there. Nobody's personal computer runs someone else's work.
- No computer and no shared computer: the goal waits ("Connect your computer to start this goal").

**Agents are shared roles; sessions belong to a goal.** A member (name, role, skill, model,
instructions, directory grants) is defined once per workspace. Its CLI conversations are per goal
(member × goal, on that goal's computer) and per person for chat. A member works for several people
at once, one task at a time per goal. Continuity across goals comes from the hub's notes (plans,
decisions, goal summaries), which every new session starts from. Nothing moves between computers.

**Robust**

- A computer that's off pauses only the goals running on it ("Waiting for Riley's laptop"). The
  goal's person (or an admin) can move it to another computer they may use; its sessions restart
  there from a handoff: task state, steps done, outputs and notes.
- The same handoff starts a fresh session whenever a CLI session can't resume locally (reinstall,
  crash, lost files), so nothing depends on one machine's session files.
- The hub holds all state (goals, tasks, chats, notes, outputs, directory copies). Computers hold
  only CLI session caches and directory caches, and can be replaced.
- Writing someone else's directory: with write access, the agent edits a local copy and each
  changed file goes to the hub with its base hash; the owner's computer applies it (queued while
  off). A stale base is not a conflict for a person: the agent is told the file changed, re-reads
  and redoes the edit (twice at most) before asking its person. Without write access, the owner
  approves and the work runs on the owner's computer. The owner sees a sync conflict only when a
  queued change meets their own offline edit of the same file.
- Usage limits are per account: when Riley's subscription hits its limit, only work on Riley's
  account pauses, and only Riley chooses Wait for reset or Use an API key.

**Cost**

- Everyone's work uses their own subscription, which is flat-rate and usually the cheapest way to
  run Claude Code. Pay-per-token keys appear only on a shared computer, with a daily cap.
- Per-goal sessions keep context small: a goal starts from compact hub notes, not a member's whole
  history, and stable instructions plus notes go first so the CLI's prompt cache reuses them.
- Idle sessions sleep after 3 minutes; staff wake only for a task; managers don't poll.
- Directories download to a computer only when a task there needs them, only changed files
  (content-addressed cache), honoring ignore files. Your own directories are used in place.
- Defaults: managers on the strongest model, staff on the middle one.

**Backend changes**

- runners: owner user, shared flag; each membership has a default runner. Drop
  `workspaces.runner_id` and `workspaces.folder_path`.
- goals: runner chosen at creation, changeable while the goal isn't running there. Tasks run on the
  goal's runner; owner-approved changes run on the owner's runner.
- sessions: add goal and person (chat); one live session per member × goal and member × person.
- accounts keyed by runner and CLI, not organization; usage_limit and cap signals name the runner
  and go to its owner (admins for a shared one).
- orchestrator: schedule per member × goal; handoff prompt for fresh sessions; automatic redo on a
  stale write. Chat runs on the chatting person's runner.

## Code in git repositories (2026-11-19) — refines Directories and Where work runs

Two kinds of directory, chosen automatically when someone adds one:

- **Repository**: a git repository with a remote. Shared **through git, not file sync**. The
  workspace stores its identity (remote URL, default branch); each person links their own clone
  (matched by remote when they add it from their computer). The hub keeps no copy of the working
  tree, only metadata: who has a clone, goal branches and their state, and each diff as an output.
- **Folder**: anything else (notes, design files, a repo with no remote). Uses the hub copy, owner
  approval and queued writes described under Directories.

**How agents work in a repository**

- Agents never touch a person's working tree or checked-out branch. Each goal gets a branch,
  `canopy/<goal-slug>`, created from a base the person chooses when giving the goal: the default
  branch, or their current branch. Optionally it includes their uncommitted changes as a snapshot
  commit (`git stash create`), which leaves their working tree untouched.
- Each task works in its own `git worktree` (on its own task branch from the goal branch) inside the
  clone on the goal's computer. Worktrees share the clone's object store, so they cost little disk.
  Dependencies install from the local package store (pnpm/npm cache). An optional per-repo setup
  command runs once per worktree; files a repo needs but doesn't commit (such as `.env.local`) can
  be listed to copy into worktrees on that computer only, never uploaded.
- When a task finishes, Canopy merges its task branch into the goal branch. If that conflicts, the
  same agent resolves it before the task counts as done. Before review, the goal branch is rebased
  onto the latest base; the manager resolves conflicts.
- A computer without a clone (for example a shared server) clones the repository with the
  credentials on that computer: a person's own git sign-in, or a deploy key or app token an admin
  set up for a shared computer. Agents get exactly the git access of whoever the work is for.

**Shipping the work.** When a goal's tasks are done, its person chooses:

- **Open a pull request** into the base branch: the branch is pushed with that person's git
  credentials and the PR is opened in their name. Review and merge happen in the code host as usual.
- **Merge into <base> on my computer**: allowed when their working tree is clean; otherwise
  Canopy asks them to commit or stash first. It never overwrites uncommitted work.
- **Keep the branch**: they merge it themselves.

**When a computer is off.** A repository has no source computer: the remote is the source, and
the person who added it only linked their own clone. When Riley's laptop is off:

- Other people's goals keep running in their own clones; a computer without a clone clones from
  the remote. Nothing anyone else does needs Riley's laptop.
- Riley's goals on that laptop wait ("Waiting for Riley's laptop") until it's back or Riley moves
  them. To make moving possible without pushing anything to the remote, after each task merges the
  client uploads a **git bundle** of the goal branch to the hub: the commits not on the remote, from
  the base's merge-base with its remote branch. The bundle includes local-only base commits and the
  uncommitted-changes snapshot, so a goal started from unpushed work can move too. The new computer
  fetches the base from the remote, applies the bundle and continues; sessions restart from the
  handoff. A task in progress when the laptop went off restarts from the goal branch.
- Diffs are outputs in the hub, so anyone can review the goal's code while the laptop is off.
- Open a pull request works from any computer the person may use (it restores from the bundle).
  Merge into <base> on my computer needs that clone, so it waits for the laptop.
- Riley's own uncommitted work and unpushed branches outside a goal stay on the laptop. Canopy
  never syncs working trees.
- Bundles are stored like outputs (object storage, same access as the goal's outputs) and are
  deleted 30 days after the goal is done. Files copied into worktrees (`.env.local` and the like)
  are added to the worktree's exclude list, so they can't be committed or bundled.
- Backend: goal repos get `bundle_key` and `tip_sha`; runner protocol adds `goal.bundle_upload`
  (after each task merge) and `goal.restore` (on move).

Folders work as before: reads come from the hub copy, writes with access queue for the owner's
computer, and an owner approval can be given any time but its work runs when the owner's computer
is back.

**Two developers on one repository.** Each has their own clone on their own computer and gives
goals independently. Riley's goals branch from Riley's chosen base and run on Riley's computer; Sam's
do the same on Sam's. Neither touches the other's working tree or branch. The code meets the way
it always does, through branches and pull requests on the remote. To build on someone else's agent
work, push its branch and give a goal starting from `origin/canopy/<goal>`.

**Several repositories in one goal.** A goal that changes several repositories gets a branch of
the same name in each. Its review shows every diff, and Open a pull request opens one per
repository, each linking to the others.

**Access.** A person's workspace access to a repository (read or read & write) decides whether
agents working for them may commit to goal branches and push. Their own git credentials decide what
the remote allows. Without push access, the goal's code still comes back as a diff output for
someone else to apply. Owner approval (Directories) applies to folders only.

**Usage.** Unchanged: work runs on the computer of the person it's for and uses their own sign-in.
Git itself costs no tokens; resolving a rebase conflict is agent work and counts like any other.

## Notifications and owner actions (2026-11-20)

**Notifications.** Each person picks, per row (questions and approvals, errors, goal ready, daily
summary), desktop and email. Defaults: everything that needs you is on for both; the daily
summary is off. Emails go out only for signals still open after two minutes, so people working
on the board don't get mail for what they answer at once; each signal is emailed once. A signal
goes to the person it's for (the goal's person, the task's attributed person, or the folder
owner); with no one in particular, to the workspace's admins. Everyone is checked through
`authz` before mail goes out. The daily summary goes out at 8:00 in the time zone of the person's
current organization, and only when something moved. Desktop notifications are the browser's
own, shown while a Canopy tab is open in the background.

**Restart all sessions** ends every agent conversation in a workspace. Work in progress continues
in a fresh conversation with the usual handoff. A session waiting on a person's answer is left
alone, because the answer goes back into that conversation.

**Organization owner.** Only the owner transfers ownership (to anyone in the organization; the old
owner becomes an admin) or deletes the organization (typing its name). Deleting disconnects its
computers; folders and branches stay on their disks.

**macOS computers** have no sandbox yet, so the client refuses to run agents there without
`--unsafe-no-sandbox`, and the install script doesn't start a background service on macOS.
A Seatbelt (`sandbox-exec`) profile per session is the planned fix.

**Draft goals.** A goal can be saved as a draft (status `draft`). It shows on the board as a
dashed card, but no one plans or works on it. Its person (or an admin) can edit, give or delete
it: `PUT /goals/:id/draft`, `POST /goals/:id/give`, `DELETE /goals/:id` (drafts only). Giving a
draft over a usage limit is refused instead of queued, so the draft stays put. Deleting sends a
`goal.deleted` board event.

**Attachments.** People attach files (up to 25 MB each) to goals, drafts and tasks; they are
stored by SHA-256 like outputs. Anyone who sees the goal or task can add and download them; the
person who added one, or an admin, removes it. Agents get two tools: `get_attachments` downloads
the files on their goal and on every task of that goal into `./attachments`, and `attach_file`
shares a file to the goal (default) or their task, so agents on a goal share files with each
other. Prompts list the attached file names. Board events: `attachment.created`, `attachment.deleted`.

## Tools agents get (2026-11-21)

Agents run Claude Code with `--mcp-config` (without `--strict-mcp-config`) and
`--setting-sources user,project,local`. Canopy's own MCP server is added next to whatever the
person already has, so claude.ai connectors (`mcp__claude_ai_*`), their own MCP servers, user
plugins and skills are available to managers and staff. Before this,
`--setting-sources project` hid user skills (15 vs 24 in the init list on a real check) and
`--strict-mcp-config` hid connectors. The session still signs in with the person's Claude account;
an API key is set only when the person chose one for the computer.

**Trade-off.** Agents now see the person's user-level settings and MCP servers, not just the
project's. Connectors that need a browser sign-in or an interactive prompt can't finish that in
print mode; the person must connect them in Claude first.

**Artifact.** Artifact is not on by default in headless sessions. It needs `CLAUDE_CODE_ARTIFACT=1`,
and `CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC` turns it off, so Canopy no longer sets that one.
It works on Claude Code 2.1.316 and later. Telemetry vars still set: `DISABLE_ERROR_REPORTING=1`
and `DISABLE_BUG_COMMAND=1`. `DISABLE_TELEMETRY=1` is not set, because with it only Artifact
loads and ArtifactComments and ArtifactData are lost.

## Prototype previews run on an opaque origin (2026-11-21)

HTML outputs can be opened as live pages (`POST /outputs/{id}/preview`, then `GET /preview/{token}/{name}`).
Agent pages need JavaScript and CSS, but they are untrusted code served from the hub's own host. So the
preview response is sandboxed with `allow-scripts` and never `allow-same-origin`: the page gets an opaque
origin and cannot read hub cookies, call the API as the person, or touch the parent page. The token in the
URL is the only credential (random, 1 hour, kept in Redis, tied to the person, workspace, task and goal), and
the file route stays as before (`default-src 'none'; sandbox`). Page names are looked up in the same task, then
in the other tasks of the goal, so pages can link to each other with relative paths. Responses also send
`Access-Control-Allow-Origin: *`, because pages on an opaque origin need CORS for fonts, modules and fetch.

Pages have no real `localStorage` on an opaque origin, so the injected helper keeps it in `window.name`. That
lasts across the pages of one preview and is lost when the viewer reloads the frame.

**Trade-off.** Preview pages can call any https site (`connect-src https:`), so they can send what they know
elsewhere. They know nothing of the hub, but people should still only preview work from their own team.

## Notification outbox and workspace Discord

Notifications are rows in `notification_outbox`, written by `notify.Enqueue` in the same transaction as the
change that causes them, so a rolled-back change sends nothing. Signals that need a person queue a row where
`signal.opened` is emitted; plans, finished tasks, done goals and staff drafts queue their own. Kinds reuse the
account rows (`questions`, `progress`, `goal_ready`, `errors`); `progress` is new and off by default. Each
row stores its recipients, picked with the same authz check as email, so a push can never reach someone who
can't see the work.

A `Dispatcher` sends each row once per channel (Discord, push). It claims rows with `FOR UPDATE SKIP LOCKED`,
retries up to 5 times with doubling backoff, keeps the last error and deletes rows after 30 days. Channels
must keep webhook URLs out of the errors they return. Push in `Pref` follows the desktop choice until set.

Each workspace has one Discord webhook. Only workspace and org admins can read or change it (others get 404),
the API only returns a masked URL, and only `discord.com`, `ptb.`, `canary.` and `discordapp.com` webhook URLs
are accepted, so the hub can't be aimed at other hosts. The webhook is stored in the database in plain text
because the hub must send with it; it is not a provider key. Git problems have no signal of their own yet; only
agent errors reach the `errors` kind.

## Discord channel

`notify.DiscordChannel` sends each outbox row to its workspace's webhook. Username and embed title are the row title; images from outputs (png, jpg, gif, webp, up to 8 MB) are attached and the first becomes the embed image, other outputs link to the viewer (`?output=`). The role is pinged only for `questions`. In forum mode a goal's first message creates a thread and its id is kept in `goals.discord_thread_id`; if that thread is gone a new one is made.

A 401/404 from Discord turns Discord off for the workspace (the webhook is cleared, other settings stay) and opens one `error` signal for its admins. Other rejected messages are not retried; the error shows in `discord_last_error`. The test endpoint sends a real message with a generated sample image and ignores the kind filters.

The avatar is the workspace logo, served without login at `/api/v1/public/workspaces/{id}/logo` and only while Discord is on for that workspace. It is sent only when PUBLIC_URL is an https address that is not local. Members have no picture, so the workspace logo is used for all of them.

## Web push and one-tap actions

The hub keeps one VAPID key pair in `vapid_keys`, made on first start like the CA (`VAPID_PUBLIC_KEY` and
`VAPID_PRIVATE_KEY` override it). A browser endpoint is unique and belongs to one person; subscribing again
from the same browser under another account moves it, and only the owner can delete it. The hub posts to the
endpoint a browser gives it, so on a real hub (`COOKIE_SECURE=true`) only known push services are accepted,
which stops it being used to reach internal addresses. A 404 or 410 from the push service removes the
subscription; other failures retry through the dispatcher, and a row counts as sent when any device took it.
Snooze (`users.snooze_until`) skips push only; email and Discord are unchanged.

Buttons carry a random token, stored as a hash, bound to one person, one signal and a list of actions, valid
for 24 hours and used once. `POST /notifications/act` skips the CSRF header (a service worker has none) but
needs the person's session cookie and the token. It then runs the board's own checks (the person must see the
signal; only a directory's owner decides owner changes) and calls `Resolve`. A refused attempt doesn't use the
token up. Only safe actions get buttons: allow or deny, grant or deny, approve, and a question's suggested
answer (picked on the hub, never sent by the client). "Always allow" and "ask for changes" stay in the app.
`GET /me` has `waiting` (open things that notify the person) for the app badge; resolved cards already reach
open tabs as `signal.resolved` with the signal id.

## Agents send their own notifications

Managers and staff have a `notify(message, level, files, links)` tool for real milestones and for being blocked
outside Canopy. It queues a `progress` row (`needs_attention` uses `errors`) titled like every other
notification, for the goal's person, and marks the row `media.agent` so it can be counted. Limits are 5 per
task per hour and 20 per member per day; past them the agent is told to use `complete_task` or `ask_question`.
Files are output names and must belong to the same task or another task on the same goal, so an agent can't
attach another goal's work. Links must be https. The prompt says not to use it for routine steps or for what
questions, outputs and task completion already send.
