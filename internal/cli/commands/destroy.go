package commands

import (
	"bufio"
	"fmt"
	"path"
	"strings"

	"github.com/7irelo/helmforge/internal/adapters/remote"
	"github.com/7irelo/helmforge/internal/util/lock"
	"github.com/7irelo/helmforge/internal/util/log"
	"github.com/spf13/cobra"
)

func NewDestroyCmd() *cobra.Command {
	var env, app, repo, ref, host string
	var volumes, purge, assumeYes bool

	cmd := &cobra.Command{
		Use:   "destroy",
		Short: "Remove a deployed application",
		Long: `Stops and removes an application's containers on every target host by running
'docker compose down', then deletes the helmforge release marker.

This does not delete release history, so 'helmforge rollback' can still
redeploy a previous release afterwards.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			out := cmd.OutOrStdout()

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

			fmt.Fprintf(out, "This will remove %s/%s from %d host(s):\n", env, app, len(targets))
			for _, t := range targets {
				fmt.Fprintf(out, "  - %s (%s)\n", t.Host, t.Path)
			}
			if volumes {
				fmt.Fprintln(out, "Named volumes WILL be deleted (--volumes).")
			}
			if purge {
				fmt.Fprintln(out, "The remote directory WILL be deleted (--purge).")
			}

			if !assumeYes {
				ok, err := confirm(cmd, "Are you sure?")
				if err != nil {
					return err
				}
				if !ok {
					fmt.Fprintln(out, "Aborted.")
					return nil
				}
			}

			// Take the same lock apply/rollback use so a destroy cannot race a deploy.
			lk, err := lock.Acquire(env, app)
			if err != nil {
				return fmt.Errorf("cannot acquire lock: %w", err)
			}
			defer lk.Release()

			runner := remote.NewSSHRunner()
			var failures int

			for _, target := range targets {
				fmt.Fprintf(out, "\n%s: stopping services...\n", target.Host)

				downCmd := fmt.Sprintf("cd %s && %s down --remove-orphans", shellQuoteArg(target.Path), composeBase(cfg))
				if volumes {
					downCmd += " --volumes"
				}
				if _, err := runner.Run(ctx, target, downCmd); err != nil {
					log.L().Error().Str("host", target.Host).Err(err).Msg("compose down failed")
					fmt.Fprintf(out, "%s: failed: %v\n", target.Host, err)
					failures++
					continue
				}

				markerPath := path.Join(target.Path, ".helmforge-release")
				if _, err := runner.Run(ctx, target, fmt.Sprintf("rm -f %s", shellQuoteArg(markerPath))); err != nil {
					log.L().Warn().Str("host", target.Host).Err(err).Msg("could not remove release marker")
				}

				if purge {
					if _, err := runner.Run(ctx, target, fmt.Sprintf("rm -rf %s", shellQuoteArg(target.Path))); err != nil {
						log.L().Warn().Str("host", target.Host).Err(err).Msg("could not remove remote directory")
					}
				}

				fmt.Fprintf(out, "%s: removed\n", target.Host)
			}

			if failures > 0 {
				return fmt.Errorf("destroy failed on %d of %d host(s)", failures, len(targets))
			}

			fmt.Fprintf(out, "\nDestroyed %s/%s on %d host(s).\n", env, app, len(targets))
			return nil
		},
	}

	cmd.Flags().StringVarP(&env, "env", "e", "", "Environment (required)")
	cmd.Flags().StringVarP(&app, "app", "a", "", "Application name (required)")
	cmd.Flags().StringVar(&repo, "repo", "", "Git repository URL (defaults to the local project)")
	cmd.Flags().StringVar(&ref, "ref", "", "Git ref (branch/tag/sha)")
	cmd.Flags().StringVar(&host, "host", "", "Limit the operation to a single target host")
	cmd.Flags().BoolVar(&volumes, "volumes", false, "Also remove named volumes")
	cmd.Flags().BoolVar(&purge, "purge", false, "Also remove the remote deployment directory")
	cmd.Flags().BoolVarP(&assumeYes, "yes", "y", false, "Skip the confirmation prompt")

	return cmd
}

// confirm asks a y/N question on the command's input stream.
func confirm(cmd *cobra.Command, question string) (bool, error) {
	fmt.Fprintf(cmd.OutOrStdout(), "%s (y/N): ", question)

	reader := bufio.NewReader(cmd.InOrStdin())
	line, err := reader.ReadString('\n')
	if err != nil && line == "" {
		return false, nil // treat EOF as "no"
	}

	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true, nil
	default:
		return false, nil
	}
}
