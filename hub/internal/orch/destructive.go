package orch

import (
	"path"
	"regexp"
	"strings"
)

type shellCmd struct {
	words  []string
	piped  bool
	redirs []string
}

var sqlDestructive = regexp.MustCompile(`(?i)\b(drop|truncate)\b`)

// isDestructive reports whether any part of a shell command could destroy data.
// Commands it can't parse count as destructive.
func isDestructive(cmd string) bool {
	cmds, ok := parseShell(cmd, 0)
	if !ok {
		return true
	}
	for i, c := range cmds {
		for _, r := range c.redirs {
			if redirectOutside(r) {
				return true
			}
		}
		if destructiveCmd(c.words, c.piped, prevProg(cmds, i), 0) {
			return true
		}
	}
	return false
}

func prevProg(cmds []shellCmd, i int) string {
	if i > 0 && len(cmds[i-1].words) > 0 {
		return path.Base(cmds[i-1].words[0])
	}
	return ""
}

func redirectOutside(t string) bool {
	switch t {
	case "/dev/null", "/dev/stdout", "/dev/stderr":
		return false
	}
	if strings.HasPrefix(t, "/tmp/") || strings.HasPrefix(t, "/var/folders/") {
		return strings.Contains(t, "..")
	}
	return strings.HasPrefix(t, "/") || strings.HasPrefix(t, "~") || strings.HasPrefix(t, "$") || strings.Contains(t, "..")
}

// parseShell splits a command into simple commands, including $( ) and backtick bodies.
func parseShell(s string, depth int) ([]shellCmd, bool) {
	if depth > 5 {
		return nil, false
	}
	var out []shellCmd
	var cur shellCmd
	var word strings.Builder
	has, redirNext, pipeNext := false, false, false
	flushWord := func() {
		if !has {
			return
		}
		w := word.String()
		if redirNext {
			cur.redirs = append(cur.redirs, w)
			redirNext = false
		} else {
			cur.words = append(cur.words, w)
		}
		word.Reset()
		has = false
	}
	flushCmd := func(piped bool) {
		flushWord()
		if len(cur.words) > 0 || len(cur.redirs) > 0 {
			cur.piped = pipeNext
			out = append(out, cur)
		}
		cur = shellCmd{}
		pipeNext = piped
	}
	sub := func(body string) bool {
		inner, ok := parseShell(body, depth+1)
		out = append(out, inner...)
		return ok
	}
	var inS, inD bool
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case inS:
			if c == '\'' {
				inS = false
			} else {
				word.WriteByte(c)
			}
		case c == '\\' && i+1 < len(s):
			i++
			word.WriteByte(s[i])
			has = true
		case c == '`':
			j := strings.IndexByte(s[i+1:], '`')
			if j < 0 || !sub(s[i+1:i+1+j]) {
				return nil, false
			}
			i += j + 1
			has = true
		case c == '$' && i+1 < len(s) && s[i+1] == '(':
			depthP, j := 1, i+2
			for ; j < len(s) && depthP > 0; j++ {
				if s[j] == '(' {
					depthP++
				} else if s[j] == ')' {
					depthP--
				}
			}
			if depthP != 0 || !sub(s[i+2:j-1]) {
				return nil, false
			}
			i = j - 1
			has = true
		case inD:
			if c == '"' {
				inD = false
			} else {
				word.WriteByte(c)
			}
		case c == '\'':
			inS, has = true, true
		case c == '"':
			inD, has = true, true
		case c == ' ' || c == '\t':
			flushWord()
		case c == ';' || c == '\n':
			flushCmd(false)
		case c == '&':
			if i+1 < len(s) && s[i+1] == '&' {
				i++
			}
			flushCmd(false)
		case c == '|':
			if i+1 < len(s) && s[i+1] == '|' {
				i++
				flushCmd(false)
			} else {
				flushCmd(true)
			}
		case c == '>':
			// "2>&1" duplicates a descriptor and has no target.
			if has && word.String() != "" && strings.Trim(word.String(), "0123456789") == "" {
				word.Reset()
				has = false
			}
			flushWord()
			for i+1 < len(s) && s[i+1] == '>' {
				i++
			}
			if i+1 < len(s) && s[i+1] == '&' {
				i++
				for i+1 < len(s) && (s[i+1] == '-' || (s[i+1] >= '0' && s[i+1] <= '9')) {
					i++
				}
			} else {
				redirNext = true
			}
		default:
			word.WriteByte(c)
			has = true
		}
	}
	if inS || inD {
		return nil, false
	}
	flushCmd(false)
	return out, true
}

