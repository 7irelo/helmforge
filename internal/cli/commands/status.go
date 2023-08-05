package commands

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/7irelo/helmforge/internal/adapters/git"
	"github.com/7irelo/helmforge/internal/adapters/remote"
	"github.com/7irelo/helmforge/internal/adapters/store"
	"github.com/7irelo/helmforge/internal/core/plan"
	"github.com/7irelo/helmforge/internal/core/reconcile"
	"github.com/7irelo/helmforge/internal/core/release"
	"github.com/spf13/cobra"
)

func NewStatusCmd() *cobra.Command {
	var env, app, repo, ref string
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show deployment status and drift",
		Long:  "Shows the last release, per-host status, and whether current state matches desired.",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			// Open store.
			st, err := store.NewSQLiteStore()
			if err != nil {
				return fmt.Errorf("open store: %w", err)
			}

			// Get latest release.
			rel, err := st.GetLatestRelease(env, app)
			if err != nil {
				return fmt.Errorf("get latest release: %w", err)
			}
			if rel == nil {
				fmt.Fprintf(os.Stdout, "No releases found for %s/%s\n", env, app)
				return nil
			}

			fmt.Fprint(os.Stdout, release.FormatReleaseText(rel))

			// Check drift if repo is provided.
			if repo != "" {
				gitClient := git.NewClient()
				planner := &plan.Planner{Git: gitClient}
				p, err := planner.Generate(ctx, plan.GenerateInput{
					Env:  env,
					App:  app,
					Repo: repo,
					Ref:  ref,
				})
				if err != nil {
					return fmt.Errorf("generate plan for drift: %w", err)
				}

				engine := &reconcile.Engine{
					Remote: remote.NewSSHRunner(),
				}

				fmt.Fprintln(os.Stdout, "\nDrift Check:")
				var driftResults []interface{}
				for _, target := range p.Config.Targets {
					result := engine.CheckDrift(ctx, target, p.CommitSHA)
					result.App = app
					result.Env = env
					if jsonOutput {
						driftResults = append(driftResults, result)
					} else {
						status := "InSync"
						if !result.InSync {
							status = "OutOfSync"
						}
						if result.Error != "" {
							status = "Error"
						}
						fmt.Fprintf(os.Stdout, "  %s: %s (desired: %s, actual: %s)\n",
							target.Host, status,
							shortSHA(result.DesiredSHA),
							shortSHA(result.ActualSHA),
						)
						if result.Error != "" {
							fmt.Fprintf(os.Stdout, "    error: %s\n", result.Error)
						}
					}
				}
				if jsonOutput {
					data, _ := json.MarshalIndent(driftResults, "", "  ")
					fmt.Fprintln(os.Stdout, string(data))
				}
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&env, "env", "e", "", "Environment (required)")
	cmd.Flags().StringVarP(&app, "app", "a", "", "Application name (required)")
	cmd.Flags().StringVar(&repo, "repo", "", "Git repository URL (for drift check)")
	cmd.Flags().StringVar(&ref, "ref", "main", "Git ref (branch/tag/sha)")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "JSON output")

	cmd.MarkFlagRequired("env")
	cmd.MarkFlagRequired("app")

	return cmd
}

func shortSHA(sha string) string {
	if len(sha) > 8 {
		return sha[:8]
	}
	return sha
}
