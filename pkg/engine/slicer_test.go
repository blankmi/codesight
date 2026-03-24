package engine

import (
	"strings"
	"testing"
)

func TestSliceDefinition(t *testing.T) {
	source := `package main

import "fmt"

func LongFunction() {
	fmt.Println("Start")
	if err := doSomething(); err != nil {
		fmt.Println("Error handled")
		return
	}
	// ... many lines ...
	// ... many lines ...
	// ... many lines ...
	// ... many lines ...
	// ... many lines ...
	// ... many lines ...
	// ... many lines ...
	// ... many lines ...
	// ... many lines ...
	// ... many lines ...
	data, err := os.ReadFile("config.json")
	if err != nil {
		return
	}
	fmt.Println(string(data))
	fmt.Println("End")
}
`
	// Budget is 10 lines, but LongFunction is ~25 lines.
	sig, slices, strategy := SliceDefinition([]byte(source), "go", 5, 27, 10)

	if strategy != "signature_plus_slices" {
		t.Errorf("strategy = %q, want signature_plus_slices", strategy)
	}

	if sig != "func LongFunction() {" {
		t.Errorf("sig = %q, want func LongFunction() {", sig)
	}

	if len(slices) == 0 {
		t.Fatal("expected slices, got none")
	}

	// Should have at least Header, Error path (doSomething), I/O site (ReadFile), and Return path.
	// Note: salientSliceMax is 3, plus Header and Return path might be more.
	// Wait, SliceDefinition appends Header, then salient tree slices, then Return path.
	// total = 1 (header) + 3 (max salient) + 1 (return) = 5 max.

	foundError := false
	foundIO := false
	for _, s := range slices {
		if strings.Contains(s.Label, "Error path") {
			foundError = true
		}
		if strings.Contains(s.Label, "I/O site") {
			foundIO = true
		}
	}

	if !foundError {
		t.Error("missing error path slice")
	}
	if !foundIO {
		t.Error("missing I/O site slice")
	}
}

func TestSliceDefinitionSmall(t *testing.T) {
	source := `func Short() {
	return
}`
	// Budget is 10 lines, Short is 3 lines.
	_, slices, strategy := SliceDefinition([]byte(source), "go", 1, 3, 10)

	if strategy != "full_body" {
		t.Errorf("strategy = %q, want full_body", strategy)
	}
	if slices != nil {
		t.Error("expected nil slices for full_body strategy")
	}
}
