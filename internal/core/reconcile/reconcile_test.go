package reconcile

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/7irelo/helmforge/internal/core/model"
)

// mockRunner implements remote.Runner.
type mockRunner struct {
	runs        []runCall
	readFiles   map[string]string
	writtenFiles map[string]string
	failOnStep  string
}

type runCall struct {
	host    string
	command string
}

func newMockRunner() *mockRunner {
	return &mockRunner{
		readFiles:    make(map[string]string),
		writtenFiles: make(map[string]string),
	}
}

func (m *mockRunner) Run(ctx context.Context, target model.Target, command string) (string, error) {
	m.runs = append(m.runs, runCall{host: target.Host, command: command})
	if m.failOnStep != "" && containsStep(command, m.failOnStep) {
		return "", fmt.Errorf("mock failure on %s", m.failOnStep)
	}
	return "", nil
}

func (m *mockRunner) CopyFiles(ctx context.Context, target model.Target, localDir string, files []string) error {
	if m.failOnStep == "copy_files" {
		return fmt.Errorf("mock failure on copy_files")
	}
	return nil
}

func (m *mockRunner) ReadFile(ctx context.Context, target model.Target, remotePath string) (string, error) {
	key := target.Host + ":" + remotePath
	return m.readFiles[key], nil
}

func (m *mockRunner) WriteFile(ctx context.Context, target model.Target, remotePath, content string) error {
	key := target.Host + ":" + remotePath
	m.writtenFiles[key] = content
	return nil
}

func containsStep(command, step string) bool {
	switch step {
	case "docker_pull":
		return len(command) > 0 && (command == "docker compose pull" || len(command) > 5)
	}
	return false
}

// mockChecker implements health.Checker.
type mockChecker struct {
	shouldFail bool
}

func (m *mockChecker) Check(ctx context.Context, hc model.HealthCheck) error {
	if m.shouldFail {
		return fmt.Errorf("mock health check failure")
	}
	return nil
}

// mockStore implements store.ReleaseStore.
type mockStore struct {
	releases map[string]*model.Release
}

func newMockStore() *mockStore {
	return &mockStore{releases: make(map[string]*model.Release)}
}

func (m *mockStore) Init() error { return nil }

func (m *mockStore) SaveRelease(r *model.Release) error {
	m.releases[r.ID] = r
	return nil
}

func (m *mockStore) UpdateRelease(r *model.Release) error {
	m.releases[r.ID] = r
	return nil
}

func (m *mockStore) GetRelease(id string) (*model.Release, error) {
	return m.releases[id], nil
}

func (m *mockStore) GetLatestRelease(env, app string) (*model.Release, error) {
	for _, r := range m.releases {
		if r.Env == env && r.App == app {
			return r, nil
		}
	}
	return nil, nil
}

func (m *mockStore) ListReleases(env, app string, limit int) ([]*model.Release, error) {
	var result []*model.Release
	for _, r := range m.releases {
		if r.Env == env && r.App == app {
			result = append(result, r)
		}
	}
	return result, nil
}

func testPlan() *model.DeployPlan {
	return &model.DeployPlan{
		Env:       "staging",
		App:       "web",
		Repo:      "https://example.com/repo.git",
		Ref:       "main",
		CommitSHA: "abc123def456",
		Config: &model.AppConfig{
			App: "web",
			Env: "staging",
			Targets: []model.Target{
				{Host: "host1", User: "deploy", Port: 22, Path: "/opt/web"},
			},
			Source: model.Source{Type: "compose", ComposeFile: "docker-compose.yaml"},
			Deploy: model.Deploy{
				Strategy: "rolling",
				Health:   model.HealthCheck{Type: "http", URL: "http://host1:8080/health", TimeoutSeconds: 5},
			},
		},
	}
}

