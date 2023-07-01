package commands

import (
	"fmt"
	"os"

	"github.com/7irelo/helmforge/internal/adapters/git"
	"github.com/7irelo/helmforge/internal/core/plan"
	"github.com/7irelo/helmforge/internal/core/release"
	"github.com/spf13/cobra"
)

func NewPlanCmd() *cobra.Command {
	var env, app, repo, ref string
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "plan",
		Short: "Show deployment plan without executing",
		Long:  "Generates a diff-like plan of deployment actions per host.",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			planner := &plan.Planner{Git: git.NewClient()}
			p, err := planner.Generate(ctx, plan.GenerateInput{
				Env:  env,
				App:  app,
				Repo: repo,
				Ref:  ref,
			})
			if err != nil {
				return fmt.Errorf("plan failed: %w", err)
			}

			if jsonOutput {
				out, err := release.FormatPlanJSON(p)
				if err != nil {
					return err
				}
				fmt.Fprintln(os.Stdout, out)
			} else {
				fmt.Fprint(os.Stdout, release.FormatPlanText(p))
			}
			return nil
		},
	}

	cmd.Flags().StringVarP(&env, "env", "e", "", "Environment (required)")
	cmd.Flags().StringVarP(&app, "app", "a", "", "Application name (required)")
	cmd.Flags().StringVar(&repo, "repo", "", "Git repository URL (required)")
	cmd.Flags().StringVar(&ref, "ref", "main", "Git ref (branch/tag/sha)")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output plan as JSON")

	cmd.MarkFlagRequired("env")
	cmd.MarkFlagRequired("app")
	cmd.MarkFlagRequired("repo")

	return cmd
}
