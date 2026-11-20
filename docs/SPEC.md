# Canopy: design and feature spec

Nov 19, 2026 · @Ashim Upadhaya

## Overview

Canopy is a control plane for coding-agent CLIs (Claude Code, Codex, Gemini CLI, Pi) that shows each workspace as a live, Miro-style tree: people give goals to manager agents, managers plan and hand tasks to staff agents, and everything that needs a human appears right on the tree where it happened.

The interactive prototype (single HTML file, simulated agents) is the visual and behavioral reference for this spec. Where this doc and the prototype disagree, this doc wins.

**Who it is for**

- Engineering leads who run several agents in parallel and need to see what each is doing.
- Teams that share agents across people and need permissions, approvals and usage visibility.
- Organization admins who need per-person usage, limits and an audit trail.

**Goals**

1. Make multi-agent work legible at a glance: hierarchy, status, blockers and outputs on one canvas.
2. Keep humans in control: questions, approvals, directory access and reviews are answered in place, never in a separate inbox.
3. Least privilege by default: each agent reaches only the directories its work needs.
4. Work with existing CLIs and their sign-ins (subscription or API key) without Canopy holding secrets.
5. Give admins usage per person (current session and weekly) plus organization analytics.

**Non-goals for v1**

- Hosting models or proxying model traffic. Agents call providers through their own CLIs.
- A general-purpose whiteboard. The canvas only shows Canopy objects.
- Replacing Git hosting, CI or issue trackers. Canopy links to them.

**How to use this spec with Claude Code**

- Put this doc in the repo as `docs/SPEC.md` (export as Markdown) and reference it from `CLAUDE.md`.
- Build milestone by milestone (see Build plan). Each milestone lists acceptance criteria Claude Code should verify with tests before moving on.
- Treat the prototype HTML as the UI reference. Port its layout rules, copy and states; do not port its simulation code.

## Concepts and hierarchy

Canopy has four levels: organization, people, workspaces, and the manager and staff agents inside each workspace.

&#91;embedded content: Canopy hierarchy: organization, people, workspaces, agents\]

People belong to the organization and get access workspace by workspace; agents never cross workspaces.

| Term | Meaning |
| --- | --- |
| Organization | The top-level account. Has one owner, admins and members, and many workspaces. A user can belong to many organizations. |
| Person (user) | A human with a login. Organization role: owner, admin or member. Workspace role: admin or staff. |
| Workspace | A unit of work with its own folder, directories, agents, goals and board. |
| Member | An agent in a workspace. Either a manager or staff. Runs as a CLI session (Claude Code, Codex, Gemini CLI, Pi). |
| Manager | Takes goals from people, plans, assigns tasks, borrows independent staff. |
| Staff | Reports to one manager and works the tasks it is given. |
| Independent staff | Staff with no manager; any manager can borrow it for a task. |
| Goal | Work a person gives a manager, with optional done-when. |
| Task | A unit of work assigned to one member, with steps and dependencies. |
| Session | One CLI process for a member; sleeps, wakes, crashes and recovers. |
| Signal | Something that needs a person: question, approval, directory access, error, review, usage limit, cap. |
| Output | A file a member produced, with versions and optional review. |
| Workspace folder | Shared knowledge (plans, notes, decisions, outputs) readable by every member. |
| Directory | A folder from a person's machine shared into the workspace; that machine is its source of truth. |

## Architecture

A runner process on each machine owns the agent CLI sessions and the directories; the hosted API coordinates state, permissions and realtime, and never touches provider credentials.

&#91;embedded content: System architecture: web, API, stores, runner, providers\]

Runners dial out to the API, so no machine needs inbound ports; CLIs call providers directly with their own sign-in.

**Recommended stack** (TypeScript monorepo with pnpm workspaces)

| Package | Tech | Responsibility |
| --- | --- | --- |
| apps/web | React 18, Vite, Zustand, React Router | All pages; the board renders nodes as absolutely positioned DOM in a transformed world layer, one canvas for the dot field, one SVG for edges |
| apps/api | Node 20, Fastify, Zod, Drizzle ORM, ws | REST, board and runner WebSockets, auth, permissions, rollups |
| apps/runner | Node 20, node-pty, Docker or Podman with an OS sandbox fallback, chokidar | Orchestrator, CLI adapters, sandboxes, directory mounts, output capture, usage metering |
| packages/shared | TypeScript types, Zod event schemas | One source of truth for entities and events |
| packages/ui | Design tokens and components | Shared by pages and the board |
| Infra | Postgres 16, Redis 7, S3-compatible storage, SMTP | Persistence, pub/sub, files, email |

**CLI adapters.** One adapter per CLI implements start, send, events (messages, tool calls, file writes, token usage, limit notices), sleep, wake and stop. Use each CLI's non-interactive or streaming mode where it exists, such as Claude Code's print mode with streaming JSON output; otherwise drive a PTY and parse output. Test adapters against recorded transcripts.

**Orchestrator.** Runs on the runner per workspace. It turns goals into plan prompts for managers, validates plans, assigns tasks, starts and sleeps sessions, applies grants, raises signals and meters usage. It is deterministic code around agent calls, not an agent itself.

**Realtime.** The API publishes every domain event to Redis; board sockets subscribe per workspace and filter by the viewer's access before sending.

## Data model

All persistent data lives in Postgres; live agent state is also mirrored in memory on the runner and streamed to clients. Every row carries `id` (UUID), `created_at` and `updated_at`.

