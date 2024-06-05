package gui

import (
	"fmt"
	"os"
	"syscall"
	"time"

	"github.com/mitchellh/go-ps"
)

func killProcess(pid int) error {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	if err = proc.Signal(syscall.SIGKILL); err != nil {
		return err
	}

	return nil
}

func waitForProcessesToExit(processes map[int]string) error {
	iterations := 0
	for ; len(processes) > 0 && iterations < 5; iterations++ {
		for pid := range processes {
			proc, err := ps.FindProcess(pid)
			if err != nil {
				return fmt.Errorf("failed to check if killed process is still running: %s", err)
			}

			// Remove process from map if it exited (was no longer found)
			if proc == nil {
				delete(processes, pid)
			}
		}
		time.Sleep(1 * time.Second)
	}

	// Return error if not all processes exited yet
	if len(processes) > 0 {
		return fmt.Errorf("timed out waiting for killed processes to exit")
	}

	return nil
}

func padRight(b []byte, c byte, l int) []byte {
	if len(b) >= l {
		return b
	}

	p := make([]byte, len(b), l)
	copy(p, b)
	for len(p) < l {
		p = append(p, c)
	}

	return p
}
