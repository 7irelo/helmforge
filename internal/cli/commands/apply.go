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

func NewApplyCmd() *cobra.Command {
	var env, app, repo, ref string
	var maxParallel int

	cmd := &cobra.Command{
		Use:   "apply",
		Short: "Execute deployment",
		Long:  "Deploys the application to target hosts using a rolling strategy.",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			// Acquire deploy lock.
			lk, err := lock.Acquire(env, app)
			if err != nil {
				return fmt.Errorf("cannot acquire lock: %w", err)
			}
			defer lk.Release()

			// Generate plan.
			gitClient := git.NewClient()
			planner := &plan.Planner{Git: gitClient}
			p, err := planner.Generate(ctx, plan.GenerateInput{
				Env:  env,
				App:  app,
				Repo: repo,
				Ref:  ref,
			})
			if err != nil {
				return fmt.Errorf("plan failed: %w", err)
			}

			// Show plan.
			fmt.Fprint(os.Stdout, release.FormatPlanText(p))
			fmt.Fprintln(os.Stdout, "\nExecuting deployment...")

			// Open store.
			st, err := store.NewSQLiteStore()
			if err != nil {
				return fmt.Errorf("open store: %w", err)
			}

			// Determine local app directory for file copy.
			home, _ := os.UserHomeDir()
			repoHash := filepath.Base(filepath.Dir(filepath.Join(home, ".helmforge", "repos")))
			_ = repoHash
			// Re-derive the local path from git.
			localRepo, err := gitClient.CloneOrPull(ctx, repo)
			if err != nil {
				return fmt.Errorf("resolve local repo: %w", err)
			}
			localAppDir := filepath.Join(localRepo, "environments", env, "apps", app)

			// Execute apply.
			engine := &reconcile.Engine{
				Remote: remote.NewSSHRunner(),
				Health: health.NewChecker(),
				Store:  st,
			}
			rel, err := engine.Apply(ctx, reconcile.ApplyInput{
				Plan:        p,
				MaxParallel: maxParallel,
				LocalAppDir: localAppDir,
			})
			if err != nil {
				return fmt.Errorf("apply failed: %w", err)
			}

			fmt.Fprintln(os.Stdout)
			fmt.Fprint(os.Stdout, release.FormatReleaseText(rel))

			if rel.Status != "success" {
				log.L().Error().Str("status", string(rel.Status)).Msg("deployment did not succeed")
				os.Exit(1)
			}
			return nil
		},
	}

	cmd.Flags().StringVarP(&env, "env", "e", "", "Environment (required)")
	cmd.Flags().StringVarP(&app, "app", "a", "", "Application name (required)")
	cmd.Flags().StringVar(&repo, "repo", "", "Git repository URL (required)")
	cmd.Flags().StringVar(&ref, "ref", "main", "Git ref (branch/tag/sha)")
	cmd.Flags().IntVar(&maxParallel, "max-parallel", 1, "Max parallel host deploys")

	cmd.MarkFlagRequired("env")
	cmd.MarkFlagRequired("app")
	cmd.MarkFlagRequired("repo")

	return cmd
}
