package store

import (
	"testing"
	"time"

	"github.com/7irelo/helmforge/internal/core/model"
)

func testStore(t *testing.T) ReleaseStore {
	t.Helper()
	dir := t.TempDir()
	db, err := openDB(dir + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	s := &sqliteStore{db: db}
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestSaveAndGetRelease(t *testing.T) {
	s := testStore(t)

	rel := &model.Release{
		ID:        "rel-1",
		Env:       "staging",
		App:       "web",
		Repo:      "https://example.com/repo.git",
		Ref:       "main",
		CommitSHA: "abc123",
		StartedAt: time.Now().UTC().Truncate(time.Second),
		Status:    model.ReleaseStatusRunning,
	}

	if err := s.SaveRelease(rel); err != nil {
		t.Fatalf("save: %v", err)
	}

	got, err := s.GetRelease("rel-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got == nil {
		t.Fatal("release not found")
	}
	if got.ID != "rel-1" {
		t.Errorf("id = %q, want %q", got.ID, "rel-1")
	}
	if got.Status != model.ReleaseStatusRunning {
		t.Errorf("status = %q, want %q", got.Status, model.ReleaseStatusRunning)
	}
}

func TestUpdateRelease(t *testing.T) {
	s := testStore(t)

	rel := &model.Release{
		ID:        "rel-2",
		Env:       "prod",
		App:       "api",
		Repo:      "https://example.com/repo.git",
		Ref:       "v1.0",
		CommitSHA: "def456",
		StartedAt: time.Now().UTC().Truncate(time.Second),
		Status:    model.ReleaseStatusRunning,
	}

	s.SaveRelease(rel)

	rel.Status = model.ReleaseStatusSuccess
	rel.FinishedAt = time.Now().UTC().Truncate(time.Second)
	rel.HostResults = []model.HostResult{
		{Host: "host1", Status: model.ReleaseStatusSuccess},
	}

	if err := s.UpdateRelease(rel); err != nil {
		t.Fatalf("update: %v", err)
	}

	got, _ := s.GetRelease("rel-2")
	if got.Status != model.ReleaseStatusSuccess {
		t.Errorf("status = %q, want %q", got.Status, model.ReleaseStatusSuccess)
	}
	if len(got.HostResults) != 1 {
		t.Errorf("host results len = %d, want 1", len(got.HostResults))
	}
}

func TestGetLatestRelease(t *testing.T) {
	s := testStore(t)

	s.SaveRelease(&model.Release{
		ID: "rel-old", Env: "staging", App: "web",
		Repo: "repo", Ref: "main", CommitSHA: "old",
		StartedAt: time.Now().UTC().Add(-1 * time.Hour),
		Status:    model.ReleaseStatusSuccess,
	})
	s.SaveRelease(&model.Release{
		ID: "rel-new", Env: "staging", App: "web",
		Repo: "repo", Ref: "main", CommitSHA: "new",
		StartedAt: time.Now().UTC(),
		Status:    model.ReleaseStatusSuccess,
	})

	got, err := s.GetLatestRelease("staging", "web")
	if err != nil {
		t.Fatalf("get latest: %v", err)
	}
	if got.ID != "rel-new" {
		t.Errorf("id = %q, want %q", got.ID, "rel-new")
	}
}

func TestListReleases(t *testing.T) {
	s := testStore(t)

	for i := 0; i < 5; i++ {
		s.SaveRelease(&model.Release{
			ID: "rel-" + string(rune('a'+i)), Env: "staging", App: "web",
			Repo: "repo", Ref: "main", CommitSHA: "sha",
			StartedAt: time.Now().UTC().Add(time.Duration(i) * time.Minute),
			Status:    model.ReleaseStatusSuccess,
		})
	}

	list, err := s.ListReleases("staging", "web", 3)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 3 {
		t.Errorf("list len = %d, want 3", len(list))
	}
}

func TestGetRelease_NotFound(t *testing.T) {
	s := testStore(t)

	got, err := s.GetRelease("nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Error("expected nil for nonexistent release")
	}
}
