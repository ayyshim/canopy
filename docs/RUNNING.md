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


## Try it on one computer (about 5 minutes)

What the computer needs:

- Linux. On macOS the client can't sandbox agents yet; see [Known gaps](#known-gaps).
- Claude Code installed and signed in (`claude`, then `/login`), or an Anthropic API key.
- `git`. Add `gh`, signed in with `gh auth login`, if you want pull requests opened for you.
- bubblewrap for the sandbox: `sudo apt install bubblewrap`, `sudo pacman -S bubblewrap` or
  `sudo dnf install bubblewrap`.

1. Start the hub:

   ```sh
   ./dist/canopy-hub-linux-amd64
   ```

   It creates `./canopy-data`, starts its own PostgreSQL inside it, and prints a setup link:

   ```
   WARN this hub isn't set up yet; open the setup link ... setup_url="http://localhost:8080/hub-setup?token=..."
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

   # or, with the binary you already have
   ./dist/canopy-linux-amd64 pair --hub http://localhost:8080 --code ABCD-EFGH
   ./dist/canopy-linux-amd64 run
   ```

   The setup page turns green when the hub can reach the computer.
5. Finish the wizard: name the workspace, pick repositories and folders on your computer, choose
   how Claude Code signs in, review the members, and press **Create workspace**. Give a manager a
   goal and watch the tree.

Stop the hub with Ctrl+C. Start it again with the same command; everything is in `canopy-data`.

## Run it for a team

People reach the hub in a browser; their computers connect to it on a second port with mutual
TLS. Open two ports on the server: the web port (443, or 8080 behind your own proxy) and the
runner port (8443).

### Option A: the hub handles HTTPS itself

Point a DNS name at the server, then:

```sh
sudo ./canopy-hub-linux-amd64 --domain canopy.example.com --data /var/lib/canopy
```

It gets certificates from Let’s Encrypt automatically. It listens on 80 and 443 for people and on
8443 for computers.

### Option B: behind Caddy or nginx

```sh
TRUST_PROXY=true ./canopy-hub-linux-amd64 \
  --addr 127.0.0.1:8080 \
  --public-url https://canopy.example.com \
  --data /var/lib/canopy
```

Proxy everything for that name to `127.0.0.1:8080`, including WebSockets on `/ws/`.
`deploy/Caddyfile` has a ready config. Computers connect to port 8443 directly, not through the
proxy, so open 8443 on the firewall.

### Email

Without SMTP the hub writes emails (invites, password resets, notifications) to its log. To send
them:

```sh
SMTP_ADDR=smtp.example.com:587 SMTP_FROM="Canopy <canopy@example.com>" ./canopy-hub-linux-amd64 ...
```

People choose their own notifications on **Account and notifications**. Questions, approvals,
errors and goals that are ready are emailed after they’ve waited two minutes. The daily summary
goes out at 8:00 in the time zone of the person’s current organization. Desktop notifications show while Canopy is open in
a background tab.

### Keep it running (systemd)

`/etc/systemd/system/canopy-hub.service`:

```ini
[Unit]
Description=Canopy hub
After=network-online.target

[Service]
User=canopy
ExecStart=/usr/local/bin/canopy-hub --domain canopy.example.com --data /var/lib/canopy
Environment=SMTP_ADDR=smtp.example.com:587
Environment="SMTP_FROM=Canopy <canopy@example.com>"
AmbientCapabilities=CAP_NET_BIND_SERVICE
Restart=always

[Install]
WantedBy=multi-user.target
```

```sh
sudo systemctl enable --now canopy-hub
journalctl -u canopy-hub -f      # the first start prints the setup link here
```

### Testing on a home network over plain http

Browsers only keep secure cookies on https and on localhost. For a quick test from other
machines over http, turn that off:

```sh
COOKIE_SECURE=false ./canopy-hub-linux-amd64 --public-url http://192.168.1.20:8080 --addr :8080
```

### Upgrades and backups