func TestApply_Success(t *testing.T) {
	runner := newMockRunner()
	checker := &mockChecker{shouldFail: false}
	st := newMockStore()

	engine := &Engine{
		Remote: runner,
		Health: checker,
		Store:  st,
	}

	rel, err := engine.Apply(context.Background(), ApplyInput{
		Plan:        testPlan(),
		MaxParallel: 1,
		LocalAppDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rel.Status != model.ReleaseStatusSuccess {
		t.Errorf("status = %q, want %q", rel.Status, model.ReleaseStatusSuccess)
	}
	if len(rel.HostResults) != 1 {
		t.Fatalf("host results len = %d, want 1", len(rel.HostResults))
	}
	if rel.HostResults[0].Status != model.ReleaseStatusSuccess {
		t.Errorf("host status = %q, want %q", rel.HostResults[0].Status, model.ReleaseStatusSuccess)
	}

	// Verify release marker was written.
	markerKey := "host1:/opt/web/.helmforge-release"
	markerContent, ok := runner.writtenFiles[markerKey]
	if !ok {
		t.Error("release marker was not written")
	} else {
		var marker model.ReleaseMarker
		if err := json.Unmarshal([]byte(markerContent), &marker); err != nil {
			t.Errorf("invalid marker JSON: %v", err)
		}
		if marker.CommitSHA != "abc123def456" {
			t.Errorf("marker commit = %q, want %q", marker.CommitSHA, "abc123def456")
		}
	}

	// Verify release was saved in store.
	saved, _ := st.GetRelease(rel.ID)
	if saved == nil {
		t.Error("release not found in store")
	}
}

func TestApply_HealthCheckFailure(t *testing.T) {
	runner := newMockRunner()
	checker := &mockChecker{shouldFail: true}
	st := newMockStore()

	engine := &Engine{
		Remote: runner,
		Health: checker,
		Store:  st,
	}

	rel, err := engine.Apply(context.Background(), ApplyInput{
		Plan:        testPlan(),
		MaxParallel: 1,
		LocalAppDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rel.Status != model.ReleaseStatusFailed {
		t.Errorf("status = %q, want %q", rel.Status, model.ReleaseStatusFailed)
	}
	if rel.HostResults[0].Error == "" {
		t.Error("expected error in host result")
	}
}

func TestApply_MultiHost_StopsOnFailure(t *testing.T) {
	runner := newMockRunner()
	checker := &mockChecker{shouldFail: true}
	st := newMockStore()

	plan := testPlan()
	plan.Config.Targets = append(plan.Config.Targets, model.Target{
		Host: "host2", User: "deploy", Port: 22, Path: "/opt/web",
	})

	engine := &Engine{
		Remote: runner,
		Health: checker,
		Store:  st,
	}

	rel, err := engine.Apply(context.Background(), ApplyInput{
		Plan:        plan,
		MaxParallel: 1,
		LocalAppDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rel.Status != model.ReleaseStatusFailed {
		t.Errorf("status = %q, want %q", rel.Status, model.ReleaseStatusFailed)
	}
	// Rolling strategy: should stop after first host fails.
	if len(rel.HostResults) != 1 {
		t.Errorf("host results len = %d, want 1 (should stop on failure)", len(rel.HostResults))
	}
}

func TestApply_Cancellation(t *testing.T) {
	runner := newMockRunner()
	checker := &mockChecker{}
	st := newMockStore()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately.

	engine := &Engine{
		Remote: runner,
		Health: checker,
		Store:  st,
	}

	rel, err := engine.Apply(ctx, ApplyInput{
		Plan:        testPlan(),
		MaxParallel: 1,
		LocalAppDir: t.TempDir(),
	})
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
	if rel.Status != model.ReleaseStatusCancelled {
		t.Errorf("status = %q, want %q", rel.Status, model.ReleaseStatusCancelled)
	}
}

func TestCheckDrift_InSync(t *testing.T) {
	runner := newMockRunner()
	target := model.Target{Host: "host1", User: "deploy", Port: 22, Path: "/opt/web"}

	marker := model.ReleaseMarker{
		ReleaseID: "rel-1",
		CommitSHA: "abc123",
		App:       "web",
		Env:       "staging",
	}
	markerJSON, _ := json.Marshal(marker)
	runner.readFiles["host1:/opt/web/.helmforge-release"] = string(markerJSON)

	engine := &Engine{Remote: runner}
	result := engine.CheckDrift(context.Background(), target, "abc123")

	if !result.InSync {
		t.Error("expected InSync")
	}
	if result.ActualSHA != "abc123" {
		t.Errorf("actual SHA = %q, want %q", result.ActualSHA, "abc123")
	}
}

func TestCheckDrift_OutOfSync(t *testing.T) {
	runner := newMockRunner()
	target := model.Target{Host: "host1", User: "deploy", Port: 22, Path: "/opt/web"}

	marker := model.ReleaseMarker{
		ReleaseID: "rel-1",
		CommitSHA: "old-sha",
	}
	markerJSON, _ := json.Marshal(marker)
	runner.readFiles["host1:/opt/web/.helmforge-release"] = string(markerJSON)

	engine := &Engine{Remote: runner}
	result := engine.CheckDrift(context.Background(), target, "new-sha")

	if result.InSync {
		t.Error("expected OutOfSync")
	}
}

func TestCheckDrift_NoMarker(t *testing.T) {
	runner := newMockRunner()
	target := model.Target{Host: "host1", User: "deploy", Port: 22, Path: "/opt/web"}

	engine := &Engine{Remote: runner}
	result := engine.CheckDrift(context.Background(), target, "abc123")

	if result.InSync {
		t.Error("expected not InSync when no marker")
	}
	if result.Error == "" {
		t.Error("expected error when no marker found")
	}
}
