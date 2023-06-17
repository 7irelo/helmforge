package commands

import (
	"context"
	"os"
	"os/signal"

	"github.com/7irelo/helmforge/internal/util/log"
	"github.com/spf13/cobra"
)

var (
	verbose    bool
	jsonLogger bool
)

func NewRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "helmforge",
		Short: "GitOps deployment engine for Docker Compose over SSH",
		Long: `helmforge deploys applications to remote Linux hosts via SSH using Docker Compose.
It uses a Git repository as the source of truth and supports plan/apply/status/drift/rollback.`,
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			log.Init(verbose, jsonLogger)
		},
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	root.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Enable verbose logging")
	root.PersistentFlags().BoolVar(&jsonLogger, "log-json", false, "Use JSON structured logging")

	root.AddCommand(NewPlanCmd())
	root.AddCommand(NewApplyCmd())
	root.AddCommand(NewStatusCmd())
	root.AddCommand(NewDriftCmd())
	root.AddCommand(NewRollbackCmd())

	return root
}

// Execute runs the CLI with signal handling for graceful cancellation.
func Execute() int {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	root := NewRootCmd()
	if err := root.ExecuteContext(ctx); err != nil {
		log.L().Error().Err(err).Msg("command failed")
		return 1
	}
	return 0
}
