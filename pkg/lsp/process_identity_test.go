//go:build linux || darwin

package lsp

import (
	"os"
	"strings"
	"testing"
)

func TestProcessStartIDCurrentProcessStable(t *testing.T) {
	pid := os.Getpid()

	first, err := processStartID(pid)
	if err != nil {
		t.Fatalf("processStartID first read returned error: %v", err)
	}
	if strings.TrimSpace(first) == "" {
		t.Fatal("processStartID returned empty start ID for current process")
	}

	second, err := processStartID(pid)
	if err != nil {
		t.Fatalf("processStartID second read returned error: %v", err)
	}
	if second != first {
		t.Fatalf("processStartID changed between reads: first=%q second=%q", first, second)
	}
}
