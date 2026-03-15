package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestRunWithTimeoutWrapsTimeoutErrors(t *testing.T) {
	timeout := 20 * time.Millisecond
	start := time.Now()

	err := runWithTimeout(timeout, "running search", func(ctx context.Context) error {
		<-ctx.Done()
		return fmt.Errorf("embedding query: %w", ctx.Err())
	})
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	if !strings.Contains(err.Error(), "running search timed out after 20ms") {
		t.Fatalf("unexpected error message: %v", err)
	}
	if !strings.Contains(err.Error(), "network access may be blocked in this sandbox") {
		t.Fatalf("expected sandbox hint, got: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 250*time.Millisecond {
		t.Fatalf("timeout wrapper took too long: %s", elapsed)
	}
}

func TestRunWithTimeoutPassesThroughNonTimeoutErrors(t *testing.T) {
	want := errors.New("boom")

	err := runWithTimeout(time.Second, "running search", func(context.Context) error {
		return want
	})
	if !errors.Is(err, want) {
		t.Fatalf("expected wrapped error %v, got %v", want, err)
	}
	if strings.Contains(err.Error(), "network access may be blocked") {
		t.Fatalf("unexpected timeout hint in error: %v", err)
	}
}