var shells = map[string]bool{"sh": true, "bash": true, "zsh": true, "dash": true, "ksh": true}

var interpreters = map[string]bool{"python": true, "python3": true, "perl": true, "ruby": true, "node": true}

func hasShortFlag(args []string, letters string) bool {
	for _, a := range args {
		if a == "--" {
			return false
		}
		if len(a) > 1 && a[0] == '-' && a[1] != '-' && strings.ContainsAny(a[1:], letters) {
			return true
		}
	}
	return false
}

func hasArg(args []string, names ...string) bool {
	for _, a := range args {
		for _, n := range names {
			if a == n {
				return true
			}
		}
	}
	return false
}

func destructiveCmd(w []string, piped bool, prev string, depth int) bool {
	if depth > 5 {
		return true
	}
	for len(w) > 0 && strings.Contains(w[0], "=") && !strings.HasPrefix(w[0], "-") && !strings.Contains(w[0], "/") {
		w = w[1:]
	}
	if len(w) == 0 {
		return false
	}
	prog := path.Base(w[0])
	args := w[1:]
	switch prog {
	case "rm":
		return hasShortFlag(args, "rRf") || hasArg(args, "--recursive", "--force")
	case "rmdir", "dd", "shred", "sudo", "su", "doas", "kill", "pkill", "killall", "shutdown", "reboot", "halt",
		"truncate", "diskutil", "fdisk", "parted", "wipefs", "unlink":
		return true
	case "chmod", "chown", "chgrp":
		return hasShortFlag(args, "R") || hasArg(args, "--recursive")
	case "git":
		return destructiveGit(args)
	case "docker", "podman", "kubectl", "helm":
		for _, a := range args {
			switch a {
			case "delete", "rm", "rmi", "prune", "kill", "uninstall":
				return true
			}
		}
		return false
	case "psql", "mysql", "sqlite3", "mongosh", "redis-cli":
		return sqlDestructive.MatchString(strings.Join(args, " ")) || hasArg(args, "FLUSHALL", "FLUSHDB")
	case "find":
		if hasArg(args, "-delete") {
			return true
		}
		for i, a := range args {
			if (a == "-exec" || a == "-execdir" || a == "-ok") && destructiveCmd(args[i+1:], false, "", depth+1) {
				return true
			}
		}
		return false
	case "xargs":
		i := 0
		for i < len(args) && strings.HasPrefix(args[i], "-") {
			i++
		}
		return destructiveCmd(args[i:], false, "", depth+1)
	case "env", "command", "nohup", "time", "nice", "exec", "builtin":
		i := 0
		for i < len(args) && strings.HasPrefix(args[i], "-") {
			i++
		}
		return destructiveCmd(args[i:], false, "", depth+1)
	case "eval":
		return isDestructive(strings.Join(args, " "))
	}
	if strings.HasPrefix(prog, "mkfs") {
		return true
	}
	if shells[prog] {
		if piped && (prev == "curl" || prev == "wget") {
			return true
		}
		for i, a := range args {
			if a == "-c" && i+1 < len(args) {
				return isDestructive(args[i+1])
			}
		}
		return false
	}
	if interpreters[prog] && piped && (prev == "curl" || prev == "wget") {
		return true
	}
	return false
}

func destructiveGit(args []string) bool {
	i := 0
	for i < len(args) && strings.HasPrefix(args[i], "-") {
		if args[i] == "-C" || args[i] == "-c" {
			i++
		}
		i++
	}
	if i >= len(args) {
		return false
	}
	sub, rest := args[i], args[i+1:]
	switch sub {
	case "push", "clean", "rebase", "restore", "filter-branch", "gc", "prune":
		return true
	case "reset":
		return hasArg(rest, "--hard", "--merge", "--keep")
	case "checkout", "switch":
		return hasArg(rest, "--", ".", "-f", "--force", "-B")
	case "branch":
		return hasShortFlag(rest, "D") || hasArg(rest, "--force", "-f")
	case "stash":
		return hasArg(rest, "drop", "clear")
	case "tag":
		return hasShortFlag(rest, "d") || hasArg(rest, "--delete")
	}
	return false
}
