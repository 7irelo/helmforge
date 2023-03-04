package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/7irelo/helmforge/internal/core/model"
	_ "modernc.org/sqlite"
)

// ReleaseStore persists release records.
type ReleaseStore interface {
	Init() error
	SaveRelease(r *model.Release) error
	UpdateRelease(r *model.Release) error
	GetRelease(id string) (*model.Release, error)
	GetLatestRelease(env, app string) (*model.Release, error)
	ListReleases(env, app string, limit int) ([]*model.Release, error)
}

type sqliteStore struct {
	db *sql.DB
}

// NewSQLiteStore creates a new SQLite-backed release store.
func NewSQLiteStore() (ReleaseStore, error) {
	dbPath := dbFilePath()
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o700); err != nil {
		return nil, fmt.Errorf("create store dir: %w", err)
	}
	return openAndInit(dbPath)
}

func openDB(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("set WAL mode: %w", err)
	}
	return db, nil
}

func openAndInit(path string) (ReleaseStore, error) {
	db, err := openDB(path)
	if err != nil {
		return nil, err
	}

	s := &sqliteStore{db: db}
	if err := s.Init(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *sqliteStore) Init() error {
	schema := `
	CREATE TABLE IF NOT EXISTS releases (
		id          TEXT PRIMARY KEY,
		env         TEXT NOT NULL,
		app         TEXT NOT NULL,
		repo        TEXT NOT NULL,
		ref         TEXT NOT NULL,
		commit_sha  TEXT NOT NULL,
		started_at  TEXT NOT NULL,
		finished_at TEXT,
		status      TEXT NOT NULL,
		host_results TEXT,
		created_at  TEXT NOT NULL DEFAULT (datetime('now'))
	);
	CREATE INDEX IF NOT EXISTS idx_releases_env_app ON releases(env, app);
	`
	_, err := s.db.Exec(schema)
	if err != nil {
		return fmt.Errorf("init schema: %w", err)
	}
	return nil
}

func (s *sqliteStore) SaveRelease(r *model.Release) error {
	hostResults, err := json.Marshal(r.HostResults)
	if err != nil {
		return fmt.Errorf("marshal host results: %w", err)
	}

	_, err = s.db.Exec(`
		INSERT INTO releases (id, env, app, repo, ref, commit_sha, started_at, finished_at, status, host_results)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.ID, r.Env, r.App, r.Repo, r.Ref, r.CommitSHA,
		r.StartedAt.Format(time.RFC3339),
		formatOptionalTime(r.FinishedAt),
		string(r.Status),
		string(hostResults),
	)
	if err != nil {
		return fmt.Errorf("save release: %w", err)
	}
	return nil
}

func (s *sqliteStore) UpdateRelease(r *model.Release) error {
	hostResults, err := json.Marshal(r.HostResults)
	if err != nil {
		return fmt.Errorf("marshal host results: %w", err)
	}

	_, err = s.db.Exec(`
		UPDATE releases SET finished_at=?, status=?, host_results=? WHERE id=?`,
		formatOptionalTime(r.FinishedAt),
		string(r.Status),
		string(hostResults),
		r.ID,
	)
	if err != nil {
		return fmt.Errorf("update release: %w", err)
	}
	return nil
}

func (s *sqliteStore) GetRelease(id string) (*model.Release, error) {
	row := s.db.QueryRow(`SELECT id, env, app, repo, ref, commit_sha, started_at, finished_at, status, host_results FROM releases WHERE id=?`, id)
	return scanRelease(row)
}

func (s *sqliteStore) GetLatestRelease(env, app string) (*model.Release, error) {
	row := s.db.QueryRow(`SELECT id, env, app, repo, ref, commit_sha, started_at, finished_at, status, host_results FROM releases WHERE env=? AND app=? ORDER BY started_at DESC LIMIT 1`, env, app)
	return scanRelease(row)
}

func (s *sqliteStore) ListReleases(env, app string, limit int) ([]*model.Release, error) {
	rows, err := s.db.Query(`SELECT id, env, app, repo, ref, commit_sha, started_at, finished_at, status, host_results FROM releases WHERE env=? AND app=? ORDER BY started_at DESC LIMIT ?`, env, app, limit)
	if err != nil {
		return nil, fmt.Errorf("list releases: %w", err)
	}
	defer rows.Close()

	var releases []*model.Release
	for rows.Next() {
		r, err := scanReleaseRow(rows)
		if err != nil {
			return nil, err
		}
		releases = append(releases, r)
	}
	return releases, rows.Err()
}

type scanner interface {
	Scan(dest ...any) error
}

func scanRelease(row *sql.Row) (*model.Release, error) {
	r := &model.Release{}
	var startedAt, finishedAt, status, hostResults string
	err := row.Scan(&r.ID, &r.Env, &r.App, &r.Repo, &r.Ref, &r.CommitSHA, &startedAt, &finishedAt, &status, &hostResults)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("scan release: %w", err)
	}
	return populateRelease(r, startedAt, finishedAt, status, hostResults)
}

func scanReleaseRow(rows *sql.Rows) (*model.Release, error) {
	r := &model.Release{}
	var startedAt, finishedAt, status, hostResults string
	err := rows.Scan(&r.ID, &r.Env, &r.App, &r.Repo, &r.Ref, &r.CommitSHA, &startedAt, &finishedAt, &status, &hostResults)
	if err != nil {
		return nil, fmt.Errorf("scan release row: %w", err)
	}
	return populateRelease(r, startedAt, finishedAt, status, hostResults)
}

func populateRelease(r *model.Release, startedAt, finishedAt, status, hostResults string) (*model.Release, error) {
	r.Status = model.ReleaseStatus(status)
	if t, err := time.Parse(time.RFC3339, startedAt); err == nil {
		r.StartedAt = t
	}
	if t, err := time.Parse(time.RFC3339, finishedAt); err == nil {
		r.FinishedAt = t
	}
	if hostResults != "" {
		if err := json.Unmarshal([]byte(hostResults), &r.HostResults); err != nil {
			return nil, fmt.Errorf("unmarshal host results: %w", err)
		}
	}
	return r, nil
}

func formatOptionalTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}

func dbFilePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = os.TempDir()
	}
	return filepath.Join(home, ".helmforge", "releases.db")
}