- **Upgrade:** stop the hub, replace the binary, start it. It backs up the database into
  `DATA/backups` and then upgrades it. `canopy-hub migrate status` shows what an upgrade would
  change; `--no-auto-migrate` makes the hub refuse to start until you run `canopy-hub migrate up`.
- **Back up:** stop the hub and run `canopy-hub backup --data /var/lib/canopy`. Also copy
  `DATA/blobs` (outputs, folder copies, branch backups) unless you use S3.
- **Your own PostgreSQL:** set `DATABASE_URL=postgres://user:pass@host/canopy`. Back it up with
  `pg_dump`.

What’s in the data directory: `postgres/` (the database), `blobs/` (files), `certs/` (HTTPS
certificates with `--domain`), `backups/`, and `hub.lock`, which stops two hubs from sharing one
directory.

## Connect people’s computers

Each person connects their own computer. Their goals run there with their own Claude sign-in.

1. In Canopy: **Organization → Computers → Get install command**. Copy the command.
2. On their computer:

   ```sh
   curl -fsSL https://canopy.example.com/install.sh | sh -s -- --code ABCD-EFGH --name "Sam’s laptop"
   ```

   On Linux with systemd this installs `~/.local/bin/canopy`, pairs it, and runs it as a user
   service. Manage it with:

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
canopy-hub [serve] [flags]     run the hub (the default)
canopy-hub migrate status      show the data's version and what an upgrade would change
canopy-hub migrate up          upgrade the data now (the hub also does this on start)
canopy-hub backup [--data DIR] copy the database into DIR/backups (stop the hub first)
canopy-hub version             version, database schema and PostgreSQL version
canopy-hub help
```

Flags for `serve` (`migrate` and `backup` take `--data` too):

| Flag | Environment | Default | Meaning |
|---|---|---|---|
| `--data DIR` | `CANOPY_DATA_DIR` | `./canopy-data` | Where the hub keeps everything |
| `--addr ADDR` | `HUB_ADDR` | `:8080` | Web and API listen address |
| `--public-url URL` | `PUBLIC_URL` | `http://localhost:8080` | The address people open; used in links and emails |
| `--domain NAME` | | | Serve HTTPS for NAME on 443, with automatic certificates |
| `--runner-addr ADDR` | `RUNNER_ADDR` | `:8443` | Where computers connect (mutual TLS) |
| `--no-auto-migrate` | | off | Refuse to start if the data needs an upgrade |

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
canopy pair --hub URL --code CODE [--name NAME] [--address HOST] [--run]
canopy run [--hub URL] [--sandbox auto|bwrap|none] [--unsafe-no-sandbox] [--foreground] [-v]
canopy stop
canopy status
canopy version
canopy help
```

| Command and flag | Meaning |
|---|---|
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
| `status` | Whether canopy is running, pairings, installed CLIs and their sign-in, and the sandbox |

`canopy mcp` and `canopy sandbox-init` also exist. The client runs them itself inside agent
sessions; you don’t call them.

Environment:

| Variable | Default | Meaning |
|---|---|---|
| `CANOPY_HOME` | `~/.config/canopy` | Pairings and keys, one folder per hub under `hubs/`. Delete one to unpair. |
| `CANOPY_DATA` | `~/.local/share/canopy` | Work folders (git worktrees), folder copies, sockets, `canopy.log` and `canopy.pid` |
| `CANOPY_CLAUDE_BIN` | `claude` on `PATH` | The Claude Code binary to run |
| `CLAUDE_CONFIG_DIR` | `~/.claude` | Where Claude Code keeps its sign-in |
| `CANOPY_BIN_DIR` | `~/.local/bin` | Install script only: where to put `canopy` |

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

## Known gaps

- **macOS computers have no sandbox yet.** The client refuses to run agents without one. On a
  Mac, `canopy run --unsafe-no-sandbox` works, but agents can then read everything your user
  can. For real work, use a Linux computer or VM, or a shared Linux server.
- **Claude Code only.** Codex, Gemini and Pi are detected but not run yet.
- Deleting an organization disconnects its computers; their folders and git branches stay on disk.
