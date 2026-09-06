// Package project handles the local .helmforge.yml project file that supplies
// default flag values (repo, ref, app) so commands can be run as
// `helmforge plan --env staging` from inside a project directory.
package project

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// FileName is the project marker file looked up from the working directory.
const FileName = ".helmforge.yml"

// Config is the contents of .helmforge.yml.
type Config struct {
	Project      string   `yaml:"project"`
	Repo         string   `yaml:"repo"`
	Ref          string   `yaml:"ref"`
	App          string   `yaml:"app"`
	DefaultEnv   string   `yaml:"defaultEnv"`
	Environments []string `yaml:"environments"`

	// Root is the directory containing .helmforge.yml. Not serialised.
	Root string `yaml:"-"`
}

// Find walks up from dir looking for .helmforge.yml and loads it. It returns a
// nil Config (and nil error) when no project file is found, so callers can
// treat "not in a project" as an ordinary case rather than a failure.
func Find(dir string) (*Config, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("resolve %s: %w", dir, err)
	}

	for {
		candidate := filepath.Join(abs, FileName)
		if _, err := os.Stat(candidate); err == nil {
			return Load(candidate)
		}

		parent := filepath.Dir(abs)
		if parent == abs {
			return nil, nil // reached the filesystem root
		}
		abs = parent
	}
}

// FindFromCwd is Find rooted at the current working directory.
func FindFromCwd() (*Config, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("get working directory: %w", err)
	}
	return Find(cwd)
}

// Load reads and parses a project file at the given path.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	cfg.Root = filepath.Dir(path)
	return &cfg, nil
}

// AppDir returns the directory holding app.yaml for an app in an environment.
func (c *Config) AppDir(env, app string) string {
	return filepath.Join(c.Root, "environments", env, "apps", app)
}

// Write serialises the config to <root>/.helmforge.yml.
func (c *Config) Write() error {
	data, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("marshal project config: %w", err)
	}
	path := filepath.Join(c.Root, FileName)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}
