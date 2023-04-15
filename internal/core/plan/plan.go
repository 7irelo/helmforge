package plan

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/7irelo/helmforge/internal/adapters/git"
	"github.com/7irelo/helmforge/internal/core/model"
	"github.com/7irelo/helmforge/internal/core/validate"
	"github.com/7irelo/helmforge/internal/util/log"
)

// Planner generates deployment plans.
type Planner struct {
	Git git.Client
}

// GenerateInput holds the parameters for generating a plan.
type GenerateInput struct {
	Env  string
	App  string
	Repo string
	Ref  string
}

// Generate creates a deployment plan without executing anything.
func (p *Planner) Generate(ctx context.Context, input GenerateInput) (*model.DeployPlan, error) {
	if input.Ref == "" {
		input.Ref = "main"
	}

	log.L().Info().Str("repo", input.Repo).Str("ref", input.Ref).Msg("cloning/fetching repo")
	localPath, err := p.Git.CloneOrPull(ctx, input.Repo)
	if err != nil {
		return nil, fmt.Errorf("git clone/pull: %w", err)
	}

	if err := p.Git.Checkout(ctx, localPath, input.Ref); err != nil {
		return nil, fmt.Errorf("git checkout: %w", err)
	}

	commitSHA, err := p.Git.HeadSHA(ctx, localPath)
	if err != nil {
		return nil, fmt.Errorf("get commit SHA: %w", err)
	}

	// Read and validate app.yaml.
	appDir := filepath.Join(localPath, "environments", input.Env, "apps", input.App)
	configPath := filepath.Join(appDir, "app.yaml")
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("read app.yaml at %s: %w", configPath, err)
	}

	cfg, err := validate.ParseAndValidate(data)
	if err != nil {
		return nil, fmt.Errorf("validate app.yaml:\n%w", err)
	}

	// Verify compose file exists.
	composePath := filepath.Join(appDir, cfg.Source.ComposeFile)
	if _, err := os.Stat(composePath); err != nil {
		return nil, fmt.Errorf("compose file not found: %s", composePath)
	}

	// Build plan actions.
	plan := &model.DeployPlan{
		Env:       input.Env,
		App:       input.App,
		Repo:      input.Repo,
		Ref:       input.Ref,
		CommitSHA: commitSHA,
		Config:    cfg,
	}

	for _, target := range cfg.Targets {
		host := fmt.Sprintf("%s@%s:%d", target.User, target.Host, target.Port)
		remotePath := target.Path

		plan.Actions = append(plan.Actions,
			model.PlanAction{
				Host:        host,
				Step:        "ensure_dir",
				Description: fmt.Sprintf("Create remote directory %s", remotePath),
				Command:     fmt.Sprintf("mkdir -p %s", remotePath),
			},
			model.PlanAction{
				Host:        host,
				Step:        "copy_files",
				Description: fmt.Sprintf("Copy %s to %s:%s", cfg.Source.ComposeFile, target.Host, remotePath),
			},
			model.PlanAction{
				Host:        host,
				Step:        "docker_pull",
				Description: "Pull latest images",
				Command:     fmt.Sprintf("cd %s && docker compose pull", remotePath),
			},
			model.PlanAction{
				Host:        host,
				Step:        "docker_up",
				Description: "Start/update services",
				Command:     fmt.Sprintf("cd %s && docker compose up -d --remove-orphans", remotePath),
			},
		)

		if cfg.Deploy.Health.Type == "http" {
			plan.Actions = append(plan.Actions, model.PlanAction{
				Host:        host,
				Step:        "health_check",
				Description: fmt.Sprintf("HTTP health check %s (timeout %ds)", cfg.Deploy.Health.URL, cfg.Deploy.Health.TimeoutSeconds),
			})
		}

		plan.Actions = append(plan.Actions, model.PlanAction{
			Host:        host,
			Step:        "write_marker",
			Description: "Write release marker file",
			Command:     fmt.Sprintf("Write .helmforge-release to %s", remotePath),
		})
	}

	return plan, nil
}
