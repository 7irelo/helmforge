package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/7irelo/helmforge/internal/core/project"
	"github.com/spf13/cobra"
)

const appYAMLTemplate = `app: %[1]s
env: %[2]s

targets:
  - host: 10.0.1.10        # Hostname or IP of the deploy target
    user: deploy           # SSH user
    port: 22
    path: /opt/apps/%[1]s  # Remote directory for compose files

source:
  type: compose
  composeFile: docker-compose.yaml

deploy:
  strategy: rolling
  health:
    type: http
    url: http://10.0.1.10:8080/health
    timeoutSeconds: 60

policy:
  allowedBranches:
    - main
  requireCleanWorktree: true
`

const composeTemplate = `services:
  %[1]s:
    image: nginx:1.27
    restart: unless-stopped
    ports:
      - "8080:80"
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost/"]
      interval: 10s
      timeout: 3s
      retries: 3
`

func NewInitCmd() *cobra.Command {
	var envList []string
	var force bool

	cmd := &cobra.Command{
		Use:   "init [name]",
		Short: "Initialize a new helmforge project",
		Long: `Creates the directory layout helmforge expects: a .helmforge.yml project
file and an environments/<env>/apps/<app> tree with a starter app.yaml and
docker-compose.yaml for each environment.

With no name, the project is initialized in the current directory.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()

			name := ""
			root := "."
			if len(args) == 1 {
				name = args[0]
				root = args[0]
			} else {
				cwd, err := os.Getwd()
				if err != nil {
					return fmt.Errorf("get working directory: %w", err)
				}
				name = filepath.Base(cwd)
			}

			if len(envList) == 0 {
				return fmt.Errorf("--env must name at least one environment")
			}

			projectFile := filepath.Join(root, project.FileName)
			if _, err := os.Stat(projectFile); err == nil && !force {
				return fmt.Errorf("%s already exists (use --force to overwrite)", projectFile)
			}

			fmt.Fprintln(out, "Creating project structure...")
			if err := os.MkdirAll(root, 0o755); err != nil {
				return fmt.Errorf("create %s: %w", root, err)
			}

			fmt.Fprintln(out, "Generating configuration files...")
			fmt.Fprintf(out, "Setting up environments (%s)...\n", strings.Join(envList, ", "))
			for _, env := range envList {
				appDir := filepath.Join(root, "environments", env, "apps", name)
				if err := os.MkdirAll(appDir, 0o755); err != nil {
					return fmt.Errorf("create %s: %w", appDir, err)
				}

				appYAML := filepath.Join(appDir, "app.yaml")
				if err := writeIfAbsent(appYAML, fmt.Sprintf(appYAMLTemplate, name, env), force); err != nil {
					return err
				}

				compose := filepath.Join(appDir, "docker-compose.yaml")
				if err := writeIfAbsent(compose, fmt.Sprintf(composeTemplate, name), force); err != nil {
					return err
				}
			}

			fmt.Fprintf(out, "Initializing %s...\n", project.FileName)
			cfg := &project.Config{
				Project:      name,
				App:          name,
				Ref:          "main",
				DefaultEnv:   envList[0],
				Environments: envList,
				Root:         root,
			}
			if err := cfg.Write(); err != nil {
				return err
			}

			fmt.Fprintf(out, "\nProject %q initialized successfully!\n", name)
			fmt.Fprintln(out, "\nNext steps:")
			step := 1
			if root != "." {
				fmt.Fprintf(out, "  %d. cd %s\n", step, root)
				step++
			}
			fmt.Fprintf(out, "  %d. Edit %s and set your repo URL\n", step, project.FileName)
			step++
			fmt.Fprintf(out, "  %d. Edit environments/%s/apps/%s/docker-compose.yaml\n", step, envList[0], name)
			step++
			fmt.Fprintf(out, "  %d. Run 'helmforge plan --env %s'\n", step, envList[0])

			return nil
		},
	}

	cmd.Flags().StringSliceVar(&envList, "env", []string{"dev", "staging", "prod"}, "Environments to scaffold")
	cmd.Flags().BoolVar(&force, "force", false, "Overwrite existing files")

	return cmd
}

// writeIfAbsent writes content unless the file exists and force is false.
func writeIfAbsent(path, content string, force bool) error {
	if !force {
		if _, err := os.Stat(path); err == nil {
			return nil
		}
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}