| Entity | Key fields | Relationships |
| --- | --- | --- |
| User | email (unique), name, password\_hash, auth\_providers, avatar\_color, last\_org\_id | Belongs to many Organizations through Membership |
| Organization | name, slug, size, owner\_user\_id, default\_session\_limit, default\_weekly\_limit | Has many Workspaces, Memberships, Invites, CliConnections |
| Membership | org\_id, user\_id, org\_role (owner, admin, member), status (active, invited), personal\_session\_limit, personal\_weekly\_limit | Joins User and Organization |
| Invite | org\_id, email, org\_role, token\_hash, invited\_by, expires\_at, accepted\_at, workspace\_grants (json) | Becomes a Membership when accepted |
| Workspace | org\_id, name, about, folder\_path, runner\_id | Has many Members, Directories, Goals, WorkspaceAccess |
| WorkspaceAccess | workspace\_id, user\_id, role (admin, staff), manager\_ids\[\], independent\_staff (bool) | What a person can see in a workspace |
| Directory | workspace\_id, path, owner\_user\_id, machine\_id | Granted to Members through DirectoryGrant |
| Member (agent) | workspace\_id, name, kind (manager, staff), skill, reports\_to (manager id or null), cli, model, account\_id, instructions, sleeping | Has Sessions and Tasks |
| DirectoryGrant | member\_id, directory\_id, mode (read, read\_write), source (suggested, human), granted\_by | Least-privilege access |
| Goal | workspace\_id, manager\_id, title, done\_when, color, status (planning, active, done), created\_by, planned\_at, finished\_at | Has many Tasks and GoalEvents |
| Task | goal\_id (nullable), member\_id, title, status (todo, doing, review, done), progress, depends\_on\[\], steps\[\], started\_at, finished\_at, created\_by | Has Outputs, Comments, Signals |
| Session | member\_id, number, status (running, waiting, gate, review, asleep, crashed, error, ended), started\_at, ended\_at, tokens\_in, tokens\_out, recovered\_from | Has LogLines |
| LogLine | session\_id, task\_id, at, kind (agent, human, ask, error), text | Activity and the session terminal |
| Signal | workspace\_id, type (question, gate, dir\_access, error, review, usage\_limit, cap), member\_id, task\_id, payload (json), status (open, resolved, timed\_out), resolved\_by, resolution | Everything that needs a person |
| Output | task\_id, member\_id, kind (image, recording, document, diff, report), title, status (new, review, changes, approved) | Has OutputVersions |
| OutputVersion | output\_id, number, blob\_key, mime, note | Files live in object storage |
| ChatMessage | workspace\_id, member\_id, author (user id or member id), kind (human, agent, event), text, as\_goal, refs (json) | One thread per member per workspace |
| Comment | task\_id, author, text | Forwarded to the assignee |
| CliConnection | org\_id, cli, mode (subscription, api\_key), cap\_tokens\_per\_day, enabled | Secrets stay on the runner machine |
| Account | org\_id, cli, mode, used\_tokens, limited\_until, paused | Live usage per sign-in |
| UsageEvent | org\_id, workspace\_id, member\_id, attributed\_user\_id, session\_id, tokens, at | Source for per-person usage and analytics |
| AuditEvent | org\_id, workspace\_id, actor\_user\_id, type, summary, at | Activity log |

**Attribution rule.** Every token spent by an agent is attributed to the person who started the work: the goal giver for planning and its tasks, the commenter or chatter for work triggered by their message, the approver for nothing (approvals cost no tokens).

## Identity, organizations and invitations

Registering always creates an organization first and then its owner. Only an invitation creates a user without an organization. One user can belong to many organizations and switches between them.

**Sign up (two steps, `/signup`)**

1. Create your organization: name (required), size (Just me, 2 to 10, 11 to 50, 51 or more), what agents will work on (optional).
2. About you: name, work email, password (8+ characters), terms checkbox. Email already registered → inline error with a link to sign in and create the organization from the switcher.
3. Result: User, Organization (owner = user), Membership (owner), default limits. Redirect to workspace setup.

**Sign in (`/login`)**

- Email and password, or GitHub / Google OAuth. Unknown email and wrong password each get their own inline message.
- After sign in, open the user's last organization; if they have none, go to Create an organization.
- Forgot password → email link (valid 30 minutes) → choose a new password → signed in.

**Invitations (`/invite/:token`)**

