# Job scheduler: how Canopy runs work today

Findings for G-29. Read this before the plan doc. Paths are under `hub/internal/` unless noted.

## 1. Short version

- Canopy has **no clock-based trigger for work**. Goals and tasks start only when a person (or an agent) creates them.
- There are timers, but none start work: the orchestrator tick (5s), a notifier loop (1 min, daily summary), a queue drain (30s).
- A job fits best as **a new hub loop that creates a goal** with the existing `core.CreateGoal`. Everything after that (plan, tasks, sessions, outputs) already works.
- Two parts of the goal flow need a decision for unattended runs: the **plan step** and the **handover brief** (both involve a person today).
- **Memory for continuous jobs can reuse notes**, which are stored in the hub, versioned, linkable by `N-` ID and readable by every new session. Nothing new is needed for a first version.

## 2. How a goal becomes tasks and sessions

1. **Create the goal.** `POST /workspaces/:id/goals` (`api/agents.go` `handleCreateGoal`) calls `core.CreateGoal` (`core/goals.go`). It inserts a goal in status `planning` (or `draft`) for a manager, writes a goal event, and kicks the orchestrator. The same function is used when queued work is released (`api/limits.go` `DrainQueue`).
2. **Orchestrator pass.** `orch.schedule` (`orch/schedule.go`) runs per workspace on each kick and on a 5s tick (`orch/orch.go`). It holds a workspace lock and runs five steps in order:
   - `placeGoals`: picks a computer (runner): the goal giver's own, else a shared one. No computer means the goal waits.
   - `addPlanTasks`: a `planning` goal gets a `plan` task for its manager.
   - `updateWaiting`: sets tasks to `waiting` or `todo` from `depends_on`.
   - `startWork`: starts one task per member per goal when the member is free and the computer is ready.
   - `finishGoals`: see section 5.
3. **Plan.** The manager runs the plan task and calls `submit_plan` (`orch/plan.go`). The hub validates it (skills exist, no cycles) and creates the work tasks and assigns staff (own staff, then independent staff, then the manager).
4. **Work.** `startTask` checks directory grants (a missing one opens a `dir_access` signal), builds the prompt, marks the task `doing` and calls `deliver`. Staff report with `report_progress`, `submit_output`, `complete_task`.
5. **Close.** When every work task is done, the manager gets a `handover` task (section 5). After approval the goal moves to `ready`, or straight to `done` when the workspace setting `goal_completion` is `auto`. Closing ends the goal's sessions and tells the runner to delete the goal's work folders (`closeGoal` in `orch/goals.go`).

A session is one CLI conversation per **member × goal** (or member × person for chat). Key: `sessKey` in `orch/where.go`.

## 3. What a fresh session gets as context

| Source | Where | Notes |
| --- | --- | --- |
| System prompt | `systemPrompt`, `orch/prompts.go` | Role, tool list, ID rules, mounted paths, **titles of up to 8 notes** (not their text), admin instructions, skills. |
| Task prompt | `taskPrompt`, `orch/prompts.go` | Task, goal title, description, done-when, steps, directories, results of tasks it depends on, comments, attachment names. |
| Plan prompt | `planPrompt` | Goal, team, skills, directories. It tells the manager to call `read_notes` first. The `notes` argument is unused (`_ = notes`). |
| Referenced items | `taskReferences`, `orch/items.go` | If goal or task text contains `G-12`, `T-34`, `N-5`, a block with those items (max 10, about 1500 chars each) is appended. |
| Handoff | `handoff`, `orch/prompts.go` | Only when a new session replaces an older one for the same goal: list of done tasks and a pointer to the plan note. |
| Attachments | `orch/attachments.go` | Names are in the prompt. The agent downloads them with `get_attachments`. |
| Tools | `orch/notes.go`, `orch/tools.go` | `read_notes`, `read_item`, `write_note`, `update_note` let an agent pull memory on demand. |

Key point: **a new goal starts a new session with no conversation history.** The only carry-over is what the agent reads from notes, items and attachments. This matches the "Single Run" job type for free. DECISIONS ("Where work runs") says the same: continuity comes from hub notes.

## 4. What can be reused as memory

