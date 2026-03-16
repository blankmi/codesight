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

func TestParseOllamaMaxInputCharsOverride(t *testing.T) {
	tests := []struct {
		name     string
		envValue string
		want     int
		wantErr  bool
	}{
		{
			name:     "unset",
			envValue: "",
			want:     0,
		},
		{
			name:     "valid",
			envValue: "4096",
			want:     4096,
		},
		{
			name:     "non numeric",
			envValue: "abc",
			wantErr:  true,
		},
		{
			name:     "zero",
			envValue: "0",
			wantErr:  true,
		},
		{
			name:     "negative",
			envValue: "-10",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(ollamaMaxInputCharsEnv, tt.envValue)

			got, err := parseOllamaMaxInputCharsOverride()
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if !strings.Contains(err.Error(), ollamaMaxInputCharsEnv) || !strings.Contains(err.Error(), "positive integer") {
					t.Fatalf("unexpected error message: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("got %d, want %d", got, tt.want)
			}
		})
	}
}

func TestCapMaxInputChars(t *testing.T) {
	tests := []struct {
		name string
		base int
		cap  int
		want int
	}{
		{
			name: "no override uses base",
			base: 8000,
			cap:  0,
			want: 8000,
		},
		{
			name: "lower cap wins",
			base: 8000,
			cap:  4000,
			want: 4000,
		},
		{
			name: "higher cap does not increase",
			base: 8000,
			cap:  12000,
			want: 8000,
		},
		{
			name: "base missing falls back to cap",
			base: 0,
			cap:  3000,
			want: 3000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := capMaxInputChars(tt.base, tt.cap); got != tt.want {
				t.Fatalf("capMaxInputChars(%d, %d) = %d, want %d", tt.base, tt.cap, got, tt.want)
			}
		})
	}
}
