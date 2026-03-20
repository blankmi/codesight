//go:build darwin

package lsp

import (
	"fmt"

	"golang.org/x/sys/unix"
)

func processStartID(pid int) (string, error) {
	if pid <= 0 {
		return "", fmt.Errorf("invalid pid: %d", pid)
	}

	kinfo, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil {
		return "", err
	}
	if int(kinfo.Proc.P_pid) != pid {
		return "", fmt.Errorf("unexpected pid from kern.proc.pid: got %d, want %d", kinfo.Proc.P_pid, pid)
	}

	start := kinfo.Proc.P_starttime
	return fmt.Sprintf("%d:%d", start.Sec, start.Usec), nil
}
