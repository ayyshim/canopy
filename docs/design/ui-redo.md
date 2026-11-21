# UI redo: audit and design direction

Goal: make the whole UI feel consistent, descriptive and minimal. Long sections become tabs. Every
goal and task shows what it cost in tokens and dollars.

Screenshots are in `docs/design/shots/` (desktop 1440×900 and phone 390×844, real app, seeded workspace).

## 1. What looks chopped

Measured from `frontend/src/app/globals.css` (712 lines) and the components.

| Problem | Evidence |
|---|---|
| No type scale | 20 different `font-size` values (8, 10, 10.5, 11, 11.5, 12, 12.5, 13, 13.5, 14, 14.5, 15, 15.5, 16, 16.5, 17, 19, 21, 26, 28px). Half-pixel sizes sit next to each other. |
| No radius scale | 16 radii (2 to 20px, plus 99px). Cards use 12, 14, 16 or 18px depending on the screen. |
| Ad-hoc spacing | Gaps and padding use 1 to 48px, with 5, 7, 9, 11 and 18 common. 297 inline `style={{…}}` in components. |
| Raw colors | About 19 `#fff` and many hex values outside the tokens, so dark mode patches are repeated three times (media query, `data-theme="dark"`). |
| Long scroll panels | The goal panel (done goal) stacks status, runs on, owner, attachments, tasks by member and more in one scroll with the actions pinned at the bottom. The workspace panel stacks goals, completion mode, notes. Task panel stacks 8 blocks. |
| Settings pages are one long form | Workspace general mixes name, description, logo, completion mode and notes in one page. |
| Raw activity dump | Chat Activity tab is a monospace wall of log lines with no grouping or time. |
| Mixed button sizes | Panel footers mix `Add a task`, `Chat with Riley` and wrapped secondary buttons (workspace panel footer wraps onto two rows). |
| Default browser links | Sign-in and admin pages show unstyled blue underlined links. |
| Crowded tiny board text | At the default zoom (57 to 77%) node text is 7 to 9px and unreadable. On phones the board opens cropped. |
| No cost anywhere | Cards and panels show session token counts at best (`3m · 360`). No dollars, no per-goal or per-task totals. |
| Tables overflow on phones | People and Members tables cut off the right-hand columns. |
| Nothing says what to do next | The outputs tray and empty areas have no empty state copy, only blank space. |

## 2. Patterns to borrow

Mobbin blocked automated access (HTTP 403), so I could not browse its showcase. These patterns come from public
write-ups of Linear, Vercel, Stripe and Notion dashboards; please check them against Mobbin screens you like.

1. **Object tabs** (Vercel project, Stripe customer): one header, then tabs for facets of the same object. Max 5 tabs.
2. **Stat tile row** (Stripe, Linear Insights): 3 to 4 calm tiles above the fold, number large, label small, detail on click.
3. **Right side sheet** (Linear issue, Notion peek): detail opens beside the list, full-screen on phones.
4. **Calm list rows** (Linear): one line per item, status dot on the left, meta on the right, no borders on every row.
5. **Empty state with one action** (Stripe): one icon, one line of why, one button.
6. **Sticky header and action bar** (Vercel): title and primary action stay put, body scrolls.

## 3. Foundations

### Type scale (Inter for UI, Bricolage for display, JetBrains Mono for numbers and code)

Seven steps only. Replace every other size.

| Token | Size / line | Weight | Use |
|---|---|---|---|
| `--t-xs` | 12 / 16 | 500 | Meta, badges, table headers |
| `--t-sm` | 13 / 18 | 400 | Secondary text, hints |
| `--t-md` | 14 / 20 | 400 | Body, inputs, buttons |
| `--t-lg` | 16 / 24 | 600 | Section titles, card titles |
| `--t-xl` | 20 / 28 | 600 display | Panel and dialog titles |
| `--t-2xl` | 28 / 34 | 700 display | Page titles |
| `--t-num` | 24 / 28 | 600 mono tabular | Stat tile numbers |

Minimum 12px anywhere. Board node text is 13px at 100% zoom, and the start zoom is never below 80%.

### Spacing (4px grid)

`--sp-1:4 --sp-2:8 --sp-3:12 --sp-4:16 --sp-5:24 --sp-6:32 --sp-7:48`. No other values. Panel padding is `--sp-4`
(16) on phones and `--sp-5` (24) on desktop. Gap between sections is `--sp-5`; inside a card `--sp-3`.

### Radius and borders

`--r-sm:6` (chips, inputs inner), `--r-md:10` (buttons, inputs, rows), `--r-lg:14` (cards, panels, sheets),
`--r-full:999` (pills, avatars). One border: 1px `--edge`. Shadow only on floating things (menus, sheets, dialogs).

### Color tokens

Keep the existing palette; make it semantic and stop using raw hex. Define once for light and once for dark, and let
the media query and `data-theme` share one block.

| Token | Light | Dark | Use |
|---|---|---|---|
| `--bg` (was `--paper`) | #F6F7F9 | #101419 | Page |
| `--surface` (was `--card`) | #FFFFFF | #1A2028 | Cards, panels |
| `--surface-2` (was `--soft`) | #F1F3F6 | #222934 | Rows, tab track, tiles |
| `--edge` | #DDE1E7 | #2C3440 | Borders |
| `--ink` / `--muted` / `--faint` | #14181F / #4A5462 / #6B7584 | #E6EBF0 / #A3AEBB / #8693A2 | Text |
| `--accent` (was `--green`) | #16794F | #3FA877 | Primary action, active tab, progress |
| `--warn` / `--bad` / `--info` | amber / red / blue | existing dark values | Status |
| `--cost` | #6E56CF | #9D86FF | Tokens and cost only, so money is always the same color |

