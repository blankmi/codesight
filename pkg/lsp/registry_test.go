package lsp

import (
	"errors"
	"strings"
	"testing"
)

func TestRegistryLookupKnownLanguage(t *testing.T) {
	registry := NewRegistry()

	spec, err := registry.Lookup("go")
	if err != nil {
		t.Fatalf("Lookup returned error: %v", err)
	}
	if spec.Language != "go" {
		t.Fatalf("Language = %q, want %q", spec.Language, "go")
	}
	if spec.Binary != "gopls" {
		t.Fatalf("Binary = %q, want %q", spec.Binary, "gopls")
	}
}

func TestRegistryLookupUnknownLanguage(t *testing.T) {
	registry := NewRegistry()

	_, err := registry.Lookup("elixir")
	if err == nil {
		t.Fatal("expected lookup error for unsupported language")
	}
	if !errors.Is(err, ErrUnsupportedLanguage) {
		t.Fatalf("error = %v, expected ErrUnsupportedLanguage", err)
	}
	if !strings.Contains(err.Error(), "supported:") {
		t.Fatalf("error %q does not include supported languages guidance", err.Error())
	}
}

func TestRegistryLookupReturnsIndependentArgsSlice(t *testing.T) {
	registry := NewRegistry()

	spec, err := registry.Lookup("typescript")
	if err != nil {
		t.Fatalf("Lookup returned error: %v", err)
	}
	spec.Args = append(spec.Args, "--mutated")

	specAgain, err := registry.Lookup("typescript")
	if err != nil {
		t.Fatalf("second Lookup returned error: %v", err)
	}
	for _, arg := range specAgain.Args {
		if arg == "--mutated" {
			t.Fatalf("registry args were mutated across lookups: %v", specAgain.Args)
		}
	}
}
