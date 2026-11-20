// Package netproxy is the sandbox's only way out: an HTTP CONNECT proxy that allows the CLI's
// provider, package registries and hosts a person approved.
package netproxy

import (
	"bufio"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Defaults are reachable from every sandbox: providers and package registries.
var Defaults = []string{
	"api.anthropic.com", ".anthropic.com", "claude.ai", ".claude.ai",
	"registry.npmjs.org", "registry.yarnpkg.com", "pypi.org", "files.pythonhosted.org",
	"proxy.golang.org", "sum.golang.org", "storage.googleapis.com", "crates.io", "static.crates.io", "index.crates.io",
	"rubygems.org", "repo.maven.apache.org", "repo1.maven.org", "registry-1.docker.io",
}

type Proxy struct {
	mu    sync.RWMutex
	allow []string
}

func New(extra ...string) *Proxy {
	return &Proxy{allow: append(append([]string{}, Defaults...), extra...)}
}

// Allow adds a host approved by a person (e.g. a fetch they allowed).
func (p *Proxy) Allow(host string) {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" {
		return
	}
	p.mu.Lock()
	p.allow = append(p.allow, host)
	p.mu.Unlock()
}

func (p *Proxy) Allowed(host string) bool {
	host = strings.ToLower(host)
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	for _, a := range p.allow {
		if a == host || (strings.HasPrefix(a, ".") && strings.HasSuffix(host, a)) {
			return true
		}
	}
	return false
}

// Serve accepts proxy connections on l until it's closed.
func (p *Proxy) Serve(l net.Listener) {
	for {
		c, err := l.Accept()
		if err != nil {
			return
		}
		go p.handle(c)
	}
}

func (p *Proxy) handle(c net.Conn) {
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(30 * time.Second))
	br := bufio.NewReader(c)
	req, err := http.ReadRequest(br)
	if err != nil {
		return
	}
	host := req.Host
	if req.Method != http.MethodConnect {
		if req.URL != nil && req.URL.Host != "" {
			host = req.URL.Host
		}
	}
	if !p.Allowed(host) {
		slog.Info("sandbox network blocked", "host", host)
		_, _ = io.WriteString(c, "HTTP/1.1 403 Forbidden\r\nContent-Type: text/plain\r\nContent-Length: 49\r\n\r\nCanopy blocked this host; ask a person to allow it.")
		return
	}
	target := host
	if !strings.Contains(target, ":") {
		if req.Method == http.MethodConnect {
			target += ":443"
		} else {
			target += ":80"
		}
	}
	up, err := net.DialTimeout("tcp", target, 15*time.Second)
	if err != nil {
		_, _ = io.WriteString(c, "HTTP/1.1 502 Bad Gateway\r\nContent-Length: 0\r\n\r\n")
		return
	}
	defer up.Close()
	_ = c.SetDeadline(time.Time{})
	if req.Method == http.MethodConnect {
		_, _ = io.WriteString(c, "HTTP/1.1 200 Connection established\r\n\r\n")
	} else {
		req.RequestURI = ""
		if err := req.Write(up); err != nil {
			return
		}
	}
	done := make(chan struct{}, 2)
	go func() { _, _ = io.Copy(up, br); done <- struct{}{} }()
	go func() { _, _ = io.Copy(c, up); done <- struct{}{} }()
	<-done
}

// Forward runs inside the sandbox: it listens on loopback and passes each connection to the
// proxy's Unix socket (the sandbox has no other network).
func Forward(listenAddr, socket string) error {
	l, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return err
	}
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				u, err := net.Dial("unix", socket)
				if err != nil {
					return
				}
				defer u.Close()
				done := make(chan struct{}, 2)
				go func() { _, _ = io.Copy(u, c); done <- struct{}{} }()
				go func() { _, _ = io.Copy(c, u); done <- struct{}{} }()
				<-done
			}()
		}
	}()
	return nil
}
