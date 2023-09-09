package commands

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/7irelo/helmforge/internal/adapters/git"
	"github.com/7irelo/helmforge/internal/adapters/health"
	"github.com/7irelo/helmforge/internal/adapters/remote"
	"github.com/7irelo/helmforge/internal/adapters/store"
	"github.com/7irelo/helmforge/internal/core/plan"
	"github.com/7irelo/helmforge/internal/core/reconcile"
	"github.com/7irelo/helmforge/internal/core/release"
	"github.com/7irelo/helmforge/internal/util/lock"
	"github.com/7irelo/helmforge/internal/util/log"
	"github.com/spf13/cobra"
)

func NewRollbackCmd() *cobra.Command {
	var env, app, toRelease string

	cmd := &cobra.Command{
		Use:   "rollback",
		Short: "Roll back to a previous release",
		Long:  "Re-deploys a prior release commit and verifies health.",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			// Open store and find the target release.
			st, err := store.NewSQLiteStore()
			if err != nil {
				return fmt.Errorf("open store: %w", err)
			}

			target, err := st.GetRelease(toRelease)
			if err != nil {
				return fmt.Errorf("get release: %w", err)
			}
			if target == nil {
				return fmt.Errorf("release %s not found", toRelease)
			}

			if target.Env != env || target.App != app {
				return fmt.Errorf("release %s belongs to %s/%s, not %s/%s", toRelease, target.Env, target.App, env, app)
			}

			log.L().Info().
				Str("release", toRelease).
				Str("commit", target.CommitSHA).
				Msg("rolling back to release")

			// Acquire lock.
			lk, err := lock.Acquire(env, app)
			if err != nil {
				return fmt.Errorf("cannot acquire lock: %w", err)
			}
			defer lk.Release()

			// Generate plan using the target release's repo and ref.
			gitClient := git.NewClient()
			planner := &plan.Planner{Git: gitClient}
			p, err := planner.Generate(ctx, plan.GenerateInput{
				Env:  env,
				App:  app,
				Repo: target.Repo,
				Ref:  target.CommitSHA, // checkout exact commit
			})
			if err != nil {
				return fmt.Errorf("plan for rollback failed: %w", err)
			}

			fmt.Fprintf(os.Stdout, "Rolling back %s/%s to release %s (commit %s)\n\n",
				env, app, toRelease, shortSHA(target.CommitSHA))
			fmt.Fprint(os.Stdout, release.FormatPlanText(p))
			fmt.Fprintln(os.Stdout, "\nExecuting rollback...")

			localRepo, err := gitClient.CloneOrPull(ctx, target.Repo)
			if err != nil {
				return fmt.Errorf("resolve local repo: %w", err)
			}
			localAppDir := filepath.Join(localRepo, "environments", env, "apps", app)

			engine := &reconcile.Engine{
				Remote: remote.NewSSHRunner(),
				Health: health.NewChecker(),
				Store:  st,
			}
			rel, err := engine.Apply(ctx, reconcile.ApplyInput{
				Plan:        p,
				MaxParallel: 1,
				LocalAppDir: localAppDir,
			})
			if err != nil {
				return fmt.Errorf("rollback apply failed: %w", err)
			}

			fmt.Fprintln(os.Stdout)
			fmt.Fprint(os.Stdout, release.FormatReleaseText(rel))

			if rel.Status != "success" {
				log.L().Error().Str("status", string(rel.Status)).Msg("rollback did not succeed")
				os.Exit(1)
			}

			fmt.Fprintln(os.Stdout, "\nRollback completed successfully.")
			return nil
		},
	}

	cmd.Flags().StringVarP(&env, "env", "e", "", "Environment (required)")
	cmd.Flags().StringVarP(&app, "app", "a", "", "Application name (required)")
	cmd.Flags().StringVar(&toRelease, "to", "", "Release ID to roll back to (required)")

	cmd.MarkFlagRequired("env")
	cmd.MarkFlagRequired("app")
	cmd.MarkFlagRequired("to")

	return cmd
}
