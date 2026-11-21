# Running Canopy

Canopy ships as two programs:

| Program | Runs on | What it does |
|---|---|---|
| `canopy-hub` | one server (or your own computer to try it) | The web app, the API, the live boards, email, and its own PostgreSQL. One file, nothing else to install. |
| `canopy` | every computer that runs agents | Pairs with the hub, runs Claude Code in a sandbox, works in git branches, syncs folders. |

Build them with `make release` (or `scripts/release.sh`). They land in `dist/`:

```
dist/canopy-hub-linux-amd64    dist/canopy-linux-amd64
dist/canopy-hub-linux-arm64    dist/canopy-linux-arm64
dist/canopy-hub-darwin-amd64   dist/canopy-darwin-amd64
dist/canopy-hub-darwin-arm64   dist/canopy-darwin-arm64
```

`PLATFORMS="linux/amd64" scripts/release.sh` builds one platform, which is quicker. The hub embeds
the client builds it was built with, so people can download `canopy` straight from the hub.

### Releases on GitHub

`.github/workflows/release.yml` runs the same script on GitHub. Push a version tag to publish a
release with all eight binaries and a `SHA256SUMS` file:

```sh
git tag v0.1.0
git push origin v0.1.0
```

A tag with a dash, such as `v0.2.0-rc.1`, becomes a pre-release. Running the workflow by hand
from the Actions tab builds the binaries and attaches them to the run without publishing.

## Install

Both programs install themselves into a folder on your `PATH`, so you can run them from any
folder. The quickest way is the installer attached to each release. It picks the build for your
system, checks it against `SHA256SUMS`, and installs it:

```sh
# the client, as canopy
curl -fsSL https://github.com/ayyshim/canopy/releases/latest/download/install.sh | sh

# the hub, as canopy-hub
curl -fsSL https://github.com/ayyshim/canopy/releases/latest/download/install.sh | sh -s -- hub
```

Add `--version v0.1.0` after `--` for a particular release. For a private fork, where `curl`
can’t download, use the GitHub CLI, signed in:
`gh release download -R OWNER/canopy -p install.sh -O - | CANOPY_REPO=OWNER/canopy sh`.

If you downloaded or built a binary yourself, let it install itself:

```sh
chmod +x canopy-linux-amd64
./canopy-linux-amd64 install          # becomes canopy
./canopy-hub-linux-amd64 install      # becomes canopy-hub
```

