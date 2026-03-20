//go:build linux

package lsp

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func processStartID(pid int) (string, error) {
	if pid <= 0 {
		return "", fmt.Errorf("invalid pid: %d", pid)
	}

	statPath := filepath.Join("/proc", strconv.Itoa(pid), "stat")
	data, err := os.ReadFile(statPath)
	if err != nil {
		return "", err
	}

	raw := strings.TrimSpace(string(data))
	closing := strings.LastIndex(raw, ")")
	if closing == -1 || closing+2 > len(raw) || raw[closing+1] != ' ' {
		return "", fmt.Errorf("parse %s: malformed stat payload", statPath)
	}

	fields := strings.Fields(raw[closing+2:])
	if len(fields) <= 19 {
		return "", fmt.Errorf("parse %s: expected at least 20 fields after comm, got %d", statPath, len(fields))
	}

	return fields[19], nil
}
