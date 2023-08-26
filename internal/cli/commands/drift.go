package commands

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/7irelo/helmforge/internal/adapters/git"
	"github.com/7irelo/helmforge/internal/adapters/remote"
	"github.com/7irelo/helmforge/internal/core/model"
	"github.com/7irelo/helmforge/internal/core/reconcile"
	"github.com/7irelo/helmforge/internal/core/validate"
	"github.com/7irelo/helmforge/internal/util/log"
	"github.com/spf13/cobra"
)

func NewDriftCmd() *cobra.Command {
	var env, repo, ref string
	var all, jsonOutput bool

	cmd := &cobra.Command{
		Use:   "drift",
		Short: "Check for configuration drift",
		Long:  "Checks apps in an environment and reports OutOfSync if remote state differs from desired.",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			if repo == "" {
				return fmt.Errorf("--repo is required for drift check")
			}

			gitClient := git.NewClient()
			localPath, err := gitClient.CloneOrPull(ctx, repo)
			if err != nil {
				return fmt.Errorf("git clone/pull: %w", err)
			}
			if err := gitClient.Checkout(ctx, localPath, ref); err != nil {
				return fmt.Errorf("git checkout: %w", err)
			}
			commitSHA, err := gitClient.HeadSHA(ctx, localPath)
			if err != nil {
				return fmt.Errorf("get commit SHA: %w", err)
			}

			// Discover apps in the environment.
			envDir := filepath.Join(localPath, "environments", env, "apps")
			entries, err := os.ReadDir(envDir)
			if err != nil {
				return fmt.Errorf("read env dir %s: %w", envDir, err)
			}

			runner := remote.NewSSHRunner()
			var allResults []model.DriftResult

			for _, entry := range entries {
				if !entry.IsDir() {
					continue
				}
				appName := entry.Name()
				if !all && len(args) > 0 {
					// If specific apps were given as args, filter.
					found := false
					for _, a := range args {
						if a == appName {
							found = true
							break
						}
					}
					if !found {
						continue
					}
				}

				configPath := filepath.Join(envDir, appName, "app.yaml")
				data, err := os.ReadFile(configPath)
				if err != nil {
					log.L().Warn().Str("app", appName).Err(err).Msg("skip: cannot read app.yaml")
					continue
				}
				cfg, err := validate.ParseAndValidate(data)
				if err != nil {
					log.L().Warn().Str("app", appName).Err(err).Msg("skip: invalid app.yaml")
					continue
				}

				engine := &reconcile.Engine{Remote: runner}
				for _, target := range cfg.Targets {
					result := engine.CheckDrift(ctx, target, commitSHA)
					result.App = appName
					result.Env = env
					allResults = append(allResults, result)
				}
			}

			if jsonOutput {
				data, _ := json.MarshalIndent(allResults, "", "  ")
				fmt.Fprintln(os.Stdout, string(data))
			} else {
				if len(allResults) == 0 {
					fmt.Fprintln(os.Stdout, "No apps found to check.")
					return nil
				}
				fmt.Fprintf(os.Stdout, "Drift Report for %s (desired: %s)\n", env, commitSHA[:8])
				fmt.Fprintln(os.Stdout, "===========================================")
				for _, r := range allResults {
					status := "InSync"
					if !r.InSync {
						status = "OutOfSync"
					}
					if r.Error != "" {
						status = "Error"
					}
					fmt.Fprintf(os.Stdout, "  %-25s %-12s %s -> %s",
						r.App+"/"+r.Host, status,
						shortSHA(r.DesiredSHA), shortSHA(r.ActualSHA),
					)
					if r.Error != "" {
						fmt.Fprintf(os.Stdout, "  (%s)", r.Error)
					}
					fmt.Fprintln(os.Stdout)
				}
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&env, "env", "e", "", "Environment (required)")
	cmd.Flags().StringVar(&repo, "repo", "", "Git repository URL (required)")
	cmd.Flags().StringVar(&ref, "ref", "main", "Git ref (branch/tag/sha)")
	cmd.Flags().BoolVar(&all, "all", false, "Check all apps in environment")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "JSON output")

	cmd.MarkFlagRequired("env")
	cmd.MarkFlagRequired("repo")

	return cmd
}
