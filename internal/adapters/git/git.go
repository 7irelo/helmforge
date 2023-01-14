package git

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/7irelo/helmforge/internal/util/log"
)

// Client wraps Git operations. For v1 we shell out to the system git binary.
type Client interface {
	// CloneOrPull ensures the repo is available locally and up-to-date.
	CloneOrPull(ctx context.Context, repoURL string) (localPath string, err error)
	// Checkout checks out a specific ref (branch, tag, or SHA).
	Checkout(ctx context.Context, localPath, ref string) error
	// HeadSHA returns the current HEAD commit SHA.
	HeadSHA(ctx context.Context, localPath string) (string, error)
}

type shellClient struct{}

// NewClient returns a git client that shells out to the system git binary.
func NewClient() Client {
	return &shellClient{}
}

func (c *shellClient) CloneOrPull(ctx context.Context, repoURL string) (string, error) {
	base := reposDir()
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(repoURL)))
	localPath := filepath.Join(base, hash[:16])

	if err := os.MkdirAll(base, 0o700); err != nil {
		return "", fmt.Errorf("create repos dir: %w", err)
	}

	if _, err := os.Stat(filepath.Join(localPath, ".git")); err == nil {
		log.L().Debug().Str("repo", repoURL).Str("path", localPath).Msg("fetching existing repo")
		if err := runGit(ctx, localPath, "fetch", "--all", "--prune"); err != nil {
			return "", fmt.Errorf("git fetch: %w", err)
		}
	} else {
		log.L().Debug().Str("repo", repoURL).Str("path", localPath).Msg("cloning repo")
		if err := runGit(ctx, base, "clone", repoURL, localPath); err != nil {
			return "", fmt.Errorf("git clone: %w", err)
		}
	}

	return localPath, nil
}

func (c *shellClient) Checkout(ctx context.Context, localPath, ref string) error {
	// Try as remote branch first, fall back to direct ref.
	if err := runGit(ctx, localPath, "checkout", ref); err != nil {
		return fmt.Errorf("git checkout %s: %w", ref, err)
	}
	// If it's a branch, pull latest.
	_ = runGit(ctx, localPath, "pull", "--ff-only")
	return nil
}

func (c *shellClient) HeadSHA(ctx context.Context, localPath string) (string, error) {
	out, err := runGitOutput(ctx, localPath, "rev-parse", "HEAD")
	if err != nil {
		return "", fmt.Errorf("git rev-parse HEAD: %w", err)
	}
	return strings.TrimSpace(out), nil
}

func runGit(ctx context.Context, dir string, args ...string) error {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	log.L().Debug().Strs("args", args).Str("dir", dir).Msg("git")
	return cmd.Run()
}

func runGitOutput(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	return string(out), err
}

func reposDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = os.TempDir()
	}
	return filepath.Join(home, ".helmforge", "repos")
}
