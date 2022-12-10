package lock

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/7irelo/helmforge/internal/util/log"
)

// Lock is a simple file-based lock to prevent concurrent deploys.
type Lock struct {
	path string
}

// Acquire tries to take a lock for the given env/app combination.
// Returns an error if a lock is already held.
func Acquire(env, app string) (*Lock, error) {
	dir := lockDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create lock dir: %w", err)
	}
	name := fmt.Sprintf("%s_%s.lock", env, app)
	p := filepath.Join(dir, name)

	// Check for stale lock.
	if data, err := os.ReadFile(p); err == nil {
		parts := strings.SplitN(string(data), "\n", 2)
		if len(parts) == 2 {
			if pid, err := strconv.Atoi(strings.TrimSpace(parts[0])); err == nil {
				if !processExists(pid) {
					log.L().Warn().Int("stale_pid", pid).Msg("removing stale lock file")
					os.Remove(p)
				}
			}
		}
	}

	f, err := os.OpenFile(p, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		if os.IsExist(err) {
			content, _ := os.ReadFile(p)
			return nil, fmt.Errorf("deploy lock held for %s/%s (lockfile: %s)\n%s", env, app, p, string(content))
		}
		return nil, fmt.Errorf("acquire lock: %w", err)
	}
	defer f.Close()

	fmt.Fprintf(f, "%d\n%s\n", os.Getpid(), time.Now().UTC().Format(time.RFC3339))

	return &Lock{path: p}, nil
}

// Release removes the lock file.
func (l *Lock) Release() {
	if l == nil {
		return
	}
	if err := os.Remove(l.path); err != nil && !os.IsNotExist(err) {
		log.L().Warn().Err(err).Str("path", l.path).Msg("failed to release lock")
	}
}

func lockDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = os.TempDir()
	}
	return filepath.Join(home, ".helmforge", "locks")
}