- Admins invite by email with an organization role (admin or member) and optional workspace grants (role and which managers' teams plus independent staff).
- The page shows who invited them, the organization, the starting workspace, their role and what they will be able to see.
- No account for that email → name + password creates only the user, then the membership. Existing account → password only, and the organization is added to their list.
- Accepting signs the invitee in. Declining marks the invite declined and notifies the inviter. Expired or revoked tokens show an invalid-invite page.

**Multiple organizations**

- Organization switcher in the top bar, left of the workspace switcher: each organization with the user's role and workspace count, plus Create organization.
- Creating another organization makes the current user its owner and starts workspace setup.
- Account page lists all organizations with Switch.

**Sessions and logout**

- HTTP-only, secure session cookie; 30-day rolling expiry; revoke on password change.
- A visible Log out button sits in the board top bar, every settings page header, setup and the no-workspace screen, and in the avatar menu.
- Members with no workspace access land on a page that names the organization, suggests asking an admin, and offers switch organization, create organization and log out.

## Workspace setup

A workspace is created through a stepped flow that ends on a review page; nothing is created until the person presses Create workspace. The first workspace in an organization runs every step; later ones skip machine and CLI steps.

1. **Welcome** (first time only): one-screen explanation and a tiny tree illustration.
2. **Create a workspace**: name (required), what it works on, workspace folder (Browse picker; holds plans, notes, decisions and outputs; every member can read it), and directories this workspace can use (add from the person's machine or type a path). Note: each directory stays on the computer of the person who added it, which is the source of truth.
3. **Where agents run** (first workspace): this computer or a remote server (address validated as http/https URL).
4. **Connect your CLIs** (first workspace): per installed CLI, Use on/off and sign-in mode: subscription (no remaining-usage visibility) or API key (key stored only in that CLI's config on the runner; optional daily cap). Not-installed CLIs show a copy-install-command button. At least one CLI must be on.
5. **Members and access**: at least one manager. Each manager: name, CLI, model, directory access (all workspace directories or only selected). Staff under each manager and an Independent staff section: name, skill, works for, CLI, model, and directory chips pre-ticked by the manager ("Suggested by Riley" plus a one-line reason). Editing chips flips the label to "Changed by you" with "Ask Riley to suggest again".
6. **Invite people** (or **People and access** for later workspaces): existing organization members to tick, plus new emails. Role per person: Admin (full workspace access) or Staff (choose managers' teams and independent staff). Skip for now is allowed.
7. **First goal**: pick a manager, goal text and optional done-when, or Skip the goal.
8. **Review and create**: workspace and folder; a directory table (path, source of truth, members with access); every member with exactly what they can reach and whether it was suggested or changed; people; first goal. Each section has Edit back to its step. A warning lists staff with no directories.

**Validation and state**

- Validation runs per step on Continue; errors show inline under the field and focus moves to the first invalid field.
- Back keeps everything entered. Cancel (later workspaces only) returns to the board without creating anything.
- The rail shows numbered steps with done ticks; the current step has aria-current=step.

## The board

The board is an infinite canvas showing one workspace as a tree: workspace card on top, managers below it, each manager's staff branching down and to the right, and an Independent staff group on its own branch.

**Top bar (left to right):** logo, organization switcher, workspace switcher, items-that-need-you pill (previous, count, next), presence avatars, Outputs (with unseen badge), Give a goal, your avatar menu, Log out, overflow menu.

**Node types**

| Node | Content | Click |
| --- | --- | --- |
| Workspace card | Name, about, members and goals count, task progress, one usage line per account | Workspace panel in sidebar |
| Goal card (stack beside workspace) | Color, title, owning manager avatar, progress, status | Goal detail in sidebar; other goals dim |
| + Give a goal / + Add manager / + Add staff / + Add independent staff | Dashed cards on the tree | Opens the matching dialog |
| Manager | Avatar, name, MANAGER tag, model, live status, unread chat badge | Opens chat on the board |
| Staff | Avatar, name, skill, model, status, INDEPENDENT tag when shared, unread badge | Opens chat on the board |
| Task sticky | Title, goal color edge, status or progress bar, "?" pin when a question is open, red outline when blocked on a person | Task detail in sidebar |
| Output fruit | 40px thumbnails hanging under a task; blue dot unseen, amber ring needs review, green check approved | Output viewer |
| Question fork | Question pill and one leaf per answer under the task | Leaf answers it |
| Gate | Lock on the member's branch, command or directory, Allow once / Always / Deny or Grant / Deny, timeout | Resolves the gate |
| Error card | Red card with message, Retry / Skip this step | Resolves the error |
| Session card | Terminal-styled: number, status, minutes, tokens, Recovered | Opens chat Activity tab |
| Account card | Usage limit or cap card docked left of the workspace | Wait / Use API key or Raise cap / Keep |
| Summary note | Goals done, waiting, outputs | Workspace summary dialog |

**Edges:** workspace-to-manager bus, manager-to-staff bus, member trunk with rounded elbows to its column, stems to output fruit, violet dashed dependency arrows that turn solid red when a task waits on a person, and a dotted teal arc from a manager to an independent staff member it is borrowing.

**Living dot field.** The background is a canvas-drawn dot grid. Dots within about 72 world pixels of a node take a subtle state color: blue slow ripple when working, amber slow breathing when it needs a person, red pulse when stopped on an error. It holds still when the simulation is paused or prefers-reduced-motion is set. Dot growth is capped at +0.8px and tint at 70% to stay quiet.

**Navigation:** drag to pan, wheel or pinch to zoom (0.25 to 2.2), F or the percentage to fit, \] and \[ to walk items that need you, auto-fit until the user moves the view. Keyboard: Space pause, G new goal, N new task, O outputs, ? shortcuts, Esc closes the top-most layer.

**Layout rules:** column width 320px; manager card 240×84; staff row 70px below managers; task 220×64; fruit row 50px; question fork 150px; gate and error 118px; session 58px. Layout recomputes on every change; new nodes fade in.

## Members

Members are the agents in a workspace. Every workspace has at least one manager; staff either report to one manager or are independent and help any manager that needs them.

| Kind | Does | Default model | Directory access |
| --- | --- | --- | --- |
| Manager | Takes goals from people, analyzes them, writes the plan, creates and assigns tasks, borrows independent staff, decides when asked | Most capable model of its CLI | All workspace directories, or only selected |
| Staff (reports to a manager) | Works tasks its manager assigns; asks questions, requests approvals, makes outputs | Balanced model | Only what its manager suggests, always a subset of the manager's access |
| Independent staff | Same as staff, but any manager can borrow it for a task | Balanced model | Suggested by Canopy from its skill, from all workspace directories |

**Skills:** Developer, Reviewer, Researcher, Designer, Writer. Skill drives task routing and directory suggestions.

**Managing members on the board**

- Add with dashed cards: + Add manager, + Add staff (under a manager), + Add independent staff. Dialog fields: name, kind, skill, works for, CLI, model; shows billing account.
- Edit in the chat window's Settings tab: name, skill, works for, CLI, model, directory access, Sleep or Wake, Give a task, Remove.
- Remove asks what happens to open tasks: hand them to a named teammate or delete them with their outputs. Removing a manager also asks where its staff go (another manager or independent). The last manager cannot be removed.
- Changing CLI resets the model to that CLI's default and moves billing to that CLI's account.

**Statuses** (shown on the card and in chat): Working, Waiting for your answer, Paused at a gate, Waiting for your review, Stopped on an error, Crashed, restarting, Asleep, Usage limit back hh:mm, Paused at API cap, Not started.

## Goals, tasks and orchestration

People give goals only to managers; the manager plans, creates tasks for its own staff first, borrows independent staff when its staff lack a skill, and keeps the goal moving until every task is done.

**Giving a goal:** Give a goal button, G, + Give a goal card, a manager's chat with "Send as a new goal", or the goal sidebar. Fields: manager, goal, done when (optional).

**Orchestration loop (runs on the runner per workspace)**

1. Manager gets a Plan task. Its prompt includes the goal, done-when, workspace folder knowledge, staff list with skills, and directory grants.
2. The manager returns a structured plan: tasks with title, skill, steps, depends\_on and required directories. Canopy validates it (no cycles, skills exist) and writes Plan output to the workspace folder.
3. Assignment: own staff with the matching skill → independent staff with that skill (recorded as borrowed) → any own staff → the manager itself.
4. A missing directory becomes a directory-access signal before the task starts.
5. Each member works one task at a time in its own CLI session. Tasks start when dependencies are done and the member is free.
6. Outputs that need approval pause the task in review. Changes requested send the note back and produce a new version.
7. The goal is done when the plan is complete and every task is done; the manager posts a summary to its chat and the workspace folder.

**Task states**

| State | Enters when | Leaves when |
| --- | --- | --- |
| To do | Created | Dependencies done and member free → Doing |
| Waiting | Has unfinished dependencies | Last dependency done → To do |
| Doing | Member picks it up | Signal raised → stays Doing but session waits; finished → Done; output needs review → Review |
| Review | Output submitted for approval | Approved → Doing to finish; changes asked → Doing (progress rolls back) |
| Done | Member reports completion and checks pass | Never (reopen creates a follow-up task) |

**Session lifecycle:** starts when a member has work; sleeps after 3 idle minutes keeping its conversation; wakes on new work or a message; crashes restart automatically into a new session marked Recovered; usage limits and caps pause it with work saved.

**Manual tasks:** New task dialog (title, goal, assign to, waits on). Comments on a task are forwarded to the assignee, who replies in the thread.

## Signals that need people

There is no inbox: every item that needs a person appears on the tree at the node it belongs to, in the member's chat, in the task or goal sidebar, and in the top-bar pill.

| Signal | Shown as | Actions | Timeout and effect |
| --- | --- | --- | --- |
| Question | Fork under the task: question pill and one leaf per option; the suggested leaf is filled | Pick a leaf, write your own (sidebar), Let the manager decide | None; blocks dependents, which show "Waiting on your answer" |
| Approval gate | Lock on the member's branch with the command | Allow once, Always (saves a rule per member and action kind), Deny | 30 minutes, then counts as Deny |
| Directory access | Gate with the path | Grant access (adds to the staff member, and to its manager if missing), Deny | 30 minutes, then Deny |
| Error | Red card on the branch | Retry, Skip this step | None; red dot field |
| Output review | Amber ring on the output fruit | Approve, Ask for changes (note required) | None; task waits in Review |
| Usage limit | Account card left of the workspace | Wait for reset, Use an API key until reset | Clears at the reset time |
| API cap | Account card at 80% (warning) and 100% (paused) | Raise cap, Keep cap or Stay paused | None |

**Rules**

- The items-need-you count includes every open signal the viewer is allowed to see. Previous and next center the camera on the node and open its detail.
- Resolving a signal anywhere resolves it everywhere and writes an AuditEvent.
- Staff people can only resolve signals in parts of the workspace they can see; usage-limit and cap cards are admin-only.
- A toast appears for each new signal with a Show action; at most two toasts are visible.
- Blocked dependents get a red solid arrow only when the blocker is waiting on a person or a paused account, never just for normal waiting.

## Chat with members

Clicking any manager or staff node opens a 400×560 chat window anchored beside that node on the canvas; the sidebar is never used for members.

**Window**

- Header: avatar, name, live status and role, Your messages pill (count of messages you sent), close. Drag the header to park the window; double-click to snap it back to the node. Without parking it follows the node while panning.
- Tabs: Chat, Activity (live session log and outputs), Settings (member settings and directory access).
- On phones it becomes a bottom sheet at 70% height.
- Esc closes the jump list first, then the window. Drafts are kept per member.

**Thread**

- Human messages right-aligned in green, member messages left in neutral; consecutive messages from the same sender within 3 minutes are grouped under one name and time.
- Events are compact centered lines with an icon and one action: Started, Finished, Made output (Open), Question or approval (Respond, amber while open), Plan ready and Goal complete (Details).
- A typing indicator shows while the member composes a reply.
- If the reader has scrolled up, new messages show a "New messages ↓" button instead of jumping.

**Jump list**

- The Your messages pill opens a floating list on the left of the thread with every message you sent (newest first, two-line snippet, time, GOAL tag).
- Clicking one scrolls smoothly so the message sits a third down the thread, outlines it in amber for 1.6 s, and marks that list item as current. The list stays open for more jumps.

**Composer**

- Auto-growing textarea (up to 120px); Enter sends, Shift+Enter adds a line; send button disabled until there is text.
- Suggestions row (managers: How is it going, What's blocking you, Summarize today; staff: What are you working on, Anything blocking you) hides while typing.
- Managers only: "Send as a new goal" switch turns the box violet; the message is tagged GOAL and creates a goal for that manager.
- Staff windows show "Usually you'd go through \<manager>".

**Replies:** members answer from live state (current task and progress, open signals, goal summaries). Messages are also appended to the member's session context so they influence its work. Unread counts show as a blue badge on the node.

## Goal and task sidebar

The right sidebar (360px; bottom sheet on phones) shows details for goals, tasks, the workspace, independent staff and people. Clicking a member instead opens chat.

**Goal detail**

- Header: color, "Goal", title. Status pill, who gave it and when.
- Progress box: tasks done of total, outputs, time running or total time.
- Owner: manager row with live status and Chat.
- Done when.
- Waiting on you: every open signal on the goal's tasks, each answerable inline.
- Tasks by member: grouped by assignee; borrowed independent staff marked "helping"; each row has status dot, title and a progress bar and opens the task.
- Plan excerpt with Open the plan.
- Outputs grid and a timeline of goal events (given, plan ready, tasks started and finished, outputs, complete).
- Footer: Add a task, Chat with \<manager>. While open, other goals' tasks and arrows dim to 22%.

**Task detail**

- Header: "Task", title, status pill (To do, Waiting, In progress, Needs you, Done) and goal chip (opens the goal).
- Progress box with started or finished time.
- The open signal for this task with its actions (question options, gate grant or allow, error retry or skip, review open).
- Assigned to (with "helping \<manager>" when borrowed) and Chat.
- Steps checklist from the plan, ticked as progress passes each step, the current step bold.
- Details: created, started, finished, blocked for, directories the assignee can reach.
- Waits on and Unlocks lists (open each task), outputs, task-scoped activity from the session log.
- Comments thread; the footer input posts a comment that is forwarded to the assignee, who replies.

The sidebar body refreshes live without re-rendering inputs in the footer.

## Outputs

Anything a member produces (screenshots, recordings, documents, code diffs, reports) hangs as fruit under the task that made it and is collected in the Outputs tray and viewer.

**Capture.** The runner watches each member's working tree and CLI output for files written to `<workspace folder>/outputs/<task>/` and for explicit output markers in the agent's response. Each becomes an Output with version 1; later writes with the same name become new versions.

**On the tree.** Up to three thumbnails per task plus "+n". Badges: blue dot unseen by you, amber pulsing ring needs review, green check approved.

**Tray (Outputs button or O).** Bottom drawer, 300px; filter chips by kind with counts; group By task (lanes with a Show on tree link) or Newest. Hovering a card outlines its task on the board.

**Viewer.** Modal with a dark stage and a 340px side panel.

- Previews: image, recording with play and scrubber, document rendered from Markdown or HTML, diff with added and removed lines, report with chart and table.
- Side panel: kind, title, version switcher, made by (member, model), task, session, made at; review box when it needs approval; Show on the tree; Copy for text kinds; Download.
- Arrow keys move between outputs; Esc closes.

**Review loop.** Approve finishes the task step. Ask for changes requires a note, rolls the task back to 72%, and the member produces the next version on the same fruit. Every decision is recorded on the goal timeline and in the activity log.

## Directories and access

Each workspace has one knowledge folder every member can read and a list of shared directories; managers get all or selected directories, and staff get only what their manager suggests and a human approves.

**Workspace folder.** Holds plans, notes, decisions, summaries and outputs. Created if missing. Read for every member; managers also write.

**Directories and source of truth.** A directory is added by a person from their own machine; that copy is the source of truth. The runner on that machine mounts it into the workspace's shared directory (a bind mount or sync, see Architecture). If the owner's runner is offline, members that need it pause with "Directory unavailable".

**Suggestion rules (manager to staff)**

| Skill | Suggested directories |
| --- | --- |
| Developer | The main codebase only (the code directory matching the workspace or first code directory) |
| Reviewer | The main codebase |
| Designer | Design directories and docs |
| Writer | Docs directories |
| Researcher | Docs and code, read only |

In production the manager agent produces these suggestions from the plan; the table is the fallback heuristic. Suggestions are always a subset of the manager's access. Narrowing a manager's access narrows its staff. A human edit sets source = human and stops automatic suggestions until "Ask \<manager> to suggest again".

**Enforcement.** Each member's CLI runs in a sandbox (container or OS sandbox) that mounts only the workspace folder and its granted directories, with read or read-write as granted. Paths outside are invisible, not just denied.

**Access requests.** When a member needs a directory it lacks, the runner raises a directory-access signal on its branch (path plus reason). Grant adds a DirectoryGrant (and to the manager if missing); Deny makes the member work around it. Both are audited.

**Where it is managed:** setup step 5, the review page, a member's chat Settings tab, workspace settings Directories, and the workspace panel.

## People, permissions and presence

Permissions are two layers: an organization role decides who can administer, and a workspace role decides what someone sees in each workspace.

| Action | Owner | Org admin | Member, workspace admin | Member, workspace staff |
| --- | --- | --- | --- | --- |
| See usage and analytics, activity log | Yes | Yes | Own usage only | Own usage only |
| Organization settings, invites, roles, limits, CLI connections | Yes | Yes | No | No |
| Create or delete workspaces | Yes | Yes | No | No |
| Workspace settings, members, directories, access | Yes | Yes | Yes | No |
| See managers and their staff | All | All | All | Only granted managers' teams |
| See independent staff | Yes | Yes | Yes | Only if granted |
| Give goals, chat, answer signals | Anywhere | Anywhere | Anywhere | Only where they can see |
| Usage-limit and cap cards | Yes | Yes | Yes | No |
| Delete the organization or transfer ownership | Yes | No | No | No |

**Staff view.** Parts a staff person cannot see render at 28% opacity in grayscale and are not clickable; a banner reads "You're staff in this workspace, so you only see the managers and staff you've been given." Admins can preview any staff person's view from People and access; the banner then has Exit preview.

**Presence.** Everyone on a workspace board appears as an avatar in the top bar (online dot) and as a named, colored cursor in world coordinates, so cursors stay on the right node while others pan and zoom. Cursor positions broadcast at up to 15 Hz and interpolate on receive. A person's cursor is only sent to viewers who are allowed to see the area under it; otherwise it is hidden. Joining and leaving show a toast.

## Billing modes, limits and caps

Canopy never assumes API billing: each CLI signs in either with the person's subscription or an API key, and the UI shows only what each mode can honestly know.

| Mode | Example accounts | What Canopy knows | What the UI shows | When it runs out |
| --- | --- | --- | --- | --- |
| Subscription | Claude subscription (Pro, Max, Team, Enterprise), ChatGPT plan for Codex | Only that the CLI reported a usage limit and its reset time | "Available" or "Limit, back hh:mm"; no percentage bar | Every member on that account pauses together; account card offers Wait for reset or Use an API key until reset |
| API key | Anthropic API key, OpenAI API key | Tokens used through Canopy | Percentage of the optional daily cap, amber at 80% | Members on that key pause at 100%; Raise cap or Stay paused |

**Detection.** The runner parses each CLI's limit messages and exit codes into a usage\_limit signal with reset time. Exact message formats differ by CLI and version, so parsing lives in one adapter per CLI with tests against recorded output.

**Per-person limits (Canopy-level).** Separate from provider limits, admins set organization defaults for a current session window (5 hours) and a week (resets Monday 00:00 in the organization's time zone), plus optional personal overrides. At 100% of a personal limit, new goals and chats from that person queue with an explanation; running work finishes its current step.

**Keys and secrets.** API keys stay in each CLI's own config on the runner machine. The server stores only mode, cap and a fingerprint (last 4 characters).

## Usage, analytics and activity log

Owners and admins get an organization-wide usage page with every person's current-session and weekly usage, agent and model breakdowns and an activity log; everyone else sees only their own usage.

**Overview (`/org/usage`)**

- Filters: This week or Last week; All workspaces or one.
- Six tiles: tokens in the period, people on the board now, agent sessions running, goals completed (with human decisions), waiting on people (with average answer time), outputs made.
- Near-limit banner naming anyone at 80% or more of a session or weekly limit.
- Tokens per day, stacked by workspace, today highlighted.
- People table: person and role, current session bar with reset countdown, weekly bar, tokens, goals, decisions, last active, Details. Bars are green below 70%, amber from 70%, red from 90%.
- Tokens by model, and a busiest-hours heatmap (day × hour).
- Agents table: agent, workspace, model, status, tokens, sessions, tasks done, errors, average task time.
- Accounts and limits: each CLI account's state; editable organization session and weekly limits.
- Recent activity, linking to the full log.

**Person detail (`/org/usage/:personId`)**

- Session and weekly meters with resets and change versus the previous week; personal limit overrides (admins).
- Tiles: tokens, goals given, messages to agents, decisions, outputs reviewed, average answer time.
- Tokens per day by workspace, sessions table (when, start, length, workspace, tokens; live session marked Now), workspace split, agents they use most, when they work heatmap, recent activity.

**Activity log (`/org/activity`).** Newest-first table: when (Live chip for current), person, what happened, where. Filter chips: Everything, Goals, Approvals, Answers, Reviews, Access, People, Chats. Retained 400 days; exportable as CSV.

**Data pipeline.** Runners emit UsageEvent per model call (tokens, member, session, attributed person). The API aggregates into hourly rollups per person, member, workspace and model; pages read rollups plus the live tail for the current hour.

## Pages and routes

Every screen has a URL; settings use one layout with a left nav grouped into the organization, the current workspace and you.

| Route | Page | Who |
| --- | --- | --- |
| /signup | Two-step: organization, then you | Public |
| /login | Sign in, OAuth, forgot password link | Public |
| /forgot, /sent, /reset/:token | Password reset | Public |
| /invite/:token | Invitation accept or decline | Public |
| /new-org | Create another organization | Signed in |
| /setup, /setup/new | Workspace setup | Org admins |
| /board (/w/:workspaceId) | The board | Anyone with workspace access |
| /org/usage, /org/usage/:personId | Usage and analytics, person detail | Admins; members see their own |
| /org/activity | Activity log | Admins |
| /org/general | Organization name, your role, counts | Admins |
| /org/people | Invite with workspace grants; roles; workspace chips; invite page link; remove | Admins |
| /org/workspaces | Workspace cards with counts and running goals; Open, Settings, New | Admins |
| /org/connections | CLI sign-in modes and caps for all workspaces | Admins |
| /settings/general | Workspace name, about, folder | Workspace admins |
| /settings/directories | Directories with source of truth and who can use them; add, remove | Workspace admins |
| /settings/members | Members table with role, CLI, model, directories; Open chat; add | Workspace admins |
| /settings/people | Access: every organization person, give or remove access, role, visible teams | Workspace admins |
| /settings/danger | Restart, delete workspace | Org admins |
| /account | Profile, theme, notifications, organizations, workspaces, your usage, log out | Signed in |

**Notifications (account page).** Toggles for questions and approvals, errors, goal complete, daily summary; channels desktop and email. Daily summary emails list goals moved, decisions waiting and usage.

**Empty and error states.** No workspaces (admin → setup; member → ask an admin page). Invalid invite. Board with no goals shows the + Give a goal card highlighted. Offline runner: banner "Can't reach \<machine>; members are paused" with retry.

## Design system

Canopy uses a cool neutral canvas, a deep forest green for brand and the workspace card, sticky-note colors for tasks, and three state colors reserved for status.

| Token | Light | Dark | Use |
| --- | --- | --- | --- |
| paper | #F6F7F9 | #101419 | Canvas and page background |
| card | #FFFFFF | #1A2028 | Cards, panels, menus |
| ink / muted | #14181F / #4A5462 | #E6EBF0 / #A3AEBB | Text |
| edge | #DDE1E7 | #2C3440 | Borders |
| green (brand, primary action) | #16794F | #3FA877 | Buttons, human chat bubbles, approved |
| team | #14322A | #1E4537 | Workspace card |
| blue (working, unseen) | #3E7BFA | same | Dot field, unseen badges |
| amber (needs a person) | #E39B1B | same | Signals, forks, gates |
| red (error, blocked on a person) | #C4352B | #FF9C8F text | Errors, red arrows |
| violet (dependencies, goal mode) | #6E56CF | #9D86FF | Dependency arrows, Send as a new goal |
| task todo / doing / done | #FFE58A / #FFD9A0 / #BFE6D1 | slightly muted | Task stickies |
| goal colors | #8E5BD9, #0E9AA7, #D6457A, #E8743B, #4C5FD5 | same | Goal edge on stickies |

**Type.** Bricolage Grotesque 600–700 for headings and the brand; Inter 400–700 for UI (14px base, 1.45 line height); JetBrains Mono for sessions, paths, commands and code.

**Components.** Buttons (primary, default, ghost, small, danger), segmented control, chips, pills, inputs with inline errors, menus, toasts (max 2, bottom right), modals, sidebar, bottom tray, chat window, viewer, cards and tables, meters and mini bars, bar chart, heatmap.

**Motion.** Node fade-in 350 ms; camera moves 450 ms ease; dot field ripples at 1.1–2.6 rad/s; all motion stops under prefers-reduced-motion.

**Copy rules.** Sentence case, plain verbs on buttons (Give a goal, Grant access, Ask for changes), curly quotes and apostrophes in UI text, no jargon such as "signal" in the UI.

## Non-functional requirements

The hard requirements are: no provider secrets on the server, every agent sandboxed to its grants, every human decision audited, and the board staying smooth at 60 fps with 50 members.

**Security**

- Passwords hashed with Argon2id; OAuth via GitHub and Google; CSRF protection on cookie sessions; rate-limited auth endpoints (10 per minute per IP).
- Invite tokens are 32-byte random, stored hashed, single use, 7-day expiry.
- Runner-to-server traffic over mutual TLS with per-machine keys; runners initiate the connection (no inbound ports).
- Authorization checked server-side on every API call and every realtime message, including cursor broadcasts.
- Agent sandboxes: no network except the provider endpoints and allowed package registries unless an approval allows more.

**Performance**

- Board: 60 fps pan and zoom with 50 members, 300 tasks, 200 outputs on a 2020 laptop; layout recompute under 8 ms; dot field drawn on one canvas.
- Realtime: board state deltas under 250 ms from event to screen; cursors at 15 Hz.
- Analytics pages load under 1.5 s from rollups.

**Reliability**

- Agent sessions survive server restarts (runner owns them); a crashed CLI restarts once automatically and is marked Recovered.
- Every state change is an event; the board can be rebuilt from events.
- Runner offline → members pause, signals stay open, the UI shows a banner.

**Accessibility**

- WCAG 2.2 AA contrast; full keyboard use (tab order, shortcuts, Esc layering); visible focus rings.
- Every canvas node is a real DOM element with a label; the dot field is decorative and aria-hidden.
- Live regions for chat and toasts; reduced motion respected; 44px minimum touch targets on phones.

## API and realtime events

The web app talks to a REST API for reads and commands and to one WebSocket per open workspace for live state; runners use a separate authenticated WebSocket.

**REST (JSON, `/api/v1`)**

| Method and path | Purpose |
| --- | --- |
| POST /auth/signup | Create organization, then user (body: org, user) |
| POST /auth/login, /auth/logout, /auth/forgot, /auth/reset | Sessions and passwords |
| GET /me, PATCH /me | Profile, organizations, preferences |
| POST /orgs, PATCH /orgs/:id | Create another organization, rename, limits |
| GET/POST /orgs/:id/invites, POST /invites/:token/accept, /decline | Invitations |
| GET/PATCH/DELETE /orgs/:id/memberships/:userId | Roles, personal limits, removal |
| GET/POST /orgs/:id/workspaces, PATCH/DELETE /workspaces/:id | Workspaces (POST takes the whole reviewed setup payload) |
| GET/POST/PATCH/DELETE /workspaces/:id/members, /directories, /grants, /access | Members, directories, grants, workspace access |
| POST /workspaces/:id/goals, POST /goals/:id/tasks, PATCH /tasks/:id | Goals and tasks |
| POST /signals/:id/resolve | Answer, allow, always, deny, grant, retry, skip, approve, changes, wait, use\_key, raise\_cap |
| GET/POST /members/:id/chat | Chat history and send (as\_goal flag) |
| POST /tasks/:id/comments | Comments |
| GET /outputs/:id, /outputs/:id/versions/:n/file | Outputs and files (signed URLs) |
| GET /orgs/:id/usage?week=&workspace=, /orgs/:id/usage/people/:userId, /orgs/:id/activity?type= | Analytics and log |

**Board WebSocket (`/ws/workspaces/:id`).** On connect: a snapshot filtered to the viewer's access, then deltas. Event types: member.updated, session.updated, session.log, task.created, task.updated, goal.updated, goal.event, signal.opened, signal.resolved, output.created, output.version, chat.message, chat.typing, account.updated, presence.joined, presence.left, presence.cursor. Client sends: presence.cursor and chat.typing only; everything else goes through REST.

**Runner protocol (`/ws/runners`).** Server → runner: start\_session, send\_input, sleep, wake, stop, apply\_grants, resolve\_signal. Runner → server: session events, log lines, usage events, signals, file-written notices, limit messages, heartbeat every 10 s.

## Build plan for Claude Code

Build in six milestones; each ends with its acceptance criteria passing in automated tests before the next starts.

&#91;embedded content: Build plan: six milestones\]

Milestone 3 is the riskiest (CLI adapters and sandboxing), so spike the Claude Code adapter during milestone 1.

**Acceptance criteria**

1. Foundations
   - [ ] Sign-up creates organization, then user, owner membership and default limits in one transaction.
   - [ ] Login rejects unknown emails and wrong passwords with distinct messages; reset link works once within 30 minutes.
   - [ ] A user in two organizations can switch between them; data never leaks across organizations (tested at the API).
   - [ ] Log out is reachable from the board, settings, setup and the no-workspace page.
2. Workspace setup
   - [ ] Nothing is persisted until Create workspace; the POST payload matches the review page exactly.
   - [ ] At least one manager is enforced in the UI and the API.
   - [ ] Staff directory grants are always a subset of their manager's; narrowing a manager prunes staff.
   - [ ] Invite accept creates only a user for new emails and only a membership for existing accounts.
3. Runner and agents
   - [ ] A runner pairs with an organization by one-time code and reconnects after restarts.
   - [ ] A member's sandbox can read only the workspace folder and its granted directories (tested by attempting outside reads).
   - [ ] Giving a goal yields a validated plan, tasks with dependencies, and assignment that prefers own staff, then independent staff.
   - [ ] Questions, gates, directory access, errors, reviews, usage limits and caps raise and resolve end to end; gates time out at 30 minutes.
4. Board
   - [ ] Layout matches the prototype's rules; 60 fps with 50 members and 300 tasks.
   - [ ] Dot field colors match states and stops under reduced motion.
   - [ ] Staff viewers receive only permitted nodes and cursors over the socket.
5. Chat and outputs
   - [ ] Chat opens beside the clicked node, can be parked, keeps drafts, and the jump list scrolls to and highlights the chosen message.
   - [ ] "Send as a new goal" creates the goal for that manager.
   - [ ] Files written to the outputs folder appear as fruit within 2 seconds; changes requested create version 2.
6. Usage and analytics
   - [ ] Every token is attributed to a person by the attribution rule; rollups match raw events within 0.1%.
   - [ ] Session (5 hours) and weekly (Monday reset) meters and personal overrides work; at 100% new work queues.
   - [ ] Usage and activity pages are admin-only at the API; members get only their own usage.

**Repo layout**

```
canopy/
  CLAUDE.md              how to work in this repo; points to docs/SPEC.md
  docs/SPEC.md           this document, exported as Markdown
  docs/prototype.html    the interactive prototype (UI reference)
  apps/web/              React app (pages, board, chat, viewer)
  apps/api/              Fastify API, WebSockets, migrations
  apps/runner/           orchestrator, CLI adapters, sandbox, mounts
  packages/shared/       types and Zod event schemas
  packages/ui/           tokens and components
  e2e/                   Playwright tests per milestone
```

**Working rules for Claude Code**

- Read docs/SPEC.md and open docs/prototype.html before changing UI; match its copy, spacing and states.
- Write the tests for a milestone's acceptance criteria first, then implement until they pass.
- Keep entities and events defined once in packages/shared; the API and web import them.
- Never store provider keys in the database or logs; never widen a sandbox without a grant.

## Open questions

- [ ] Directory sharing across machines: bind mounts on one runner only, or sync (Mutagen, rsync) when members run on a different machine than the directory owner?
- [ ] How do managers and staff exchange context: through the workspace folder only, or a structured message bus the runner relays?
- [ ] Exact limit-message formats for each CLI version, and whether any CLI exposes remaining subscription usage.
- [ ] Should staff people be allowed to give goals directly, or only through a manager they can see?
- [ ] Are approvals per member and action kind enough for "Always", or do we need per-command patterns?
- [ ] Billing for Canopy itself: per seat, per workspace, or per runner?
- [ ] Ownership transfer and organization deletion flow, including data export.
- [ ] Retention for session logs and outputs (proposal: logs 90 days, outputs until deleted).
