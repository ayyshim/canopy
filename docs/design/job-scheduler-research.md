# Job scheduler research (T-172)

Prior art for the Canopy job scheduler (G-29). Two jobs to serve:
**single run** (no memory, e.g. daily log check) and **continuous run**
(each run remembers earlier ones, e.g. a daily episode).

Short version: copy Kubernetes and Temporal for trigger rules, copy a
"brief + rolling summary + run log" shape for memory, and add a few cheap
guard rails (caps, auto-pause, dry-run preview).

## 1. How schedulers behave

| Topic | cron | K8s CronJob | Temporal Schedules | Airflow |
|---|---|---|---|---|
| Timezone | Server local time. `CRON_TZ` only in some crons | `spec.timeZone` (IANA name) | Calendar specs with a time zone | Timezone-aware DAGs (UTC inside) |
| Missed runs | Lost. (`anacron` exists to catch up) | Skipped if past `startingDeadlineSeconds`. Gives up after 100 missed | `catchupWindow` decides how far back to run missed actions | `catchup` flag: run every missed interval or only the latest |
| Overlap | None. Two copies can run | `concurrencyPolicy`: Allow / Forbid / Replace | Overlap policy: Skip, BufferOne, BufferAll, CancelOther, TerminateOther, AllowAll | `max_active_runs` per DAG |
| Retries | None | `backoffLimit` on the Job | Retry policy on the workflow/activity | `retries`, `retry_delay`, exponential backoff |
| Pause | Comment the line out | `suspend: true` | Pause/unpause with a note | Pause toggle on the DAG |
| History | Mail or logs | Keeps last N jobs (`successfulJobsHistoryLimit`, `failedJobsHistoryLimit`) | Recent actions on the schedule, plus every run in workflow history | Every run and task in the metadata DB |

### What it means for Canopy

