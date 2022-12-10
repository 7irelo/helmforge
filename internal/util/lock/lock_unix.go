//go:build !windows

package lock

import (
	"os"
	"syscall"
)

func processExists(pid int) bool {
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = p.Signal(syscall.Signal(0))
	return err == nil
}
