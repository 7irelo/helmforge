package remote

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/7irelo/helmforge/internal/core/model"
	"github.com/7irelo/helmforge/internal/util/log"
)

// Runner executes commands on remote hosts. For v1 we shell out to ssh/scp.
type Runner interface {
	// Run executes a command on the remote host and returns stdout.
	Run(ctx context.Context, target model.Target, command string) (stdout string, err error)
	// CopyFiles transfers local files to the remote host's target path.
	CopyFiles(ctx context.Context, target model.Target, localDir string, files []string) error
	// ReadFile reads a file from the remote host.
	ReadFile(ctx context.Context, target model.Target, remotePath string) (string, error)
	// WriteFile writes content to a file on the remote host.
	WriteFile(ctx context.Context, target model.Target, remotePath, content string) error
}

type sshRunner struct{}

// NewSSHRunner creates a Runner that uses system ssh/scp.
func NewSSHRunner() Runner {
	return &sshRunner{}
}

func (r *sshRunner) Run(ctx context.Context, target model.Target, command string) (string, error) {
	args := sshArgs(target)
	args = append(args, command)

	log.L().Debug().
		Str("host", target.Host).
		Str("command", command).
		Msg("ssh exec")

	cmd := exec.CommandContext(ctx, "ssh", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return stdout.String(), fmt.Errorf("ssh %s: %s: %w", target.Host, stderr.String(), err)
	}
	return stdout.String(), nil
}

func (r *sshRunner) CopyFiles(ctx context.Context, target model.Target, localDir string, files []string) error {
	// Use scp for each file. For v1 this is simple and reliable.
	for _, f := range files {
		localPath := localDir + "/" + f
		remotePath := fmt.Sprintf("%s@%s:%s/%s", target.User, target.Host, target.Path, f)

		args := []string{"-P", fmt.Sprintf("%d", target.Port), "-o", "StrictHostKeyChecking=accept-new"}
		args = append(args, localPath, remotePath)

		log.L().Debug().
			Str("host", target.Host).
			Str("file", f).
			Msg("scp copy")

		cmd := exec.CommandContext(ctx, "scp", args...)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("scp %s to %s: %s: %w", f, target.Host, stderr.String(), err)
		}
	}
	return nil
}

func (r *sshRunner) ReadFile(ctx context.Context, target model.Target, remotePath string) (string, error) {
	out, err := r.Run(ctx, target, fmt.Sprintf("cat %s 2>/dev/null || true", shellQuote(remotePath)))
	return strings.TrimSpace(out), err
}

func (r *sshRunner) WriteFile(ctx context.Context, target model.Target, remotePath, content string) error {
	cmd := fmt.Sprintf("cat > %s << 'HELMFORGE_EOF'\n%s\nHELMFORGE_EOF", shellQuote(remotePath), content)
	_, err := r.Run(ctx, target, cmd)
	return err
}

func sshArgs(target model.Target) []string {
	return []string{
		"-p", fmt.Sprintf("%d", target.Port),
		"-o", "StrictHostKeyChecking=accept-new",
		"-o", "ConnectTimeout=10",
		fmt.Sprintf("%s@%s", target.User, target.Host),
	}
}

// shellQuote does minimal quoting for shell arguments.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}