Status dots: todo amber, doing blue, done accent, blocked red, asleep faint.

## 4. Components

Build once in `frontend/src/components/ui/`; replace the ad-hoc CSS.

- **Tabs**: underline style, 40px high, horizontally scrollable on phones, count badge optional, `role=tablist`, keyboard arrows. Remembers the last tab per panel type.
- **StatTile**: label (xs, muted), value (num), sub (sm, muted). Grid of 2 columns on phones, 4 on desktop.
- **UsageBadge**: `12.4k tokens · $0.31` in the cost color, mono, one line. Variants: inline (cards), tile (detail). Shows `–` when no data.
- **Card**: surface, 1px edge, `--r-lg`, `--sp-4` padding. Optional header row (title left, action right).
- **SectionHeader**: title (lg) + muted description line + optional action.
- **EmptyState**: icon, one line, one button. Used in tabs, outputs, tables.
- **Sheet**: right side panel 420px on desktop, full-screen on phones, with sticky header (title, close, tabs) and sticky action bar.
- **Row**: 44px tall, status dot, title, meta right, chevron. Replaces the bordered task and person boxes.

## 5. Which long sections become tabs

| Screen | Tabs | Contents |
|---|---|---|
| Goal panel | **Overview**, **Tasks**, **Outputs**, **Activity**, **Usage** | Overview: status, progress, owner, runs on, description, done-when, attachments. Tasks: rows grouped by member. Outputs: thumbs. Activity: grouped timeline. Usage: totals and per-model breakdown. Pinned bar: Add a task, Chat. |
| Task panel | **Overview**, **Steps**, **Discussion**, **Usage** | Overview: status, assignee, result, times, access. Steps: checklist and waits on. Discussion: comments and the Post box. Usage: tokens in/out, cache, cost, model. |
| Workspace panel | **Goals**, **Notes**, **Directories**, **People** | Goals: filter chips and rows. Notes: plans, summaries, decisions. Directories and People move out of the footer buttons. |
| Member chat window | **Chat**, **Activity**, **Usage**, **Settings** | Add Usage; Activity becomes a timeline instead of raw log lines. |
| Workspace settings page | **General**, **Directories**, **Members**, **Skills**, **Access**, **Danger** | Already routed pages; General is split so Notes and completion mode are their own cards, and long cards become tabs on the page. |
| Org usage page | **Overview**, **People**, **Agents**, **Goals** | Tile row on top, chart and tables per tab. Adds goals and tasks with cost. |
| Org settings (People) | **Members**, **Invitations**, **Invite** | The invite form moves to a dialog. |
| Admin | **Overview**, **Organizations**, **Users**, **Settings** | Keep as pages, but use the same tile row and tab bar on phones. |
| Onboarding | Stays a stepper | Collapse the left step list into a progress bar of 8 dots on phones. |

Rule: if a panel scrolls more than about 1.5 screens on a 900px display, split it.

## 6. Token and cost stats

- **Cards (board, task rows, goal rows):** one `UsageBadge` on the meta line: `12.4k · $0.31`.
- **Goal Usage tab:** 4 tiles (Total cost, Tokens in, Tokens out, Cache read). Then a per-model table (model, in, out, cache, cost) and a by-task list sorted by cost. A small bar for each task's share.
- **Task Usage tab:** same tiles for the task, plus the sessions it used.
- **Member and session nodes:** keep the existing token count, add cost on hover and in the chat Usage tab.
- **Org usage page:** tile row for cost this period, with per-goal and per-task tables.
- Cost is calculated by the hub from a model price table (tokens in, out, cache). Numbers show `$0.31` above $1 as `$12.40`, below one cent as `<$0.01`. Tokens use k and M with one decimal.
- Live cost updates arrive over the board websocket with the task, so tiles update while a task runs.

## 7. Copy rules

Sentence case, plain verbs, curly quotes, no jargon. Every section header has at most one muted line below it.
Buttons are verbs (`Add a task`, `Give a goal`). Empty states say what goes here and how to add it.

## 8. Phone rules

- 44px minimum tap targets; 16px inputs (already set).
- Sheets are full-screen; dialogs are bottom sheets (already set).
- Tab bars scroll sideways and never wrap.
- Tables become stacked rows under 640px, with the key value on the first line.
- Board opens at 80% and anchors to the manager row; a one-tap "fit" button resets.

## 9. Order of work

1. Prototype (docs/design/prototype.html) to approve look and tab layout.
2. Tokens in `globals.css` and `components/ui/*` (type scale, spacing, radius, shared components).
3. Usage tracking in the hub (task and goal ids, price table) so the numbers are real.
4. Board, goal and task panels, chat, dialogs.
5. Settings, admin and onboarding pages.
6. Review against the prototype on desktop and phone.

## 10. Open questions

- Dark mode stays as is (follows system, with a manual toggle)? Assumed yes.
- Cost uses list prices, not what a subscription actually costs. Label it “estimated cost”? Assumed yes.
