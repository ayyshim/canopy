package runner

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"time"

	"canopy/client/internal/detect"
	"canopy/shared/protocol"
)

// ask runs Claude once with no tools and returns its answer.
func ask(ctx context.Context, m protocol.Ask) (string, error) {
	bin := detect.ResolveCLI("claude")
	if bin == "" {
		return "", errors.New("Claude isn’t installed on this computer")
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	args := []string{"-p", "--output-format", "text", "--tools", "", "--setting-sources", "", "--system-prompt", m.System}
	if m.Model != "" {
		args = append(args, "--model", m.Model)
	}
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = os.TempDir()
	cmd.Stdin = strings.NewReader(m.Prompt)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return "", errors.New("Claude took too long to answer")
		}
		return "", errors.New("Claude couldn’t answer: " + strings.TrimSpace(errb.String()))
	}
	return strings.TrimSpace(out.String()), nil
}
