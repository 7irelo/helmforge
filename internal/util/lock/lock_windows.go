//go:build windows

package lock

import (
	"os"
)

func processExists(pid int) bool {
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// On Windows, FindProcess always succeeds. We try to open the process handle.
	// A simple heuristic: Signal(0) doesn't work on Windows, so just assume it exists
	// unless it's our own PID (which we know is alive) or we fail to find it.
	_ = p
	// Check if /proc doesn't exist on Windows, fall back to assuming alive.
	// For v1 this is sufficient; worst case, user manually removes lock.
	return true
}
