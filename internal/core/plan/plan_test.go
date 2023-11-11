package plan

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// mockGitClient implements git.Client for testing.
type mockGitClient struct {
	localPath string
	commitSHA string
}

func (m *mockGitClient) CloneOrPull(ctx context.Context, repoURL string) (string, error) {
	return m.localPath, nil
}

func (m *mockGitClient) Checkout(ctx context.Context, localPath, ref string) error {
	return nil
}

func (m *mockGitClient) HeadSHA(ctx context.Context, localPath string) (string, error) {
	return m.commitSHA, nil
}

func setupTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	appDir := filepath.Join(dir, "environments", "staging", "apps", "web")
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		t.Fatal(err)
	}

	appYAML := `
app: web
env: staging
targets:
  - host: 10.0.1.10
    user: deploy
    path: /opt/web
source:
  type: compose
  composeFile: docker-compose.yaml
deploy:
  strategy: rolling
  health:
    type: http
    url: http://10.0.1.10:8080/health
    timeoutSeconds: 15
`
	if err := os.WriteFile(filepath.Join(appDir, "app.yaml"), []byte(appYAML), 0o644); err != nil {
		t.Fatal(err)
	}

	compose := `version: "3.8"
services:
  web:
    image: nginx:latest
    ports:
      - "8080:80"
`
	if err := os.WriteFile(filepath.Join(appDir, "docker-compose.yaml"), []byte(compose), 0o644); err != nil {
		t.Fatal(err)
	}

	return dir
}

func TestGenerate_Basic(t *testing.T) {
	repoDir := setupTestRepo(t)

	planner := &Planner{
		Git: &mockGitClient{
			localPath: repoDir,
			commitSHA: "abc123def456",
		},
	}

	p, err := planner.Generate(context.Background(), GenerateInput{
		Env:  "staging",
		App:  "web",
		Repo: "https://example.com/repo.git",
		Ref:  "main",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if p.Env != "staging" {
		t.Errorf("env = %q, want %q", p.Env, "staging")
	}
	if p.App != "web" {
		t.Errorf("app = %q, want %q", p.App, "web")
	}
	if p.CommitSHA != "abc123def456" {
		t.Errorf("commitSHA = %q, want %q", p.CommitSHA, "abc123def456")
	}

	// Expect 6 actions: ensure_dir, copy_files, docker_pull, docker_up, health_check, write_marker.
	if len(p.Actions) != 6 {
		t.Fatalf("actions len = %d, want 6", len(p.Actions))
	}

	expectedSteps := []string{"ensure_dir", "copy_files", "docker_pull", "docker_up", "health_check", "write_marker"}
	for i, expected := range expectedSteps {
		if p.Actions[i].Step != expected {
			t.Errorf("action[%d].Step = %q, want %q", i, p.Actions[i].Step, expected)
		}
	}
}

func TestGenerate_DefaultRef(t *testing.T) {
	repoDir := setupTestRepo(t)

	planner := &Planner{
		Git: &mockGitClient{
			localPath: repoDir,
			commitSHA: "abc123",
		},
	}

	p, err := planner.Generate(context.Background(), GenerateInput{
		Env:  "staging",
		App:  "web",
		Repo: "https://example.com/repo.git",
		// No ref specified, should default to "main".
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Ref != "main" {
		t.Errorf("ref = %q, want %q", p.Ref, "main")
	}
}

func TestGenerate_MissingAppYAML(t *testing.T) {
	dir := t.TempDir()
	planner := &Planner{
		Git: &mockGitClient{
			localPath: dir,
			commitSHA: "abc123",
		},
	}

	_, err := planner.Generate(context.Background(), GenerateInput{
		Env:  "staging",
		App:  "nonexistent",
		Repo: "https://example.com/repo.git",
	})
	if err == nil {
		t.Fatal("expected error for missing app.yaml")
	}
}

func TestGenerate_NoHealthCheck(t *testing.T) {
	dir := t.TempDir()
	appDir := filepath.Join(dir, "environments", "dev", "apps", "api")
	os.MkdirAll(appDir, 0o755)

	appYAML := `
app: api
env: dev
targets:
  - host: dev1
    user: deploy
    path: /opt/api
source:
  type: compose
  composeFile: docker-compose.yaml
deploy:
  health:
    type: none
`
	os.WriteFile(filepath.Join(appDir, "app.yaml"), []byte(appYAML), 0o644)
	os.WriteFile(filepath.Join(appDir, "docker-compose.yaml"), []byte("version: '3'\nservices:\n  api:\n    image: api:latest\n"), 0o644)

	planner := &Planner{
		Git: &mockGitClient{localPath: dir, commitSHA: "deadbeef"},
	}

	p, err := planner.Generate(context.Background(), GenerateInput{
		Env:  "dev",
		App:  "api",
		Repo: "https://example.com/repo.git",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have 5 actions (no health check): ensure_dir, copy_files, docker_pull, docker_up, write_marker.
	if len(p.Actions) != 5 {
		t.Errorf("actions len = %d, want 5 (no health check)", len(p.Actions))
	}

	for _, a := range p.Actions {
		if a.Step == "health_check" {
			t.Error("found unexpected health_check action")
		}
	}
}
