// Package web serves the web app (the Next.js static export embedded in the hub binary), the
// client downloads and the install script.
package web

import (
	"embed"
	"fmt"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

//go:embed all:dist
var dist embed.FS

//go:embed all:clients
var clients embed.FS

// dynamicRoutes are exported once with a "_" placeholder; real ids are read by the page from
// the URL.
var dynamicRoutes = []string{"/reset/", "/invite/", "/w/", "/admin/organizations/", "/org/usage/"}

func site() fs.FS {
	sub, _ := fs.Sub(dist, "dist")
	return sub
}

// HasApp reports whether this binary carries the web app.
func HasApp() bool {
	_, err := fs.Stat(site(), "index.html")
	return err == nil
}

// App serves the web app.
func App() http.Handler {
	if !HasApp() {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprint(w, `<!doctype html><title>Canopy</title><p style="font-family:sans-serif">This hub was built without its web app. Use a release build of canopy-hub, or run the web app’s dev server.`)
		})
	}
	return serveSite(site())
}

// serveSite serves a static export. Files are served with ServeFileFS and the request's own
// path: http.FileServer would answer a request it sees as /index.html with a redirect to ./,
// which loops forever on the home page.
func serveSite(files fs.FS) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := path.Clean("/" + r.URL.Path)
		if strings.HasPrefix(p, "/_next/static/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		if name, ok := resolve(files, p); ok {
			http.ServeFileFS(w, r, files, name)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusNotFound)
		if b, err := fs.ReadFile(files, "404.html"); err == nil {
			_, _ = w.Write(b)
		}
	})
}

// resolve maps a URL path to a file in the export: exact files, .html pages, folder indexes,
// and dynamic routes through their "_" placeholder.
func resolve(files fs.FS, p string) (string, bool) {
	try := func(name string) (string, bool) {
		name = strings.TrimPrefix(name, "/")
		if name == "" {
			name = "index.html"
		}
		if fi, err := fs.Stat(files, name); err == nil && !fi.IsDir() {
			return name, true
		}
		return "", false
	}
	for _, c := range []string{p, p + ".html", strings.TrimSuffix(p, "/") + "/index.html"} {
		if n, ok := try(c); ok {
			return n, true
		}
	}
	for _, prefix := range dynamicRoutes {
		if !strings.HasPrefix(p, prefix) {
			continue
		}
		rest := strings.TrimPrefix(p, prefix)
		seg, tail, _ := strings.Cut(rest, "/")
		if seg == "" {
			continue
		}
		// The segment may carry a suffix (route payload files like "<id>.txt").
		ext := ""
		if i := strings.Index(seg, "."); i > 0 {
			seg, ext = seg[:i], seg[i:]
		}
		mapped := prefix + "_" + ext
		if tail != "" {
			mapped += "/" + tail
		}
		for _, c := range []string{mapped, mapped + ".html", mapped + "/index.html"} {
			if n, ok := try(c); ok {
				return n, true
			}
		}
	}
	return "", false
}

// Platforms lists the client builds this hub can hand out.
func Platforms() []string {
	entries, _ := fs.ReadDir(clients, "clients")
	var out []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "canopy-") {
			out = append(out, strings.TrimPrefix(e.Name(), "canopy-"))
		}
	}
	return out
}

// Download serves /download/canopy-<os>-<arch>.
func Download() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := path.Base(r.URL.Path)
		if !strings.HasPrefix(name, "canopy-") || strings.Contains(name, "..") {
			http.NotFound(w, r)
			return
		}
		b, err := fs.ReadFile(clients, "clients/"+name)
		if err != nil {
			http.Error(w, "No canopy build for "+strings.TrimPrefix(name, "canopy-")+" on this hub. Available: "+strings.Join(Platforms(), ", "), http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", `attachment; filename="canopy"`)
		_, _ = w.Write(b)
	})
}

// InstallScript downloads the client for this machine, pairs it with a code and keeps it
// running as a user service.
func InstallScript(hubURL string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/x-shellscript; charset=utf-8")
		fmt.Fprintf(w, installScript, hubURL)
	})
}

const installScript = `#!/bin/sh
# Installs the Canopy client, pairs it with this hub, and keeps it running.
# Usage: curl -fsSL %[1]s/install.sh | sh -s -- --code XXXX-XXXX [--name "My laptop"]
set -eu
HUB=%[1]q
CODE=""
NAME=""
while [ $# -gt 0 ]; do
  case "$1" in
    --code) CODE="$2"; shift 2 ;;
    --name) NAME="$2"; shift 2 ;;
    *) shift ;;
  esac
done
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)
case "$ARCH" in x86_64|amd64) ARCH=amd64 ;; aarch64|arm64) ARCH=arm64 ;; esac
BIN_DIR="${CANOPY_BIN_DIR:-$HOME/.local/bin}"
mkdir -p "$BIN_DIR"
DOWNLOAD="$BIN_DIR/.canopy-download.$$"
trap 'rm -f "$DOWNLOAD"' EXIT
echo "Downloading canopy for $OS/$ARCH from ${HUB}…"
curl -fsSL "$HUB/download/canopy-$OS-$ARCH" -o "$DOWNLOAD"
chmod +x "$DOWNLOAD"
# Puts it at $BIN_DIR/canopy and adds $BIN_DIR to PATH if it isn't there yet.
"$DOWNLOAD" install --dir "$BIN_DIR"
if [ "$OS" = "linux" ] && ! command -v bwrap >/dev/null 2>&1; then
  echo "Note: agents run in a bubblewrap sandbox. Install it first: sudo apt install bubblewrap (or your distribution's package)."
fi
if [ -n "$CODE" ]; then
  if [ -n "$NAME" ]; then "$BIN_DIR/canopy" pair --hub "$HUB" --code "$CODE" --name "$NAME"; else "$BIN_DIR/canopy" pair --hub "$HUB" --code "$CODE"; fi
fi
if [ "$OS" = "linux" ] && command -v systemctl >/dev/null 2>&1; then
  mkdir -p "$HOME/.config/systemd/user"
  cat > "$HOME/.config/systemd/user/canopy.service" <<UNIT
[Unit]
Description=Canopy client
After=network-online.target

[Service]
ExecStart=$BIN_DIR/canopy run --foreground
Restart=on-failure
RestartSec=5

[Install]
WantedBy=default.target
UNIT
  systemctl --user daemon-reload
  systemctl --user enable --now canopy.service
  echo "Canopy is running (systemctl --user status canopy)."
elif [ "$OS" = "darwin" ]; then
  # Agents must run in a sandbox, and the macOS client doesn't have one yet, so nothing is
  # started in the background here.
  echo "Installed. On macOS, agents can't run in a sandbox yet, so Canopy doesn't start them."
  echo "To run agents anyway (they can see everything your user can):"
  echo "  canopy run --unsafe-no-sandbox"
else
  echo "Start it with: canopy run"
fi
`
