package commands

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/7irelo/helmforge/internal/adapters/git"
	"github.com/7irelo/helmforge/internal/core/model"
	"github.com/7irelo/helmforge/internal/core/project"
	"github.com/7irelo/helmforge/internal/core/validate"
	"github.com/spf13/cobra"
)

// resolveDefaults fills empty flag values from the nearest .helmforge.yml, so
// commands can be run as `helmforge logs --env staging` from inside a project.
// Explicit flags always win. A missing project file is not an error.
func resolveDefaults(env, app, repo, ref *string) (*project.Config, error) {
	proj, err := project.FindFromCwd()
	if err != nil {
		return nil, err
	}
	if proj == nil {
		return nil, nil
	}

	if *env == "" {
		*env = proj.DefaultEnv
	}
	if app != nil && *app == "" {
		*app = proj.App
	}
	if repo != nil && *repo == "" {
		*repo = proj.Repo
	}
	if ref != nil && *ref == "" {
		*ref = proj.Ref
	}
	return proj, nil
}

// requireFlags returns an error naming every flag that is still empty after
// project defaults have been applied.
func requireFlags(pairs map[string]string) error {
	var missing []string
	for _, name := range []string{"env", "app", "repo"} {
		if value, ok := pairs[name]; ok && value == "" {
			missing = append(missing, "--"+name)
		}
	}
	switch len(missing) {
	case 0:
		return nil
	case 1:
		return fmt.Errorf("%s is required (or set it in %s)", missing[0], project.FileName)
	default:
		return fmt.Errorf("%v are required (or set them in %s)", missing, project.FileName)
	}
}

// bindOutputFlag adds `--output <format>` as an alias for `--json`, matching the
// spelling most CI tooling expects. Valid values are "text" (the default) and
// "json"; passing --json is equivalent to --output json.
func bindOutputFlag(cmd *cobra.Command, jsonOutput *bool) {
	var output string

	cmd.Flags().StringVarP(&output, "output", "o", "", `Output format: "text" or "json"`)

	previous := cmd.PreRunE
	cmd.PreRunE = func(c *cobra.Command, args []string) error {
		switch strings.ToLower(strings.TrimSpace(output)) {
		case "":
			// Not passed; leave --json alone.
		case "json":
			*jsonOutput = true
		case "text":
			*jsonOutput = false
		default:
			return fmt.Errorf(`invalid --output value %q: expected "text" or "json"`, output)
		}

		if previous != nil {
			return previous(c, args)
		}
		return nil
	}
}

// loadAppConfig resolves an app's configuration either from a Git repository or,
// when no repo is given, from the local project checkout. It returns the parsed
// config and the directory that holds app.yaml.
func loadAppConfig(ctx context.Context, proj *project.Config, env, app, repo, ref string) (*model.AppConfig, string, error) {
	var appDir string

	switch {
	case repo != "":
		gitClient := git.NewClient()
		localPath, err := gitClient.CloneOrPull(ctx, repo)
		if err != nil {
			return nil, "", fmt.Errorf("git clone/pull: %w", err)
		}
		if ref == "" {
			ref = "main"
		}
		if err := gitClient.Checkout(ctx, localPath, ref); err != nil {
			return nil, "", fmt.Errorf("git checkout: %w", err)
		}
		appDir = filepath.Join(localPath, "environments", env, "apps", app)

	case proj != nil:
		appDir = proj.AppDir(env, app)

	default:
		return nil, "", fmt.Errorf("--repo is required when not inside a %s project", project.FileName)
	}

	configPath := filepath.Join(appDir, "app.yaml")
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, "", fmt.Errorf("read app.yaml at %s: %w", configPath, err)
	}

	cfg, err := validate.ParseAndValidate(data)
	if err != nil {
		return nil, "", fmt.Errorf("validate app.yaml:\n%w", err)
	}
	return cfg, appDir, nil
}
