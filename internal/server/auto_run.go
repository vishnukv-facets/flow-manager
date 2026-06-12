package server

import (
	"errors"
	"os"
	"syscall"
)

// processAliveProbe is a display-only liveness check for auto-run supervisor
// pids. Local copy of internal/app/auto.go — server must not import app.
func processAliveProbe(pid int) bool {
	if pid <= 0 {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = proc.Signal(syscall.Signal(0))
	if err == nil {
		return true
	}
	return errors.Is(err, syscall.EPERM)
}
