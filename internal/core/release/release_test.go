package release

import (
	"strings"
	"testing"
	"time"

	"github.com/7irelo/helmforge/internal/core/model"
)

func TestFormatPlanText(t *testing.T) {
	plan := &model.DeployPlan{
		Env:       "staging",
		App:       "web",
		Repo:      "https://example.com/repo.git",
		Ref:       "main",
		CommitSHA: "abc123def456",
		Actions: []model.PlanAction{
			{Host: "deploy@host1:22", Step: "ensure_dir", Description: "Create /opt/web"},
			{Host: "deploy@host1:22", Step: "copy_files", Description: "Copy docker-compose.yaml"},
			{Host: "deploy@host1:22", Step: "docker_up", Description: "Start services", Command: "docker compose up -d"},
		},
	}

	text := FormatPlanText(plan)
	if !strings.Contains(text, "staging") {
		t.Error("plan text should contain env")
	}
	if !strings.Contains(text, "web") {
		t.Error("plan text should contain app")
	}
	if !strings.Contains(text, "abc123def456") {
		t.Error("plan text should contain commit SHA")
	}
	if !strings.Contains(text, "host1") {
		t.Error("plan text should contain host")
	}
}

func TestFormatPlanJSON(t *testing.T) {
	plan := &model.DeployPlan{
		Env:       "staging",
		App:       "web",
		CommitSHA: "abc123",
		Actions: []model.PlanAction{
			{Host: "host1", Step: "docker_up", Description: "Start"},
		},
	}

	jsonStr, err := FormatPlanJSON(plan)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(jsonStr, `"staging"`) {
		t.Error("JSON should contain env")
	}
	if !strings.Contains(jsonStr, `"docker_up"`) {
		t.Error("JSON should contain step")
	}
}

func TestFormatReleaseText(t *testing.T) {
	now := time.Now().UTC()
	rel := &model.Release{
		ID:        "rel-123",
		Env:       "staging",
		App:       "web",
		Status:    model.ReleaseStatusSuccess,
		CommitSHA: "abc123def456",
		StartedAt: now.Add(-5 * time.Second),
		FinishedAt: now,
		HostResults: []model.HostResult{
			{Host: "host1", Status: model.ReleaseStatusSuccess},
		},
	}

	text := FormatReleaseText(rel)
	if !strings.Contains(text, "rel-123") {
		t.Error("should contain release ID")
	}
	if !strings.Contains(text, "success") {
		t.Error("should contain status")
	}
	if !strings.Contains(text, "host1") {
		t.Error("should contain host")
	}
}

func TestFormatDriftText(t *testing.T) {
	results := []model.DriftResult{
		{Host: "host1", DesiredSHA: "abc123def456", ActualSHA: "abc123def456", InSync: true},
		{Host: "host2", DesiredSHA: "abc123def456", ActualSHA: "old123456789", InSync: false},
	}

	text := FormatDriftText(results)
	if !strings.Contains(text, "InSync") {
		t.Error("should contain InSync")
	}
	if !strings.Contains(text, "OutOfSync") {
		t.Error("should contain OutOfSync")
	}
}
