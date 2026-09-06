package remote

import (
	"context"
	"fmt"
	"io"
	"os/exec"

	"github.com/7irelo/helmforge/internal/core/model"
	"github.com/7irelo/helmforge/internal/util/log"
)

// Stream runs a command on a remote host and copies its output to the given
// writers as it arrives, rather than buffering until the command exits. It is a
// package-level function rather than a Runner method so that follow-mode output
// (`helmforge logs --follow`) does not force every Runner implementation to
// support streaming.
//
// When ctx is cancelled the underlying ssh process is killed, which is what
// ends a `--follow` session on Ctrl-C.
func Stream(ctx context.Context, target model.Target, command string, stdout, stderr io.Writer) error {
	args := sshArgs(target)
	args = append(args, command)

	log.L().Debug().
		Str("host", target.Host).
		Str("command", command).
		Msg("ssh stream")

	cmd := exec.CommandContext(ctx, "ssh", args...)
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	if err := cmd.Run(); err != nil {
		// A cancelled context is the normal way a follow session ends.
		if ctx.Err() != nil {
			return nil
		}
		return fmt.Errorf("ssh %s: %w", target.Host, err)
	}
	return nil
}