`install` copies the binary to `~/.local/bin` (`/usr/local/bin` when run with `sudo`; choose
another folder with `--dir` or `CANOPY_BIN_DIR`). If that folder isn’t on your `PATH`, it adds it to
your shell’s startup file (`~/.zshrc`, `~/.bashrc`, `~/.bash_profile` on macOS, fish’s
`conf.d`, or `~/.profile`). Open a new terminal afterwards. `--no-path` leaves startup files
alone. To upgrade later, use [`update`](#upgrades-and-backups).

On macOS, a file downloaded in a browser is blocked until you clear its quarantine flag:
`xattr -d com.apple.quarantine canopy-darwin-arm64`. The installer above doesn’t need this.

## Try it on one computer (about 5 minutes)

What the computer needs:

- Linux. On macOS the client can't sandbox agents yet; see [Known gaps](#known-gaps).
- Claude Code installed and signed in (`claude`, then `/login`), or an Anthropic API key.
- `git`. Add `gh`, signed in with `gh auth login`, if you want pull requests opened for you.
- bubblewrap for the sandbox: `sudo apt install bubblewrap`, `sudo pacman -S bubblewrap` or
  `sudo dnf install bubblewrap`.

1. [Install](#install) the hub, then start it from any folder:

   ```sh
   canopy-hub
   ```

   It starts in the background, keeps its data in `~/.local/share/canopy-hub` with its own
   PostgreSQL, and prints where to open it and a setup link:

   ```
   The hub is running (pid 4182).
     Open:   http://localhost:8080
     Set up: http://localhost:8080/hub-setup?token=...
     Data:   /home/you/.local/share/canopy-hub
     Logs:   /home/you/.local/share/canopy-hub/hub.log
   ```

2. Open the setup link. Name the hub and create the first hub admin (you). You land on the hub
   admin pages, signed in.
3. Press **Create an organization** at the top of the admin page and name it.
4. Pair this computer: open **Set up a workspace**, or go to **Organization → Computers** and
   press **Get install command**. Canopy shows a one-time code and a command. In a second
   terminal, run either:

   ```sh
   # downloads canopy from the hub, pairs it, and keeps it running as a user service
   curl -fsSL http://localhost:8080/install.sh | sh -s -- --code ABCD-EFGH

   # or, with canopy already installed
   canopy pair --hub http://localhost:8080 --code ABCD-EFGH
   canopy run
   ```

   The setup page turns green when the hub can reach the computer.
5. Finish the wizard: name the workspace, pick repositories and folders on your computer, choose
   how Claude Code signs in, review the members, and press **Create workspace**. Give a manager a
   goal and watch the tree.

`canopy-hub status` shows the same again, including the setup link until setup is done.
`canopy-hub stop` stops the hub; `canopy-hub` starts it again from any folder. Everything is in
`~/.local/share/canopy-hub`. To keep the hub in the terminal instead, with its log on screen,
run `canopy-hub --foreground` (Ctrl+C stops it).

Older hubs kept their data in `./canopy-data` under the folder they started in. The hub warns
when it finds one; keep using it with `canopy-hub --data ./canopy-data`.

## Run it for a team

People reach the hub in a browser; their computers connect to it on a second port with mutual
TLS. Open two ports on the server: the web port (443, or 8080 behind your own proxy) and the
runner port (8443).

### Option A: the hub handles HTTPS itself

Point a DNS name at the server, then install the hub as a boot service (see
[Start at boot](#start-at-boot-systemd)):

```sh
sudo canopy-hub --as-srv --domain canopy.example.com
```

It gets certificates from Let’s Encrypt automatically. It listens on 80 and 443 for people and on
8443 for computers.

### Option B: behind Caddy or nginx

```sh
sudo TRUST_PROXY=true canopy-hub --as-srv --addr 127.0.0.1:8080 --public-url https://canopy.example.com
```

Proxy everything for that name to `127.0.0.1:8080`, including WebSockets on `/ws/`.
`deploy/Caddyfile` has a ready config. Computers connect to port 8443 directly, not through the
proxy, so open 8443 on the firewall.

### Email

Without SMTP the hub writes emails (invites, password resets, notifications) to its log. To send
them:

```sh
sudo SMTP_ADDR=smtp.example.com:587 SMTP_FROM="Canopy <canopy@example.com>" canopy-hub --as-srv ...
```

Put the settings after `sudo`, as above: `sudo` drops settings set before it. `--as-srv` saves
them in `DATA/hub.env`. To change them later, edit that file and run
`sudo systemctl restart canopy-hub`.

People choose their own notifications on **Account and notifications**. Questions, approvals,
errors and goals that are ready are emailed after they’ve waited two minutes. The daily summary
goes out at 8:00 in the time zone of the person’s current organization. Desktop notifications show while Canopy is open in
a background tab.

### Start at boot (systemd)

On Linux with systemd, `--as-srv` installs the hub as a service that starts at every boot:

```sh
sudo canopy-hub --as-srv [flags]
```

It:

- creates the system user `canopy`, because the built-in PostgreSQL refuses to run as root;
- copies the binary to `/usr/local/bin/canopy-hub`;
- keeps data in `/var/lib/canopy` (or `--data`), owned by `canopy`. If you ran the hub as yourself
  before, it copies your `~/.local/share/canopy-hub` there first, so nothing is lost;
- saves the settings you passed after `sudo` in `/var/lib/canopy/hub.env`;
- writes `/etc/systemd/system/canopy-hub.service`, enables it, starts it, and prints the address
  and setup link.

Run it again with new flags to change them. Then:

```sh
sudo canopy-hub status        # running?, address, setup link
sudo canopy-hub stop          # stop now; it starts again at the next boot
sudo canopy-hub               # start it again now
journalctl -u canopy-hub -f   # its log
sudo canopy-hub remove-srv    # remove the service; the data stays in /var/lib/canopy
```

`sudo canopy-hub migrate …` and `sudo canopy-hub backup` run as `canopy` by themselves.

On macOS, or without systemd, start the hub in the background with `canopy-hub`, or run
`canopy-hub --foreground` from your own service manager.

### Testing on a home network over plain http

Browsers only keep secure cookies on https and on localhost. For a quick test from other
machines over http, turn that off:

```sh
COOKIE_SECURE=false canopy-hub --public-url http://192.168.1.20:8080
```

### Upgrades and backups

- **Upgrade the hub:** `canopy-hub update` (`sudo canopy-hub update` for the boot service). It
  downloads the latest GitHub release, checks it against the release’s `SHA256SUMS`, replaces the
  binary and restarts the hub with the same flags and settings. On start the hub backs up the
  database into `DATA/backups` and then upgrades it. `canopy-hub update --check` only says
  whether there is a newer release; `--version v0.2.0` picks one. `canopy-hub migrate status`
  shows what an upgrade would change; `--no-auto-migrate` makes the hub refuse to start until you
  run `canopy-hub migrate up`.
- **Then the computers:** the new hub serves the matching client, so run `canopy update` on each
  computer. It downloads the hub’s build, replaces `canopy`, and restarts it (the user service
  or the background process). A computer that isn’t paired updates from the latest GitHub release.
- Hubs and clients from `v0.1.0` don’t have `update` yet; install those once more with the
  [installer](#install).
- **Back up:** stop the hub and run `canopy-hub backup` (`sudo canopy-hub backup` for the boot
  service). Also copy
  `DATA/blobs` (outputs, folder copies, branch backups) unless you use S3.
- **Your own PostgreSQL:** set `DATABASE_URL=postgres://user:pass@host/canopy`. Back it up with
  `pg_dump`.

What’s in the data directory: `postgres/` (the database), `blobs/` (files), `certs/` (HTTPS
certificates with `--domain`), `backups/`, `hub.env` (settings), `hub.log` (the log in the
background), `hub.json` (the running hub’s address, for `status`), and `hub.lock`, which holds the
running hub’s process id and stops two hubs from sharing one directory.

## Connect people’s computers

Each person connects their own computer. Their goals run there with their own Claude sign-in.

1. In Canopy: **Organization → Computers → Get install command**. Copy the command.
2. On their computer:

   ```sh
   curl -fsSL https://canopy.example.com/install.sh | sh -s -- --code ABCD-EFGH --name "Sam’s laptop"
   ```

   This installs `~/.local/bin/canopy`, adds that folder to their `PATH` if needed (open a new
   terminal afterwards), and pairs it. On Linux with systemd it also runs it as a user service.
   Manage it with:

   ```sh
   systemctl --user status canopy
   systemctl --user restart canopy
   journalctl --user -u canopy -f
   ```

   Without systemd, run `canopy run` yourself. It starts in the background, writes its log to
   `~/.local/share/canopy/canopy.log`, and `canopy stop` stops it. It doesn’t start again after
   a restart of the computer.

Admins can also share a server: pair it the same way, sign its Claude Code in with an API key
on **Organization → Computers**, and press **Share with everyone**. People without
their own computer then run goals there.

## Command reference

### canopy-hub

```
canopy-hub [serve] [flags]     start the hub in the background (the default)
canopy-hub --foreground [flags]
                               run the hub in this terminal
canopy-hub --as-srv [flags]    install the hub as a boot service (Linux, with sudo)
canopy-hub status [--data DIR] whether the hub runs, its address and setup link
canopy-hub stop [--data DIR]   stop the hub
canopy-hub remove-srv          remove the boot service; its data stays
canopy-hub update [--check] [--version vX.Y.Z]
                               install the latest release and restart the hub
canopy-hub install [--dir DIR] [--no-path]
                               copy this binary into a folder on PATH as canopy-hub
canopy-hub migrate status      show the data's version and what an upgrade would change
canopy-hub migrate up          upgrade the data now (the hub also does this on start)
canopy-hub backup [--data DIR] copy the database into DIR/backups (stop the hub first)
canopy-hub version             version, database schema and PostgreSQL version
canopy-hub help
```

Flags for `serve` (`migrate` and `backup` take `--data` too):

| Flag | Environment | Default | Meaning |
|---|---|---|---|
| `--data DIR` | `CANOPY_DATA_DIR` | `~/.local/share/canopy-hub` (`$XDG_DATA_HOME/canopy-hub` if set), `/var/lib/canopy` as root | Where the hub keeps everything |
| `--addr ADDR` | `HUB_ADDR` | `:8080` | Web and API listen address |
| `--public-url URL` | `PUBLIC_URL` | `http://localhost:` plus the `--addr` port | The address people open; used in links and emails |
| `--domain NAME` | | | Serve HTTPS for NAME on 443, with automatic certificates |
| `--runner-addr ADDR` | `RUNNER_ADDR` | `:8443` | Where computers connect (mutual TLS) |
| `--no-auto-migrate` | | off | Refuse to start if the data needs an upgrade |
| `--foreground` | | off | Stay in this terminal and log there, instead of starting in the background |
| `--as-srv` | | | Install as a systemd service that starts at boot, with the other flags (Linux, needs `sudo`) |

Settings can also go in `DATA/hub.env` as `KEY=value` lines. Settings in the environment win
over the file.

Other environment variables:

| Variable | Default | Meaning |
|---|---|---|
| `DATABASE_URL` | built-in PostgreSQL | Use your own PostgreSQL 16 |
| `SMTP_ADDR` | log only | SMTP server, `host:port` |
| `SMTP_FROM` | `Canopy <no-reply@canopy.local>` | Sender address |
| `HUB_SETUP_TOKEN` | generated | The code in the first-run setup link |
| `RUNNER_URL` | `wss://PUBLIC_URL host:8443/ws/runners` | Where computers dial, if different |
| `RUNNER_HOSTS` | | Extra names for the runner certificate, comma separated |
| `COOKIE_SECURE` | `true` (`false` on plain localhost) | Set `false` only for plain http that isn’t localhost |
| `TRUST_PROXY` | `false` | `true` behind a proxy that sets `X-Forwarded-For` |
| `AUTH_RATE_LIMIT` | `10` | Sign-in attempts per minute per address |
| `DEFAULT_SESSION_LIMIT` | `2000000` | Token limit per session for new organizations |
| `DEFAULT_WEEKLY_LIMIT` | `20000000` | Weekly token limit for new organizations |
| `REDIS_URL` | | Share live updates and rate limits between several hubs |
| `S3_ENDPOINT`, `S3_ACCESS_KEY`, `S3_SECRET_KEY`, `S3_BUCKET` (`canopy`), `S3_SECURE` | | Keep files in S3-compatible storage |
| `BLOB_DIR` | `DATA/blobs` | Where files go without S3 |

### canopy

```
canopy install [--dir DIR] [--no-path]
canopy pair --hub URL --code CODE [--name NAME] [--address HOST] [--run]
canopy run [--hub URL] [--sandbox auto|bwrap|none] [--unsafe-no-sandbox] [--foreground] [-v]
canopy stop
canopy update [--check] [--version vX.Y.Z] [--hub URL]
canopy status
canopy version
canopy help
```

| Command and flag | Meaning |
|---|---|
| `install` | Copy this binary into a folder on `PATH` as `canopy`, adding the folder to `PATH` if needed |
| `install --dir DIR` | Install into DIR (default `~/.local/bin`, `/usr/local/bin` as root, or `CANOPY_BIN_DIR`) |
| `install --no-path` | Don’t change shell startup files |
| `pair --hub URL` | The hub’s address, as people open it |
| `pair --code CODE` | One-time code from Canopy (valid 15 minutes) |
| `pair --name NAME` | Name shown in Canopy (default: the hostname) |
| `pair --address HOST` | For servers: the name people know it by |
| `pair --run` | Start running in the background right after pairing |
| `run` | Connect to every paired hub and run agents, in the background. Prints the process id and log file, then returns. Refuses to start a second copy. |
| `run --foreground` | Stay in this terminal and log there. The systemd service uses this; so can you when debugging. |
| `run --hub URL` | Connect to just this hub |
| `run --sandbox auto\|bwrap\|none` | Sandbox to use (`auto` picks bubblewrap on Linux) |
| `run --unsafe-no-sandbox` | Allow running without a sandbox. Agents then see everything your user can. Development only. |
| `run -v` | Verbose logs |
| `stop` | Stop canopy running in the background (waits up to 15 seconds) |
| `update` | Replace `canopy` with the build your hub serves, then restart it if it was running. Without a pairing: the latest GitHub release. |
| `update --check` | Only say whether there is a different build |
| `update --version vX.Y.Z` | Install that GitHub release instead of the hub’s build |
| `update --hub URL` | With several pairings: update from this hub |
| `status` | Whether canopy is running, pairings, installed CLIs and their sign-in, and the sandbox |

`canopy mcp` and `canopy sandbox-init` also exist. The client runs them itself inside agent
sessions; you don’t call them.

Environment:

| Variable | Default | Meaning |
|---|---|---|
| `CANOPY_HOME` | `~/.config/canopy` | Pairings and keys, one folder per hub under `hubs/`. Delete one to unpair. |
| `CANOPY_DATA` | `~/.local/share/canopy` | Work folders (git worktrees), folder copies, sockets, `canopy.log` and `canopy.pid` |
| `CANOPY_CLAUDE_BIN` | `claude` on `PATH` | The Claude Code binary to run |
| `CANOPY_BIN_DIR` | `~/.local/bin` | Where `install` and the install scripts put the binary |
| `CLAUDE_CONFIG_DIR` | `~/.claude` | Where Claude Code keeps its sign-in |

Hub routes that serve the client: `/install.sh` (the install script) and
`/download/canopy-<os>-<arch>` (the binary).

## Development

```sh
make infra     # Postgres, Redis, RustFS (S3), Mailpit (http://localhost:8025), Caddy (http://localhost:8000)
make hub       # API on :8080; prints a setup link on first start
make web       # Next.js on :3000; open http://localhost:8000
make test      # Go tests (needs make infra)
make system    # hub + client + a fake Claude as real processes (needs make infra)
make lint      # go vet, eslint, TypeScript
make gen       # regenerate sqlc queries and TypeScript types
make build     # development binaries in hub/bin and client/bin (no embedded web app)
make release   # the two release binaries for every platform, in dist/
```

To re-record the board GIF in the README, run `scripts/record-board.sh`. It starts its own Postgres,
hub, web app and client (with `CANOPY_CLAUDE_BIN` set to fakeclaude), plays a goal with
`[tasks:3] [ask] [slow:2]`, and writes `docs/assets/board.gif`. It needs docker, go, node, pnpm and ffmpeg.

## Board shortcuts and attachments

- **Minimap.** The small map at the bottom right shows every node and your current view. Click or drag on it to move the board. Press **M**, or use the button by the zoom control, to hide or show it; the choice is kept in your browser. It starts hidden on phones.
- **Finished goals.** Open a finished goal from the workspace panel (filter: Done) and choose **Show its tasks on the board**.
- **Attachments.** Drop files on any attachment field, or paste a screenshot, up to 25 MB each and 20 at a time. Chat doesn't take files yet.

## Referencing items

Every goal, task and note has a number that is unique in its workspace: **G-12**, **T-34**, **N-5**. The number shows on task stickies, goal chips and in the details panels, with a **Copy ID** button.

- **Write an ID anywhere** you write text: a goal or task, a chat message, a comment, the "What should change?" box in a review, a note. Agents see the same IDs and can read an item with `read_item`.
- **Type `#`** in those fields to pick from a list of goals, tasks and notes. Use Up and Down, Enter to insert, Esc to close.
- **IDs become links** in text the board shows. A click opens the goal, task or note and centers it on the board.
- IDs in code (`T-1` between backticks, or a code block) stay plain text. An ID you can't see, or that doesn't exist, also stays plain text.

## Sticky notes

A note with a board position shows on the canvas as a sticky note, with its N-5 ID, a color, its title and the first lines of the text (Markdown works). Pin or unpin it in the Notes panel, and it also shows on the minimap.

- **Add one** with **Sticky note** in the top bar (or its menu on phones), or double-click empty canvas. You write it in place. New stickies are **Private**.
- **Move it** by dragging. The position is saved a moment after you stop.
- **Private or Shared.** A private note is seen only by you and by the agents working for you (goals you gave, chats with you). Nobody else sees it, admins included, and it never reaches their browsers. Shared notes are seen by the whole workspace. Only the author switches a note between the two (Notes panel).
- **Who edits.** The author edits their notes; admins also edit shared ones. Agents can update notes with `update_note` when asked or clearly useful. A sticky shows "Edited by <agent>" and glows briefly when an agent changes it.
- **History.** Every change to the title or text keeps the earlier version (the last 20). Open the note in the Notes panel and choose **Restore**; the current text is kept in the history too.

## Prototype pages

When an agent submits an HTML file, the output viewer shows it as a live page ("Prototype"), with its CSS,
JavaScript and images. Agents save every page and asset in the task's outputs folder and link them with
relative paths (`about.html`, `assets/app.css`); subfolders keep their names. Click a link and the side panel
switches to that page. Use Full screen (or the F key) for the whole screen, and the Desktop, Tablet and Phone
buttons to check widths.

To try it without a real agent, give the fake runner a task with `[prototype]` in it. It writes a sample
three-page site and submits `index.html` for review. Pages run in a sandbox with no access to the hub, so
`localStorage` lasts across the pages of one preview but not after you close it. See DECISIONS.md.

## Known gaps

- **macOS computers have no sandbox yet.** The client refuses to run agents without one. On a
  Mac, `canopy run --unsafe-no-sandbox` works, but agents can then read everything your user
  can. For real work, use a Linux computer or VM, or a shared Linux server.
- **Claude Code only.** Codex, Gemini and Pi are detected but not run yet.
- Deleting an organization disconnects its computers; their folders and git branches stay on disk.

## Tools your agents can use

Agents use your Claude sign-in and see your user-level Claude setup: claude.ai connectors, your
MCP servers, plugins and skills, plus Canopy's own tools. Connect a connector in Claude first;
agents can't do browser sign-ins themselves. Using the API key option instead of a Claude
account means claude.ai connectors aren't available.

Artifact works only with a Claude account sign-in, not the API key option. The agent writes a
local HTML file first, then publishes it.

claude.ai connectors load a few seconds after the session starts, so an empty list at the start
doesn't mean they're off.