- **Timezone:** store an IANA zone on each job (default: the owner's zone)
  and compute "next run" in that zone, store it as UTC. Never store a bare
  offset. Daylight saving: a time that does not exist (spring forward) runs
  at the next valid moment; a time that happens twice (fall back) runs once.
- **Missed runs (hub was down):** default is **run once, now, if the miss is
  inside a window, otherwise skip**. This is K8s's deadline idea and
  Temporal's catch-up window. Never replay a pile of missed runs: five
  "daily episodes" in one minute is worse than one. Let the job pick
  `skip` or `run once` (default `run once` for continuous, `skip` for
  single-run checks, since a stale log check is noise).
- **Overlap:** default **skip** (Forbid / Skip). If a run is still going
  when the next is due, record a "skipped: previous still running" run so it
  shows in history. A continuous job must never overlap, because two runs
  would both read the same memory and one would overwrite the other.
  Offer `buffer one` later, not in the MVP.
- **Retries:** a small fixed count (2) with backoff (1 min, 5 min) per run.
  A retry is the same run, not a new one, so memory is written once.
- **Pause/resume:** a plain `paused` flag plus who paused it and why
  (Temporal's pause note). On resume, do not catch up what was missed
  while paused. Recompute the next run from now.
- **History:** one row per run, including skipped and failed ones, with
  status, start/end, trigger (schedule / manual / retry), the goal or task
  it created, cost, and a link to the output. Keep history forever; it is
  small, and it is the audit trail.
- **Manual "run now":** every system above has it (Temporal trigger, Airflow
  trigger, `kubectl create job --from`). Manual runs should not shift the
  schedule.

Sources:
- cron: https://man7.org/linux/man-pages/man5/crontab.5.html
- anacron: https://man7.org/linux/man-pages/man8/anacron.8.html
- Kubernetes CronJob: https://kubernetes.io/docs/concepts/workloads/controllers/cron-jobs/
- Temporal Schedules: https://docs.temporal.io/schedule
- Temporal overlap and catch-up (Go SDK reference): https://pkg.go.dev/go.temporal.io/sdk/client#ScheduleOptions
- Airflow DAG runs and catchup: https://airflow.apache.org/docs/apache-airflow/stable/core-concepts/dag-run.html
- Airflow timezones: https://airflow.apache.org/docs/apache-airflow/stable/authoring-and-scheduling/timezone.html

## 2. How agents remember across runs

Patterns seen in LLM agent systems:

| Pattern | Idea | Good for | Weakness |
|---|---|---|---|
| Fixed brief ("series bible") | Human-written theme, rules, tone, never changes by itself | Stable rules and style | Does not know what already happened |
| Run log | Append-only list of past runs: date, title, one-line result, link | Audit, "what did we do before" | Grows without limit |
| Rolling summary | One short text, rewritten by the agent after each run | Cheap, bounded context | Loses detail; errors can compound |
| Episodic memory | Specific past events ("episode 12 used the lighthouse story") | Avoiding repeats, continuity | Needs retrieval once it is big |
| Semantic memory | Distilled facts ("the villain is afraid of water") | Consistency | Must be kept up to date |
| Retrieval of past outputs | Search past outputs by recency/relevance and put the top few in the prompt | Large histories | Extra infra (embeddings) |
| Compaction | When memory passes a budget, summarise older items into shorter ones | Staying inside the context window | Lossy, so keep originals |

Notable systems:
- **MemGPT / Letta:** a small always-in-context "core memory" the agent edits
  itself, plus a larger "archival" store searched on demand.
- **Generative Agents (Park et al.):** a memory stream scored by recency,
  importance and relevance, with periodic "reflections" that turn events
  into facts.
- **LangGraph / LangMem:** names the three kinds (semantic, episodic,
  procedural) and has the agent write memory in the background after a run.
- **Anthropic:** the memory tool (agent reads and writes files in a memory
  directory), context editing/compaction, and the long-running agent
  guidance, which uses a progress file that each fresh session reads first
  and updates last.

### What it means for Canopy

Keep it boring: **files you can read, no vector database in the MVP.**

1. **Brief** (human-owned, fixed): theme, rules, format, do/don't. Edited
   only by people. Always in the prompt.
2. **Rolling summary** (agent-owned): about 500 to 800 words. The run's last
   step is "update the summary", written as a new revision so people can
   see diffs and roll back. Always in the prompt.
3. **Run log** (system-owned): one line per past run (number, date, title,
   output link). The newest 10 go in the prompt; older ones are linked, not
   inlined.
4. **Past outputs** are fetched on demand by the agent through a tool (read
   run N), not stuffed into the prompt. This is retrieval without embeddings.
   If search is ever needed, add keyword search over run-log titles first.
5. **Budget and compaction:** set a token budget for the memory part of the
   prompt (about 4k tokens). When the summary passes it, the next run is
   asked to compact it: fold the oldest details into facts, keep names and
   open threads. Keep the old revision, so nothing is lost.
6. **Single-run jobs get none of this**, only the brief. That keeps the two
   types honest: the type decides what goes in the prompt.
7. **Write memory last and only on success.** A run that fails halfway
   leaves memory unchanged, which makes retries safe.
8. People can open and edit the brief and summary like any note. Canopy
   already has notes with revisions; reuse them if the findings doc (from
   T-171) agrees.

Sources:
- MemGPT paper: https://arxiv.org/abs/2310.08560
- Letta memory docs: https://docs.letta.com/
- Generative Agents: https://arxiv.org/abs/2304.03442
- LangMem concepts: https://langchain-ai.github.io/langmem/concepts/conceptual_guide/
- Anthropic, effective context engineering for agents: https://www.anthropic.com/engineering/effective-context-engineering-for-ai-agents
- Anthropic, effective harnesses for long-running agents: https://www.anthropic.com/engineering/effective-harnesses-for-long-running-agents
- Anthropic memory tool docs: https://docs.claude.com/en/docs/agents-and-tools/tool-use/memory-tool

## 3. "Smart" features worth stealing

| Feature | Where it exists | Recommendation |
|---|---|---|
| Natural-language schedule ("every weekday at 9") | Chat assistants with scheduled tasks; libraries like `chrono-node` and `rrule` | **MVP.** Let the model turn the text into a cron expression plus zone, then **show it back** in plain words and the next 5 run times for the person to confirm. Store only the cron, never the text. Do not trust the model's result without the preview |
| Skip if nothing changed | CI path filters, `make`-style checks | **Phase 2.** Per-job optional check ("only run if the log file or data changed since last run"). Cheap check first, agent only if true. Record "skipped: no change" in history |
| Cost cap per job | LLM gateways with budgets (e.g. LiteLLM), CLI budget flags | **MVP.** Per-run and per-month caps. Stop the run at the cap, mark it `capped`. Canopy already has billing caps in SPEC; hook into those |
| Auto-pause after repeated failures | Temporal `pauseOnFailure`, circuit-breaker pattern | **MVP.** Pause after 3 failed runs in a row and notify the owner. Resume is manual |
| Dead-man alert (job did not run at all) | Healthchecks.io, Cronitor | **Phase 2.** Alert if no run started within a grace time of due |
| Jitter | Temporal, many fleet schedulers | **Later.** Only matters at many jobs on the same minute |
| Auto-disable when idle | GitHub Actions disables scheduled workflows in inactive repos | **Later.** Optional "stop after N runs / end date" on the job is simpler and more useful now |
| Dry run / preview | Terraform plan, `kubectl --dry-run` | **MVP-lite.** "Run now" that is marked as a test and does not write memory |

Sources:
- chrono-node: https://github.com/wanasit/chrono
- rrule (RFC 5545): https://datatracker.ietf.org/doc/html/rfc5545#section-3.3.10
- Temporal pause on failure: https://docs.temporal.io/schedule
- LiteLLM budgets: https://docs.litellm.ai/docs/proxy/users
- Healthchecks.io: https://healthchecks.io/docs/
- GitHub Actions schedule event: https://docs.github.com/en/actions/writing-workflows/choosing-when-your-workflow-runs/events-that-trigger-workflows#schedule

## 4. Top recommendations

1. Store cron plus IANA zone; compute next run in UTC; run once on a missed
   window (continuous) or skip (single-run).
2. No overlap, ever. Skipped runs still get a history row.
3. Retries belong to a run, and memory is written only when a run succeeds.
4. Memory for continuous jobs = brief + rolling summary + run log, as
   editable notes with revisions. No vector search yet.
5. Natural-language input is converted to cron and confirmed with a preview.
6. Ship caps and auto-pause in the MVP; they protect against a runaway job
   spending money every day.

## Note on sources

Links point to the official docs or papers. Behaviour was written from
knowledge of those docs and not re-fetched while writing, so check exact
defaults (for example Airflow's `catchup` default, which changed between
2.x and 3.x) before building on them.