| Thing | Table / code | Fit for a continuous job |
| --- | --- | --- |
| **Notes** | `notes` (`00005`, `00025`), `orch/notes.go` | Best fit. Kinds: plan, summary, decision, note. Shared or private. A note can be tied to a goal or none. Agents append or replace with `update_note`. |
| **Note revisions** | `note_revisions` (`00025`) | Last 20 versions saved before each change, with the editor. Gives undo and history of the memory for free. |
| **Goal summary note** | `goalTasksDone` and `approveHandover` write a "Summary: <title>" note per goal | One summary per goal. A job could read the last N run summaries. |
| **Goal handover** | `goals.handover_*`, `orch/handover.go` | A reviewed brief per goal; becomes the goal summary. Useful as a run report, but blocks on a person (section 5). |
| **Task summaries** | `tasks.summary`, shown in `handoff` and dependent tasks | Short result per task. |
| **Outputs and versions** | `outputs`, `output_versions` (`00005`), `orch/outputs.go` | Files per task, versioned by name. Stored in blob storage. Unique per `(task_id, name)`, so a new run cannot add a version to an old run's output. A job needs its own link table. |
| **Attachments** | `attachments` (`00015`) | Tied to a goal or a task (exactly one). Good for a fixed brief or reference files per run. |
| **Refs** | `hub/internal/refs`, `orch/items.go` | `G-/T-/N-` numbers, per workspace, never reused. Text with a ref gets a "Referenced items" block automatically, so a job brief can say "continue from N-70" and the agent gets it. `read_item` reads any ref. |
| **Chat messages** | `chat_messages` | Per member and person. Not suited for memory. |

Gaps for continuous jobs:
- Nothing says "this goal came from job X". A `job_id` on goals (or a `job_runs` table) is needed.
- Notes have no size limit or compaction. `read_notes` stops at 40,000 chars and the prompt lists only 8 titles.
- A note linked to a goal gets no special treatment when the goal closes, but the goal's work folders are deleted. Anything the next run needs must be in a note, output or attachment, not on disk.

## 5. Where a timed trigger could plug in

**Where it runs.** A new loop in the hub, started in `cmd/hub/main.go` next to the 30s ticker (line ~476) or inside `orch.tick` (`orch/orch.go`). The tick already runs every 5s and is the natural place to ask "which jobs are due". Use the same database transaction and workspace lock pattern as `schedule`.

**What it creates.** Two options:

| Option | How | Pros | Cons |
| --- | --- | --- | --- |
| A. Create a **goal** per run | `core.CreateGoal` with the job's manager, title, done-when, description (the brief), `By` = job owner | Reuses plan, tasks, board, outputs, notifications, usage. Visible on the board. | Goes through plan and handover; heavier for simple jobs. |
| B. Create a **task** directly | `CreateTask` with `Kind` normal and a staff member | Lighter and faster. | Tasks sit under goals in the UI and sessions are keyed by goal. Would need a standing goal per job, or changes to `sessKey`, `taskKey`. |

Recommendation: start with **A**, and let a job optionally skip the plan task (create the work task right away). The manager step costs tokens on every run.

**Who it runs as.** Work runs on the goal giver's computer with their sign-in (`placeGoals`, DECISIONS "Where work runs"). A job needs a stated owner. If the owner's computer is off, the goal waits (`ErrComputerOff`); a job then needs a missed-run policy. A shared computer works for jobs that should run when laptops are off.

**Limits already in place.** `queueIfOverLimit` (`api/limits.go`) holds goals from people over their session or weekly limit, then `DrainQueue` releases them. A job should go through the same check so it cannot burn a person's limit unseen.

**Blocking steps that matter for unattended runs:**
1. **Plan task.** Always created for a `planning` goal (`addPlanTasks`). Not a person, but costs a manager session.
2. **Handover brief.** `finishGoals` always adds a `handover` task and **waits for a person to approve it**, even when `goal_completion` is `auto` (the setting only skips the ready card). A scheduled run would sit in review until someone approves. Needs a job flag to auto-approve or skip the brief.
3. **Review outputs.** Only when an agent submits with `needs_review`; a job brief can tell it not to.
4. **Signals.** Questions, gates and directory requests wait for a person. There are no timeouts for questions. A job needs a rule such as "no questions; decide yourself" in its brief, and a failure path.
5. **Goal cleanup.** `closeGoal` deletes work folders and ends sessions. Fine for single runs; for continuous jobs memory must live in the hub.

## 6. Existing timers to copy

- `orch.Run` / `tick` (`orch/orch.go`): 5s tick: expire gates, sleep idle sessions, refresh accounts, schedule all.
- `notify.Notifier.Run` (`notify/notify.go`): 1 min loop with a daily send at `DailyHour` (8). It already handles "once per day per person" logic and is the closest example of time-of-day work. Check it for time zone handling before copying.
- `cmd/hub/main.go` ~L476: 30s loop for `DrainQueue` and hourly pruning.

No cron parser or time zone library for schedules exists in the repo yet. Organization time zone is mentioned in SPEC (weekly limit reset) but not used for schedules.

## 7. Questions this raises for the plan doc

1. Goal per run (A) or a lighter task path (B)? Recommended: A, with optional plan skip.
2. Handover brief on job runs: skip, or auto-approve? Recommended: skip by default, and store the run summary as the run record.
3. Where does job memory live? Recommended: one shared note per job (brief, rolling summary) plus links to each run's summary note and outputs.
4. Who owns a job and which computer runs it? Recommended: the creator, with a shared computer as a fallback.
5. What if the hub was down at run time? Needs a missed-run policy (skip or run once) and a `next_run_at` column, not an in-memory timer.
