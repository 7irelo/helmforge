package model

import "time"

// AppConfig is the top-level configuration read from app.yaml.
type AppConfig struct {
	App     string   `yaml:"app"`
	Env     string   `yaml:"env"`
	Targets []Target `yaml:"targets"`
	Source  Source   `yaml:"source"`
	Deploy  Deploy   `yaml:"deploy"`
	Policy  Policy   `yaml:"policy"`
}

// Target describes a remote host to deploy to.
type Target struct {
	Host string `yaml:"host"`
	User string `yaml:"user"`
	Port int    `yaml:"port"`
	Path string `yaml:"path"`
}

// Source describes how the app is composed.
type Source struct {
	Type        string `yaml:"type"`
	ComposeFile string `yaml:"composeFile"`
}

// Deploy describes deployment strategy and health checks.
type Deploy struct {
	Strategy string      `yaml:"strategy"`
	Health   HealthCheck `yaml:"health"`
}

// HealthCheck defines how to verify a deployment is healthy.
type HealthCheck struct {
	Type           string `yaml:"type"`
	URL            string `yaml:"url"`
	TimeoutSeconds int    `yaml:"timeoutSeconds"`
}

// Policy defines guardrails for deployments.
type Policy struct {
	AllowedBranches      []string `yaml:"allowedBranches"`
	RequireCleanWorktree *bool    `yaml:"requireCleanWorktree"`
	RequireSignedCommits *bool    `yaml:"requireSignedCommits"`
}

// ReleaseStatus represents the state of a release.
type ReleaseStatus string

const (
	ReleaseStatusPending   ReleaseStatus = "pending"
	ReleaseStatusRunning   ReleaseStatus = "running"
	ReleaseStatusSuccess   ReleaseStatus = "success"
	ReleaseStatusFailed    ReleaseStatus = "failed"
	ReleaseStatusCancelled ReleaseStatus = "cancelled"
	ReleaseStatusRolledBack ReleaseStatus = "rolled_back"
)

// HostResult records the outcome of a deploy to one host.
type HostResult struct {
	Host      string        `json:"host"`
	Status    ReleaseStatus `json:"status"`
	StartedAt time.Time    `json:"started_at"`
	FinishedAt time.Time   `json:"finished_at"`
	Error     string        `json:"error,omitempty"`
	Logs      string        `json:"logs,omitempty"`
}

// Release records a deployment attempt.
type Release struct {
	ID         string        `json:"id"`
	Env        string        `json:"env"`
	App        string        `json:"app"`
	Repo       string        `json:"repo"`
	Ref        string        `json:"ref"`
	CommitSHA  string        `json:"commit_sha"`
	StartedAt  time.Time     `json:"started_at"`
	FinishedAt time.Time     `json:"finished_at"`
	Status     ReleaseStatus `json:"status"`
	HostResults []HostResult `json:"host_results"`
}

// PlanAction describes a single step in a deployment plan.
type PlanAction struct {
	Host        string `json:"host"`
	Step        string `json:"step"`
	Description string `json:"description"`
	Command     string `json:"command,omitempty"`
}

// DeployPlan is the full plan for a deployment.
type DeployPlan struct {
	Env       string       `json:"env"`
	App       string       `json:"app"`
	Repo      string       `json:"repo"`
	Ref       string       `json:"ref"`
	CommitSHA string       `json:"commit_sha"`
	Actions   []PlanAction `json:"actions"`
	Config    *AppConfig   `json:"-"`
}

// DriftResult reports whether an app is in sync.
type DriftResult struct {
	App          string `json:"app"`
	Env          string `json:"env"`
	Host         string `json:"host"`
	DesiredSHA   string `json:"desired_sha"`
	ActualSHA    string `json:"actual_sha"`
	InSync       bool   `json:"in_sync"`
	Error        string `json:"error,omitempty"`
}

// ReleaseMarker is written to remote hosts at <path>/.helmforge-release.
type ReleaseMarker struct {
	ReleaseID string `json:"release_id"`
	CommitSHA string `json:"commit_sha"`
	App       string `json:"app"`
	Env       string `json:"env"`
	Timestamp string `json:"timestamp"`
}
