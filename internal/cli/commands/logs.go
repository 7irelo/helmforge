package commands

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/7irelo/helmforge/internal/adapters/remote"
	"github.com/7irelo/helmforge/internal/core/model"
	"github.com/spf13/cobra"
)

func NewLogsCmd() *cobra.Command {
	var env, app, repo, ref, service, host string
	var follow bool
	var tail int

	cmd := &cobra.Command{
		Use:   "logs",
		Short: "Tail service logs",
		Long: `Streams 'docker compose logs' from the hosts an app is deployed to.

With --follow the output is streamed until interrupted; without it the last
--tail lines are printed and the command exits.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			out := cmd.OutOrStdout()
			errOut := cmd.ErrOrStderr()

			proj, err := resolveDefaults(&env, &app, &repo, &ref)
			if err != nil {
				return err
			}
			if err := requireFlags(map[string]string{"env": env, "app": app}); err != nil {
				return err
			}

			cfg, _, err := loadAppConfig(ctx, proj, env, app, repo, ref)
			if err != nil {
				return err
			}

			targets := filterTargets(cfg.Targets, host)
			if len(targets) == 0 {
				if host != "" {
					return fmt.Errorf("no target matching host %q in %s/%s", host, env, app)
				}
				return fmt.Errorf("no targets configured for %s/%s", env, app)
			}

			// Following multiple hosts at once would interleave unlabelled lines,
			// so prefix each line with its host when there is more than one.
			prefix := len(targets) > 1

			if !follow {
				for _, target := range targets {
					if prefix {
						fmt.Fprintf(out, "==> %s <==\n", target.Host)
					}
					cmdStr := composeLogsCommand(target.Path, service, tail, false)
					if err := remote.Stream(ctx, target, cmdStr, out, errOut); err != nil {
						fmt.Fprintf(errOut, "%s: %v\n", target.Host, err)
					}
					if prefix {
						fmt.Fprintln(out)
					}
				}
				return nil
			}

			// One mutex shared by every host's writer, so concurrent streams
			// serialise on the output rather than interleaving mid-line.
			var outMu sync.Mutex

			var wg sync.WaitGroup
			for _, target := range targets {
				wg.Add(1)
				go func(t model.Target) {
					defer wg.Done()
					w := out
					if prefix {
						w = &prefixWriter{w: out, mu: &outMu, prefix: t.Host + " | "}
					}
					cmdStr := composeLogsCommand(t.Path, service, tail, true)
					if err := remote.Stream(ctx, t, cmdStr, w, errOut); err != nil {
						fmt.Fprintf(errOut, "%s: %v\n", t.Host, err)
					}
				}(target)
			}
			wg.Wait()
			return nil
		},
	}

	cmd.Flags().StringVarP(&env, "env", "e", "", "Environment (required)")
	cmd.Flags().StringVarP(&app, "app", "a", "", "Application name (required)")
	cmd.Flags().StringVar(&repo, "repo", "", "Git repository URL (defaults to the local project)")
	cmd.Flags().StringVar(&ref, "ref", "", "Git ref (branch/tag/sha)")
	cmd.Flags().StringVarP(&service, "service", "s", "", "Compose service to tail (default: all services)")
	cmd.Flags().StringVar(&host, "host", "", "Limit output to a single target host")
	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "Stream logs until interrupted")
	cmd.Flags().IntVar(&tail, "tail", 100, "Number of lines to show from the end of the logs")

	return cmd
}

// composeLogsCommand builds the remote docker compose logs invocation.
func composeLogsCommand(path, service string, tail int, follow bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "cd %s && docker compose logs --no-color --tail %d", shellQuoteArg(path), tail)
	if follow {
		b.WriteString(" --follow")
	}
	if service != "" {
		b.WriteString(" " + shellQuoteArg(service))
	}
	return b.String()
}

// filterTargets returns the targets matching host, or all targets when host is empty.
func filterTargets(targets []model.Target, host string) []model.Target {
	if host == "" {
		return targets
	}
	var out []model.Target
	for _, t := range targets {
		if t.Host == host {
			out = append(out, t)
		}
	}
	return out
}

// prefixWriter prefixes each complete line written through it, buffering any
// trailing partial line until its newline arrives.
type prefixWriter struct {
	w       io.Writer
	mu      *sync.Mutex // shared across all writers targeting the same output
	prefix  string
	partial []byte
}

func (p *prefixWriter) Write(b []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	n := len(b)
	p.partial = append(p.partial, b...)
	for {
		i := bytes.IndexByte(p.partial, '\n')
		if i < 0 {
			break
		}
		line := p.partial[:i+1]
		p.partial = p.partial[i+1:]
		if _, err := p.w.Write(append([]byte(p.prefix), line...)); err != nil {
			return n, err
		}
	}
	return n, nil
}

// shellQuoteArg quotes a value for safe interpolation into a remote shell command.
func shellQuoteArg(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}
